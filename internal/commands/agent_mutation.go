package commands

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// AgentStopOpts holds options for the agent stop command.
type AgentStopOpts struct {
	InvocationRef string
	RepoRef       string
	JSON          bool
}

// AgentStop sends a graceful stop signal (Ctrl-C) to a running invocation.
func AgentStop(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStopOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID string `json:"invocation_id,omitempty"`
		}{
			commandJSONBase: newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, "", resp.RequestID),
			InvocationID:    invocationID,
		})
	}

	_, _ = fmt.Fprintf(stdout, "Stop signal sent to invocation %s\n", invocationID)
	_, _ = fmt.Fprintf(stdout, "Note: The runner may ignore the interrupt. Use 'agency agent <invocation-ref> kill' to force termination.\n")

	return nil
}

// AgentKillOpts holds options for the agent kill command.
type AgentKillOpts struct {
	InvocationRef string
	RepoRef       string
	JSON          bool
}

// AgentKill forcefully terminates a running invocation.
func AgentKill(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentKillOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID string `json:"invocation_id,omitempty"`
		}{
			commandJSONBase: newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, "", resp.RequestID),
			InvocationID:    invocationID,
		})
	}

	_, _ = fmt.Fprintf(stdout, "Killed invocation %s\n", invocationID)

	return nil
}

// AgentLandOpts holds options for the agent land command.
type AgentLandOpts struct {
	InvocationRef string
	RepoRef       string
	Apply         bool
	RequireBase   bool
	JSON          bool
}

// AgentLand lands sandbox changes to the integration worktree via daemon.
func AgentLand(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLandOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent land",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.Land(ctx, opts.InvocationRef, repoCtx.RepoID, daemon.LandRequest{
		Apply:       opts.Apply,
		RequireBase: opts.RequireBase,
	})
	if err != nil {
		if !opts.JSON {
			var landErr *daemon.LandResponse
			var dae *daemonclient.DaemonActionError
			if stderrors.As(err, &dae) {
				landErr = &daemon.LandResponse{}
				if decodeErr := dae.DecodeResponse(landErr); decodeErr != nil {
					landErr = nil
				}
			}
			if landErr != nil && len(landErr.ConflictFiles) > 0 && errors.GetCode(err) == errors.ELandConflict {
				_, _ = fmt.Fprintf(stderr, "Conflicting files:\n")
				for _, f := range landErr.ConflictFiles {
					_, _ = fmt.Fprintf(stderr, "  - %s\n", f)
				}
			}
		}
		return fail(err)
	}

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID          string             `json:"invocation_id,omitempty"`
			AppliedMode           daemon.LandingMode `json:"applied_mode,omitempty"`
			IntegrationHeadBefore string             `json:"integration_head_before,omitempty"`
			IntegrationHeadAfter  string             `json:"integration_head_after,omitempty"`
			CommitsLanded         int                `json:"commits_landed,omitempty"`
		}{
			commandJSONBase:       newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, "", resp.RequestID),
			InvocationID:          invocationID,
			AppliedMode:           resp.AppliedMode,
			IntegrationHeadBefore: resp.IntegrationHeadBefore,
			IntegrationHeadAfter:  resp.IntegrationHeadAfter,
			CommitsLanded:         resp.CommitsLanded,
		})
	}

	if resp.AppliedMode == daemon.LandingModeCleanup {
		_, _ = fmt.Fprintf(stdout, "Successfully completed landing cleanup for invocation %s\n", invocationID)
	} else {
		_, _ = fmt.Fprintf(stdout, "Successfully landed invocation %s\n", invocationID)
	}
	_, _ = fmt.Fprintf(stdout, "  mode:        %s\n", resp.AppliedMode)
	_, _ = fmt.Fprintf(stdout, "  commits:     %d\n", resp.CommitsLanded)
	if resp.AppliedMode == daemon.LandingModeCleanup || len(resp.IntegrationHeadBefore) < 12 || len(resp.IntegrationHeadAfter) < 12 {
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "  head_before: %s\n", resp.IntegrationHeadBefore[:12])
	_, _ = fmt.Fprintf(stdout, "  head_after:  %s\n", resp.IntegrationHeadAfter[:12])

	return nil
}

