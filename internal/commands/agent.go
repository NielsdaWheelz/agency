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
	"strings"
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
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/version"
)

// AgentStartOpts holds options for the agent start command.
type AgentStartOpts struct {
	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string

	// Runner is the runner id (claude-code, codex, amp, opencode, cursor, droid; claude/cursor-cli aliases supported).
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

	// JSON outputs as JSON.
	JSON bool

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots (PR-08).
	NoIncludeUntracked bool

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStart starts a new agent invocation.
// PR-10: Both headed and headless modes now delegate to daemon control plane.
// CLI never creates invocations, sandboxes, or tmux sessions directly.
func AgentStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStartOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return fail(errors.New(errors.ENoRepo, "not inside a git repository"))
	}

	// Validate runner
	runner, err := resolveAgentRunner(opts.Runner)
	if err != nil {
		return fail(err)
	}

	// For headless mode (PR-05): delegate everything to daemon control plane
	if opts.Headless {
		return fail(agentStartHeadlessControlPlane(ctx, cr, fsys, repoRoot.Path, dirs, opts, runner, stdout, stderr))
	}

	// PR-10: For headed mode: delegate to daemon control plane
	// CLI never creates invocations, sandboxes, or tmux sessions directly
	return fail(agentStartHeadedControlPlane(ctx, cr, fsys, repoRoot.Path, dirs, opts, runner, stdout, stderr))
}

func resolveAgentRunner(input string) (string, error) {
	runner := input
	if runner == "" {
		runner = runners.RunnerClaudeCode
	}

	canonicalRunner, err := runners.Canonicalize(runner)
	if err != nil {
		return "", errors.NewWithDetails(
			errors.EUsage,
			"invalid runner: "+runner,
			map[string]string{
				"runner": runner,
				"valid":  strings.Join(runners.CanonicalIDs(), ", "),
			},
		)
	}
	return canonicalRunner, nil
}

type agentMutationEnvelope struct {
	OK              bool   `json:"ok"`
	ErrorCode       string `json:"error_code"`
	Message         string `json:"message"`
	Hint            string `json:"hint"`
	RequestID       string `json:"request_id"`
	APIVersion      int    `json:"api_version"`
	BuildVersion    string `json:"build_version"`
	ClientRequestID string `json:"client_request_id"`

	InvocationID            string                    `json:"invocation_id,omitempty"`
	RepoID                  string                    `json:"repo_id,omitempty"`
	IntegrationWorktreeID   string                    `json:"integration_worktree_id,omitempty"`
	IntegrationWorktreeName string                    `json:"integration_worktree_name,omitempty"`
	SandboxPath             string                    `json:"sandbox_path,omitempty"`
	LogPaths                *daemon.LogPaths          `json:"log_paths,omitempty"`
	PID                     int                       `json:"pid,omitempty"`
	PGID                    int                       `json:"pgid,omitempty"`
	DaemonInstanceID        string                    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning          bool                      `json:"already_running,omitempty"`
	AlreadyApplied          bool                      `json:"already_applied,omitempty"`
	TimelineEntryID         string                    `json:"timeline_entry_id,omitempty"`
	CheckpointID            int                       `json:"checkpoint_id,omitempty"`
	SnapshotCommit          string                    `json:"snapshot_commit,omitempty"`
	RestoredAt              string                    `json:"restored_at,omitempty"`
	AppliedMode             daemon.LandingMode        `json:"applied_mode,omitempty"`
	IntegrationHeadBefore   string                    `json:"integration_head_before,omitempty"`
	IntegrationHeadAfter    string                    `json:"integration_head_after,omitempty"`
	CommitsLanded           int                       `json:"commits_landed,omitempty"`
	Branch                  string                    `json:"branch,omitempty"`
	PRNumber                int                       `json:"pr_number,omitempty"`
	PRURL                   string                    `json:"pr_url,omitempty"`
	PRAction                string                    `json:"pr_action,omitempty"`
	Strategy                string                    `json:"strategy,omitempty"`
	DeleteBranch            bool                      `json:"delete_branch,omitempty"`
	MergeLogPath            string                    `json:"merge_log_path,omitempty"`
	VerifyLogPath           string                    `json:"verify_log_path,omitempty"`
	ReportSource            string                    `json:"report_source,omitempty"`
	ReportFallbackUsed      bool                      `json:"report_fallback_used,omitempty"`
	ReportDiagnostics       []daemon.ReportDiagnostic `json:"report_diagnostics,omitempty"`
}

func newAgentMutationEnvelope() agentMutationEnvelope {
	return agentMutationEnvelope{
		OK:              false,
		ErrorCode:       "",
		Message:         "",
		Hint:            "",
		RequestID:       "",
		APIVersion:      daemon.APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: "",
	}
}

