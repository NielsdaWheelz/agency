package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentOpenCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<invocation-ref> open",
		Aliases: []string{"_open"},
		Short:   "Open sandbox in editor",
		Long: `Open the sandbox directory for one invocation in your configured editor.

Examples:
  agency agent 20260131 open
  agency agent my-invocation open --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentOpen(ctx, cr, fsys, cwd, commands.AgentOpenOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "navigate"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentPathCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<invocation-ref> path",
		Aliases: []string{"_path"},
		Short:   "Print sandbox path",
		Long: `Print the sandbox path for an agent invocation.

This prints only the resolved path, so it is suitable for scripting.

Examples:
  agency agent 20260131 path
  agency agent my-invocation path --repo agency
  cd $(agency agent 20260131 path)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentPath(ctx, cr, fsys, cwd, commands.AgentPathOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "navigate"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentShellCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<invocation-ref> shell",
		Aliases: []string{"_shell"},
		Short:   "Open shell in sandbox",
		Long: `Open a login shell with the working directory set to the sandbox path.

Use this when you want to inspect or edit the sandbox directly from a shell.

Examples:
  agency agent 20260131 shell
  agency agent my-invocation shell --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentShell(ctx, cr, fsys, cwd, commands.AgentShellOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "navigate"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentAttachCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<invocation-ref> attach",
		Aliases: []string{"_attach"},
		Short:   "Attach to a running headed invocation",
		Long: `Attach to a running headed invocation's tmux session.

This is the canonical interactive navigation command for headed invocations.
Headless invocations are rejected. Detach from the session with Ctrl+b, d.

Examples:
  agency agent 20260131 attach
  agency agent 20260131 attach --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentAttach(ctx, cr, fsys, cwd, commands.AgentAttachOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "navigate"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}

func newAgentClientsCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:     "<invocation-ref> clients",
		Aliases: []string{"_clients"},
		Short:   "List connected tmux clients",
		Long: `List the currently connected tmux clients for a live headed invocation.

Examples:
  agency agent 20260131 clients
  agency agent 20260131 clients --repo agency`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentClients(ctx, cr, fsys, cwd, commands.AgentClientsOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "inspect"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	setInvocationArgCompletion(cmd, "all")
	registerRepoFlagCompletion(cmd)
	return cmd
}