// AgentDiscardOpts holds options for the agent discard command.
type AgentDiscardOpts struct {
	InvocationRef string
	RepoRef       string
	JSON          bool
}

// AgentDiscard discards a sandbox without landing via daemon.
func AgentDiscard(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiscardOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
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

	invocationID := resp.InvocationID
	if invocationID == "" {
		invocationID = opts.InvocationRef
	}
	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID string `json:"invocation_id,omitempty"`
		}{
			commandJSONBase: newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, "", resp.RequestID),
			InvocationID:    invocationID,
		})
	}

	_, _ = fmt.Fprintf(stdout, "Discarded invocation %s\n", invocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox and checkpoint refs have been removed.\n")

	return nil
}

// AgentFollowupOpts holds options for the agent followup command.
type AgentFollowupOpts struct {
	InvocationRef   string
	RepoRef         string
	Prompt          string
	PromptFile      string
	JSON            bool
	DataDirOverride string
}

// AgentFollowup submits a follow-up prompt to an existing headless invocation.
func AgentFollowup(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentFollowupOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
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
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent followup",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.SubmitFollowUp(ctx, opts.InvocationRef, repoCtx.RepoID, daemon.ControlPlaneFollowUpRequest{
		Prompt: prompt,
	})
	if err != nil {
		return fail(err)
	}

	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID   string `json:"invocation_id,omitempty"`
			TimelineEntry  string `json:"timeline_entry_id,omitempty"`
			AlreadyApplied bool   `json:"already_applied,omitempty"`
			DeliveryMode   string `json:"delivery_mode,omitempty"`
		}{
			commandJSONBase: newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, resp.ClientRequestID, resp.RequestID),
			InvocationID:    resp.InvocationID,
			TimelineEntry:   resp.TimelineEntry,
			AlreadyApplied:  resp.AlreadyApplied,
			DeliveryMode:    resp.DeliveryMode,
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

// AgentRecreateOpts holds options for the agent recreate command.
type AgentRecreateOpts struct {
	InvocationRef   string
	RepoRef         string
	Detached        bool
	JSON            bool
	DataDirOverride string
	IsInteractive   func() bool
	TmuxAttachFn    func(context.Context, string) error
}

// AgentRecreate starts a new tmux session for an existing headed invocation.
func AgentRecreate(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentRecreateOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}
	if !opts.JSON && !opts.Detached {
		isInteractive := opts.IsInteractive
		if isInteractive == nil {
			isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) }
		}
		if !isInteractive() {
			return fail(errors.NewWithDetails(
				errors.ENotInteractive,
				"headed recreate requires an interactive terminal",
				map[string]string{
					"hint": "re-run in an interactive terminal or pass --detached",
				},
			))
		}
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent recreate",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.RecreateHeaded(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return fail(err)
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
			CustomEnvKeys:    slices.Clone(resp.CustomEnvKeys),
			TmuxSession:      resp.TmuxSession,
			DaemonInstanceID: resp.DaemonInstanceID,
			AlreadyRunning:   resp.AlreadyRunning,
			LogPaths:         resp.LogPaths,
		})
	}

	_, _ = fmt.Fprintln(stdout, "recreated headed agent invocation")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:  %s\n", resp.InvocationID)
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
		_, _ = fmt.Fprintln(stdout, "\nNote: tmux session already exists; no new session was created.")
	}

	if !opts.Detached {
		attachHeadedSession(ctx, headedAttachOpts{
			AttachFn:    opts.TmuxAttachFn,
			Stdout:      stdout,
			Stderr:      stderr,
			SessionName: resp.TmuxSession,
			Invocation:  resp.InvocationID,
			RepoID:      resp.RepoID,
			Banner:      true,
			LaterHint:   true,
		})
		return nil
	}

	_, _ = fmt.Fprintln(stdout, "\nSession recreated in detached mode.")
	_, _ = fmt.Fprintf(stdout, "Use 'agency agent %s attach --repo %s' to attach.\n", resp.InvocationID, resp.RepoID)
	return nil
}