func writeAgentMutationJSON(w io.Writer, envelope agentMutationEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

func writeAgentMutationJSONSuccess(w io.Writer, mutate func(*agentMutationEnvelope)) error {
	envelope := newAgentMutationEnvelope()
	envelope.OK = true
	if mutate != nil {
		mutate(&envelope)
	}
	return writeAgentMutationJSON(w, envelope)
}

func writeAgentMutationJSONError(w io.Writer, err error) error {
	envelope := newAgentMutationEnvelope()
	code := errors.GetCode(err)
	if code == "" {
		code = errors.EInternal
	}
	envelope.ErrorCode = string(code)
	envelope.Message = err.Error()
	if ae, ok := errors.AsAgencyError(err); ok {
		envelope.Message = ae.Msg
		if ae.Details != nil {
			envelope.Hint = ae.Details["hint"]
			envelope.RequestID = ae.Details["request_id"]
		}
	}
	return writeAgentMutationJSON(w, envelope)
}

// WriteAgentMutationJSONError writes a stable mutation error envelope.
// Exported for CLI preflight validation paths that occur before command dispatch.
func WriteAgentMutationJSONError(w io.Writer, err error) error {
	return writeAgentMutationJSONError(w, err)
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
			envelope.DaemonInstanceID = resp.DaemonInstanceID
			envelope.AlreadyRunning = resp.AlreadyRunning
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.ClientRequestID = resp.ClientRequestID
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
			envelope.ClientRequestID = resp.ClientRequestID
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
// PR-05: compatibility alias over canonical AgentEnter daemon-first resolution.
func AgentAttach(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentAttachOpts, stdout, stderr io.Writer) error {
	return AgentEnter(ctx, cr, fsys, cwd, AgentEnterOpts{
		InvocationRef:   opts.InvocationRef,
		RepoFlag:        opts.RepoFlag,
		IsInteractive:   opts.IsInteractive,
		TmuxClient:      opts.TmuxClient,
		DataDirOverride: opts.DataDirOverride,
	}, stdout, stderr)
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

	// JSON outputs as JSON.
	JSON bool
}

// AgentStop sends a graceful stop signal (Ctrl-C) to a running invocation.
// PR-A: Supports --repo for CWD-less operation.
func AgentStop(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStopOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent stop",
	})
	if err != nil {
		return fail(err)
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: false,
	})
	if err != nil {
		return fail(err)
	}

	if record.Broken {
		return fail(errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		))
	}

	resp, err := client.Stop(ctx, repoCtx.RepoID, record.InvocationID)
	if err != nil {
		return fail(err)
	}

	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = record.InvocationID
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.ClientRequestID = resp.ClientRequestID
			envelope.RequestID = resp.RequestID
		})
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

	// JSON outputs as JSON.
	JSON bool
}

// AgentDiffOpts holds options for the agent diff command.
type AgentDiffOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// JSON outputs as JSON.
	JSON bool

	// TurnID selects a single timeline entry id for turn-aware diff context.
	TurnID string

	// TurnRange selects an inclusive timeline range using "<start>..<end>".
	TurnRange string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentDiff shows the diff between sandbox and base_commit.
// PR-A: Supports --repo for CWD-less operation.
func AgentDiff(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiffOpts, stdout, stderr io.Writer) error {
	if opts.TurnID != "" && opts.TurnRange != "" {
		return errors.New(errors.EUsage, "use either --turn or --turn-range, not both")
	}

	turnStart, turnEnd, err := parseTurnRange(opts.TurnRange)
	if err != nil {
		return err
	}

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
		CmdName:       "agent diff",
	})
	if err != nil {
		return err
	}

	result, err := client.GetInvocationDiff(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationDiffOpts{
		IncludePatch:       true,
		IncludeUncommitted: true,
		TurnID:             strings.TrimSpace(opts.TurnID),
		TurnStartID:        turnStart,
		TurnEndID:          turnEnd,
	})
	if err != nil {
		return err
	}

	diff := result.Diff

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(diff)
	}

	if diff.TurnContext != nil {
		_, _ = fmt.Fprintf(stdout, "Turn context:\n")
		switch diff.TurnContext.Selector.Kind {
		case "range":
			_, _ = fmt.Fprintf(stdout, "  selector:      %s..%s\n", diff.TurnContext.Selector.StartTurnID, diff.TurnContext.Selector.EndTurnID)
		default:
			_, _ = fmt.Fprintf(stdout, "  selector:      %s\n", diff.TurnContext.Selector.TurnID)
		}
		_, _ = fmt.Fprintf(stdout, "  checkpoints:   %d -> %d\n", diff.TurnContext.StartCheckpointID, diff.TurnContext.EndCheckpointID)
		_, _ = fmt.Fprintf(stdout, "  commit_range:  %s..%s\n\n", diff.TurnContext.FromCommit, diff.TurnContext.ToCommit)
	}

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

func parseTurnRange(turnRange string) (string, string, error) {
	trimmed := strings.TrimSpace(turnRange)
	if trimmed == "" {
		return "", "", nil
	}
	if strings.Count(trimmed, "..") != 1 {
		return "", "", errors.NewWithDetails(
			errors.EUsage,
			"invalid --turn-range value",
			map[string]string{
				"hint": "use --turn-range <start_entry_id>..<end_entry_id>",
			},
		)
	}
	start, end, ok := strings.Cut(trimmed, "..")
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if !ok || start == "" || end == "" {
		return "", "", errors.NewWithDetails(
			errors.EUsage,
			"invalid --turn-range value",
			map[string]string{
				"hint": "use --turn-range <start_entry_id>..<end_entry_id>",
			},
		)
	}
	return start, end, nil
}

// AgentReviewOpts holds options for the agent review command.
type AgentReviewOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value (PR-A).
	RepoFlag string

	// JSON outputs as JSON.
	JSON bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentReview reports canonical review/readiness state for invocation progression.
func AgentReview(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentReviewOpts, stdout, stderr io.Writer) error {
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
		CmdName:       "agent review",
	})
	if err != nil {
		return err
	}

	result, err := client.GetInvocationReview(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Review)
	}
	return writeAgentReviewHumanFromDTO(stdout, &result.Review)
}

