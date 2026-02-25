// Package commands implements agency CLI commands.
// This file implements agent commands (Slice 8 PR-02/03/04).
package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"time"

	"golang.org/x/term"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// AgentStartOpts holds options for the agent start command.
type AgentStartOpts struct {
	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string

	// Runner is the runner type (claude, codex).
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

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots (PR-08).
	NoIncludeUntracked bool

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStart starts a new agent invocation.
// PR-10: Both headed and headless modes now delegate to daemon control plane.
// CLI never creates invocations, sandboxes, or tmux sessions directly.
func AgentStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStartOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}

	// Validate runner
	runner := opts.Runner
	if runner == "" {
		runner = "claude"
	}
	if runner != "claude" && runner != "codex" {
		return errors.NewWithDetails(
			errors.EUsage,
			"invalid runner: "+runner,
			map[string]string{
				"runner": runner,
				"valid":  "claude, codex",
			},
		)
	}

	// For headless mode (PR-05): delegate everything to daemon control plane
	if opts.Headless {
		return agentStartHeadlessControlPlane(ctx, cr, fsys, repoRoot.Path, dirs, opts, runner, stdout, stderr)
	}

	// PR-10: For headed mode: delegate to daemon control plane
	// CLI never creates invocations, sandboxes, or tmux sessions directly
	return agentStartHeadedControlPlane(ctx, cr, fsys, repoRoot.Path, dirs, opts, runner, stdout, stderr)
}

// agentStartHeadedControlPlane handles headed invocation start via daemon control plane (PR-10).
// CLI does NOT create invocation, sandbox, or tmux session - daemon does everything.
func agentStartHeadedControlPlane(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, repoRootPath string, dirs paths.Dirs, opts AgentStartOpts, runner string, stdout, stderr io.Writer) error {
	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	// Check API version compatibility
	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// Send control plane start request to daemon (PR-10)
	// Daemon creates: invocation ID, sandbox, invocation meta, tmux session
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
			map[string]string{"hint": resp.Hint},
		)
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
	// PR-10: CLI only calls tmux attach - never creates sessions
	if !opts.Detached {
		_, _ = fmt.Fprintf(stdout, "\nAttaching to tmux session... (detach with Ctrl+b, d)\n")

		// Get tmux client - use provided or create new
		tmuxClient := opts.TmuxClient
		if tmuxClient == nil {
			tmuxClient = tmux.NewExecClient(cr)
		}

		if err := tmuxClient.Attach(ctx, resp.TmuxSession); err != nil {
			// Attach failed but session exists - not a fatal error
			_, _ = fmt.Fprintf(stderr, "warning: could not attach to tmux session: %v\n", err)
			_, _ = fmt.Fprintf(stderr, "Use 'agency agent attach %s' to attach later.\n", shortID)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "\nSession started in detached mode.\n")
		_, _ = fmt.Fprintf(stdout, "Use 'agency agent attach %s' to attach.\n", shortID)
	}

	return nil
}