// AgentRestoreOpts holds options for the agent restore command.
type AgentRestoreOpts struct {
	InvocationRef   string
	RepoRef         string
	CheckpointID    int
	TurnID          string
	JSON            bool
	DataDirOverride string
}

// AgentRestore restores an invocation sandbox to a checkpoint selected either
// explicitly or by history turn id.
func AgentRestore(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentRestoreOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}

	if opts.CheckpointID < 0 {
		return fail(errors.New(errors.EUsage, "--checkpoint must be a positive integer"))
	}
	if opts.TurnID != "" && opts.CheckpointID > 0 {
		return fail(errors.New(errors.EUsage, "use either --checkpoint or --turn, not both"))
	}
	if opts.TurnID == "" && opts.CheckpointID <= 0 {
		return fail(errors.New(errors.EUsage, "pass either --checkpoint <id> or --turn <entry_id>"))
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent restore",
	})
	if err != nil {
		return fail(err)
	}
	invocationResult, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return fail(err)
	}
	if opts.TurnID != "" {
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
				"history turn restore requires timeline entries",
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

		selectedIndex := -1
		for i := range turns {
			if turns[i].EntryID == opts.TurnID {
				selectedIndex = i
				break
			}
		}
		if selectedIndex < 0 {
			return fail(errors.NewWithDetails(
				errors.EInvalidArgument,
				"invalid value for parameter 'turn': turn id not found",
				map[string]string{
					"param": "turn",
					"turn":  opts.TurnID,
				},
			))
		}

		selected := turns[selectedIndex]
		if !selected.Restorable || selected.CheckpointID <= 0 {
			return fail(errors.NewWithDetails(
				errors.ECheckpointNotFound,
				"selected turn does not have a checkpoint",
				map[string]string{
					"hint": "choose a turn that shows checkpoint=<id>, or use --checkpoint <id>",
				},
			))
		}
		opts.CheckpointID = selected.CheckpointID
	}

	resp, err := ns.client.CheckpointApply(ctx, repoCtx.RepoID, invocationResult.Data.InvocationID, opts.CheckpointID)
	if err != nil {
		return fail(errors.Wrap(errors.EInternal, "checkpoint restore request failed", err))
	}

	if opts.JSON {
		return writeCommandJSON(stdout, struct {
			commandJSONBase
			InvocationID   string `json:"invocation_id,omitempty"`
			CheckpointID   int    `json:"checkpoint_id,omitempty"`
			SnapshotCommit string `json:"snapshot_commit,omitempty"`
			RestoredAt     string `json:"restored_at,omitempty"`
		}{
			commandJSONBase: newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, "", resp.RequestID),
			InvocationID:    invocationResult.Data.InvocationID,
			CheckpointID:    resp.CheckpointID,
			SnapshotCommit:  resp.SnapshotCommit,
			RestoredAt:      resp.RestoredAt,
		})
	}

	_, _ = fmt.Fprintln(stdout, "restored invocation to checkpoint")
	_, _ = fmt.Fprintf(stdout, "  invocation_id:    %s\n", invocationResult.Data.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  checkpoint_id:    %d\n", resp.CheckpointID)
	_, _ = fmt.Fprintf(stdout, "  snapshot_commit:  %s\n", resp.SnapshotCommit)
	_, _ = fmt.Fprintf(stdout, "  restored_at:      %s\n", resp.RestoredAt)
	return nil
}