func writeAgentReviewHumanFromDTO(w io.Writer, review *daemon.InvocationReviewData) error {
	verdict := "BLOCKED"
	if review.Ready {
		verdict = "READY"
	}
	prSyncEligible := "no"
	if review.PRSyncEligible {
		prSyncEligible = "yes"
	}

	_, _ = fmt.Fprintf(w, "Review verdict:       %s\n", verdict)
	_, _ = fmt.Fprintf(w, "pr_sync_eligible:     %s\n", prSyncEligible)
	_, _ = fmt.Fprintf(w, "invocation_id:        %s\n", review.InvocationID)
	_, _ = fmt.Fprintf(w, "repo_id:              %s\n", review.RepoID)
	_, _ = fmt.Fprintf(w, "status:               %s\n", review.Status)
	_, _ = fmt.Fprintf(w, "display_status:       %s\n", review.DisplayStatus)
	if review.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:       %s\n", review.LandingStatus)
	}
	if review.SemanticStatus != "" {
		_, _ = fmt.Fprintf(w, "semantic_status:      %s\n", review.SemanticStatus)
	}
	if review.RunnerStatus != "" {
		_, _ = fmt.Fprintf(w, "runner_status:        %s\n", review.RunnerStatus)
	}
	if review.RunnerUpdatedAt != "" {
		_, _ = fmt.Fprintf(w, "runner_updated_at:    %s\n", review.RunnerUpdatedAt)
	}
	if review.RunnerSummary != "" {
		_, _ = fmt.Fprintf(w, "runner_summary:       %s\n", review.RunnerSummary)
	}
	if review.HowToTest != "" {
		_, _ = fmt.Fprintf(w, "how_to_test:          %s\n", review.HowToTest)
	}
	if review.ReportSource != "" {
		_, _ = fmt.Fprintf(w, "report_source:        %s\n", review.ReportSource)
	}

	_, _ = fmt.Fprintf(w, "\nBlocking reasons:\n")
	if len(review.BlockingReasons) == 0 {
		_, _ = fmt.Fprintf(w, "  (none)\n")
	} else {
		for _, reason := range review.BlockingReasons {
			_, _ = fmt.Fprintf(w, "  - [%s] %s\n", reason.Code, reason.Message)
			if strings.TrimSpace(reason.Hint) != "" {
				_, _ = fmt.Fprintf(w, "      hint: %s\n", reason.Hint)
			}
		}
	}

	if len(review.ReportDiagnostics) > 0 {
		_, _ = fmt.Fprintf(w, "\nReport diagnostics:\n")
		for _, diagnostic := range review.ReportDiagnostics {
			_, _ = fmt.Fprintf(w, "  - [%s] %s\n", diagnostic.Code, diagnostic.Message)
		}
	}

	_, _ = fmt.Fprintf(w, "\nNavigation:\n")
	_, _ = fmt.Fprintf(w, "  history: %s\n", review.Navigation.HistoryCommand)
	if review.Navigation.DiffCommand != "" {
		_, _ = fmt.Fprintf(w, "  diff:    %s\n", review.Navigation.DiffCommand)
	}
	if review.Navigation.PRSyncCommand != "" {
		_, _ = fmt.Fprintf(w, "  pr_sync: %s\n", review.Navigation.PRSyncCommand)
	}
	if review.Navigation.LatestTurnID != "" {
		_, _ = fmt.Fprintf(w, "  turn:    %s\n", review.Navigation.LatestTurnID)
	}
	return nil
}

// AgentChecksOpts is retained as a compatibility alias.
type AgentChecksOpts = AgentReviewOpts

// AgentChecks is retained as a compatibility alias for AgentReview.
func AgentChecks(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentChecksOpts, stdout, stderr io.Writer) error {
	return AgentReview(ctx, cr, fsys, cwd, AgentReviewOpts(opts), stdout, stderr)
}

// AgentPRSyncOpts holds options for the agent pr sync command.
type AgentPRSyncOpts struct {
	InvocationRef   string
	RepoFlag        string
	AllowDirty      bool
	ForceWithLease  bool
	JSON            bool
	DataDirOverride string
}

// AgentPRSync performs invocation-scoped branch push + PR create/update via daemon.
func AgentPRSync(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentPRSyncOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent pr sync",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := client.PRSync(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.PRSyncOpts{
		AllowDirty:     opts.AllowDirty,
		ForceWithLease: opts.ForceWithLease,
	})
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = resp.InvocationID
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.Branch = resp.Branch
			envelope.PRNumber = resp.PRNumber
			envelope.PRURL = resp.PRURL
			envelope.PRAction = resp.PRAction
			envelope.ReportSource = resp.ReportSource
			envelope.ReportFallbackUsed = resp.ReportFallbackUsed
			envelope.ReportDiagnostics = resp.ReportDiagnostics
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}
	for _, diagnostic := range resp.ReportDiagnostics {
		_, _ = fmt.Fprintf(stderr, "warning: [%s] %s\n", diagnostic.Code, diagnostic.Message)
	}

	_, _ = fmt.Fprintln(stdout, "PR sync complete")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:  %s\n", resp.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  branch:         %s\n", resp.Branch)
	_, _ = fmt.Fprintf(stdout, "  pr_action:      %s\n", resp.PRAction)
	_, _ = fmt.Fprintf(stdout, "  pr_url:         %s\n", resp.PRURL)
	return nil
}

// AgentMergeOpts holds options for the agent merge command.
type AgentMergeOpts struct {
	InvocationRef  string
	RepoFlag       string
	Squash         bool
	Merge          bool
	Rebase         bool
	NoDeleteBranch bool
	Yes            bool
	JSON           bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// IsInteractive reports whether stdin/stderr are interactive terminals.
	// If nil, defaults to checking os.Stdin + os.Stderr.
	IsInteractive func() bool

	// ConfirmationIn provides interactive confirmation input.
	// If nil, defaults to os.Stdin.
	ConfirmationIn io.Reader
}

const maxMergeConfirmationBytes = 64

