// Package commands implements agency CLI commands.
// This file implements agent commands (Slice 8 PR-02/03/04).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"time"

	"golang.org/x/term"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
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

	// All includes finished (landed/discarded) invocations.
	All bool

	// JSON outputs as JSON.
	JSON bool
}

// AgentLS lists agent invocations.
func AgentLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLSOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree filter if provided
	var worktreeFilter string
	if opts.WorktreeRef != "" {
		st := store.NewStore(fsys, dirs.DataDir, time.Now)
		wtSvc := integrationworktree.NewService(st, cr, fsys, time.Now)
		wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
		if err != nil {
			return err
		}
		worktreeFilter = wtRecord.WorktreeID
	}

	// Scan invocations
	var records []store.InvocationRecord
	if worktreeFilter != "" {
		records, err = store.ScanInvocationsForWorktree(dirs.DataDir, repoIdentity.RepoID, worktreeFilter)
	} else {
		records, err = store.ScanInvocationsForRepo(dirs.DataDir, repoIdentity.RepoID)
	}
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to scan invocations", err)
	}

	// Filter by status unless --all
	var filtered []store.InvocationRecord
	for _, r := range records {
		if r.Broken {
			if opts.All {
				filtered = append(filtered, r)
			}
			continue
		}
		if r.Meta != nil {
			landed := r.Meta.LandingStatus == store.LandingStatusLanded
			discarded := r.Meta.LandingStatus == store.LandingStatusDiscarded
			if (landed || discarded) && !opts.All {
				continue
			}
		}
		filtered = append(filtered, r)
	}

	// Output
	if opts.JSON {
		return writeAgentLSJSON(stdout, filtered)
	}

	return writeAgentLSHuman(stdout, filtered)
}

func writeAgentLSJSON(w io.Writer, records []store.InvocationRecord) error {
	type jsonRecord struct {
		InvocationID          string `json:"invocation_id"`
		InvocationName        string `json:"invocation_name,omitempty"`
		IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
		Runner                string `json:"runner,omitempty"`
		Mode                  string `json:"mode,omitempty"`
		Status                string `json:"status,omitempty"`
		LandingStatus         string `json:"landing_status,omitempty"`
		SandboxPath           string `json:"sandbox_path,omitempty"`
		StartedAt             string `json:"started_at,omitempty"`
		SandboxExists         bool   `json:"sandbox_exists"`
		Broken                bool   `json:"broken,omitempty"`
	}

	out := make([]jsonRecord, len(records))
	for i, r := range records {
		jr := jsonRecord{
			InvocationID:  r.InvocationID,
			SandboxExists: r.SandboxExists,
			Broken:        r.Broken,
		}
		if r.Meta != nil {
			jr.InvocationName = r.Meta.InvocationName
			jr.IntegrationWorktreeID = r.Meta.IntegrationWorktreeID
			jr.Runner = r.Meta.Runner
			jr.Mode = string(r.Meta.Mode)
			jr.Status = string(r.Meta.Status)
			jr.LandingStatus = string(r.Meta.LandingStatus)
			jr.SandboxPath = r.Meta.SandboxPath
			jr.StartedAt = r.Meta.StartedAt
		}
		out[i] = jr
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeAgentLSHuman(w io.Writer, records []store.InvocationRecord) error {
	if len(records) == 0 {
		_, _ = fmt.Fprintln(w, "No agent invocations found.")
		return nil
	}

	for _, r := range records {
		if r.Broken {
			_, _ = fmt.Fprintf(w, "%s  [broken]\n", r.InvocationID)
			continue
		}

		name := ""
		if r.Meta.InvocationName != "" {
			name = " (" + r.Meta.InvocationName + ")"
		}

		sandboxStatus := ""
		if !r.SandboxExists {
			sandboxStatus = " [no sandbox]"
		}

		landingStatus := ""
		switch r.Meta.LandingStatus {
		case store.LandingStatusLanded:
			landingStatus = " [landed]"
		case store.LandingStatusDiscarded:
			landingStatus = " [discarded]"
		}

		_, _ = fmt.Fprintf(w, "%s  %s  %s  %s%s%s%s\n",
			r.InvocationID,
			r.Meta.Runner,
			r.Meta.Mode,
			r.Meta.Status,
			name,
			sandboxStatus,
			landingStatus,
		)
	}

	return nil
}

// AgentShowOpts holds options for the agent show command.
type AgentShowOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// JSON outputs as JSON.
	JSON bool
}

// AgentShow shows details of an agent invocation.
func AgentShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShowOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // show can view any invocation
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
				"hint":           "inspect or remove the directory manually",
			},
		)
	}

	// Output
	if opts.JSON {
		return writeAgentShowJSON(stdout, record)
	}

	return writeAgentShowHuman(stdout, record)
}