// agentStartHeadlessControlPlane handles headless invocation start via daemon control plane (PR-05).
// CLI does NOT create invocation or sandbox - daemon does everything.
func agentStartHeadlessControlPlane(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, repoRootPath string, dirs paths.Dirs, opts AgentStartOpts, runner string, stdout, stderr io.Writer) error {
	// Resolve prompt
	prompt := opts.Prompt
	if prompt == "" && opts.PromptFile != "" {
		data, err := os.ReadFile(opts.PromptFile)
		if err != nil {
			return errors.WrapWithDetails(
				errors.EPromptRequired,
				"failed to read prompt file",
				err,
				map[string]string{"path": opts.PromptFile},
			)
		}
		prompt = string(data)
	}

	if prompt == "" {
		return errors.New(errors.EPromptRequired, "headless mode requires a prompt (use --prompt or --prompt-file)")
	}

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	// Check API version compatibility (PR-05)
	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// Send control plane start request to daemon (PR-05)
	// Daemon creates: invocation ID, sandbox, invocation meta, and starts runner
	resp, err := client.ControlPlaneStartHeadless(ctx, daemonclient.ControlPlaneStartOpts{
		RepoRoot:           repoRootPath,
		WorktreeRef:        opts.WorktreeRef,
		Runner:             runner,
		Prompt:             prompt,
		InvocationName:     opts.InvocationName,
		RunnerArgs:         opts.RunnerArgs,
		NoIncludeUntracked: opts.NoIncludeUntracked, // PR-08
	})
	if err != nil {
		return err
	}

	if !resp.OK {
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{"hint": resp.Hint},
		)
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

// AgentLSOpts holds options for the agent ls command.
type AgentLSOpts struct {
	// WorktreeRef filters by integration worktree (optional).
	WorktreeRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// AllRepos lists across all repos (PR-A).
	AllRepos bool

	// All includes finished (landed/discarded) invocations.
	All bool

	// JSON outputs as JSON.
	JSON bool

	// Watch mode (PR-B): re-render on interval with ANSI clear-screen.
	Watch    bool
	Interval time.Duration // default 500ms, min 250ms, max 5s

	// SleepFn overrides time.Sleep for testing. If nil, uses time.Sleep.
	SleepFn func(time.Duration)

	// MaxIterations limits watch iterations for testing. 0 = unlimited.
	MaxIterations int
}

// AgentLS lists agent invocations.
// PR-12: Routes through daemon read API - CLI never reads store directly.
// PR-A: Supports --repo / --all-repos for CWD-less operation.
func AgentLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLSOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// PR-A: Resolve repo context via daemon
	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllRepos:      opts.AllRepos,
		AllowAllRepos: true,
		CmdName:       "agent ls",
	})
	if err != nil {
		return err
	}

	state := "active"
	if opts.All {
		state = "all"
	}

	var repoID string
	if !repoCtx.AllRepos {
		repoID = repoCtx.RepoID
	}

	// Non-watch mode
	if !opts.Watch {
		result, fetchErr := client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{
			RepoID:      repoID,
			WorktreeRef: opts.WorktreeRef,
			State:       state,
		})
		if fetchErr != nil {
			return fetchErr
		}
		if opts.JSON {
			return writeAgentLSJSONFromDTO(stdout, result.Invocations)
		}
		return writeAgentLSHumanFromDTO(stdout, result.Invocations)
	}

	// Watch mode (PR-B)
	fetchAndRender := func(w io.Writer) error {
		result, fetchErr := client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{
			RepoID:      repoID,
			WorktreeRef: opts.WorktreeRef,
			State:       state,
		})
		if fetchErr != nil {
			return fetchErr
		}
		return writeAgentLSHumanFromDTO(w, result.Invocations)
	}
	return watchLoop(ctx, stdout, stderr, opts.Interval, opts.SleepFn, opts.MaxIterations, fetchAndRender)
}

// writeAgentLSJSONFromDTO outputs invocation list as JSON from daemon DTOs.
// PR-12: CLI renders daemon-provided data - no local derivation.
func writeAgentLSJSONFromDTO(w io.Writer, invocations []daemon.InvocationDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(invocations)
}

// writeAgentLSHumanFromDTO outputs invocation list in human-readable format from daemon DTOs.
// PR-12: CLI renders daemon-provided display_status and attention_flags.
func writeAgentLSHumanFromDTO(w io.Writer, invocations []daemon.InvocationDTO) error {
	if len(invocations) == 0 {
		_, _ = fmt.Fprintln(w, "No agent invocations found.")
		return nil
	}

	for _, inv := range invocations {
		name := ""
		if inv.InvocationName != "" {
			name = " (" + inv.InvocationName + ")"
		}

		// Use daemon-derived display_status
		displayStatus := inv.DisplayStatus
		if displayStatus == "" {
			displayStatus = inv.Status // fallback to raw status
		}

		// Show attention flags if any
		attentionStr := ""
		if len(inv.AttentionFlags) > 0 {
			for _, flag := range inv.AttentionFlags {
				attentionStr += " [" + flag + "]"
			}
		}

		_, _ = fmt.Fprintf(w, "%s  %s  %s  %s%s%s\n",
			inv.InvocationID,
			inv.Runner,
			inv.Mode,
			displayStatus,
			name,
			attentionStr,
		)
	}

	return nil
}