// AgentMerge performs invocation-scoped verify + merge via daemon.
func AgentMerge(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentMergeOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	strategyCount := 0
	strategy := "squash"
	if opts.Squash {
		strategyCount++
		strategy = "squash"
	}
	if opts.Merge {
		strategyCount++
		strategy = "merge"
	}
	if opts.Rebase {
		strategyCount++
		strategy = "rebase"
	}
	if strategyCount > 1 {
		return fail(errors.New(errors.EUsage, "at most one of --squash, --merge, --rebase may be specified"))
	}

	confirmationMode := "yes"
	confirmed := true
	if !opts.Yes {
		isInteractive := opts.IsInteractive
		if isInteractive == nil {
			isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()) }
		}
		if !isInteractive() {
			return fail(errors.NewWithDetails(
				errors.EConfirmationRequired,
				"non-interactive merge requires explicit confirmation",
				map[string]string{
					"hint": "re-run with --yes",
				},
			))
		}

		_, _ = fmt.Fprint(stderr, "confirm: type 'merge' to proceed: ")
		confirmationIn := opts.ConfirmationIn
		if confirmationIn == nil {
			confirmationIn = os.Stdin
		}
		token, err := readBoundedMergeConfirmationToken(confirmationIn, maxMergeConfirmationBytes)
		if err != nil {
			return fail(err)
		}
		if token != "merge" {
			return fail(errors.New(errors.EAborted, "merge confirmation failed; expected 'merge'"))
		}
		confirmationMode = "typed"
		confirmed = true
	}

	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent merge",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := client.Merge(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.MergeOpts{
		Strategy:         strategy,
		ConfirmationMode: confirmationMode,
		Confirmed:        confirmed,
		NoDeleteBranch:   opts.NoDeleteBranch,
	})
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = resp.InvocationID
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.Branch = resp.Branch
			envelope.PRNumber = resp.PRNumber
			envelope.PRURL = resp.PRURL
			envelope.Strategy = resp.Strategy
			envelope.DeleteBranch = resp.DeleteBranch
			envelope.MergeLogPath = resp.MergeLogPath
			envelope.VerifyLogPath = resp.VerifyLogPath
			envelope.ReportSource = resp.ReportSource
			envelope.ReportFallbackUsed = resp.ReportFallbackUsed
			envelope.ReportDiagnostics = resp.ReportDiagnostics
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}
	for _, diagnostic := range resp.ReportDiagnostics {
		_, _ = fmt.Fprintf(stderr, "warning: [%s] %s\n", diagnostic.Code, diagnostic.Message)
	}

	_, _ = fmt.Fprintln(stdout, "merge complete")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:  %s\n", resp.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  branch:         %s\n", resp.Branch)
	_, _ = fmt.Fprintf(stdout, "  strategy:       %s\n", resp.Strategy)
	_, _ = fmt.Fprintf(stdout, "  pr_url:         %s\n", resp.PRURL)
	_, _ = fmt.Fprintf(stdout, "  merge_log:      %s\n", resp.MergeLogPath)
	return nil
}

func readBoundedMergeConfirmationToken(r io.Reader, maxBytes int) (string, error) {
	if r == nil {
		return "", errors.New(errors.EInvalidArgument, "confirmation input is required")
	}
	if maxBytes <= 0 {
		maxBytes = maxMergeConfirmationBytes
	}

	data, err := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to read merge confirmation input", err)
	}
	if len(data) > maxBytes {
		return "", errors.NewWithDetails(
			errors.EInvalidArgument,
			"confirmation input exceeds maximum length",
			map[string]string{
				"hint": "type 'merge' exactly",
			},
		)
	}

	token := string(data)
	if nl := strings.IndexAny(token, "\r\n"); nl >= 0 {
		token = token[:nl]
	}
	return strings.TrimSpace(token), nil
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

	// JSON outputs as JSON.
	JSON bool
}

// AgentLand lands sandbox changes to the integration worktree via daemon.
// PR-A: Supports --repo for CWD-less operation.
func AgentLand(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLandOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent land",
	})
	if err != nil {
		return fail(err)
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true,
	})
	if err != nil {
		return fail(err)
	}

	if record.Broken {
		return fail(errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		))
	}

	// Call daemon to land
	resp, err := client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoCtx.RepoID,
		InvocationID: record.InvocationID,
		Apply:        opts.Apply,
		RequireBase:  opts.RequireBase,
	})
	if err != nil {
		return fail(err)
	}

	if !resp.OK {
		// Handle specific error codes
		hint := resp.Hint
		if !opts.JSON && resp.ErrorCode == string(errors.ELandConflict) && len(resp.ConflictFiles) > 0 {
			_, _ = fmt.Fprintf(stderr, "Conflicting files:\n")
			for _, f := range resp.ConflictFiles {
				_, _ = fmt.Fprintf(stderr, "  - %s\n", f)
			}
		}
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = record.InvocationID
			envelope.AppliedMode = resp.AppliedMode
			envelope.IntegrationHeadBefore = resp.IntegrationHeadBefore
			envelope.IntegrationHeadAfter = resp.IntegrationHeadAfter
			envelope.CommitsLanded = resp.CommitsLanded
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
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

	// JSON outputs as JSON.
	JSON bool
}

