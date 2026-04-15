package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/tui/historypicker"
)

// AgentStopOpts holds options for the agent stop command.
type AgentStopOpts struct {
	InvocationRef string
	RepoRef       string
	TmuxClient    tmux.Client
	JSON          bool
}

// AgentStop sends a graceful stop signal (Ctrl-C) to a running invocation.
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
	InvocationRef string
	RepoRef       string
	TmuxClient    tmux.Client
	JSON          bool
}

// AgentKill forcefully terminates a running invocation.
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
		return writeAgentMutationJSONError(stdout, err)
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

	_, _ = fmt.Fprintf(stdout, "Successfully landed invocation %s\n", invocationID)
	_, _ = fmt.Fprintf(stdout, "  mode:        %s\n", resp.AppliedMode)
	_, _ = fmt.Fprintf(stdout, "  commits:     %d\n", resp.CommitsLanded)
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
		return writeAgentMutationJSONError(stdout, err)
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

	_, _ = fmt.Fprintf(stdout, "Discarded invocation %s\n", invocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox and checkpoint refs have been removed.\n")

	return nil
}

// AgentChatOpts holds options for the agent chat command.
type AgentChatOpts struct {
	InvocationRef   string
	RepoRef         string
	Prompt          string
	PromptFile      string
	JSON            bool
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
		RepoRef:       opts.RepoRef,
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

// AgentRestartOpts holds options for the agent restart command.
type AgentRestartOpts struct {
	InvocationRef       string
	RepoRef             string
	CheckpointID        int
	InteractiveHistory  bool
	RunnerArgs          []string
	Model               string
	Effort              string
	Env                 map[string]string
	JSON                bool
	DataDirOverride     string
	IsInteractive       func() bool
	HistoryPickerRun    func(turns []historypicker.Turn, opts historypicker.RunOptions) (historypicker.Turn, error)
	HistoryPickerInput  io.Reader
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
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent restart",
	})
	if err != nil {
		return fail(err)
	}
	invocationResult, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return fail(err)
	}
	effectiveRunnerArgs, err := resolveEffectiveRunnerArgs(
		invocationResult.Data.Runner,
		opts.RunnerArgs,
		opts.Model,
		opts.Effort,
		userCfg.Defaults,
	)
	if err != nil {
		return fail(err)
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