// AgentShowOpts holds options for the agent show command.
type AgentShowOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// JSON outputs as JSON.
	JSON bool
}

// AgentShow shows details of an agent invocation.
// PR-12: Routes through daemon read API - CLI never reads store directly.
// PR-A: Supports --repo for CWD-less operation.
func AgentShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShowOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent show",
	})
	if err != nil {
		return err
	}

	result, err := client.GetInvocationRich(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	// Output
	if opts.JSON {
		return writeAgentShowJSONFromDTO(stdout, &result.Invocation)
	}

	return writeAgentShowHumanFromDTO(stdout, &result.Invocation)
}

// writeAgentShowJSONFromDTO outputs invocation details as JSON from daemon DTO.
// PR-12: CLI renders daemon-provided data - no local derivation.
func writeAgentShowJSONFromDTO(w io.Writer, inv *daemon.InvocationDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}

// writeAgentShowHumanFromDTO outputs invocation details in human-readable format from daemon DTO.
// PR-12: CLI renders daemon-provided display_status and attention_flags.
func writeAgentShowHumanFromDTO(w io.Writer, inv *daemon.InvocationDTO) error {
	_, _ = fmt.Fprintf(w, "invocation_id:          %s\n", inv.InvocationID)
	if inv.InvocationName != "" {
		_, _ = fmt.Fprintf(w, "name:                   %s\n", inv.InvocationName)
	}
	_, _ = fmt.Fprintf(w, "worktree_id:            %s\n", inv.WorktreeID)
	_, _ = fmt.Fprintf(w, "runner:                 %s\n", inv.Runner)
	_, _ = fmt.Fprintf(w, "mode:                   %s\n", inv.Mode)
	_, _ = fmt.Fprintf(w, "status:                 %s\n", inv.Status)
	_, _ = fmt.Fprintf(w, "display_status:         %s\n", inv.DisplayStatus)
	if inv.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:         %s\n", inv.LandingStatus)
	}
	if inv.SemanticStatus != "" {
		_, _ = fmt.Fprintf(w, "semantic_status:        %s\n", inv.SemanticStatus)
	}
	if len(inv.AttentionFlags) > 0 {
		_, _ = fmt.Fprintf(w, "attention_flags:        %v\n", inv.AttentionFlags)
	}
	_, _ = fmt.Fprintf(w, "started_at:             %s\n", inv.StartedAt)
	if inv.FinishedAt != "" {
		_, _ = fmt.Fprintf(w, "finished_at:            %s\n", inv.FinishedAt)
	}
	_, _ = fmt.Fprintf(w, "sandbox_path:           %s\n", inv.SandboxPath)
	if inv.LogsDir != "" {
		_, _ = fmt.Fprintf(w, "logs_dir:               %s\n", inv.LogsDir)
	}
	return nil
}

// AgentAttachOpts holds options for the agent attach command.
type AgentAttachOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client

	// IsInteractive reports whether the current session is an interactive terminal.
	// If nil, defaults to checking os.Stdin via term.IsTerminal.
	IsInteractive func() bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentAttach attaches to a running headed invocation's tmux session.
