package commands

import (
	"context"
	"io"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// RepoTargetOpts holds options for repo commands parsed from the repo argument surface.
type RepoTargetOpts struct {
	Args []string
	JSON bool
	Yes  bool
}

const (
	// RepoActionAdd registers a repository.
	RepoActionAdd = "add"

	// RepoActionLS lists registered repositories.
	RepoActionLS = "ls"

	// RepoTargetActionRm removes one registered repository.
	RepoTargetActionRm = "rm"
)

// RepoTargetFlagPolicy returns the target-level flag policy for `agency repo` args.
func RepoTargetFlagPolicy(args []string) (targetFlagPolicy, bool) {
	switch {
	case len(args) == 0:
		return targetFlagPolicy{}, false
	case args[0] == RepoActionAdd:
		return newTargetFlagPolicy(RepoActionAdd, "json"), true
	case args[0] == RepoActionLS:
		return newTargetFlagPolicy(RepoActionLS, "json"), true
	case len(args) == 1:
		return newTargetFlagPolicy("<repo-ref>", "json"), true
	case len(args) == 2 && args[1] == RepoTargetActionRm:
		return newTargetFlagPolicy(RepoTargetActionRm, "json", "yes"), true
	}
	return targetFlagPolicy{}, false
}

// RepoTarget dispatches repo commands owned by internal/commands.
func RepoTarget(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts RepoTargetOpts, stdout, stderr io.Writer) error {
	args := opts.Args
	if len(args) == 0 {
		return errors.New(errors.EUsage, "specify 'add', 'ls', or a repo ref")
	}

	switch args[0] {
	case RepoActionAdd:
		if len(args) > 2 {
			return errors.New(errors.EUsage, "too many arguments for \"agency repo add\"")
		}
		path := ""
		if len(args) == 2 {
			path = args[1]
		}
		return RepoAdd(ctx, cr, fsys, RepoAddOpts{
			Path: path,
			JSON: opts.JSON,
		}, stdout, stderr)
	case RepoActionLS:
		if len(args) > 1 {
			return errors.New(errors.EUsage, "too many arguments for \"agency repo ls\"")
		}
		return RepoLS(ctx, cr, fsys, RepoLSOpts{
			JSON: opts.JSON,
		}, stdout, stderr)
	default:
		repoRef := args[0]
		if len(args) == 1 {
			return RepoShow(ctx, cr, fsys, RepoShowOpts{
				RepoRef: repoRef,
				JSON:    opts.JSON,
			}, stdout, stderr)
		}
		if len(args) == 2 && args[1] == RepoTargetActionRm {
			return RepoRm(ctx, cr, fsys, RepoRmOpts{
				RepoRef: repoRef,
				Yes:     opts.Yes,
				JSON:    opts.JSON,
			}, stdout, stderr)
		}
		return errors.New(errors.EUsage, "unknown command \""+args[1]+"\" for \"agency repo\"")
	}
}