// AgentDiscard discards a sandbox without landing via daemon.
// PR-A: Supports --repo for CWD-less operation.
func AgentDiscard(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiscardOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent discard",
	})
	if err != nil {
		return fail(err)
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true,
	})
	if err != nil {
		return fail(err)
	}

	if record.Broken {
		return fail(errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		))
	}

	// Call daemon to discard
	resp, err := client.Discard(ctx, repoCtx.RepoID, record.InvocationID)
	if err != nil {
		return fail(err)
	}

	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = record.InvocationID
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
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
	if !isInteractive() {
		return errors.NewWithDetails(
			errors.ENotInteractive,
			"this command requires an interactive terminal",
			map[string]string{
				"hint": "run this command in an interactive terminal, or use a non-interactive alternative",
			},
		)
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

// AgentChatOpts holds options for the agent chat command (S3 PR-02).
type AgentChatOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value.
	RepoFlag string

	// Prompt is direct prompt text.
	Prompt string

	// PromptFile is a file path containing the prompt text.
	PromptFile string

	// JSON outputs as JSON.
	JSON bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentChat submits a follow-up prompt to an existing headless invocation.
func AgentChat(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentChatOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	prompt, err := resolveBoundedPromptInput(
		opts.Prompt,
		opts.PromptFile,
		daemon.MaxPromptSize,
		"follow-up prompt requires --prompt or --prompt-file",
		"follow-up prompt cannot be empty",
	)
	if err != nil {
		return fail(err)
	}

	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent chat",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := client.SubmitFollowUpPrompt(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.SubmitFollowUpPromptOpts{
		Prompt: prompt,
	})
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = resp.InvocationID
			envelope.TimelineEntryID = resp.TimelineEntry
			envelope.AlreadyApplied = resp.AlreadyApplied
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.ClientRequestID = resp.ClientRequestID
			envelope.RequestID = resp.RequestID
		})
	}

	_, _ = fmt.Fprintln(stdout, "accepted follow-up prompt")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:    %s\n", resp.InvocationID)
	if resp.TimelineEntry != "" {
		_, _ = fmt.Fprintf(stdout, "  timeline_entry:   %s\n", resp.TimelineEntry)
	}
	if resp.AlreadyApplied {
		_, _ = fmt.Fprintln(stdout, "\nNote: duplicate request id detected; existing follow-up entry reused.")
	}
	return nil
}

// AgentRestartOpts holds options for the agent restart command (S3 PR-03).
type AgentRestartOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value.
	RepoFlag string

	// CheckpointID is the explicit checkpoint to restore before restart.
	CheckpointID int

	// InteractiveHistory enables arrow-key timeline selection that maps deterministically
	// to a checkpoint before executing canonical restart.
	InteractiveHistory bool

	// RunnerArgs are additional arguments for restarted runner execution.
	RunnerArgs []string

	// Env are explicit environment overrides for restarted runner execution.
	Env map[string]string

	// JSON outputs as JSON.
	JSON bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// IsInteractive reports whether stdin/stderr are interactive terminals.
	// If nil, defaults to terminal checks on os.Stdin/os.Stderr.
	IsInteractive func() bool

	// HistorySelector overrides the interactive selector implementation.
	// Primarily for tests; nil uses the built-in selector.
	HistorySelector historySelectorFunc

	// HistorySelectorIn is the selector input stream. Defaults to os.Stdin.
	HistorySelectorIn io.Reader

	// HistorySelectorOut is the selector render output stream. Defaults to stderr.
	HistorySelectorOut io.Writer
}

type historySelectorItem struct {
	Entry              daemon.TimelineEntryDTO
	Summary            string
	MappedCheckpointID int
}

type historySelectorFunc func(items []historySelectorItem, input io.Reader, output io.Writer) (historySelectorItem, error)

const (
	maxHistorySelectorEntries = 5000
	historySelectorWindowSize = 12
)

// AgentRestart performs invocation-scoped restart from an explicit checkpoint
// or an interactively selected timeline point.
func AgentRestart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentRestartOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	if opts.CheckpointID < 0 {
		return fail(errors.New(errors.EUsage, "--checkpoint must be a positive integer"))
	}
	if opts.InteractiveHistory && opts.CheckpointID > 0 {
		return fail(errors.New(errors.EUsage, "use either --checkpoint or --history, not both"))
	}
	if !opts.InteractiveHistory && opts.CheckpointID <= 0 {
		return fail(errors.New(errors.EUsage, "--checkpoint must be a positive integer (or pass --history)"))
	}
	if opts.InteractiveHistory {
		isInteractiveFn := opts.IsInteractive
		if isInteractiveFn == nil {
			isInteractiveFn = func() bool { return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()) }
		}
		if !isInteractiveFn() {
			return fail(errors.NewWithDetails(
				errors.ENotInteractive,
				"interactive history selection requires a terminal",
				map[string]string{
					"hint": "run this command in an interactive terminal or use --checkpoint <id>",
				},
			))
		}
	}

	var dataDir string
	if opts.DataDirOverride != "" {
		dataDir = opts.DataDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		dataDir = dirs.DataDir
	}

	st := store.NewStore(fsys, dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent restart",
	})
	if err != nil {
		return fail(err)
	}

	if opts.InteractiveHistory {
		timelineEntries, err := fetchAllTimelineEntries(ctx, client, opts.InvocationRef, repoCtx.RepoID)
		if err != nil {
			return fail(err)
		}
		checkpoints, err := fetchAllCheckpoints(ctx, client, opts.InvocationRef, repoCtx.RepoID)
		if err != nil {
			return fail(err)
		}
		if len(timelineEntries) == 0 {
			return fail(errors.NewWithDetails(
				errors.ECheckpointNotFound,
				"interactive history selection requires timeline entries",
				map[string]string{
					"hint": "no history is available for this invocation; use --checkpoint <id>",
				},
			))
		}

		items := buildHistorySelectorItems(timelineEntries, checkpoints)
		selectorInput := opts.HistorySelectorIn
		if selectorInput == nil {
			selectorInput = os.Stdin
		}
		selectorOutput := opts.HistorySelectorOut
		if selectorOutput == nil {
			selectorOutput = stderr
		}

		selector := opts.HistorySelector
		if selector == nil {
			if _, ok := selectorInput.(*os.File); ok && selectorInput == os.Stdin {
				selector = runInteractiveHistorySelectorTTY
			} else {
				selector = runInteractiveHistorySelector
			}
		}

		selected, err := selector(items, selectorInput, selectorOutput)
		if err != nil {
			return fail(err)
		}

		checkpointID, err := mapTimelineSelectionToCheckpoint(timelineEntries, checkpoints, selected.Entry.EntryID)
		if err != nil {
			return fail(err)
		}
		opts.CheckpointID = checkpointID
	}

	resp, err := client.RestartFromCheckpoint(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.RestartFromCheckpointOpts{
		CheckpointID: opts.CheckpointID,
		RunnerArgs:   opts.RunnerArgs,
		Env:          opts.Env,
	})
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = resp.InvocationID
			envelope.CheckpointID = resp.CheckpointID
			envelope.SnapshotCommit = resp.SnapshotCommit
			envelope.RestoredAt = resp.RestoredAt
			envelope.PID = resp.PID
			envelope.PGID = resp.PGID
			envelope.DaemonInstanceID = resp.DaemonInstanceID
			envelope.LogPaths = resp.LogPaths
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}

	_, _ = fmt.Fprintln(stdout, "restarted invocation from checkpoint")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:    %s\n", resp.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  checkpoint_id:    %d\n", resp.CheckpointID)
	_, _ = fmt.Fprintf(stdout, "  snapshot_commit:  %s\n", resp.SnapshotCommit)
	_, _ = fmt.Fprintf(stdout, "  restored_at:      %s\n", resp.RestoredAt)
	_, _ = fmt.Fprintf(stdout, "  pid:              %d\n", resp.PID)
	return nil
}

