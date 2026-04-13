// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

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

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	NoIncludeUntracked bool

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStart starts a new agent invocation.
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

	if opts.Headless {
		return fail(agentStartHeadlessControlPlane(ctx, repoRoot.Path, ns.client, opts, runner, stdout, stderr))
	}

	return fail(agentStartHeadedControlPlane(ctx, cr, repoRoot.Path, ns.client, opts, runner, stdout, stderr))
}

// agentStartHeadedControlPlane handles headed invocation start via daemon control plane.
func agentStartHeadedControlPlane(ctx context.Context, cr exec.CommandRunner, repoRootPath string, client *daemonclient.Client, opts AgentStartOpts, runner string, stdout, stderr io.Writer) error {
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

// writeAgentLSJSONFromDTO outputs invocation list as JSON from daemon DTOs.
func writeAgentLSJSONFromDTO(w io.Writer, invocations []daemon.InvocationDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(invocations)
}

// writeAgentLSHumanFromDTO outputs invocation list in human-readable format from daemon DTOs.
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

// writeAgentShowJSONFromDTO outputs invocation details as JSON from daemon DTO.
func writeAgentShowJSONFromDTO(w io.Writer, inv *daemon.InvocationDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}

// writeAgentShowHumanFromDTO outputs invocation details in human-readable format from daemon DTO.
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

		entries = append(entries, result.Data.Entries...)
		if len(entries) > maxHistoryPickerEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history picker supports at most %d timeline entries", maxHistoryPickerEntries),
				map[string]string{
					"hint": "narrow invocation scope or use explicit --checkpoint <id>",
				},
			)
		}

		if result.Data.NextCursor == "" {
			return entries, nil
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "timeline pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
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

		checkpoints = append(checkpoints, result.Data.Checkpoints...)
		if len(checkpoints) > maxHistoryPickerEntries {
			return nil, errors.NewWithDetails(
				errors.EInvalidArgument,
				fmt.Sprintf("interactive history picker supports at most %d checkpoints", maxHistoryPickerEntries),
				map[string]string{
					"hint": "use explicit --checkpoint <id> for very large histories",
				},
			)
		}

		if result.Data.NextCursor == "" {
			return checkpoints, nil
		}
		if result.Data.NextCursor == cursor {
			return nil, errors.New(errors.EInternal, "checkpoint pagination cursor did not advance")
		}
		cursor = result.Data.NextCursor
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

// base64Decode decodes a base64-encoded string.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
