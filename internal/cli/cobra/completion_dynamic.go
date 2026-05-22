package cobra

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func completionClient(cmd *cobra.Command) (context.Context, *daemonclient.Client, error) {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}

	fsys := fs.NewRealFS()
	dirs := paths.ResolveDirs(os.Getenv, homeDir)
	st := store.NewStore(fsys, dirs.DataDir, time.Now)

	client := daemonclient.NewClient(st.DaemonSocketPath())
	if !client.IsRunning(ctx) {
		return nil, nil, errors.New(errors.EDaemonNotRunning, "daemon is not running")
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return nil, nil, err
	}

	return ctx, client, nil
}

func completeRepoRefs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx, client, err := completionClient(cmd)
	if err != nil {
		return cobra.AppendActiveHelp(nil, "register a repo first with: agency repo add /path/to/repo"), cobra.ShellCompDirectiveNoFileComp
	}

	result, err := client.ListRepos(ctx)
	if err != nil {
		return cobra.AppendActiveHelp(nil, "register a repo first with: agency repo add /path/to/repo"), cobra.ShellCompDirectiveNoFileComp
	}

	candidates := make([]string, 0, len(result.Data.Repos))
	seen := map[string]struct{}{}
	for _, repo := range result.Data.Repos {
		ref := ids.RepoShortName(repo.RepoKey)
		if ref == "" {
			ref = repo.RepoID
		}
		if toComplete != "" && !strings.HasPrefix(ref, toComplete) && !strings.HasPrefix(repo.RepoKey, toComplete) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}

		desc := repo.RepoKey
		if repo.PreferredRoot != "" {
			desc = repo.RepoKey + " · " + repo.PreferredRoot
		}
		candidates = append(candidates, ref+"\t"+desc)
	}

	if len(candidates) == 0 {
		return cobra.AppendActiveHelp(nil, "register a repo first with: agency repo add /path/to/repo"), cobra.ShellCompDirectiveNoFileComp
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

func resolveCompletionRepoID(cmd *cobra.Command, subject string) (context.Context, *daemonclient.Client, string, []string, cobra.ShellCompDirective) {
	ctx, client, err := completionClient(cmd)
	if err != nil {
		return nil, nil, "", cobra.AppendActiveHelp(nil, "register a repo first with: agency repo add /path/to/repo"), cobra.ShellCompDirectiveNoFileComp
	}

	repoRef, err := cmd.Flags().GetString("repo")
	if err != nil || strings.TrimSpace(repoRef) == "" {
		return nil, nil, "", cobra.AppendActiveHelp(nil, "pass --repo <repo-ref> to complete "+subject+" from any cwd"), cobra.ShellCompDirectiveNoFileComp
	}

	repo, err := client.GetRepo(ctx, strings.TrimSpace(repoRef))
	if err != nil {
		return nil, nil, "", cobra.AppendActiveHelp(nil, "pass a valid --repo <repo-ref> to complete "+subject), cobra.ShellCompDirectiveNoFileComp
	}

	return ctx, client, repo.Data.RepoID, nil, cobra.ShellCompDirectiveNoFileComp
}

func completeWorktreeRefsForState(cmd *cobra.Command, toComplete string, state string) ([]string, cobra.ShellCompDirective) {
	ctx, client, repoID, activeHelp, directive := resolveCompletionRepoID(cmd, "worktrees")
	if repoID == "" {
		return activeHelp, directive
	}

	result, err := client.ListWorktrees(ctx, daemon.ListWorktreesParams{
		RepoID: repoID,
		State:  state,
		Limit:  500,
	})
	if err != nil {
		return cobra.AppendActiveHelp(nil, "no worktrees found for the selected repo"), cobra.ShellCompDirectiveNoFileComp
	}

	candidates := make([]string, 0, len(result.Data.Worktrees))
	for _, worktree := range result.Data.Worktrees {
		ref := worktree.WorktreeName
		if ref == "" {
			ref = worktree.WorktreeID
		}
		if toComplete != "" && !strings.HasPrefix(ref, toComplete) && !strings.HasPrefix(worktree.Branch, toComplete) {
			continue
		}
		desc := worktree.Branch
		if worktree.BaseBranch != "" {
			desc = worktree.Branch + " from " + worktree.BaseBranch
		}
		candidates = append(candidates, ref+"\t"+desc)
	}

	if len(candidates) == 0 {
		return cobra.AppendActiveHelp(nil, "no worktrees found for the selected repo"), cobra.ShellCompDirectiveNoFileComp
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

func completeInvocationRefsForState(cmd *cobra.Command, toComplete string, state string) ([]string, cobra.ShellCompDirective) {
	ctx, client, repoID, activeHelp, directive := resolveCompletionRepoID(cmd, "invocations")
	if repoID == "" {
		return activeHelp, directive
	}

	result, err := client.ListInvocations(ctx, daemon.ListInvocationsParams{
		RepoID: repoID,
		State:  state,
		Limit:  500,
	})
	if err != nil {
		return cobra.AppendActiveHelp(nil, "no invocations found for the selected repo"), cobra.ShellCompDirectiveNoFileComp
	}

	candidates := make([]string, 0, len(result.Data.Invocations))
	for _, invocation := range result.Data.Invocations {
		ref := invocation.InvocationID
		if toComplete != "" && !strings.HasPrefix(ref, toComplete) {
			continue
		}
		desc := invocation.State
		if invocation.InvocationName != "" {
			desc = invocation.InvocationName + " · " + invocation.State
		}
		candidates = append(candidates, ref+"\t"+desc)
	}

	if len(candidates) == 0 {
		return cobra.AppendActiveHelp(nil, "no invocations found for the selected repo"), cobra.ShellCompDirectiveNoFileComp
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

func registerRepoFlagCompletion(cmd *cobra.Command) {
	if cmd == nil || cmd.Flag("repo") == nil {
		return
	}
	if err := cmd.RegisterFlagCompletionFunc("repo", completeRepoRefs); err != nil {
		panic(err)
	}
}

func registerWorktreeFlagCompletion(cmd *cobra.Command, state string) {
	if cmd == nil || cmd.Flag("worktree") == nil {
		return
	}
	if err := cmd.RegisterFlagCompletionFunc("worktree", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeWorktreeRefsForState(cmd, toComplete, state)
	}); err != nil {
		panic(err)
	}
}
