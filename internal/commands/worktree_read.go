package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// WorktreeLSOpts holds options for the worktree ls command.
type WorktreeLSOpts struct {
	RepoRef  string
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
		RepoRef:       opts.RepoRef,
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

	worktrees, fetchErr := ns.client.DrainWorktrees(ctx, daemon.ListWorktreesParams{
		RepoID: repoID,
		State:  state,
	})
	if fetchErr != nil {
		return fetchErr
	}
	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(worktrees)
	}
	if len(worktrees) == 0 {
		_, _ = fmt.Fprintln(stdout, "No integration worktrees found.")
		return nil
	}

	for _, wt := range worktrees {
		worktreeName := wt.WorktreeName
		worktreeLabel := wt.WorktreeID
		if worktreeName != "" {
			worktreeLabel = worktreeName + " (" + wt.WorktreeID + ")"
		}
		state := ""
		if wt.State == "archived" {
			state = " [archived]"
		}
		merge := ""
		if wt.Merge != nil && wt.Merge.StatusSummary != "" {
			merge = " [merge: " + wt.Merge.StatusSummary + "]"
		}
		if repoCtx.AllRepos {
			repoLabel := wt.RepoID
			if wt.RepoName != "" {
				repoLabel = wt.RepoName + " (" + wt.RepoID + ")"
			}
			_, _ = fmt.Fprintf(stdout, "%s  %s%s%s  repo: %s\n", worktreeLabel, wt.Branch, state, merge, repoLabel)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "%s  %s%s%s\n", worktreeLabel, wt.Branch, state, merge)
	}

	return nil
}

// WorktreeShowOpts holds options for the worktree show command.
type WorktreeShowOpts struct {
	WorktreeRef string
	RepoRef     string
	JSON        bool
}

// WorktreeShow shows details of an integration worktree.
func WorktreeShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShowOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
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
	worktreeName := wt.WorktreeName
	worktreeLabel := wt.WorktreeID
	if worktreeName != "" {
		worktreeLabel = worktreeName + " (" + wt.WorktreeID + ")"
	}
	repoLabel := wt.RepoID
	if wt.RepoName != "" {
		repoLabel = wt.RepoName + " (" + wt.RepoID + ")"
	}
	_, _ = fmt.Fprintf(stdout, "worktree:    %s\n", worktreeLabel)
	_, _ = fmt.Fprintf(stdout, "repo:        %s\n", repoLabel)
	_, _ = fmt.Fprintf(stdout, "branch:        %s\n", wt.Branch)
	_, _ = fmt.Fprintf(stdout, "base_branch: %s\n", wt.BaseBranch)
	_, _ = fmt.Fprintf(stdout, "state:         %s\n", wt.State)
	_, _ = fmt.Fprintf(stdout, "created_at:    %s\n", wt.CreatedAt)
	_, _ = fmt.Fprintf(stdout, "tree_path:     %s\n", wt.TreePath)
	if wt.Merge != nil {
		_, _ = fmt.Fprintf(stdout, "merge_state:   %s\n", wt.Merge.State)
		_, _ = fmt.Fprintf(stdout, "merge_stage:   %s\n", wt.Merge.Stage)
		if wt.Merge.StatusSummary != "" {
			_, _ = fmt.Fprintf(stdout, "merge_status:  %s\n", wt.Merge.StatusSummary)
		}
		if wt.Merge.PRURL != "" {
			_, _ = fmt.Fprintf(stdout, "merge_pr_url:  %s\n", wt.Merge.PRURL)
		}
		if wt.Merge.ErrorCode != "" {
			_, _ = fmt.Fprintf(stdout, "merge_error:   %s\n", wt.Merge.ErrorCode)
		}
	}
	return nil
}