func writeAgentShowJSON(w io.Writer, r *store.InvocationRecord) error {
	type jsonRecord struct {
		InvocationID          string `json:"invocation_id"`
		InvocationName        string `json:"invocation_name,omitempty"`
		IntegrationWorktreeID string `json:"integration_worktree_id"`
		SandboxPath           string `json:"sandbox_path"`
		SandboxBranch         string `json:"sandbox_branch"`
		BaseCommit            string `json:"base_commit"`
		Runner                string `json:"runner"`
		Mode                  string `json:"mode"`
		Status                string `json:"status"`
		LandingStatus         string `json:"landing_status,omitempty"`
		StartedAt             string `json:"started_at"`
		FinishedAt            string `json:"finished_at,omitempty"`
		ExitCode              *int   `json:"exit_code,omitempty"`
		SandboxExists         bool   `json:"sandbox_exists"`
		InvocationDir         string `json:"invocation_dir"`
	}

	out := jsonRecord{
		InvocationID:          r.InvocationID,
		InvocationName:        r.Meta.InvocationName,
		IntegrationWorktreeID: r.Meta.IntegrationWorktreeID,
		SandboxPath:           r.Meta.SandboxPath,
		SandboxBranch:         r.Meta.SandboxBranch,
		BaseCommit:            r.Meta.BaseCommit,
		Runner:                r.Meta.Runner,
		Mode:                  string(r.Meta.Mode),
		Status:                string(r.Meta.Status),
		LandingStatus:         string(r.Meta.LandingStatus),
		StartedAt:             r.Meta.StartedAt,
		FinishedAt:            r.Meta.FinishedAt,
		ExitCode:              r.Meta.ExitCode,
		SandboxExists:         r.SandboxExists,
		InvocationDir:         r.InvocationDir,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeAgentShowHuman(w io.Writer, r *store.InvocationRecord) error {
	_, _ = fmt.Fprintf(w, "invocation_id:          %s\n", r.InvocationID)
	if r.Meta.InvocationName != "" {
		_, _ = fmt.Fprintf(w, "name:                   %s\n", r.Meta.InvocationName)
	}
	_, _ = fmt.Fprintf(w, "integration_worktree:   %s\n", r.Meta.IntegrationWorktreeID)
	_, _ = fmt.Fprintf(w, "runner:                 %s\n", r.Meta.Runner)
	_, _ = fmt.Fprintf(w, "mode:                   %s\n", r.Meta.Mode)
	_, _ = fmt.Fprintf(w, "status:                 %s\n", r.Meta.Status)
	if r.Meta.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:         %s\n", r.Meta.LandingStatus)
	}
	_, _ = fmt.Fprintf(w, "started_at:             %s\n", r.Meta.StartedAt)
	if r.Meta.FinishedAt != "" {
		_, _ = fmt.Fprintf(w, "finished_at:            %s\n", r.Meta.FinishedAt)
	}
	if r.Meta.TmuxSession != "" {
		_, _ = fmt.Fprintf(w, "tmux_session:           %s\n", r.Meta.TmuxSession)
	}
	_, _ = fmt.Fprintf(w, "base_commit:            %s\n", r.Meta.BaseCommit)
	_, _ = fmt.Fprintf(w, "sandbox_branch:         %s\n", r.Meta.SandboxBranch)
	_, _ = fmt.Fprintf(w, "sandbox_path:           %s\n", r.Meta.SandboxPath)
	_, _ = fmt.Fprintf(w, "sandbox_exists:         %v\n", r.SandboxExists)
	return nil
}

// AgentAttachOpts holds options for the agent attach command.
type AgentAttachOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

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

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// PR-10: Resolve invocation via local store read (read-only)
	st := store.NewStore(fsys, dataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
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

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStop sends a graceful stop signal (Ctrl-C) to a running invocation.
// This does not guarantee termination - the runner may ignore the signal.
// PR-10: Both headed and headless mode now route through daemon.
func AgentStop(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStopOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: false, // can only stop running invocations
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

	// PR-10: Route all invocations (headed and headless) through daemon
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	resp, err := client.Stop(ctx, repoIdentity.RepoID, record.InvocationID)
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

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentDiffOpts holds options for the agent diff command.
type AgentDiffOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string
}

// AgentDiff shows the diff between sandbox and base_commit.
// This is a CLI-local, read-only operation - no daemon call needed.
func AgentDiff(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiffOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // diff can be viewed for finished invocations
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

	// Show commit list: base_commit..sandbox_branch
	_, _ = fmt.Fprintf(stdout, "Commits in sandbox:\n")
	_, _ = fmt.Fprintf(stdout, "==================\n")

	logResult, err := cr.Run(ctx, "git", []string{
		"-C", repoRoot.Path,
		"log", "--oneline", fmt.Sprintf("%s..%s", record.Meta.BaseCommit, record.Meta.SandboxBranch),
	}, exec.RunOpts{})
	if err == nil && logResult.ExitCode == 0 {
		if logResult.Stdout == "" {
			_, _ = fmt.Fprintf(stdout, "(no commits)\n")
		} else {
			_, _ = fmt.Fprint(stdout, logResult.Stdout)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "(unable to list commits)\n")
	}

	_, _ = fmt.Fprintf(stdout, "\nFile diff (base_commit vs sandbox):\n")
	_, _ = fmt.Fprintf(stdout, "====================================\n")

	// Show file diff: base_commit..sandbox_branch
	diffResult, err := cr.Run(ctx, "git", []string{
		"-C", repoRoot.Path,
		"diff", fmt.Sprintf("%s..%s", record.Meta.BaseCommit, record.Meta.SandboxBranch),
	}, exec.RunOpts{})
	if err == nil && diffResult.ExitCode == 0 {
		if diffResult.Stdout == "" {
			_, _ = fmt.Fprintf(stdout, "(no changes)\n")
		} else {
			_, _ = fmt.Fprint(stdout, diffResult.Stdout)
		}
	} else {
		_, _ = fmt.Fprintf(stderr, "warning: unable to generate diff: %s\n", diffResult.Stderr)
	}

	// If sandbox exists and has uncommitted changes, show those too
	if record.SandboxExists {
		statusResult, err := cr.Run(ctx, "git", []string{
			"-C", record.Meta.SandboxPath,
			"status", "--porcelain",
		}, exec.RunOpts{})
		if err == nil && statusResult.ExitCode == 0 && statusResult.Stdout != "" {
			_, _ = fmt.Fprintf(stdout, "\nUncommitted changes in sandbox:\n")
			_, _ = fmt.Fprintf(stdout, "================================\n")
			_, _ = fmt.Fprint(stdout, statusResult.Stdout)
		}
	}

	return nil
}

// AgentLandOpts holds options for the agent land command.
type AgentLandOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// Apply enables apply mode for uncommitted changes.
	Apply bool

	// RequireBase fails if integration has diverged from base_commit.
	RequireBase bool
}

// AgentLand lands sandbox changes to the integration worktree via daemon.
func AgentLand(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLandOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // land works on finished invocations
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

	// Ensure daemon is running
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

	// Call daemon to land
	resp, err := client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoIdentity.RepoID,
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
}

// AgentDiscard discards a sandbox without landing via daemon.
func AgentDiscard(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiscardOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // discard works on any non-landed/discarded invocation
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

	// Ensure daemon is running
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

	// Call daemon to discard
	resp, err := client.Discard(ctx, repoIdentity.RepoID, record.InvocationID)
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

// AgentOpenOpts holds options for the agent open command.
type AgentOpenOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string
}

// AgentOpen opens the sandbox directory in the configured editor.
func AgentOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
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

	if !record.SandboxExists {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": record.InvocationID,
				"sandbox_path":  record.Meta.SandboxPath,
				"hint":          "sandbox was removed after landing or discarding",
			},
		)
	}

	// Get editor from config or environment
	userCfg, _, _ := config.LoadUserConfig(fsys, dirs.ConfigDir)
	editor := userCfg.Defaults.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "code" // default to VS Code
	}

	// Open editor
	_, _ = fmt.Fprintf(stdout, "Opening sandbox in %s: %s\n", editor, record.Meta.SandboxPath)
	result, err := cr.Run(ctx, editor, []string{record.Meta.SandboxPath}, exec.RunOpts{})
	if err != nil {
		return errors.Wrap(errors.EEditorNotConfigured, "failed to open editor", err)
	}
	if result.ExitCode != 0 {
		_, _ = fmt.Fprintf(stderr, "warning: editor exited with code %d\n", result.ExitCode)
	}

	return nil
}

// AgentKill forcefully terminates a running invocation.
// PR-10: Both headed and headless mode now route through daemon.
func AgentKill(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentKillOpts, stdout, stderr io.Writer) error {
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
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // allow killing already-finished to finalize state
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

	// PR-10: Route all invocations (headed and headless) through daemon
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	resp, err := client.Kill(ctx, repoIdentity.RepoID, record.InvocationID)
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