// This is only supported for headed invocations.
// PR-10: This is a real interactive TTY attach - refuses if stdin is not a terminal.
func AgentAttach(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentAttachOpts, stdout, stderr io.Writer) error {
	// PR-10: Check if stdin is a TTY - attach requires interactive terminal
	isInteractive := opts.IsInteractive
	if isInteractive == nil {
		isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) }
	}
	if !isInteractive() {
		return errors.NewWithDetails(
			errors.ENotInteractive,
			"attach requires an interactive terminal",
			map[string]string{
				"hint": "run this command in an interactive terminal, or use 'agency agent logs' to view output",
			},
		)
	}

	// Resolve paths
	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get home directory", err)
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	// PR-A: Resolve repo context via daemon
	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent attach",
	})
	if err != nil {
		return err
	}

	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // allow attaching to see final state
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	// Verify this is a headed invocation
	if record.Meta.Mode != store.RunnerModeHeaded {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"invocation is headless; attach is only supported for headed invocations",
			map[string]string{
				"invocation_id": record.InvocationID,
				"mode":          string(record.Meta.Mode),
				"hint":          "use 'agency agent logs' to view headless invocation output",
			},
		)
	}

	// Get session name from meta
	sessionName := record.Meta.TmuxSession
	if sessionName == "" {
		// Fall back to computed name if not in meta (shouldn't happen for properly started invocations)
		sessionName = tmux.SessionName(record.InvocationID)
	}

	// PR-10: Check if session exists using tmux client for preflight check
	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	exists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not check tmux session status: %v\n", err)
	}
	if !exists {
		return errors.NewWithDetails(
			errors.ESessionEnded,
			"tmux session not found",
			map[string]string{
				"session_name":  sessionName,
				"invocation_id": record.InvocationID,
				"hint":          "session ended; use 'agency agent logs' or 'agency agent open' to view",
			},
		)
	}

	// PR-10: Real TTY attach - use os/exec directly with stdin/stdout/stderr connected
	// This bypasses the tmux.Client interface because we need interactive I/O
	return realTmuxAttach(sessionName)
}

// realTmuxAttach performs a real interactive tmux attach with stdin/stdout/stderr connected.
// This is the only way to get proper interactive terminal behavior.
func realTmuxAttach(sessionName string) error {
	cmd := osexec.Command("tmux", "attach", "-t", sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isTerminal returns true if the given file descriptor is a terminal.
func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// AgentStopOpts holds options for the agent stop command.
type AgentStopOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStop sends a graceful stop signal (Ctrl-C) to a running invocation.
// PR-A: Supports --repo for CWD-less operation.
func AgentStop(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStopOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent stop",
	})
	if err != nil {
		return err
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: false,
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	resp, err := client.Stop(ctx, repoCtx.RepoID, record.InvocationID)
	if err != nil {
		return err
	}

	if !resp.OK {
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{"hint": resp.Hint},
		)
	}

	modeStr := "headless"
	if record.Meta.Mode == store.RunnerModeHeaded {
		modeStr = "headed"
	}

	_, _ = fmt.Fprintf(stdout, "Stop signal sent to %s invocation %s\n", modeStr, record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "Note: The runner may ignore the interrupt. Use 'agency agent kill' to force termination.\n")

	return nil
}

// AgentKillOpts holds options for the agent kill command.
type AgentKillOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentDiffOpts holds options for the agent diff command.
type AgentDiffOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string
}

