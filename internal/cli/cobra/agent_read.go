package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentLSCmd() *cobra.Command {
	var repoFlag string
	var allRepos bool
	var worktree string
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List agent invocations",
		Long: `List agent invocations for the current repository.

By default, shows active invocations (not yet landed/discarded).
Use --repo to specify a repo, or --all-repos to list globally.

Example:
  agency agent ls
  agency agent ls --repo abc123
  agency agent ls --all-repos
  agency agent ls --worktree my-feature
  agency agent ls --all --json
  agency watch`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentLS(ctx, cr, fsys, cwd, commands.AgentLSOpts{
				RepoFlag:    repoFlag,
				AllRepos:    allRepos,
				WorktreeRef: worktree,
				All:         all,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&repoFlag, "repo", "", "Filter by repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Filter by integration worktree")
	cmd.Flags().BoolVar(&all, "all", false, "Include finished (landed/discarded) invocations")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newAgentShowCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "show <invocation_id|prefix>",
		Short: "Show details of an invocation",
		Long: `Show details of an agent invocation.

The invocation can be specified by full ID or unique prefix.

Example:
  agency agent show 20260131
  agency agent show --repo abc123 20260131
  agency agent show --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentShow(ctx, cr, fsys, cwd, commands.AgentShowOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")

	return cmd
}

func newAgentDiffCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool
	var turnID string
	var turnRange string

	cmd := &cobra.Command{
		Use:   "diff <invocation_ref>",
		Short: "Show sandbox changes vs integration",
		Long: `Show the diff between sandbox and the integration branch.

Displays:
- Commits in the sandbox (since base_commit)
- File changes between base_commit and sandbox tip
- Uncommitted changes in sandbox (if any)

Optionally anchor diff context to timeline turn selectors:
- --turn <entry_id> for a single turn
- --turn-range <start_entry_id>..<end_entry_id> for an inclusive turn range

Example:
  agency agent diff 20260131
  agency agent diff --repo abc123 my-invocation
  agency agent diff --turn inv_event:2:agency.followup_prompt 20260131
  agency agent diff --turn-range stream:4..stream:9 --json 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.AgentDiff(ctx, cr, fsys, cwd, commands.AgentDiffOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
				TurnID:        turnID,
				TurnRange:     turnRange,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVar(&turnID, "turn", "", "Timeline entry id to anchor diff context")
	cmd.Flags().StringVar(&turnRange, "turn-range", "", "Inclusive turn range (<start_entry_id>..<end_entry_id>)")

	return cmd
}

func newAgentHistoryCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool
	var last bool
	var limit int
	var cursor string

	cmd := &cobra.Command{
		Use:   "history <invocation_id|prefix>",
		Short: "Show unified invocation timeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentHistory(ctx, cr, fsys, cwd, commands.AgentHistoryOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
				Last:          last,
				Limit:         limit,
				Cursor:        cursor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&last, "last", false, "Show only the last timeline entry")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum entries to show")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Cursor for pagination")

	return cmd
}

func newAgentLogsCmd() *cobra.Command {
	var repoFlag string
	var kind string
	var follow bool
	var offset int64
	var maxIterations int

	cmd := &cobra.Command{
		Use:   "logs <invocation_id|prefix>",
		Short: "View invocation logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentLogs(ctx, cr, fsys, cwd, commands.AgentLogsOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				Kind:          kind,
				Follow:        follow,
				Offset:        offset,
				MaxIterations: maxIterations,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().StringVar(&kind, "kind", "", "Log kind (raw, stderr, stream)")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Starting byte offset")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Limit follow iterations for testing")

	return cmd
}

func newAgentReviewCmd() *cobra.Command {
	var repoFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "review <invocation_id|prefix>",
		Short: "Show review/readiness surface",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentReview(ctx, cr, fsys, cwd, commands.AgentReviewOpts{
				InvocationRef: args[0],
				RepoFlag:      repoFlag,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repo name, key, id, or prefix")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}