func fetchAllTimelineEntries(ctx context.Context, client *daemonclient.Client, invocationRef, repoID string) ([]daemon.TimelineEntryDTO, error) {
	entries := make([]daemon.TimelineEntryDTO, 0, 128)
	cursor := ""

	for {
		result, err := client.GetInvocationTimeline(ctx, invocationRef, repoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		entries = append(entries, result.Entries...)
		if len(entries) > maxHistorySelectorEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history selector supports at most %d timeline entries", maxHistorySelectorEntries),
				map[string]string{
					"hint": "narrow invocation scope or use explicit --checkpoint <id>",
				},
			)
		}

		if result.NextCursor == "" {
			return entries, nil
		}
		if result.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "timeline pagination cursor did not advance")
		}
		cursor = result.NextCursor
	}
}

func fetchAllCheckpoints(ctx context.Context, client *daemonclient.Client, invocationRef, repoID string) ([]daemon.CheckpointDTO, error) {
	checkpoints := make([]daemon.CheckpointDTO, 0, 32)
	cursor := ""

	for {
		result, err := client.ListCheckpoints(ctx, invocationRef, repoID, daemonclient.ListCheckpointsOpts{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}

		checkpoints = append(checkpoints, result.Checkpoints...)
		if len(checkpoints) > maxHistorySelectorEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history selector supports at most %d checkpoints", maxHistorySelectorEntries),
				map[string]string{
					"hint": "use explicit --checkpoint <id> for very large histories",
				},
			)
		}

		if result.NextCursor == "" {
			return checkpoints, nil
		}
		if result.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "checkpoint pagination cursor did not advance")
		}
		cursor = result.NextCursor
	}
}

// Deterministic mapping rule:
// select the latest checkpoint_event (with checkpoint_id) at or before the chosen timeline entry.
func mapTimelineSelectionToCheckpoint(entries []daemon.TimelineEntryDTO, checkpoints []daemon.CheckpointDTO, selectedEntryID string) (int, error) {
	if selectedEntryID == "" {
		return 0, errors.New(errors.EInvalidArgument, "selected timeline entry is required")
	}

	checkpointSet := make(map[int]struct{}, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointSet[cp.ID] = struct{}{}
	}

	mappedCheckpointID := 0
	foundSelection := false
	for _, entry := range entries {
		if checkpointID, ok := checkpointIDFromTimelineEntry(entry); ok {
			mappedCheckpointID = checkpointID
		}
		if entry.EntryID == selectedEntryID {
			foundSelection = true
			break
		}
	}

	if !foundSelection {
		return 0, errors.NewWithDetails(
			errors.EInvalidArgument,
			"selected timeline entry was not found",
			map[string]string{"entry_id": selectedEntryID},
		)
	}
	if mappedCheckpointID <= 0 {
		return 0, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			"no checkpoint mapping exists at or before the selected history point",
			map[string]string{
				"entry_id": selectedEntryID,
				"hint":     "select a later history entry or pass --checkpoint <id>",
			},
		)
	}
	if _, ok := checkpointSet[mappedCheckpointID]; !ok {
		return 0, errors.NewWithDetails(
			errors.ECheckpointNotFound,
			fmt.Sprintf("mapped checkpoint %d is no longer available", mappedCheckpointID),
			map[string]string{
				"entry_id":      selectedEntryID,
				"checkpoint_id": fmt.Sprintf("%d", mappedCheckpointID),
				"hint":          "run 'agency checkpoint ls --invocation <id>' and retry with --checkpoint",
			},
		)
	}
	return mappedCheckpointID, nil
}

func checkpointIDFromTimelineEntry(entry daemon.TimelineEntryDTO) (int, bool) {
	if entry.Kind != "checkpoint_event" {
		return 0, false
	}
	value, ok := timelineInt(entry.Data, "checkpoint_id")
	if !ok || value <= 0 {
		return 0, false
	}
	return int(value), true
}

func buildHistorySelectorItems(entries []daemon.TimelineEntryDTO, checkpoints []daemon.CheckpointDTO) []historySelectorItem {
	checkpointSet := make(map[int]struct{}, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointSet[cp.ID] = struct{}{}
	}

	items := make([]historySelectorItem, 0, len(entries))
	latestCheckpointID := 0
	for _, entry := range entries {
		if checkpointID, ok := checkpointIDFromTimelineEntry(entry); ok {
			latestCheckpointID = checkpointID
		}
		mappedCheckpointID := 0
		if latestCheckpointID > 0 {
			if _, ok := checkpointSet[latestCheckpointID]; ok {
				mappedCheckpointID = latestCheckpointID
			}
		}
		items = append(items, historySelectorItem{
			Entry:              entry,
			Summary:            timelineEntrySummary(entry),
			MappedCheckpointID: mappedCheckpointID,
		})
	}
	return items
}

