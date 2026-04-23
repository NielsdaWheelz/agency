// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// AgentStartOpts holds options for the agent start command.
type AgentStartOpts struct {
	// RepoRef is the repository reference (name, key, id, or prefix).
	RepoRef string

	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string

	// Runner is the canonical runner id (claude-code, codex, amp, opencode, cursor, droid).
	Runner string

	// Headless indicates whether to run in headless mode.
	Headless bool

	// InvocationName is an optional human-readable label.
	InvocationName string

	// Detached starts but does not attach (headed mode only).
	Detached bool

	// Prompt is the prompt string for headless mode (either Prompt or PromptFile).
	Prompt string

	// PromptFile is the path to a file containing the prompt for headless mode.
	PromptFile string

	// AgencyConfigPath, if set, is the exact agency config file to load.
	AgencyConfigPath string

	// RunnerArgs are additional arguments to pass to the runner.
	RunnerArgs []string

	// Model selects the runner model (supported for claude-code, codex, and cursor runners).
	Model string

	// Effort selects the typed effort level (claude-code: --effort, codex: model_reasoning_effort).
	// Cursor runner does not support effort and expects thinking-capable model IDs via --model.
	Effort string

	// PermissionMode selects the Claude permission mode.
	PermissionMode string

	// JSON outputs as JSON.
	JSON bool

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	NoIncludeUntracked bool

	IsInteractive func() bool
	TmuxAttachFn  func(sessionName string) error
}

func resolveAgentStartRepo(ctx context.Context, cr exec.CommandRunner, ns *daemonNavSetup, fsys fs.FS, cwd, repoRef string) (daemon.RepoDTO, cwdTargetSelection, bool, error) {
	cwdSelection, err := inspectCWDAmbientSelection(ctx, cr, ns, cwd)
	if err != nil {
		return daemon.RepoDTO{}, cwdTargetSelection{}, false, err
	}

	if strings.TrimSpace(repoRef) != "" {
		repo, err := resolveAccessibleRepo(ctx, ns.client, repoRef)
		return repo, cwdSelection, false, err
	}

	if cwdSelection.HasRepo {
		return cwdSelection.Repo, cwdSelection, false, nil
	}

	if cwdSelection.InsideAgencyManagedTree {
		return daemon.RepoDTO{}, cwdTargetSelection{}, false, errors.NewWithDetails(
			errors.EUnsafeRepoRoot,
			"current directory is inside an agency-managed tree but not a present integration worktree",
			map[string]string{
				"hint": "re-run from the original repo checkout, or pass --repo and --worktree explicitly",
			},
		)
	}

	currentCtx, hasCurrentCtx, err := loadActiveContextFallback(ctx, ns.client, fsys, ns.dirs.ConfigDir, true)
	if err != nil {
		return daemon.RepoDTO{}, cwdTargetSelection{}, false, err
	}
	if hasCurrentCtx {
		repo, err := resolveAccessibleRepo(ctx, ns.client, currentCtx.RepoID)
		return repo, cwdSelection, true, err
	}

	return daemon.RepoDTO{}, cwdTargetSelection{}, false, errors.NewWithDetails(
		errors.ENoRepoContext,
		"cannot resolve agent start without a repo context",
		map[string]string{
			"hint": "run from a git checkout, pass --repo, or set an active context with `agency context use <worktree-ref>`",
		},
	)
}

func resolveAgentStartWorktree(ctx context.Context, ns *daemonNavSetup, fsys fs.FS, repoID, worktreeRef string, cwdSelection cwdTargetSelection, repoFromCurrentContext bool) (string, error) {
	switch {
	case strings.TrimSpace(worktreeRef) != "":
		result, err := ns.client.GetWorktree(ctx, worktreeRef, repoID)
		if err != nil {
			return "", err
		}
		worktree, err := requirePresentWorktree(result.Data, "agent start requires a present integration worktree")
		if err != nil {
			return "", err
		}
		return worktree.WorktreeID, nil
	case cwdSelection.HasWorktree && cwdSelection.Worktree.RepoID == repoID:
		return cwdSelection.Worktree.WorktreeID, nil
	default:
		currentCtx, hasCurrentCtx, err := loadActiveContextFallback(ctx, ns.client, fsys, ns.dirs.ConfigDir, repoFromCurrentContext)
		if err != nil {
			return "", err
		}
		if hasCurrentCtx && currentCtx.RepoID == repoID {
			return currentCtx.WorktreeID, nil
		}
		return "", errors.NewWithDetails(
			errors.EUsage,
			"cannot resolve agent start without a worktree",
			map[string]string{
				"hint": "pass --worktree <worktree-ref>, set an active context with `agency context use <worktree-ref>`, or run this command from the integration worktree you want to use",
			},
		)
	}
}

