package commands

import (
	"context"
	"io"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// AgentTargetOpts holds options for target-first agent commands.
type AgentTargetOpts struct {
	Args    []string
	RepoRef string

	JSON         bool
	Prompt       string
	PromptFile   string
	Detached     bool
	TurnID       string
	TurnRange    string
	Last         bool
	Limit        int
	Cursor       string
	Kind         string
	Follow       bool
	Offset       int64
	CheckpointID int
	Apply        bool
	RequireBase  bool
}

const (
	// AgentTargetActionCheck reports one invocation's readiness state.
	AgentTargetActionCheck = "check"

	// AgentTargetActionDiff shows one invocation's sandbox diff.
	AgentTargetActionDiff = "diff"

	// AgentTargetActionHistory shows one invocation's timeline.
	AgentTargetActionHistory = "history"

	// AgentTargetActionOpen opens one invocation sandbox in an editor.
	AgentTargetActionOpen = "open"

	// AgentTargetActionPath prints one invocation sandbox path.
	AgentTargetActionPath = "path"

	// AgentTargetActionShell opens a shell in one invocation sandbox.
	AgentTargetActionShell = "shell"

	// AgentTargetActionAttach attaches to one headed invocation session.
	AgentTargetActionAttach = "attach"

	// AgentTargetActionClients lists headed tmux clients for one invocation.
	AgentTargetActionClients = "clients"

	// AgentTargetActionStop stops one invocation.
	AgentTargetActionStop = "stop"

	// AgentTargetActionKill kills one invocation.
	AgentTargetActionKill = "kill"

	// AgentTargetActionLand lands one invocation back to its integration worktree.
	AgentTargetActionLand = "land"

	// AgentTargetActionDiscard discards one invocation sandbox.
	AgentTargetActionDiscard = "discard"

	// AgentTargetActionFollowup submits a follow-up prompt to one invocation.
	AgentTargetActionFollowup = "followup"

	// AgentTargetActionRecreate recreates one invocation's headed session.
	AgentTargetActionRecreate = "recreate"

	// AgentTargetActionRestore restores one invocation to a checkpoint.
	AgentTargetActionRestore = "restore"

	// AgentTargetHistoryActionLogs shows raw logs for one invocation.
	AgentTargetHistoryActionLogs = "logs"
)

// AgentTargetFlagPolicy returns the target-level flag policy for `agency agent` args.
func AgentTargetFlagPolicy(args []string) (targetFlagPolicy, bool) {
	switch {
	case len(args) == 0:
		return targetFlagPolicy{}, false
	case len(args) == 1:
		return newTargetFlagPolicy("<invocation-ref>", "json"), true
	case len(args) == 2:
		switch args[1] {
		case AgentTargetActionCheck:
			return newTargetFlagPolicy(AgentTargetActionCheck), true
		case AgentTargetActionDiff:
			return newTargetFlagPolicy(AgentTargetActionDiff, "json", "turn", "turn-range"), true
		case AgentTargetActionHistory:
			return newTargetFlagPolicy(AgentTargetActionHistory, "json", "last", "limit", "cursor"), true
		case AgentTargetActionOpen:
			return newTargetFlagPolicy(AgentTargetActionOpen), true
		case AgentTargetActionPath:
			return newTargetFlagPolicy(AgentTargetActionPath), true
		case AgentTargetActionShell:
			return newTargetFlagPolicy(AgentTargetActionShell), true
		case AgentTargetActionAttach:
			return newTargetFlagPolicy(AgentTargetActionAttach), true
		case AgentTargetActionClients:
			return newTargetFlagPolicy(AgentTargetActionClients), true
		case AgentTargetActionStop:
			return newTargetFlagPolicy(AgentTargetActionStop, "json"), true
		case AgentTargetActionKill:
			return newTargetFlagPolicy(AgentTargetActionKill, "json"), true
		case AgentTargetActionLand:
			return newTargetFlagPolicy(AgentTargetActionLand, "json", "apply", "require-base"), true
		case AgentTargetActionDiscard:
			return newTargetFlagPolicy(AgentTargetActionDiscard, "json"), true
		case AgentTargetActionFollowup:
			return newTargetFlagPolicy(AgentTargetActionFollowup, "json", "prompt", "prompt-file"), true
		case AgentTargetActionRecreate:
			return newTargetFlagPolicy(AgentTargetActionRecreate, "json", "detached"), true
		case AgentTargetActionRestore:
			return newTargetFlagPolicy(AgentTargetActionRestore, "json", "checkpoint", "turn"), true
		}
	case len(args) == 3 && args[1] == AgentTargetActionHistory:
		if args[2] == AgentTargetHistoryActionLogs {
			return newTargetFlagPolicy(AgentTargetActionHistory+" "+AgentTargetHistoryActionLogs, "kind", "follow", "offset"), true
		}
	}
	return targetFlagPolicy{}, false
}

// AgentTarget dispatches target-first agent commands owned by internal/commands.
func AgentTarget(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentTargetOpts, stdout, stderr io.Writer) error {
	args := opts.Args
	if len(args) == 0 {
		return errors.New(errors.EUsage, "specify an invocation ref")
	}

	invocationRef := args[0]
	switch {
	case len(args) == 1:
		return AgentShow(ctx, cr, fsys, cwd, AgentShowOpts{
			InvocationRef: invocationRef,
			RepoRef:       opts.RepoRef,
			JSON:          opts.JSON,
		}, stdout, stderr)
	case len(args) == 2:
		switch args[1] {
		case AgentTargetActionCheck:
			return AgentCheck(ctx, cr, fsys, cwd, AgentCheckOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
			}, stdout, stderr)
		case AgentTargetActionDiff:
			return AgentDiff(ctx, cr, fsys, cwd, AgentDiffOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				JSON:          opts.JSON,
				TurnID:        opts.TurnID,
				TurnRange:     opts.TurnRange,
			}, stdout, stderr)
		case AgentTargetActionHistory:
			return AgentHistory(ctx, cr, fsys, cwd, AgentHistoryOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				JSON:          opts.JSON,
				Last:          opts.Last,
				Limit:         opts.Limit,
				Cursor:        opts.Cursor,
			}, stdout, stderr)
		case AgentTargetActionOpen:
			return AgentOpen(ctx, cr, fsys, cwd, AgentOpenOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
			}, stdout, stderr)
		case AgentTargetActionPath:
			return AgentPath(ctx, cr, fsys, cwd, AgentPathOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
			}, stdout, stderr)
		case AgentTargetActionShell:
			return AgentShell(ctx, cr, fsys, cwd, AgentShellOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
			}, stdout, stderr)
		case AgentTargetActionAttach:
			return AgentAttach(ctx, cr, fsys, cwd, AgentAttachOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
			}, stdout, stderr)
		case AgentTargetActionClients:
			return AgentClients(ctx, cr, fsys, cwd, AgentClientsOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
			}, stdout, stderr)
		case AgentTargetActionStop:
			return AgentStop(ctx, cr, fsys, cwd, AgentStopOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				JSON:          opts.JSON,
			}, stdout, stderr)
		case AgentTargetActionKill:
			return AgentKill(ctx, cr, fsys, cwd, AgentKillOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				JSON:          opts.JSON,
			}, stdout, stderr)
		case AgentTargetActionLand:
			return AgentLand(ctx, cr, fsys, cwd, AgentLandOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				Apply:         opts.Apply,
				RequireBase:   opts.RequireBase,
				JSON:          opts.JSON,
			}, stdout, stderr)
		case AgentTargetActionDiscard:
			return AgentDiscard(ctx, cr, fsys, cwd, AgentDiscardOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				JSON:          opts.JSON,
			}, stdout, stderr)
		case AgentTargetActionFollowup:
			return AgentFollowup(ctx, cr, fsys, cwd, AgentFollowupOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				Prompt:        opts.Prompt,
				PromptFile:    opts.PromptFile,
				JSON:          opts.JSON,
			}, stdout, stderr)
		case AgentTargetActionRecreate:
			return AgentRecreate(ctx, cr, fsys, cwd, AgentRecreateOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				Detached:      opts.Detached,
				JSON:          opts.JSON,
			}, stdout, stderr)
		case AgentTargetActionRestore:
			return AgentRestore(ctx, cr, fsys, cwd, AgentRestoreOpts{
				InvocationRef: invocationRef,
				RepoRef:       opts.RepoRef,
				CheckpointID:  opts.CheckpointID,
				TurnID:        opts.TurnID,
				JSON:          opts.JSON,
			}, stdout, stderr)
		default:
			return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency agent\"")
		}
	case len(args) == 3 && args[1] == AgentTargetActionHistory:
		if args[2] != AgentTargetHistoryActionLogs {
			return errors.New(errors.EUsage, "unknown command \""+args[2]+"\" for \"agency agent "+invocationRef+" history\"")
		}
		return AgentHistoryLogs(ctx, cr, fsys, cwd, AgentHistoryLogsOpts{
			InvocationRef: invocationRef,
			RepoRef:       opts.RepoRef,
			Kind:          opts.Kind,
			Follow:        opts.Follow,
			Offset:        opts.Offset,
		}, stdout, stderr)
	default:
		return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency agent\"")
	}
}
