package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// WorktreeLSOpts holds options for the worktree ls command.
type WorktreeLSOpts struct {
	RepoFlag string
	AllRepos bool
	All      bool
	JSON     bool
}

// WorktreeLS lists integration worktrees.
func WorktreeLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeLSOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllRepos:      opts.AllRepos,
		AllowAllRepos: true,
		CmdName:       "worktree ls",
	})
	if err != nil {
		return err
	}

	state := "present"
	if opts.All {
		state = "all"
	}

	var repoID string
	if !repoCtx.AllRepos {
		repoID = repoCtx.RepoID
	}

	result, fetchErr := ns.client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{
		RepoID: repoID,
		State:  state,
	})
	if fetchErr != nil {
		return fetchErr
	}
	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Data.Worktrees)
	}
	if len(result.Data.Worktrees) == 0 {
		_, _ = fmt.Fprintln(stdout, "No integration worktrees found.")
		return nil
	}

	for _, wt := range result.Data.Worktrees {
		state := ""
		if wt.State == "archived" {
			state = " [archived]"
		}
		_, _ = fmt.Fprintf(stdout, "%s  %s  %s%s\n", wt.WorktreeID, wt.Name, wt.Branch, state)
	}

	return nil
}

// WorktreeShowOpts holds options for the worktree show command.
type WorktreeShowOpts struct {
	WorktreeRef string
	RepoFlag    string
	JSON        bool
}

// WorktreeShow shows details of an integration worktree.
func WorktreeShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShowOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "worktree show",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetWorktree(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(&result.Data)
	}

	wt := &result.Data
	_, _ = fmt.Fprintf(stdout, "worktree_id:   %s\n", wt.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "name:          %s\n", wt.Name)
	_, _ = fmt.Fprintf(stdout, "repo_id:       %s\n", wt.RepoID)
	_, _ = fmt.Fprintf(stdout, "branch:        %s\n", wt.Branch)
	_, _ = fmt.Fprintf(stdout, "parent_branch: %s\n", wt.ParentBranch)
	_, _ = fmt.Fprintf(stdout, "state:         %s\n", wt.State)
	_, _ = fmt.Fprintf(stdout, "created_at:    %s\n", wt.CreatedAt)
	_, _ = fmt.Fprintf(stdout, "tree_path:     %s\n", wt.TreePath)
	return nil
}
