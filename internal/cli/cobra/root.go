// Package cobra provides the Cobra-based CLI command tree for agency.
package cobra

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/version"
)

// NewRootCmd creates the root cobra command for agency.
func NewRootCmd() *cobra.Command {
	repoCmd := newRepoCmd()
	repoCmd.GroupID = "workflow"

	worktreeCmd := newWorktreeCmd()
	worktreeCmd.GroupID = "workflow"

	taskCmd := newTaskCmd()
	taskCmd.GroupID = "workflow"

	agentCmd := newAgentCmd()
	agentCmd.GroupID = "workflow"

	contextCmd := newContextCmd()
	contextCmd.GroupID = "workflow"

	watchCmd := newWatchCmd()
	watchCmd.GroupID = "workflow"

	configCmd := newConfigCmd()
	configCmd.GroupID = "setup"

	initCmd := newInitCmd()
	initCmd.GroupID = "setup"

	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = "setup"

	daemonCmd := newDaemonCmd()
	daemonCmd.GroupID = "operations"

	completionCmd := newCompletionCmd()
	completionCmd.GroupID = "other"

	versionCmd := newVersionCmd()
	versionCmd.GroupID = "other"

	rootCmd := &cobra.Command{
		Use:   "agency",
		Short: "Manage repos, worktrees, and agent sessions for local AI coding",
		Long: `agency manages the local workflow for AI coding in git repositories.

Primary workflow:
  1. Register a repo once so --repo works from any directory.
  2. Start a task to create an integration worktree and primary agent.
  3. Use watch to inspect running sessions and history.

Setup commands like init and doctor operate on one checkout path. If you omit
--path, they use the current directory. Workflow commands use repo refs with
--repo after a repository has been registered.`,
		Example: `  agency repo add /path/to/repo
  agency task start fix-help --repo agency --base main --prompt "Fix the help output"
  agency watch`,
		Version:       version.FullVersion(),
		SilenceErrors: true, // We handle error printing in main.go
		SilenceUsage:  true, // We handle usage printing manually
	}

	// Global flags
	rootCmd.PersistentFlags().Bool("verbose", false, "show detailed error context")

	// Disable Cobra's default completion command (we register our own)
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddGroup(
		&cobra.Group{ID: "workflow", Title: "Workflow"},
		&cobra.Group{ID: "setup", Title: "Setup"},
		&cobra.Group{ID: "operations", Title: "Operations"},
		&cobra.Group{ID: "other", Title: "Other"},
	)

	// Add all subcommands
	rootCmd.AddCommand(
		repoCmd,
		taskCmd,
		worktreeCmd,
		agentCmd,
		contextCmd,
		watchCmd,
		configCmd,
		initCmd,
		doctorCmd,
		daemonCmd,
		completionCmd,
		versionCmd,
		newInternalCmd(),
	)

	return rootCmd
}

// Execute runs the root command with the given output writers.
// This is the main entry point from main.go.
func Execute(stdout, stderr io.Writer) (bool, error) {
	rootCmd := NewRootCmd()
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(os.Args[1:])
	err := rootCmd.Execute()
	verbose, verboseErr := rootCmd.PersistentFlags().GetBool("verbose")
	if verboseErr != nil {
		return false, err
	}
	return verbose, err
}
