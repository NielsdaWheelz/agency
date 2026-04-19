package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentLSCmd() *cobra.Command {
	var repoRef string
	var allRepos bool
	var worktree string
	var all bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List agent invocations",
		Long: `List agent invocations for the current repository.

By default this lists unresolved invocations for one repo. Omit --repo only
when cwd already identifies that repo. Use --all-repos to list globally.

Examples:
  agency agent ls
  agency agent ls --repo agency
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
				RepoRef:     repoRef,
				AllRepos:    allRepos,
				WorktreeRef: worktree,
				All:         all,
				JSON:        jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "inspect"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	cmd.Flags().BoolVar(&allRepos, "all-repos", false, "List across all registered repos")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Only show invocations for this integration worktree")
	cmd.Flags().BoolVar(&all, "all", false, "Include all invocations")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.MarkFlagsMutuallyExclusive("repo", "all-repos")
	registerRepoFlagCompletion(cmd)
	registerWorktreeFlagCompletion(cmd, "present")

	return cmd
}

func newAgentShowCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "<invocation-ref> [show]",
		Short: "Show details of an invocation",
		Long: `Show details of an agent invocation.

Pass --repo when cwd does not already identify the repo. The invocation
argument should be the invocation id or an unambiguous id prefix.

Examples:
  agency agent 20260131
  agency agent 20260131 show
  agency agent 20260131 --json
  agency agent 20260131 show --repo agency`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return nil
			}
			if len(args) == 2 && args[1] == "show" {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentShow(ctx, cr, fsys, cwd, commands.AgentShowOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "inspect"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)

	return cmd
}

func newAgentDiffCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var turnID string
	var turnRange string

	cmd := &cobra.Command{
		Use:   "<invocation-ref> diff",
		Short: "Show invocation changes",
		Long: `Show invocation changes from base_commit to the sandbox tip.

This compares the sandbox against its recorded base commit. It includes sandbox
commits, the overall diff, and any remaining uncommitted sandbox changes.

Use --turn or --turn-range when you want diff context anchored to specific
timeline entries.

Examples:
  agency agent 20260131 diff
  agency agent my-invocation diff --repo agency
  agency agent 20260131 diff --turn inv_event:2:agency.followup_prompt
  agency agent 20260131 diff --turn-range stream:4..stream:9 --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && args[1] == "diff" {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDeps(cmd.Context())
			if err != nil {
				return err
			}

			return commands.AgentDiff(ctx, cr, fsys, cwd, commands.AgentDiffOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
				TurnID:        turnID,
				TurnRange:     turnRange,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "inspect"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	cmd.Flags().StringVar(&turnID, "turn", "", "Timeline entry id to anchor diff context")
	cmd.Flags().StringVar(&turnRange, "turn-range", "", "Inclusive turn range (<start_entry_id>..<end_entry_id>)")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)

	return cmd
}

func newAgentHistoryCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool
	var last bool
	var limit int
	var cursor string

	cmd := &cobra.Command{
		Use:   "<invocation-ref> history",
		Short: "Show unified invocation timeline",
		Long: `Show the unified timeline for one invocation.

This is the canonical inspection surface for a single invocation.

In an interactive terminal, plain "agency agent <id> history" opens the
full-screen history view. Use --json, --last, --cursor, or --limit when you
want direct terminal output instead.

Use the "logs" subcommand when you want the raw log subcommand rather than the
structured timeline.

Examples:
  agency agent 20260131 history
  agency agent 20260131 history --repo agency
  agency agent 20260131 history --last
  agency agent 20260131 history --limit 200 --json
  agency agent 20260131 history logs --kind stream`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && args[1] == "history" {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentHistory(ctx, cr, fsys, cwd, commands.AgentHistoryOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
				Last:          last,
				Limit:         limit,
				Cursor:        cursor,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "inspect"
	cmd.AddCommand(newAgentHistoryLogsCmd())

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Write JSON instead of human output")
	cmd.Flags().BoolVar(&last, "last", false, "Show only the last timeline entry")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum entries to show")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Cursor for pagination")
	cmd.MarkFlagsMutuallyExclusive("last", "cursor")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)

	return cmd
}

func newAgentHistoryLogsCmd() *cobra.Command {
	var repoRef string
	var kind string
	var follow bool
	var offset int64
	var maxIterations int

	cmd := &cobra.Command{
		Use:   "<invocation-ref> history logs",
		Short: "View raw invocation logs",
		Long: `Stream raw invocation log files for one invocation.

This is the raw log subcommand of the canonical history surface. Use it when
you want byte-for-byte runner logs instead of the structured history timeline.

Examples:
  agency agent 20260131 history logs
  agency agent 20260131 history logs --repo agency --kind stderr
  agency agent 20260131 history logs --kind stream --follow
  agency agent 20260131 history logs --offset 1024`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 3 && args[1] == "history" && args[2] == "logs" {
				return nil
			}
			return cobra.ExactArgs(3)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentHistoryLogs(ctx, cr, fsys, cwd, commands.AgentHistoryLogsOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				Kind:          kind,
				Follow:        follow,
				Offset:        offset,
				MaxIterations: maxIterations,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().StringVar(&kind, "kind", "", "Log kind (raw, stderr, stream, hooks, terminal)")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	cmd.Flags().Int64Var(&offset, "offset", 0, "Starting byte offset")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Limit follow iterations for testing")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	registerLogKindFlagCompletion(cmd)

	return cmd
}

func newAgentCheckCmd() *cobra.Command {
	var repoRef string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "<invocation-ref> check",
		Short: "Check invocation readiness",
		Long: `Show the daemon's readiness view for one invocation.

This is the canonical machine-friendly readiness surface for deciding whether
an invocation is blocked, needs input, or is ready to land.

Examples:
  agency agent 20260131 check
  agency agent 20260131 check --repo agency
  agency agent 20260131 check --json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && args[1] == "check" {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentCheck(ctx, cr, fsys, cwd, commands.AgentCheckOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
				JSON:          jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "inspect"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	cmd.Flags().BoolVarP(&jsonOut, "json", "j", false, "Output as JSON")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)

	return cmd
}