// AgentStart starts a new agent invocation.
func AgentStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStartOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}
	worktreeRef := strings.TrimSpace(opts.WorktreeRef)
	repoRef := strings.TrimSpace(opts.RepoRef)
	if cr == nil {
		cr = exec.NewRealRunner()
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}
	if !opts.Headless && !opts.Detached {
		isInteractive := opts.IsInteractive
		if isInteractive == nil {
			isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) }
		}
		if !isInteractive() {
			return fail(errors.NewWithDetails(
				errors.ENotInteractive,
				"headed start requires an interactive terminal",
				map[string]string{
					"hint": "re-run in an interactive terminal or pass --detached",
				},
			))
		}
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	headlessPrompt := ""
	if opts.Headless {
		headlessPrompt, err = resolveBoundedPromptInput(
			opts.Prompt,
			opts.PromptFile,
			daemon.MaxPromptSize,
			"headless mode requires a prompt (use --prompt or --prompt-file)",
			"headless mode prompt cannot be empty",
		)
		if err != nil {
			return fail(err)
		}
	}

	repo, cwdSelection, repoFromCurrentContext, err := resolveAgentStartRepo(ctx, cr, ns, fsys, cwd, repoRef)
	if err != nil {
		return fail(err)
	}
	repoRoot := repo.PreferredRoot
	repoID := repo.RepoID
	worktreeRef, err = resolveAgentStartWorktree(ctx, ns, fsys, repoID, worktreeRef, cwdSelection, repoFromCurrentContext)
	if err != nil {
		return fail(err)
	}
	opts.WorktreeRef = worktreeRef

	userCfg := config.UserConfig{}
	userCfgLoaded := false
	loadUserCfg := func(required bool) error {
		if userCfgLoaded {
			return nil
		}

		cfg, loadErr := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
		if loadErr != nil {
			if !required && errors.GetCode(loadErr) == errors.ENoUserConfig {
				return nil
			}
			return loadErr
		}

		userCfg = cfg
		userCfgLoaded = true
		return nil
	}

	if strings.TrimSpace(opts.Runner) == "" {
		if err := loadUserCfg(true); err != nil {
			return fail(err)
		}
	}

	runner, err := resolveAgentRunner(opts.Runner, userCfg.Defaults.Runner)
	if err != nil {
		return fail(err)
	}

	agencyConfigPath := strings.TrimSpace(opts.AgencyConfigPath)
	if agencyConfigPath != "" && !filepath.IsAbs(agencyConfigPath) {
		agencyConfigPath = filepath.Join(cwd, agencyConfigPath)
	}
	shouldResolveAgencyConfig := agencyConfigPath != ""
	if !shouldResolveAgencyConfig {
		repoAgencyConfigPath := filepath.Join(repoRoot, "agency.json")
		if _, err := fsys.Stat(repoAgencyConfigPath); err == nil {
			shouldResolveAgencyConfig = true
		} else if !os.IsNotExist(err) {
			shouldResolveAgencyConfig = true
		} else {
			localAgencyConfigPath := config.LocalAgencyConfigPath(ns.dirs.ConfigDir, repoID)
			if _, err := fsys.Stat(localAgencyConfigPath); err == nil || !os.IsNotExist(err) {
				shouldResolveAgencyConfig = true
			}
		}
	}

	model := strings.TrimSpace(opts.Model)
	effort := strings.TrimSpace(opts.Effort)
	permissionMode := strings.TrimSpace(opts.PermissionMode)
	if model == "" || effort == "" || permissionMode == "" {
		if err := loadUserCfg(false); err != nil {
			return fail(err)
		}
	}
	if shouldResolveAgencyConfig {
		resolvedAgencyConfig, err := config.ResolveAgencyConfig(fsys, repoRoot, ns.dirs.ConfigDir, repoID, agencyConfigPath)
		if err != nil {
			return fail(err)
		}
		runnerDefaults, ok := resolvedAgencyConfig.Config.RunnerDefaults[runner]
		if ok {
			if model == "" {
				model = runnerDefaults.Model
			}
			if effort == "" {
				effort = runnerDefaults.Effort
			}
		}
	}
	if model == "" {
		if runnerDefaults, ok := userCfg.RunnerDefaults[runner]; ok {
			model = runnerDefaults.Model
		}
	}
	if effort == "" {
		if runnerDefaults, ok := userCfg.RunnerDefaults[runner]; ok {
			effort = runnerDefaults.Effort
		}
	}
	if permissionMode == "" {
		if runnerDefaults, ok := userCfg.RunnerDefaults[runner]; ok {
			permissionMode = runnerDefaults.PermissionMode
		}
	}

	effectiveRunnerArgs, err := resolveEffectiveRunnerArgs(runner, opts.RunnerArgs, model, effort, permissionMode, opts.Headless)
	if err != nil {
		return fail(err)
	}
	opts.RunnerArgs = effectiveRunnerArgs

	if opts.Headless {
		return fail(agentStartHeadlessControlPlane(ctx, repoRoot, ns.client, opts, runner, headlessPrompt, stdout, stderr))
	}

	return fail(agentStartHeadedControlPlane(ctx, repoRoot, ns.client, opts, runner, stdout, stderr))
}

