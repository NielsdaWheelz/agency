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
	"github.com/NielsdaWheelz/agency/internal/render"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/tui/historypicker"
)

// AgentStartOpts holds options for the agent start command.
type AgentStartOpts struct {
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
	if strings.TrimSpace(opts.WorktreeRef) == "" {
		return fail(errors.New(errors.EUsage, "--worktree is required"))
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}
	userCfg, _, err := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
	if err != nil {
		return fail(err)
	}

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return fail(errors.New(errors.ENoRepo, "not inside a git repository"))
	}

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

	// For headless mode (PR-05): delegate everything to daemon control plane
	if opts.Headless {
		return fail(agentStartHeadlessControlPlane(ctx, repoRoot.Path, ns.client, opts, runner, stdout, stderr))
	}

	// PR-10: For headed mode: delegate to daemon control plane
	// CLI never creates invocations, sandboxes, or tmux sessions directly
	return fail(agentStartHeadedControlPlane(ctx, cr, repoRoot.Path, ns.client, opts, runner, stdout, stderr))
}

// agentStartHeadedControlPlane handles headed invocation start via daemon control plane (PR-10).
// CLI does NOT create invocation, sandbox, or tmux session - daemon does everything.
func agentStartHeadedControlPlane(ctx context.Context, cr exec.CommandRunner, repoRootPath string, client *daemonclient.Client, opts AgentStartOpts, runner string, stdout, stderr io.Writer) error {
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
			_, _ = fmt.Fprintf(stderr, "Use 'agency agent enter %s' to attach later.\n", shortID)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "\nSession started in detached mode.\n")
		_, _ = fmt.Fprintf(stdout, "Use 'agency agent enter %s' to attach.\n", shortID)
	}

	return nil
}

// agentStartHeadlessControlPlane handles headless invocation start via daemon control plane (PR-05).
// CLI does NOT create invocation or sandbox - daemon does everything.
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
}

// AgentLS lists agent invocations.
// PR-12: Routes through daemon read API - CLI never reads store directly.
// PR-A: Supports --repo / --all-repos for CWD-less operation.
func AgentLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLSOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	// PR-A: Resolve repo context via daemon
	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
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

	result, fetchErr := ns.client.ListInvocations(ctx, daemonclient.ListInvocationsOpts{
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

		detailParts := make([]string, 0, 2)
		if statusSummary := strings.TrimSpace(inv.StatusSummary); statusSummary != "" {
			detailParts = append(detailParts, "summary: "+statusSummary)
		}
		if inv.LatestActivity != nil {
			latestLabel := formatLatestActivityLabel(inv.LatestActivity)
			if latestLabel != "" {
				turnID := strings.TrimSpace(inv.LatestActivity.TurnID)
				if turnID != "" {
					detailParts = append(detailParts, "latest["+turnID+"]: "+latestLabel)
				} else {
					detailParts = append(detailParts, "latest: "+latestLabel)
				}
			}
		}
		if len(detailParts) > 0 {
			_, _ = fmt.Fprintf(w, "    %s\n", strings.Join(detailParts, " | "))
		}
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
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent show",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
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
	if strings.TrimSpace(inv.StatusSummary) != "" {
		_, _ = fmt.Fprintf(w, "status_summary:         %s\n", inv.StatusSummary)
	}
	if inv.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:         %s\n", inv.LandingStatus)
	}
	if inv.SemanticStatus != "" {
		_, _ = fmt.Fprintf(w, "semantic_status:        %s\n", inv.SemanticStatus)
	}
	if inv.LatestActivity != nil {
		if strings.TrimSpace(inv.LatestActivity.TurnID) != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_turn:   %s\n", inv.LatestActivity.TurnID)
		}
		if strings.TrimSpace(inv.LatestActivity.Kind) != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_kind:   %s\n", inv.LatestActivity.Kind)
		}
		if latestLabel := formatLatestActivityLabel(inv.LatestActivity); latestLabel != "" {
			_, _ = fmt.Fprintf(w, "latest_activity:        %s\n", latestLabel)
		}
		for _, toolLine := range latestActivityToolSummaries(inv.LatestActivity) {
			_, _ = fmt.Fprintf(w, "latest_activity_tool:   %s\n", toolLine)
		}
		if inv.LatestActivity.CheckpointID > 0 {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint: %d\n", inv.LatestActivity.CheckpointID)
		}
		if description := strings.TrimSpace(inv.LatestActivity.CheckpointDescription); description != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_description: %s\n", description)
		}
		if diffstat := strings.TrimSpace(inv.LatestActivity.CheckpointDiffstat); diffstat != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_diffstat: %s\n", diffstat)
		}
		if pathsSummary := latestActivityCheckpointPathSummary(inv.LatestActivity); pathsSummary != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_paths: %s\n", pathsSummary)
		}
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
	if inv.Navigation != nil {
		if strings.TrimSpace(inv.Navigation.HistoryCommand) != "" {
			_, _ = fmt.Fprintf(w, "history_command:        %s\n", inv.Navigation.HistoryCommand)
		}
		if strings.TrimSpace(inv.Navigation.DiffCommand) != "" {
			_, _ = fmt.Fprintf(w, "diff_command:           %s\n", inv.Navigation.DiffCommand)
		}
		if strings.TrimSpace(inv.Navigation.LatestTurnID) != "" {
			_, _ = fmt.Fprintf(w, "latest_turn_id:         %s\n", inv.Navigation.LatestTurnID)
		}
	}
	return nil
}