// AgentDiff shows the diff between sandbox and base_commit.
// PR-A: Supports --repo for CWD-less operation.
func AgentDiff(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiffOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent diff",
	})
	if err != nil {
		return err
	}

	result, err := client.GetInvocationDiff(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationDiffOpts{
		IncludePatch:       true,
		IncludeUncommitted: true,
	})
	if err != nil {
		return err
	}

	diff := result.Diff

	// Show commit list
	_, _ = fmt.Fprintf(stdout, "Commits in sandbox:\n")
	_, _ = fmt.Fprintf(stdout, "==================\n")

	if diff.HasCommits && diff.CommittedRange != nil {
		for _, commit := range diff.CommittedRange.Commits {
			sha := commit.SHA
			if len(sha) > 8 {
				sha = sha[:8]
			}
			_, _ = fmt.Fprintf(stdout, "%s %s\n", sha, commit.Summary)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "(no commits)\n")
	}

	// Show committed diff
	_, _ = fmt.Fprintf(stdout, "\nFile diff (base_commit vs sandbox):\n")
	_, _ = fmt.Fprintf(stdout, "====================================\n")

	if diff.HasCommits && diff.CommittedRange != nil {
		if diff.CommittedRange.Patch != "" {
			_, _ = fmt.Fprint(stdout, diff.CommittedRange.Patch)
		} else {
			_, _ = fmt.Fprintf(stdout, "(diffstat: %s)\n", diff.CommittedRange.Diffstat)
		}
		if diff.CommittedRange.PatchTruncated {
			_, _ = fmt.Fprintf(stderr, "warning: patch was truncated (max bytes: %d)\n", diff.CommittedRange.PatchBytes)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "(no changes)\n")
	}

	// Show uncommitted changes
	if diff.HasUncommitted && diff.WorkingTree != nil {
		_, _ = fmt.Fprintf(stdout, "\nUncommitted changes in sandbox:\n")
		_, _ = fmt.Fprintf(stdout, "================================\n")
		if diff.WorkingTree.Patch != "" {
			_, _ = fmt.Fprint(stdout, diff.WorkingTree.Patch)
		} else {
			_, _ = fmt.Fprintf(stdout, "(diffstat: %s)\n", diff.WorkingTree.Diffstat)
		}
	}

	return nil
}

// AgentLandOpts holds options for the agent land command.
type AgentLandOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// Apply enables apply mode for uncommitted changes.
	Apply bool

	// RequireBase fails if integration has diverged from base_commit.
	RequireBase bool
}

// AgentLand lands sandbox changes to the integration worktree via daemon.
// PR-A: Supports --repo for CWD-less operation.
func AgentLand(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLandOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent land",
	})
	if err != nil {
		return err
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true,
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	// Call daemon to land
	resp, err := client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoCtx.RepoID,
		InvocationID: record.InvocationID,
		Apply:        opts.Apply,
		RequireBase:  opts.RequireBase,
	})
	if err != nil {
		return err
	}

	if !resp.OK {
		// Handle specific error codes
		hint := resp.Hint
		if resp.ErrorCode == string(errors.ELandConflict) && len(resp.ConflictFiles) > 0 {
			_, _ = fmt.Fprintf(stderr, "Conflicting files:\n")
			for _, f := range resp.ConflictFiles {
				_, _ = fmt.Fprintf(stderr, "  - %s\n", f)
			}
		}
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{"hint": hint},
		)
	}

	// Success output
	_, _ = fmt.Fprintf(stdout, "Successfully landed invocation %s\n", record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  mode:        %s\n", resp.AppliedMode)
	_, _ = fmt.Fprintf(stdout, "  commits:     %d\n", resp.CommitsLanded)
	_, _ = fmt.Fprintf(stdout, "  head_before: %s\n", resp.IntegrationHeadBefore[:12])
	_, _ = fmt.Fprintf(stdout, "  head_after:  %s\n", resp.IntegrationHeadAfter[:12])

	return nil
}

// AgentDiscardOpts holds options for the agent discard command.
type AgentDiscardOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string
}

// AgentDiscard discards a sandbox without landing via daemon.
// PR-A: Supports --repo for CWD-less operation.
func AgentDiscard(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiscardOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent discard",
	})
	if err != nil {
		return err
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true,
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	// Call daemon to discard
	resp, err := client.Discard(ctx, repoCtx.RepoID, record.InvocationID)
	if err != nil {
		return err
	}

	if !resp.OK {
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{"hint": resp.Hint},
		)
	}

	// Success output
	_, _ = fmt.Fprintf(stdout, "Discarded invocation %s\n", record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox and checkpoint refs have been removed.\n")

	return nil
}

// ---------------------------------------------------------------------------
// Shared navigation kernel setup for agent path/open/shell/enter (S2-PR04)
// ---------------------------------------------------------------------------

type agentNavSetup struct {
	dirs   paths.Dirs
	client *daemonclient.Client
}

func setupAgentNav(ctx context.Context, fsys fs.FS) (*agentNavSetup, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return nil, err
	}

	return &agentNavSetup{dirs: dirs, client: client}, nil
}