type historySelectorKey int

const (
	historySelectorKeyUnknown historySelectorKey = iota
	historySelectorKeyUp
	historySelectorKeyDown
	historySelectorKeyConfirm
	historySelectorKeyCancel
)

func readHistorySelectorKey(input io.Reader) (historySelectorKey, error) {
	var b [1]byte
	if _, err := io.ReadFull(input, b[:]); err != nil {
		return historySelectorKeyUnknown, err
	}

	switch b[0] {
	case '\r', '\n':
		return historySelectorKeyConfirm, nil
	case 'q', 'Q', 3:
		return historySelectorKeyCancel, nil
	case 'k', 'K':
		return historySelectorKeyUp, nil
	case 'j', 'J':
		return historySelectorKeyDown, nil
	case 0x1b:
		var seq [2]byte
		if _, err := io.ReadFull(input, seq[:]); err != nil {
			// Partial escape sequence; ignore and continue.
			return historySelectorKeyUnknown, nil
		}
		if seq[0] == '[' {
			switch seq[1] {
			case 'A':
				return historySelectorKeyUp, nil
			case 'B':
				return historySelectorKeyDown, nil
			}
		}
	}

	return historySelectorKeyUnknown, nil
}

func runInteractiveHistorySelectorTTY(items []historySelectorItem, input io.Reader, output io.Writer) (historySelectorItem, error) {
	fileInput, ok := input.(*os.File)
	if !ok {
		return runInteractiveHistorySelector(items, input, output)
	}

	oldState, err := term.MakeRaw(int(fileInput.Fd()))
	if err != nil {
		return historySelectorItem{}, errors.Wrap(errors.EInternal, "failed to enable terminal raw mode", err)
	}
	defer func() { _ = term.Restore(int(fileInput.Fd()), oldState) }()

	_, _ = fmt.Fprint(output, "\x1b[?25l")
	defer func() { _, _ = fmt.Fprint(output, "\x1b[?25h") }()

	selected, err := runInteractiveHistorySelector(items, input, output)
	_, _ = fmt.Fprint(output, "\x1b[2J\x1b[H")
	return selected, err
}

func runInteractiveHistorySelector(items []historySelectorItem, input io.Reader, output io.Writer) (historySelectorItem, error) {
	if len(items) == 0 {
		return historySelectorItem{}, errors.New(errors.ECheckpointNotFound, "no timeline entries available for selection")
	}

	selectedIdx := len(items) - 1 // default to latest timeline entry
	renderInteractiveHistorySelector(output, items, selectedIdx)

	for {
		key, err := readHistorySelectorKey(input)
		if err != nil {
			if err == io.EOF {
				return historySelectorItem{}, errors.New(errors.EAborted, "history selection canceled")
			}
			return historySelectorItem{}, errors.Wrap(errors.EInternal, "failed to read selector input", err)
		}

		switch key {
		case historySelectorKeyUp:
			if selectedIdx > 0 {
				selectedIdx--
				renderInteractiveHistorySelector(output, items, selectedIdx)
			}
		case historySelectorKeyDown:
			if selectedIdx < len(items)-1 {
				selectedIdx++
				renderInteractiveHistorySelector(output, items, selectedIdx)
			}
		case historySelectorKeyConfirm:
			return items[selectedIdx], nil
		case historySelectorKeyCancel:
			return historySelectorItem{}, errors.New(errors.EAborted, "history selection canceled")
		default:
			// Ignore unsupported keys.
		}
	}
}

func renderInteractiveHistorySelector(output io.Writer, items []historySelectorItem, selectedIdx int) {
	_, _ = fmt.Fprint(output, "\x1b[2J\x1b[H")
	_, _ = fmt.Fprintln(output, "select history point to restart from")
	_, _ = fmt.Fprintln(output, "controls: up/down arrows (or k/j), enter confirm, q cancel")
	_, _ = fmt.Fprintln(output, "")

	start, end := historySelectorWindow(len(items), selectedIdx, historySelectorWindowSize)
	for i := start; i < end; i++ {
		marker := " "
		if i == selectedIdx {
			marker = ">"
		}
		timestamp := items[i].Entry.Timestamp
		if timestamp == "" {
			timestamp = "-"
		}
		checkpointLabel := "-"
		if items[i].MappedCheckpointID > 0 {
			checkpointLabel = fmt.Sprintf("%d", items[i].MappedCheckpointID)
		}
		_, _ = fmt.Fprintf(
			output,
			"%s %s  cp:%-4s %-16s %s\n",
			marker,
			timestamp,
			checkpointLabel,
			items[i].Entry.Kind,
			truncateTimelineText(items[i].Summary, 96),
		)
	}

	if start > 0 || end < len(items) {
		_, _ = fmt.Fprintf(output, "\nshowing %d-%d of %d entries\n", start+1, end, len(items))
	}
}