// agentStartHeadedControlPlane handles headed invocation start via daemon control plane.
func agentStartHeadedControlPlane(ctx context.Context, repoRootPath string, client *daemonclient.Client, opts AgentStartOpts, runner string, stdout, stderr io.Writer) error {
	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	resp, err := client.ControlPlaneStartHeaded(ctx, daemonclient.ControlPlaneStartHeadedOpts{
		RepoRoot:           repoRootPath,
		WorktreeRef:        opts.WorktreeRef,
		Runner:             runner,
		InvocationName:     opts.InvocationName,
		RunnerArgs:         opts.RunnerArgs,
		NoIncludeUntracked: opts.NoIncludeUntracked,
	})
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID     string           `json:"invocation_id,omitempty"`
			RepoID           string           `json:"repo_id,omitempty"`
			RepoName         string           `json:"repo_name,omitempty"`
			WorktreeID       string           `json:"worktree_id,omitempty"`
			WorktreeName     string           `json:"worktree_name,omitempty"`
			SandboxPath      string           `json:"sandbox_path,omitempty"`
			TmuxSession      string           `json:"tmux_session,omitempty"`
			DaemonInstanceID string           `json:"daemon_instance_id,omitempty"`
			AlreadyRunning   bool             `json:"already_running,omitempty"`
			LogPaths         *daemon.LogPaths `json:"log_paths,omitempty"`
		}{
			commandJSONBase:  newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, resp.ClientRequestID, resp.RequestID),
			InvocationID:     resp.InvocationID,
			RepoID:           resp.RepoID,
			RepoName:         resp.RepoName,
			WorktreeID:       resp.WorktreeID,
			WorktreeName:     resp.WorktreeName,
			SandboxPath:      resp.SandboxPath,
			TmuxSession:      resp.TmuxSession,
			DaemonInstanceID: resp.DaemonInstanceID,
			AlreadyRunning:   resp.AlreadyRunning,
			LogPaths:         resp.LogPaths,
		})
	}

	// Output result
	_, _ = fmt.Fprintf(stdout, "Started headed agent invocation\n")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:  %s\n", resp.InvocationID)
	if opts.InvocationName != "" {
		_, _ = fmt.Fprintf(stdout, "  name:           %s\n", opts.InvocationName)
	}
	_, _ = fmt.Fprintf(stdout, "  runner:         %s\n", runner)
	_, _ = fmt.Fprintf(stdout, "  mode:           headed\n")
	worktree := resp.WorktreeID
	if strings.TrimSpace(resp.WorktreeName) != "" {
		worktree = resp.WorktreeName + " (" + resp.WorktreeID + ")"
	}
	_, _ = fmt.Fprintf(stdout, "  worktree:       %s\n", worktree)
	_, _ = fmt.Fprintf(stdout, "  sandbox_path:   %s\n", resp.SandboxPath)
	_, _ = fmt.Fprintf(stdout, "  tmux_session:   %s\n", resp.TmuxSession)

	if resp.AlreadyRunning {
		_, _ = fmt.Fprintf(stdout, "\nNote: Invocation was already running (idempotent start).\n")
	}

	// If not detached, attach to the tmux session
	if !opts.Detached {
		_, _ = fmt.Fprintf(stdout, "\nAttaching to tmux session... (detach with Ctrl+b, d)\n")

		attachFn := opts.TmuxAttachFn
		if attachFn == nil {
			attachFn = realTmuxAttach
		}
		if err := attachFn(resp.TmuxSession); err != nil {
			// Attach failed but session exists - not a fatal error
			_, _ = fmt.Fprintf(stderr, "warning: could not attach to tmux session: %v\n", err)
			_, _ = fmt.Fprintf(stderr, "Use 'agency agent %s attach --repo %s' to attach later.\n", resp.InvocationID, resp.RepoID)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "\nSession started in detached mode.\n")
		_, _ = fmt.Fprintf(stdout, "Use 'agency agent %s attach --repo %s' to attach.\n", resp.InvocationID, resp.RepoID)
	}

	return nil
}