func (ns *agentNavSetup) buildNavDeps(cr exec.CommandRunner, cwd, repoFlag, cmdName string, isInteractive func() bool) NavigationDeps {
	return NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			return ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
				RepoFlag:      repoFlag,
				AllowAllRepos: false,
				CmdName:       cmdName,
			})
		},
		EnsureDaemon:    func(ctx context.Context) error { return nil },
		CheckAPIVersion: func(ctx context.Context) error { return ns.client.CheckAPIVersion(ctx) },
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			result, err := ns.client.GetInvocationRich(ctx, ref, repoID)
			if err != nil {
				return nil, err
			}
			return &NavigationResult{
				TargetKind:       TargetInvocation,
				ResolvedRepoID:   result.Invocation.RepoID,
				ResolvedID:       result.Invocation.InvocationID,
				ResolvedPath:     result.Invocation.SandboxPath,
				ResolutionSource: "daemon_get_invocation",
			}, nil
		},
		IsInteractive: isInteractive,
	}
}

// resolvedInvocationMode returns the daemon-resolved invocation mode for the
// navigation result's target ID. This is a separate daemon read because the
// navigation kernel only returns identity/path, not the full DTO.
func (ns *agentNavSetup) resolvedInvocationMode(ctx context.Context, invocationID, repoID string) (string, error) {
	result, err := ns.client.GetInvocationRich(ctx, invocationID, repoID)
	if err != nil {
		return "", err
	}
	return result.Invocation.Mode, nil
}

// ---------------------------------------------------------------------------
// AgentPath: canonical agent path command (S2-PR04)
// ---------------------------------------------------------------------------

// AgentPathOpts holds options for the agent path command.
type AgentPathOpts struct {
	InvocationRef string
	RepoFlag      string
}

// AgentPath outputs the daemon-resolved sandbox path for an invocation.
// S2-PR04: Routes through shared navigation kernel for daemon-first resolution.
func AgentPath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentPathOpts, stdout, stderr io.Writer) error {
	ns, err := setupAgentNav(ctx, fsys)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent path", func() bool { return false })
	intent := NavigationIntent{
		CommandFamily: "agent",
		Verb:          "path",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            opts.InvocationRef,
		},
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, result.ResolvedPath)
	return nil
}

// ---------------------------------------------------------------------------
// AgentOpen: canonical agent open command (S2-PR04 migration to nav kernel)
// ---------------------------------------------------------------------------

// AgentOpenOpts holds options for the agent open command.
type AgentOpenOpts struct {
	InvocationRef string
	RepoFlag      string
	Editor        string // override for tests; empty uses config/env/default
}

// AgentOpen opens the sandbox directory in the configured editor.
// S2-PR04: Routes through shared navigation kernel for daemon-first resolution.
// No local invocation target discovery — sandbox_path sourced from daemon.
func AgentOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
	ns, err := setupAgentNav(ctx, fsys)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent open", func() bool { return false })
	intent := NavigationIntent{
		CommandFamily: "agent",
		Verb:          "open",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            opts.InvocationRef,
		},
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	sandboxPath := result.ResolvedPath

	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"sandbox_path":  sandboxPath,
				"hint":          "sandbox was removed after landing or discarding",
			},
		)
	}

	editor := opts.Editor
	if editor == "" {
		userCfg, _, _ := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
		editor = userCfg.Defaults.Editor
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "code"
	}

	cmd := osexec.Command(editor, sandboxPath)
	cmd.Dir = sandboxPath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runErr := cmd.Run(); runErr != nil {
		if exitErr, ok := runErr.(*osexec.ExitError); ok {
			return errors.WithExitCode(
				errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", exitErr.ExitCode())),
				exitErr.ExitCode(),
			)
		}
		return errors.Wrap(errors.EEditorNotConfigured, "failed to open editor", runErr)
	}

	return nil
}

// ---------------------------------------------------------------------------
// AgentShell: canonical agent shell command (S2-PR04)
// ---------------------------------------------------------------------------