func historySelectorWindow(total, selected, size int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if size <= 0 || size >= total {
		return 0, total
	}

	half := size / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + size
	if end > total {
		end = total
		start = end - size
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func resolveBoundedPromptInput(prompt, promptFile string, maxBytes int, missingPromptMessage, emptyPromptMessage string) (string, error) {
	if prompt != "" && promptFile != "" {
		return "", errors.New(errors.EUsage, "use either --prompt or --prompt-file, not both")
	}
	if prompt != "" {
		if len(prompt) > maxBytes {
			return "", errors.NewWithDetails(
				errors.EPromptTooLarge,
				fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", maxBytes, len(prompt)),
				map[string]string{
					"max_bytes": fmt.Sprintf("%d", maxBytes),
					"got_bytes": fmt.Sprintf("%d", len(prompt)),
				},
			)
		}
		return prompt, nil
	}
	if promptFile == "" {
		return "", errors.New(errors.EPromptRequired, missingPromptMessage)
	}

	f, err := os.Open(promptFile)
	if err != nil {
		return "", errors.WrapWithDetails(
			errors.EPromptRequired,
			"failed to read prompt file",
			err,
			map[string]string{"path": promptFile},
		)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return "", errors.WrapWithDetails(
			errors.EPromptRequired,
			"failed to read prompt file",
			err,
			map[string]string{"path": promptFile},
		)
	}
	if len(data) > maxBytes {
		return "", errors.NewWithDetails(
			errors.EPromptTooLarge,
			fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", maxBytes, len(data)),
			map[string]string{
				"path":      promptFile,
				"max_bytes": fmt.Sprintf("%d", maxBytes),
				"got_bytes": fmt.Sprintf("%d", len(data)),
			},
		)
	}
	if len(data) == 0 {
		return "", errors.New(errors.EPromptRequired, emptyPromptMessage)
	}
	return string(data), nil
}

// AgentHistoryOpts holds options for the agent history command.
type AgentHistoryOpts struct {
	// InvocationRef is the invocation reference (id, name, or prefix).
	InvocationRef string

	// RepoFlag is the --repo flag value.
	RepoFlag string

	// JSON outputs as JSON.
	JSON bool

	// Limit controls page size (must be in [1, 500]).
	Limit int

	// Cursor continues from a prior page.
	Cursor string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentHistory reads the unified invocation timeline via daemon read API.
func AgentHistory(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentHistoryOpts, stdout, stderr io.Writer) error {
	if opts.Limit < 1 || opts.Limit > 500 {
		return errors.NewWithDetails(
			errors.EInvalidArgument,
			fmt.Sprintf("invalid value for parameter 'limit': %d", opts.Limit),
			map[string]string{
				"param": "limit",
				"min":   "1",
				"max":   "500",
			},
		)
	}

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
		CmdName:       "agent history",
	})
	if err != nil {
		return err
	}

	result, err := client.GetInvocationTimeline(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationTimelineOpts{
		Limit:  opts.Limit,
		Cursor: opts.Cursor,
	})
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeAgentHistoryJSONFromDTO(stdout, result.Entries, result.NextCursor)
	}
	return writeAgentHistoryHumanFromDTO(stdout, result.Entries, result.NextCursor)
}

func writeAgentHistoryJSONFromDTO(w io.Writer, entries []daemon.TimelineEntryDTO, nextCursor string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Entries    []daemon.TimelineEntryDTO `json:"entries"`
		NextCursor string                    `json:"next_cursor,omitempty"`
	}{
		Entries:    entries,
		NextCursor: nextCursor,
	})
}

func writeAgentHistoryHumanFromDTO(w io.Writer, entries []daemon.TimelineEntryDTO, nextCursor string) error {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, "No timeline entries found.")
		return nil
	}

	for _, entry := range entries {
		_, _ = fmt.Fprintf(w, "%s  %s  %s\n", entry.Timestamp, entry.Kind, timelineEntrySummary(entry))
	}
	if nextCursor != "" {
		_, _ = fmt.Fprintf(w, "\nnext_cursor: %s\n", nextCursor)
	}
	return nil
}

func timelineEntrySummary(entry daemon.TimelineEntryDTO) string {
	switch entry.Kind {
	case "prompt_seed":
		return truncateTimelineText(timelineString(entry.Data, "text"), 120)
	case "message":
		role := timelineString(entry.Data, "role")
		text := truncateTimelineText(timelineString(entry.Data, "text"), 120)
		if role != "" {
			return role + ": " + text
		}
		return text
	case "tool_use":
		name := timelineString(entry.Data, "name")
		command := timelineString(entry.Data, "command")
		details := strings.TrimSpace(strings.TrimSpace(name + " " + command))
		if details == "" {
			details = "tool activity"
		}
		if exitCode, ok := timelineInt(entry.Data, "exit_code"); ok {
			details += fmt.Sprintf(" (exit=%d)", exitCode)
		}
		return truncateTimelineText(details, 120)
	case "raw_log_coverage":
		if bytes, ok := timelineInt(entry.Data, "bytes"); ok {
			return fmt.Sprintf("%d bytes captured", bytes)
		}
		return "raw log coverage present"
	case "checkpoint_event", "invocation_event":
		if kind := timelineString(entry.Data, "event_kind"); kind != "" {
			return kind
		}
	case "followup_prompt":
		return truncateTimelineText(timelineString(entry.Data, "text"), 120)
	}
	if kind := timelineString(entry.Data, "event_kind"); kind != "" {
		return kind
	}
	return entry.Source
}

func timelineString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func timelineInt(data map[string]interface{}, key string) (int64, bool) {
	if data == nil {
		return 0, false
	}
	v, ok := data[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func truncateTimelineText(value string, max int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
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
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fail(errors.Wrap(errors.EInternal, "failed to get home directory", err))
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return fail(err)
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent kill",
	})
	if err != nil {
		return fail(err)
	}

	// Resolve invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)
	record, err := invSvc.Resolve(repoCtx.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true,
	})
	if err != nil {
		return fail(err)
	}

	if record.Broken {
		return fail(errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		))
	}

	resp, err := client.Kill(ctx, repoCtx.RepoID, record.InvocationID)
	if err != nil {
		return fail(err)
	}

	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = record.InvocationID
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.ClientRequestID = resp.ClientRequestID
			envelope.RequestID = resp.RequestID
		})
	}

	modeStr := "headless"
	if record.Meta.Mode == store.RunnerModeHeaded {
		modeStr = "headed"
	}

	_, _ = fmt.Fprintf(stdout, "Killed %s invocation %s\n", modeStr, record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox preserved at: %s\n", record.Meta.SandboxPath)

	return nil
}
