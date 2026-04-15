// Package cobra provides the Cobra-based CLI command tree for agency.
package cobra

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/version"
)

// NewRootCmd creates the root cobra command for agency.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "agency",
		Short: "Local-first runner manager for AI coding sessions",
		Long: `agency - local-first runner manager for AI coding sessions

Agency manages AI coding sessions with worktrees, tmux sessions, and lifecycle
orchestration. It creates isolated workspaces for each task, runs setup scripts,
and provides commands to control the runner session.`,
		Version:       version.FullVersion(),
		SilenceErrors: true, // We handle error printing in main.go
		SilenceUsage:  true, // We handle usage printing manually
	}

	// Global flags
	rootCmd.PersistentFlags().Bool("verbose", false, "show detailed error context")

	// Disable Cobra's default completion command (we register our own)
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Add all subcommands
	rootCmd.AddCommand(
		newInitCmd(),
		newDoctorCmd(),
		newCompletionCmd(),
		newVersionCmd(),
		newWorktreeCmd(),
		newAgentCmd(),
		newWatchCmd(),
		newDaemonCmd(),
		newRepoCmd(),
	)

	return rootCmd
}

// Execute runs the root command with the given output writers.
// This is the main entry point from main.go.
func Execute(stdout, stderr io.Writer) (bool, error) {
	rootCmd := NewRootCmd()
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	err := rootCmd.Execute()
	verbose, verboseErr := rootCmd.PersistentFlags().GetBool("verbose")
	if verboseErr != nil {
		return false, err
	}
	return verbose, err
}
