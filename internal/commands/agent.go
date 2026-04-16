// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
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

	// RunnerArgs are additional arguments to pass to the runner.
	RunnerArgs []string

	// Model selects the runner model (supported for claude-code, codex, and cursor runners).
	Model string

	// Effort selects the typed effort level (claude-code: --effort, codex: model_reasoning_effort).
	// Cursor runner does not support effort and expects thinking-capable model IDs via --model.
	Effort string

	// JSON outputs as JSON.
	JSON bool

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	NoIncludeUntracked bool

	IsInteractive func() bool
	TmuxAttachFn  func(sessionName string) error
}

// AgentStart starts a new agent invocation.
func AgentStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStartOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
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
	userCfg, _, err := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
	if err != nil {
		return fail(err)
	}

	repoRoot := ""
	if repoRef != "" {
		repo, err := ns.client.GetRepo(ctx, repoRef)
		if err != nil {
			return fail(err)
		}
		if repo.Data.PreferredRoot == "" || !repo.Data.PreferredRootAccessible {
			return fail(errors.NewWithDetails(
				errors.ERepoRootInaccessible,
				"repo preferred_root is not accessible",
				map[string]string{"repo": repoRef, "hint": "run `agency repo add /path/to/repo` from an accessible checkout"},
			))
		}
		if worktreeRef == "" {
			worktree, ok, err := findPresentWorktreeContainingCWD(ctx, ns.client, cwd)
			if err != nil {
				return fail(err)
			}
			if !ok {
				return fail(errors.New(errors.EUsage, "--worktree is required unless current directory is an integration worktree"))
			}
			if worktree.RepoID != repo.Data.RepoID {
				return fail(errors.NewWithDetails(
					errors.EUsage,
					"current integration worktree belongs to a different repo",
					map[string]string{"hint": "pass --worktree explicitly or run from the selected repo's worktree"},
				))
			}
			worktreeRef = worktree.WorktreeID
		}
		repoRoot = repo.Data.PreferredRoot
	} else {
		worktree, ok, err := findPresentWorktreeContainingCWD(ctx, ns.client, cwd)
		if err != nil {
			return fail(err)
		}
		if ok {
			if worktreeRef == "" {
				worktreeRef = worktree.WorktreeID
			}
			repo, err := ns.client.GetRepo(ctx, worktree.RepoID)
			if err != nil {
				return fail(err)
			}
			if repo.Data.PreferredRoot == "" || !repo.Data.PreferredRootAccessible {
				return fail(errors.NewWithDetails(
					errors.ERepoRootInaccessible,
					"repo preferred_root is not accessible",
					map[string]string{"repo": worktree.RepoID, "hint": "run `agency repo add /path/to/repo` from an accessible checkout"},
				))
			}
			repoRoot = repo.Data.PreferredRoot
		} else {
			if worktreeRef == "" {
				return fail(errors.New(errors.EUsage, "--worktree is required unless current directory is an integration worktree"))
			}
			if cwdInsideAgencyManagedTree(cwd, ns.dirs.DataDir) {
				return fail(errors.NewWithDetails(
					errors.EUnsafeRepoRoot,
					"current directory is inside an agency-managed tree but not a present integration worktree",
					map[string]string{"hint": "re-run from the original repo or pass --repo and --worktree explicitly"},
				))
			}
			currentRoot, err := git.GetRepoRoot(ctx, cr, cwd)
			if err != nil {
				return fail(errors.NewWithDetails(
					errors.ENoRepoContext,
					"cannot resolve agent start without a repo context",
					map[string]string{"hint": "run from a git checkout or pass --repo <repo_ref>"},
				))
			}
			reg, err := ns.client.RegisterRepo(ctx, currentRoot.Path)
			if err != nil {
				return fail(err)
			}
			if reg.Data.PreferredRoot == "" || !reg.Data.PreferredRootAccessible {
				return fail(errors.NewWithDetails(
					errors.ERepoRootInaccessible,
					"repo preferred_root is not accessible",
					map[string]string{"repo": reg.Data.RepoID, "hint": "run `agency repo add /path/to/repo` from an accessible checkout"},
				))
			}
			repoRoot = reg.Data.PreferredRoot
		}
	}
	opts.WorktreeRef = worktreeRef

	// Validate runner
	runner, err := resolveAgentRunner(opts.Runner, userCfg.Defaults.Runner)
	if err != nil {
		return fail(err)
	}
	effectiveRunnerArgs, err := resolveEffectiveRunnerArgs(runner, opts.RunnerArgs, opts.Model, opts.Effort, userCfg.Defaults)
	if err != nil {
		return fail(err)
	}
	opts.RunnerArgs = effectiveRunnerArgs

	if opts.Headless {
		return fail(agentStartHeadlessControlPlane(ctx, repoRoot, ns.client, opts, runner, stdout, stderr))
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

	if !resp.OK {
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		)
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = resp.InvocationID
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.IntegrationWorktreeName = resp.IntegrationWorktreeName
			envelope.SandboxPath = resp.SandboxPath
			envelope.TmuxSession = resp.TmuxSession
			envelope.DaemonInstanceID = resp.DaemonInstanceID
			envelope.AlreadyRunning = resp.AlreadyRunning
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
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
	_, _ = fmt.Fprintf(stdout, "  worktree:       %s\n", resp.IntegrationWorktreeID)
	_, _ = fmt.Fprintf(stdout, "  sandbox_path:   %s\n", resp.SandboxPath)
	_, _ = fmt.Fprintf(stdout, "  tmux_session:   %s\n", resp.TmuxSession)

	if resp.AlreadyRunning {
		_, _ = fmt.Fprintf(stdout, "\nNote: Invocation was already running (idempotent start).\n")
	}

	shortID := resp.InvocationID
	if len(shortID) > 8 {
		shortID = shortID[:8]
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
			_, _ = fmt.Fprintf(stderr, "Use 'agency agent enter %s' to attach later.\n", shortID)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "\nSession started in detached mode.\n")
		_, _ = fmt.Fprintf(stdout, "Use 'agency agent enter %s' to attach.\n", shortID)
	}

	return nil
}

// agentStartHeadlessControlPlane handles headless invocation start via daemon control plane.
func agentStartHeadlessControlPlane(ctx context.Context, repoRootPath string, client *daemonclient.Client, opts AgentStartOpts, runner string, stdout, stderr io.Writer) error {
	prompt, err := resolveBoundedPromptInput(
		opts.Prompt,
		opts.PromptFile,
		daemon.MaxPromptSize,
		"headless mode requires a prompt (use --prompt or --prompt-file)",
		"headless mode prompt cannot be empty",
	)
	if err != nil {
		return err
	}

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

	if !resp.OK {
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		)
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = resp.InvocationID
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.IntegrationWorktreeName = resp.IntegrationWorktreeName
			envelope.SandboxPath = resp.SandboxPath
			envelope.PID = resp.PID
			envelope.PGID = resp.PGID
			envelope.DaemonInstanceID = resp.DaemonInstanceID
			envelope.AlreadyRunning = resp.AlreadyRunning
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
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
	_, _ = fmt.Fprintf(stdout, "  worktree:       %s\n", resp.IntegrationWorktreeID)
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
	_, _ = fmt.Fprintf(stdout, "\nUse 'agency agent show %s' to view status.\n", shortID)
	_, _ = fmt.Fprintf(stdout, "Use 'agency agent stop %s' to stop gracefully.\n", shortID)

	return nil
}
