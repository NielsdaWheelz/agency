// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// AgentStartOpts holds options for the agent start command.
type AgentStartOpts struct {
	// RepoRef is the repository reference (name, key, id, or prefix).
	RepoRef string

	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string

	// Runner is the canonical runner id (claude-code, codex, amp, opencode, cursor, droid).
	Runner string

	// Mode is the execution mode (headed or headless).
	Mode string

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

	// ExecutionProfile overrides repo/default profile selection.
	ExecutionProfile string

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

func resolveAgentStartRepo(ctx context.Context, cr exec.CommandRunner, ns *daemonNavSetup, cwd, repoRef string) (daemon.RepoDTO, cwdTargetSelection, error) {
	cwdSelection, err := inspectCWDAmbientSelection(ctx, cr, ns, cwd)
	if err != nil {
		return daemon.RepoDTO{}, cwdTargetSelection{}, err
	}

	if strings.TrimSpace(repoRef) != "" {
		repo, err := resolveAccessibleRepo(ctx, ns.client, repoRef)
		return repo, cwdSelection, err
	}

	if cwdSelection.HasRepo {
		return cwdSelection.Repo, cwdSelection, nil
	}

	if cwdSelection.InsideAgencyManagedTree {
		return daemon.RepoDTO{}, cwdTargetSelection{}, errors.NewWithDetails(
			errors.EUnsafeRepoRoot,
			"current directory is inside an agency-managed tree but not a present integration worktree",
			map[string]string{
				"hint": "re-run from the original repo checkout, or pass --repo and --worktree explicitly",
			},
		)
	}

	return daemon.RepoDTO{}, cwdTargetSelection{}, errors.NewWithDetails(
		errors.ENoRepoContext,
		"cannot resolve agent start without a repo context",
		map[string]string{
			"hint": "run from a git checkout or pass --repo",
		},
	)
}

func resolveAgentStartWorktree(ctx context.Context, ns *daemonNavSetup, repoID, worktreeRef string, cwdSelection cwdTargetSelection) (string, error) {
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
		return "", errors.NewWithDetails(
			errors.EUsage,
			"cannot resolve agent start without a worktree",
			map[string]string{
				"hint": "pass --worktree <worktree-ref> or run this command from the integration worktree you want to use",
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

	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = string(store.RunnerModeHeaded)
	}
	headless := mode == string(store.RunnerModeHeadless)
	switch mode {
	case string(store.RunnerModeHeadless):
		if opts.Detached {
			return fail(errors.NewWithDetails(errors.EUsage, "--detached is only valid with --mode headed", map[string]string{"hint": "omit --detached or pass --mode headed"}))
		}
	case string(store.RunnerModeHeaded):
		if strings.TrimSpace(opts.Prompt) != "" || strings.TrimSpace(opts.PromptFile) != "" {
			return fail(errors.NewWithDetails(errors.EUsage, "headed agent start does not accept a prompt", map[string]string{"hint": "omit --prompt/--prompt-file or use --mode headless"}))
		}
		if !opts.Detached {
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
	default:
		return fail(errors.New(errors.EInvalidArgument, "mode must be headless or headed"))
	}

	headlessPrompt := ""
	if headless {
		var err error
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

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repo, cwdSelection, err := resolveAgentStartRepo(ctx, cr, ns, cwd, repoRef)
	if err != nil {
		return fail(err)
	}
	repoRoot := repo.PreferredRoot
	repoID := repo.RepoID
	worktreeRef, err = resolveAgentStartWorktree(ctx, ns, repoID, worktreeRef, cwdSelection)
	if err != nil {
		return fail(err)
	}
	opts.WorktreeRef = worktreeRef
	if opts.AgencyConfigPath != "" && !filepath.IsAbs(opts.AgencyConfigPath) {
		opts.AgencyConfigPath = filepath.Join(cwd, opts.AgencyConfigPath)
	}
	opts.ExecutionProfile = strings.TrimSpace(opts.ExecutionProfile)

	runner, effectiveRunnerArgs, err := resolveStartRunnerAndArgs(ctx, fsys, cwd, ns, repoRoot, repoID, startRunnerConfigOpts{
		Runner:           opts.Runner,
		RunnerArgs:       opts.RunnerArgs,
		Model:            opts.Model,
		Effort:           opts.Effort,
		PermissionMode:   opts.PermissionMode,
		AgencyConfigPath: opts.AgencyConfigPath,
		Headless:         headless,
	})
	if err != nil {
		return fail(err)
	}
	opts.RunnerArgs = effectiveRunnerArgs

	if headless {
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
		ExecutionProfile:   opts.ExecutionProfile,
		AgencyConfigPath:   opts.AgencyConfigPath,
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
			ExecutionProfile string           `json:"execution_profile,omitempty"`
			CheckoutRoot     string           `json:"checkout_root,omitempty"`
			CustomEnvKeys    []string         `json:"custom_env_keys,omitempty"`
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
			ExecutionProfile: resp.ExecutionProfile,
			CheckoutRoot:     resp.CheckoutRoot,
			CustomEnvKeys:    append([]string(nil), resp.CustomEnvKeys...),
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
	_, _ = fmt.Fprintf(stdout, "  profile:        %s\n", resp.ExecutionProfile)
	_, _ = fmt.Fprintf(stdout, "  checkout_root:  %s\n", resp.CheckoutRoot)
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
		ExecutionProfile:   opts.ExecutionProfile,
		AgencyConfigPath:   opts.AgencyConfigPath,
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
			ExecutionProfile string           `json:"execution_profile,omitempty"`
			CheckoutRoot     string           `json:"checkout_root,omitempty"`
			CustomEnvKeys    []string         `json:"custom_env_keys,omitempty"`
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
			ExecutionProfile: resp.ExecutionProfile,
			CheckoutRoot:     resp.CheckoutRoot,
			CustomEnvKeys:    append([]string(nil), resp.CustomEnvKeys...),
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
	_, _ = fmt.Fprintf(stdout, "  profile:        %s\n", resp.ExecutionProfile)
	_, _ = fmt.Fprintf(stdout, "  checkout_root:  %s\n", resp.CheckoutRoot)
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