// realTmuxAttach performs a real interactive tmux attach with stdin/stdout/stderr connected.
// This is the only way to get proper interactive terminal behavior.
func realTmuxAttach(sessionName string) error {
	result, err := exec.RunAttached(context.Background(), "tmux", []string{"attach", "-t", sessionName}, exec.AttachedRunOpts{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tmux attach exited with code %d", result.ExitCode)
	}
	return nil
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

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent stop",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.Stop(ctx, repoCtx.RepoID, opts.InvocationRef)
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = invocationID
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}

	_, _ = fmt.Fprintf(stdout, "Stop signal sent to invocation %s\n", invocationID)
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

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent diff",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocationDiff(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationDiffOpts{
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
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent review",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocationReview(ctx, opts.InvocationRef, repoCtx.RepoID)
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
	if review == nil {
		return errors.New(errors.EInternal, "review payload is missing")
	}

	verdict := "BLOCKED"
	if review.Ready || strings.EqualFold(strings.TrimSpace(review.Readiness), "ready") {
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
	if review.StatusSummary != "" {
		_, _ = fmt.Fprintf(w, "status_summary:       %s\n", review.StatusSummary)
	}
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
	if review.LatestActivity != nil {
		if strings.TrimSpace(review.LatestActivity.TurnID) != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_turn: %s\n", review.LatestActivity.TurnID)
		}
		if strings.TrimSpace(review.LatestActivity.Kind) != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_kind: %s\n", review.LatestActivity.Kind)
		}
		if latestLabel := formatLatestActivityLabel(review.LatestActivity); latestLabel != "" {
			_, _ = fmt.Fprintf(w, "latest_activity:      %s\n", latestLabel)
		}
		for _, toolLine := range latestActivityToolSummaries(review.LatestActivity) {
			_, _ = fmt.Fprintf(w, "latest_activity_tool: %s\n", toolLine)
		}
		if review.LatestActivity.CheckpointID > 0 {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint: %d\n", review.LatestActivity.CheckpointID)
		}
		if description := strings.TrimSpace(review.LatestActivity.CheckpointDescription); description != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_description: %s\n", description)
		}
		if diffstat := strings.TrimSpace(review.LatestActivity.CheckpointDiffstat); diffstat != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_diffstat: %s\n", diffstat)
		}
		if pathsSummary := latestActivityCheckpointPathSummary(review.LatestActivity); pathsSummary != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_paths: %s\n", pathsSummary)
		}
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