// agentStartHeadlessControlPlane handles headless invocation start via daemon control plane.
func agentStartHeadlessControlPlane(ctx context.Context, repoRootPath string, client *daemonclient.Client, opts AgentStartOpts, runner string, prompt string, stdout, stderr io.Writer) error {
	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	resp, err := client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:           repoRootPath,
		WorktreeRef:        opts.WorktreeRef,
		Runner:             runner,
		Prompt:             prompt,
		InvocationName:     opts.InvocationName,
		RunnerArgs:         opts.RunnerArgs,
		NoIncludeUntracked: opts.NoIncludeUntracked,
	})
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID     string           `json:"invocation_id,omitempty"`
			RepoID           string           `json:"repo_id,omitempty"`
			RepoName         string           `json:"repo_name,omitempty"`
			WorktreeID       string           `json:"worktree_id,omitempty"`
			WorktreeName     string           `json:"worktree_name,omitempty"`
			SandboxPath      string           `json:"sandbox_path,omitempty"`
			PID              int              `json:"pid,omitempty"`
			PGID             int              `json:"pgid,omitempty"`
			DaemonInstanceID string           `json:"daemon_instance_id,omitempty"`
			AlreadyRunning   bool             `json:"already_running,omitempty"`
			LogPaths         *daemon.LogPaths `json:"log_paths,omitempty"`
		}{
			commandJSONBase:  newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, resp.ClientRequestID, resp.RequestID),
			InvocationID:     resp.InvocationID,
			RepoID:           resp.RepoID,
			RepoName:         resp.RepoName,
			WorktreeID:       resp.WorktreeID,
			WorktreeName:     resp.WorktreeName,
			SandboxPath:      resp.SandboxPath,
			PID:              resp.PID,
			PGID:             resp.PGID,
			DaemonInstanceID: resp.DaemonInstanceID,
			AlreadyRunning:   resp.AlreadyRunning,
			LogPaths:         resp.LogPaths,
		})
	}

	// Output result
	_, _ = fmt.Fprintf(stdout, "Started headless agent invocation\n")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:  %s\n", resp.InvocationID)
	if opts.InvocationName != "" {
		_, _ = fmt.Fprintf(stdout, "  name:           %s\n", opts.InvocationName)
	}
	_, _ = fmt.Fprintf(stdout, "  runner:         %s\n", runner)
	_, _ = fmt.Fprintf(stdout, "  mode:           headless\n")
	worktree := resp.WorktreeID
	if strings.TrimSpace(resp.WorktreeName) != "" {
		worktree = resp.WorktreeName + " (" + resp.WorktreeID + ")"
	}
	_, _ = fmt.Fprintf(stdout, "  worktree:       %s\n", worktree)
	_, _ = fmt.Fprintf(stdout, "  sandbox_path:   %s\n", resp.SandboxPath)
	_, _ = fmt.Fprintf(stdout, "  pid:            %d\n", resp.PID)

	if resp.LogPaths != nil {
		_, _ = fmt.Fprintf(stdout, "  logs:\n")
		_, _ = fmt.Fprintf(stdout, "    raw:    %s\n", resp.LogPaths.Raw)
		_, _ = fmt.Fprintf(stdout, "    stderr: %s\n", resp.LogPaths.Stderr)
		_, _ = fmt.Fprintf(stdout, "    stream: %s\n", resp.LogPaths.Stream)
	}

	if resp.AlreadyRunning {
		_, _ = fmt.Fprintf(stdout, "\nNote: Invocation was already running (idempotent start).\n")
	}

	shortID := resp.InvocationID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	_, _ = fmt.Fprintf(stdout, "\nUse 'agency agent %s' to view status.\n", shortID)
	_, _ = fmt.Fprintf(stdout, "Use 'agency agent %s stop' to stop gracefully.\n", shortID)

	return nil
}