// AgentShellOpts holds options for the agent shell command.
type AgentShellOpts struct {
	InvocationRef string
	RepoFlag      string
}

// AgentShell opens a shell with cwd set to the daemon-resolved sandbox path.
// S2-PR04: Routes through shared navigation kernel for daemon-first resolution.
func AgentShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShellOpts, stdout, stderr io.Writer) error {
	ns, err := setupAgentNav(ctx, fsys)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent shell", func() bool { return false })
	intent := NavigationIntent{
		CommandFamily: "agent",
		Verb:          "shell",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            opts.InvocationRef,
		},
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	sandboxPath := result.ResolvedPath

	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"sandbox_path":  sandboxPath,
				"hint":          "sandbox was removed after landing or discarding",
			},
		)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := osexec.Command(shell, "-l")
	cmd.Dir = sandboxPath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runErr := cmd.Run(); runErr != nil {
		if exitErr, ok := runErr.(*osexec.ExitError); ok {
			return errors.WithExitCode(
				errors.New(errors.EInternal, fmt.Sprintf("shell exited with code %d", exitErr.ExitCode())),
				exitErr.ExitCode(),
			)
		}
		return errors.Wrap(errors.EInternal, "failed to run shell", runErr)
	}

	return nil
}

// ---------------------------------------------------------------------------
// AgentEnter: canonical agent enter command (S2-PR04)
// ---------------------------------------------------------------------------

// AgentEnterOpts holds options for the agent enter command.
type AgentEnterOpts struct {
	InvocationRef string
	RepoFlag      string

	// IsInteractive reports whether the current session is an interactive terminal.
	// If nil, defaults to checking os.Stdin via term.IsTerminal.
	IsInteractive func() bool

	// TmuxClient is the tmux client for session checks (optional, uses real client if nil).
	TmuxClient tmux.Client

	// TmuxAttachFn is a narrow seam for testability (D-003).
	// Defaults to realTmuxAttach in production.
	TmuxAttachFn func(sessionName string) error

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentEnter attaches to a running headed invocation via daemon-first resolution.
// S2-PR04: Canonical interactive navigation — consumes PR-02 kernel with TTY preflight.
// Headed-only: headless invocations return E_INVOCATION_INVALID_MODE.
func AgentEnter(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentEnterOpts, stdout, stderr io.Writer) error {
	isInteractive := opts.IsInteractive
	if isInteractive == nil {
		isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) }
	}

	var ns *agentNavSetup
	var err error
	if opts.DataDirOverride != "" {
		st := store.NewStore(fsys, opts.DataDirOverride, time.Now)
		socketPath := st.DaemonSocketPath()
		logPath := st.DaemonLogPath()
		client, clientErr := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
		if clientErr != nil {
			return clientErr
		}
		homeDir, _ := os.UserHomeDir()
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dirs.DataDir = opts.DataDirOverride
		ns = &agentNavSetup{dirs: dirs, client: client}
	} else {
		ns, err = setupAgentNav(ctx, fsys)
		if err != nil {
			return err
		}
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent enter", isInteractive)
	intent := NavigationIntent{
		CommandFamily: "agent",
		Verb:          "enter",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            opts.InvocationRef,
		},
		Interactive: true,
		RequiresTTY: true,
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	mode, err := ns.resolvedInvocationMode(ctx, result.ResolvedID, result.ResolvedRepoID)
	if err != nil {
		return err
	}
	if mode != "headed" {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"invocation is headless; enter is only supported for headed invocations",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"mode":          mode,
				"hint":          "use 'agency agent logs' to view headless invocation output",
			},
		)
	}

	sessionName := tmux.SessionName(result.ResolvedID)

	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	exists, checkErr := tmuxClient.HasSession(ctx, sessionName)
	if checkErr != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not check tmux session status: %v\n", checkErr)
	}
	if !exists {
		return errors.NewWithDetails(
			errors.ESessionEnded,
			"tmux session not found",
			map[string]string{
				"session_name":  sessionName,
				"invocation_id": result.ResolvedID,
				"hint":          "session ended; use 'agency agent logs' or 'agency agent open' to view",
			},
		)
	}

	attachFn := opts.TmuxAttachFn
	if attachFn == nil {
		attachFn = realTmuxAttach
	}
	return attachFn(sessionName)
}

