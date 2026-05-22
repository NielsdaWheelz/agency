package commands

import (
	"context"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// WorktreeTargetOpts holds options for target-first worktree commands.
type WorktreeTargetOpts struct {
	Args    []string
	RepoRef string

	JSON             bool
	Editor           string
	Force            bool
	Yes              bool
	AllowDirty       bool
	ForceWithLease   bool
	Strategy         string
	NoDeleteBranch   bool
	AgencyConfigPath string
}

const (
	// WorktreeTargetActionPath prints one worktree path.
	WorktreeTargetActionPath = "path"

	// WorktreeTargetActionOpen opens one worktree in an editor.
	WorktreeTargetActionOpen = "open"

	// WorktreeTargetActionShell opens a shell in one worktree.
	WorktreeTargetActionShell = "shell"

	// WorktreeTargetActionRm removes one worktree.
	WorktreeTargetActionRm = "rm"

	// WorktreeTargetActionRebase rebases one worktree.
	WorktreeTargetActionRebase = "rebase"

	// WorktreeTargetActionPR scopes pull request actions for one worktree.
	WorktreeTargetActionPR = "pr"

	// WorktreeTargetPRActionSync pushes and syncs one worktree pull request.
	WorktreeTargetPRActionSync = "sync"

	// WorktreeTargetPRActionMerge verifies, merges, and archives one worktree pull request.
	WorktreeTargetPRActionMerge = "merge"
)

// WorktreeTargetFlagPolicy returns the target-level flag policy for `agency worktree` args.
func WorktreeTargetFlagPolicy(args []string) (targetFlagPolicy, bool) {
	switch {
	case len(args) == 0:
		return targetFlagPolicy{}, false
	case len(args) == 1:
		return newTargetFlagPolicy("<worktree-ref>", "json"), true
	case len(args) == 2:
		switch args[1] {
		case WorktreeTargetActionPath:
			return newTargetFlagPolicy(WorktreeTargetActionPath), true
		case WorktreeTargetActionShell:
			return newTargetFlagPolicy(WorktreeTargetActionShell), true
		case WorktreeTargetActionOpen:
			return newTargetFlagPolicy(WorktreeTargetActionOpen, "editor"), true
		case WorktreeTargetActionRm:
			return newTargetFlagPolicy(WorktreeTargetActionRm, "force", "yes"), true
		case WorktreeTargetActionRebase:
			return newTargetFlagPolicy(WorktreeTargetActionRebase, "json"), true
		}
	case len(args) == 3 && args[1] == WorktreeTargetActionPR:
		switch args[2] {
		case WorktreeTargetPRActionSync:
			return newTargetFlagPolicy(WorktreeTargetActionPR+" "+WorktreeTargetPRActionSync, "json", "allow-dirty", "force-with-lease"), true
		case WorktreeTargetPRActionMerge:
			return newTargetFlagPolicy(WorktreeTargetActionPR+" "+WorktreeTargetPRActionMerge, "json", "squash", "merge", "rebase", "no-delete-branch", "yes", "agency-config"), true
		}
	}
	return targetFlagPolicy{}, false
}

// WorktreeTarget dispatches target-first worktree commands owned by internal/commands.
func WorktreeTarget(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeTargetOpts, stdout, stderr io.Writer) error {
	args := opts.Args
	if len(args) == 0 {
		return errors.New(errors.EUsage, "specify a worktree ref")
	}

	worktreeRef := args[0]
	switch {
	case len(args) == 1:
		return WorktreeShow(ctx, cr, fsys, cwd, WorktreeShowOpts{
			WorktreeRef: worktreeRef,
			RepoRef:     opts.RepoRef,
			JSON:        opts.JSON,
		}, stdout, stderr)
	case len(args) == 2:
		switch args[1] {
		case WorktreeTargetActionPath:
			return WorktreePath(ctx, cr, fsys, cwd, WorktreePathOpts{
				WorktreeRef: worktreeRef,
				RepoRef:     opts.RepoRef,
			}, stdout, stderr)
		case WorktreeTargetActionOpen:
			return WorktreeOpen(ctx, cr, fsys, cwd, WorktreeOpenOpts{
				WorktreeRef: worktreeRef,
				RepoRef:     opts.RepoRef,
				Editor:      opts.Editor,
			}, stdout, stderr)
		case WorktreeTargetActionShell:
			return WorktreeShell(ctx, cr, fsys, cwd, WorktreeShellOpts{
				WorktreeRef: worktreeRef,
				RepoRef:     opts.RepoRef,
			}, stdout, stderr)
		case WorktreeTargetActionRm:
			return WorktreeRm(ctx, cr, fsys, cwd, WorktreeRmOpts{
				WorktreeRef: worktreeRef,
				RepoRef:     opts.RepoRef,
				Force:       opts.Force,
				Yes:         opts.Yes,
				Interactive: isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()),
			}, stdout, stderr)
		case WorktreeTargetActionRebase:
			return WorktreeRebase(ctx, cr, fsys, cwd, WorktreeRebaseOpts{
				WorktreeRef: worktreeRef,
				RepoRef:     opts.RepoRef,
				JSON:        opts.JSON,
			}, stdout, stderr)
		case WorktreeTargetActionPR:
			return errors.New(errors.EUsage, "use 'agency worktree <worktree-ref> pr sync' or 'agency worktree <worktree-ref> pr merge'")
		default:
			return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency worktree\"")
		}
	case len(args) == 3 && args[1] == WorktreeTargetActionPR:
		switch args[2] {
		case WorktreeTargetPRActionSync:
			return WorktreePRSync(ctx, cr, fsys, cwd, WorktreePRSyncOpts{
				WorktreeRef:    worktreeRef,
				RepoRef:        opts.RepoRef,
				AllowDirty:     opts.AllowDirty,
				ForceWithLease: opts.ForceWithLease,
				JSON:           opts.JSON,
			}, stdout, stderr)
		case WorktreeTargetPRActionMerge:
			return WorktreePRMerge(ctx, cr, fsys, cwd, WorktreePRMergeOpts{
				WorktreeRef:      worktreeRef,
				RepoRef:          opts.RepoRef,
				Strategy:         opts.Strategy,
				NoDeleteBranch:   opts.NoDeleteBranch,
				Yes:              opts.Yes,
				JSON:             opts.JSON,
				AgencyConfigPath: opts.AgencyConfigPath,
				Interactive:      isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()),
			}, stdout, stderr)
		default:
			return errors.New(errors.EUsage, "unknown command \""+args[2]+"\" for \"agency worktree "+worktreeRef+" pr\"")
		}
	default:
		return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency worktree\"")
	}
}
