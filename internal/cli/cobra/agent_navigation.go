package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newAgentOpenCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:   "open <invocation_ref>",
		Short: "Open sandbox in editor",
		Long: `Open the sandbox directory in your configured editor.

Example:
  agency agent open 20260131
  agency agent open --repo agency my-invocation`,
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
	return cmd
}

func newAgentPathCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:   "path <invocation_ref>",
		Short: "Print sandbox path",
		Long: `Print the sandbox path for an agent invocation.

The path is resolved via the daemon and printed to stdout.

Example:
  agency agent path 20260131
  agency agent path --repo agency my-invocation
  cd $(agency agent path 20260131)`,
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
	return cmd
}

func newAgentShellCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:   "shell <invocation_ref>",
		Short: "Open shell in sandbox",
		Long: `Open a login shell with the working directory set to the sandbox path.

The sandbox path is resolved via the daemon before shell dispatch.

Example:
  agency agent shell 20260131
  agency agent shell --repo agency my-invocation`,
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

	cmd.Flags().StringVar(&repoRef, "repo", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	return cmd
}

func newAgentEnterCmd() *cobra.Command {
	var repoRef string

	cmd := &cobra.Command{
		Use:   "enter <invocation_ref>",
		Short: "Attach to a running headed invocation",
		Long: `Attach to a running headed invocation's tmux session.

This is the canonical interactive navigation command for headed invocations.
Headless invocations are rejected. Detach from the session with Ctrl+b, d.

Example:
  agency agent enter 20260131
  agency agent enter --repo agency 20260131`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.AgentEnter(ctx, cr, fsys, cwd, commands.AgentEnterOpts{
				InvocationRef: args[0],
				RepoRef:       repoRef,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.GroupID = "navigate"

	cmd.Flags().StringVarP(&repoRef, "repo", "r", "", "Repo ref: name, owner/repo, repo key, id, or prefix")
	return cmd
}