// AgentLogsOpts holds options for the agent logs command.
type AgentLogsOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// Kind is the log kind: raw, stderr, stream (default: raw).
	Kind string

	// Follow enables follow mode: poll for new data after reaching EOF.
	Follow bool

	// Offset is the byte offset to start reading from (default 0).
	Offset int64

	// PollInterval is the follow-mode poll interval (default 500ms, min 250ms, max 5s).
	PollInterval time.Duration

	// SleepFn overrides time.Sleep for testing. If nil, uses time.Sleep.
	SleepFn func(time.Duration)

	// MaxIterations limits follow iterations for testing. 0 = unlimited.
	MaxIterations int

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentLogs views invocation logs via daemon offset-based API (PR-B).
// Without --follow: pages to EOF and exits.
// With --follow: pages to EOF, then polls for new data until interrupted.
func AgentLogs(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLogsOpts, stdout, stderr io.Writer) error {
	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get home directory", err)
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent logs",
	})
	if err != nil {
		return err
	}

	kind := opts.Kind
	if kind == "" {
		kind = "raw"
	}

	offset := opts.Offset
	sleepFn := opts.SleepFn
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond
	}

	// Page to EOF
	for {
		result, err := client.GetInvocationLogsOffset(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationLogsOffsetOpts{
			Kind:   kind,
			Offset: offset,
			Limit:  65536,
		})
		if err != nil {
			return err
		}

		if result.Logs.DataB64 != "" {
			decoded, decErr := base64Decode(result.Logs.DataB64)
			if decErr != nil {
				return errors.Wrap(errors.EInternal, "failed to decode log data", decErr)
			}
			_, _ = stdout.Write(decoded)
		}

		// No new data — we've reached EOF
		if result.Logs.NextOffset == offset {
			break
		}
		offset = result.Logs.NextOffset
	}

	// If not following, we're done
	if !opts.Follow {
		return nil
	}

	// Follow mode: poll for new data
	iterations := 0
	for {
		// Check context cancellation before sleeping
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		sleepFn(pollInterval)

		// Re-check after sleep — context may have been cancelled during sleep
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		result, err := client.GetInvocationLogsOffset(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationLogsOffsetOpts{
			Kind:   kind,
			Offset: offset,
			Limit:  65536,
		})
		if err != nil {
			// On error during follow, print and exit
			_, _ = fmt.Fprintf(stderr, "\nerror: %v\n", err)
			return err
		}

		if result.Logs.DataB64 != "" {
			decoded, decErr := base64Decode(result.Logs.DataB64)
			if decErr != nil {
				return errors.Wrap(errors.EInternal, "failed to decode log data", decErr)
			}
			_, _ = stdout.Write(decoded)
		}

		offset = result.Logs.NextOffset

		iterations++
		if opts.MaxIterations > 0 && iterations >= opts.MaxIterations {
			break
		}
	}

	return nil
}

// base64Decode decodes a base64-encoded string.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// AgentKill forcefully terminates a running invocation.
// PR-A: Supports --repo for CWD-less operation.
func AgentKill(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentKillOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent kill",
	})
	if err != nil {
		return err
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true,
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	resp, err := client.Kill(ctx, repoCtx.RepoID, record.InvocationID)
	if err != nil {
		return err
	}

	if !resp.OK {
		return errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{"hint": resp.Hint},
		)
	}

	modeStr := "headless"
	if record.Meta.Mode == store.RunnerModeHeaded {
		modeStr = "headed"
	}

	_, _ = fmt.Fprintf(stdout, "Killed %s invocation %s\n", modeStr, record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox preserved at: %s\n", record.Meta.SandboxPath)

	return nil
}