const maxMergeConfirmationBytes = 64

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

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent land",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.Land(ctx, daemonclient.LandOpts{
		RepoID:       repoCtx.RepoID,
		InvocationID: opts.InvocationRef,
		Apply:        opts.Apply,
		RequireBase:  opts.RequireBase,
	})
	if err != nil {
		return fail(err)
	}

	if !resp.OK {
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = invocationID
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
	_, _ = fmt.Fprintf(stdout, "Successfully landed invocation %s\n", invocationID)
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

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent discard",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.Discard(ctx, repoCtx.RepoID, opts.InvocationRef)
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = invocationID
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
	_, _ = fmt.Fprintf(stdout, "Discarded invocation %s\n", invocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox and checkpoint refs have been removed.\n")

	return nil
}

// ---------------------------------------------------------------------------
// Shared navigation kernel setup for agent path/open/shell/enter (S2-PR04)
// ---------------------------------------------------------------------------

func (ns *daemonNavSetup) buildNavDeps(cr exec.CommandRunner, cwd, repoFlag, cmdName string, isInteractive func() bool) NavigationDeps {
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
			result, err := ns.client.GetInvocation(ctx, ref, repoID)
			if err != nil {
				return nil, err
			}
			return &NavigationResult{
				TargetKind:     TargetInvocation,
				ResolvedRepoID: result.Invocation.RepoID,
				ResolvedID:     result.Invocation.InvocationID,
				ResolvedPath:   result.Invocation.SandboxPath,
			}, nil
		},
		IsInteractive: isInteractive,
	}
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
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent path", nil)
	intent := NavigationIntent{
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

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentOpen opens the sandbox directory in the configured editor.
// S2-PR04: Routes through shared navigation kernel for daemon-first resolution.
// No local invocation target discovery — sandbox_path sourced from daemon.
func AgentOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent open", nil)
	intent := NavigationIntent{
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

	runResult, runErr := runAttachedInDir(ctx, editor, []string{sandboxPath}, sandboxPath)
	if runErr != nil {
		return errors.Wrap(errors.EEditorNotConfigured, "failed to open editor", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
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
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent shell", nil)
	intent := NavigationIntent{
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

	runResult, runErr := runAttachedInDir(ctx, shell, []string{"-l"}, sandboxPath)
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "failed to run shell", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("shell exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
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

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent enter", isInteractive)
	intent := NavigationIntent{
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetInvocation,
			Ref:            opts.InvocationRef,
		},
		RequiresTTY: true,
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	invocationResult, err := ns.client.GetInvocation(ctx, result.ResolvedID, result.ResolvedRepoID)
	if err != nil {
		return err
	}
	if invocationResult.Invocation.Mode != "headed" {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"invocation is headless; enter is only supported for headed invocations",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"mode":          invocationResult.Invocation.Mode,
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

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent chat",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.SubmitFollowUpPrompt(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.SubmitFollowUpPromptOpts{
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

	// Model selects the runner model for restart (supported for claude-code, codex, and cursor runners).
	Model string

	// Effort selects the typed effort level for restart (claude-code: --effort, codex: model_reasoning_effort).
	// Cursor runner does not support effort and expects thinking-capable model IDs via --model.
	Effort string

	// Env are explicit environment overrides for restarted runner execution.
	Env map[string]string

	// JSON outputs as JSON.
	JSON bool

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// IsInteractive reports whether stdin/stderr are interactive terminals.
	// If nil, defaults to terminal checks on os.Stdin/os.Stderr.
	IsInteractive func() bool

	// HistoryPickerRun overrides the interactive history picker.
	// Primarily for tests; nil uses the built-in bubbletea picker.
	HistoryPickerRun func(turns []historypicker.Turn, opts historypicker.RunOptions) (historypicker.Turn, error)

	// HistoryPickerInput is the picker input stream. Defaults to os.Stdin.
	HistoryPickerInput io.Reader

	// HistoryPickerOutput is the picker output stream. Defaults to stderr.
	HistoryPickerOutput io.Writer
}

const maxHistoryPickerEntries = 5000

// AgentRestart performs invocation-scoped restart from an explicit checkpoint
// or an interactively selected timeline point.
func AgentRestart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentRestartOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
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

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}
	userCfg, _, err := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent restart",
	})
	if err != nil {
		return fail(err)
	}
	effectiveRunnerArgs := append([]string(nil), opts.RunnerArgs...)
	needsRunnerOptionResolution := strings.TrimSpace(opts.Model) != "" ||
		strings.TrimSpace(opts.Effort) != "" ||
		strings.TrimSpace(userCfg.Defaults.Model) != "" ||
		strings.TrimSpace(userCfg.Defaults.Effort) != "" ||
		hasTypedOptionRunnerArgs(opts.RunnerArgs)
	if needsRunnerOptionResolution {
		invocationResult, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
		if err != nil {
			return fail(err)
		}
		effectiveRunnerArgs, err = resolveEffectiveRunnerArgs(
			invocationResult.Invocation.Runner,
			opts.RunnerArgs,
			opts.Model,
			opts.Effort,
			userCfg.Defaults,
		)
		if err != nil {
			return fail(err)
		}
	}

	if opts.InteractiveHistory {
		timelineEntries, err := fetchAllTimelineEntries(ctx, ns.client, opts.InvocationRef, repoCtx.RepoID)
		if err != nil {
			return fail(err)
		}
		checkpoints, err := fetchAllCheckpoints(ctx, ns.client, opts.InvocationRef, repoCtx.RepoID)
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

		turns := daemon.ProjectTimelineTurns(timelineEntries, checkpoints)
		if len(turns) == 0 {
			return fail(errors.NewWithDetails(
				errors.ECheckpointNotFound,
				"no displayable conversation turns found",
				map[string]string{
					"hint": "use --checkpoint <id> instead",
				},
			))
		}

		pickerInput := opts.HistoryPickerInput
		if pickerInput == nil {
			pickerInput = os.Stdin
		}
		pickerOutput := opts.HistoryPickerOutput
		if pickerOutput == nil {
			pickerOutput = stderr
		}

		runPicker := opts.HistoryPickerRun
		if runPicker == nil {
			runPicker = historypicker.Run
		}

		selected, err := runPicker(turns, historypicker.RunOptions{
			Input:   pickerInput,
			Output:  pickerOutput,
			NoColor: os.Getenv("NO_COLOR") != "",
		})
		if err != nil {
			return fail(err)
		}

		if !selected.Restorable || selected.CheckpointID <= 0 {
			return fail(errors.NewWithDetails(
				errors.ECheckpointNotFound,
				"selected turn does not have a checkpoint",
				map[string]string{
					"hint": "select a turn that shows a checkpoint badge, or use --checkpoint <id>",
				},
			))
		}
		opts.CheckpointID = selected.CheckpointID
	}

	resp, err := ns.client.RestartFromCheckpoint(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.RestartFromCheckpointOpts{
		CheckpointID: opts.CheckpointID,
		RunnerArgs:   effectiveRunnerArgs,
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
		if len(entries) > maxHistoryPickerEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history picker supports at most %d timeline entries", maxHistoryPickerEntries),
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
		if len(checkpoints) > maxHistoryPickerEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history picker supports at most %d checkpoints", maxHistoryPickerEntries),
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

	// Last requests only the chronologically last timeline entry.
	// Mutually exclusive with Cursor.
	Last bool

	// Limit controls page size (must be in [1, 500]).
	Limit int

	// Cursor continues from a prior page.
	Cursor string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string
}

// AgentHistory reads the unified invocation timeline via daemon read API.
func AgentHistory(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentHistoryOpts, stdout, stderr io.Writer) error {
	if opts.Last && opts.Cursor != "" {
		return errors.New(errors.EInvalidArgument, "--last cannot be used with --cursor")
	}

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

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent history",
	})
	if err != nil {
		return err
	}

	// JSON remains the machine-fidelity escape hatch for raw timeline entries.
	// For default human history and --last resolution we project from shared turns
	// so history aligns with restart --history semantics.
	if opts.JSON && !opts.Last {
		result, err := ns.client.GetInvocationTimeline(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationTimelineOpts{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		})
		if err != nil {
			return err
		}
		return writeAgentHistoryJSONFromDTO(stdout, result.Entries, result.NextCursor)
	}

	entries, err := fetchAllTimelineEntries(ctx, ns.client, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}
	checkpoints, err := fetchAllCheckpoints(ctx, ns.client, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return err
	}
	turns := daemon.ProjectTimelineTurns(entries, checkpoints)

	if opts.Last {
		if opts.JSON {
			if len(turns) == 0 {
				return writeAgentHistoryJSONFromDTO(stdout, []daemon.TimelineEntryDTO{}, "")
			}
			return writeAgentHistoryJSONFromDTO(stdout, daemon.TimelineEntriesForTurn(entries, turns, turns[len(turns)-1].EntryID), "")
		}
		if len(turns) == 0 {
			return writeAgentHistoryHumanFromTurns(stdout, nil, "")
		}
		return writeAgentHistoryHumanFromTurns(stdout, []historypicker.Turn{turns[len(turns)-1]}, "")
	}

	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" && !daemon.HistoryTurnExists(turns, cursor) {
		return errors.NewWithDetails(
			errors.EInvalidArgument,
			"invalid value for parameter 'cursor': turn id not found",
			map[string]string{
				"param":  "cursor",
				"cursor": cursor,
			},
		)
	}

	page, nextCursor := daemon.PaginateHistoryTurns(turns, opts.Cursor, opts.Limit)
	return writeAgentHistoryHumanFromTurns(stdout, page, nextCursor)
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

func latestActivityToolCount(activity *daemon.InvocationLatestActivity) int {
	if activity == nil {
		return 0
	}
	if activity.ToolCallCount > 0 {
		return activity.ToolCallCount
	}
	return len(activity.ToolCalls)
}

func formatLatestActivityLabel(activity *daemon.InvocationLatestActivity) string {
	if activity == nil {
		return ""
	}
	kind := strings.TrimSpace(activity.Kind)
	summary := strings.TrimSpace(activity.Summary)
	toolCount := latestActivityToolCount(activity)
	if kind == "" && summary == "" && toolCount == 0 && activity.CheckpointID <= 0 {
		return ""
	}
	return render.FormatActivityWithExtras(
		kind,
		summary,
		toolCount,
		activity.CheckpointID,
		activity.Restorable,
	)
}

func latestActivityToolSummaries(activity *daemon.InvocationLatestActivity) []string {
	if activity == nil || len(activity.ToolCalls) == 0 {
		return nil
	}
	summaries := make([]string, 0, len(activity.ToolCalls))
	for _, tool := range activity.ToolCalls {
		summaries = append(summaries, render.FormatToolCallSummary(
			tool.Name,
			tool.Command,
			tool.HasExit,
			tool.ExitCode,
		))
	}
	return summaries
}

func latestActivityCheckpointPathSummary(activity *daemon.InvocationLatestActivity) string {
	if activity == nil {
		return ""
	}
	return render.FormatChangedPathSummary(
		activity.CheckpointChangedPaths,
		activity.CheckpointChangedCount,
		activity.CheckpointPathsTrimmed,
	)
}

func writeAgentHistoryHumanFromTurns(w io.Writer, turns []historypicker.Turn, nextCursor string) error {
	if len(turns) == 0 {
		_, _ = fmt.Fprintln(w, "No timeline entries found.")
		return nil
	}
	for _, turn := range turns {
		timestamp := strings.TrimSpace(turn.ShortTimestamp)
		if timestamp == "" {
			timestamp = strings.TrimSpace(turn.Timestamp)
		}
		if timestamp == "" {
			timestamp = "-"
		}

		summary := truncateTimelineText(turn.Summary, 160)
		activity := render.FormatActivityWithExtras(string(turn.Kind), summary, len(turn.ToolCalls), turn.CheckpointID, turn.Restorable)

		_, _ = fmt.Fprintf(w, "%s  %s  %s\n", timestamp, turn.EntryID, activity)
	}

	if nextCursor != "" {
		_, _ = fmt.Fprintf(w, "\nnext_cursor: %s\n", nextCursor)
	}
	return nil
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
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
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
		result, err := ns.client.GetInvocationLogsOffset(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationLogsOffsetOpts{
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

		result, err := ns.client.GetInvocationLogsOffset(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationLogsOffsetOpts{
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

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent kill",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.Kill(ctx, repoCtx.RepoID, opts.InvocationRef)
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.InvocationID = invocationID
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}

	_, _ = fmt.Fprintf(stdout, "Killed invocation %s\n", invocationID)

	return nil
}
