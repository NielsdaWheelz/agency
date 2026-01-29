// Command agency is a local-first runner manager for AI coding sessions.
package main

import (
	"os"

	"github.com/NielsdaWheelz/agency/internal/cli/cobra"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func main() {
	err := cobra.Execute(os.Stdout, os.Stderr)
	if err != nil {
		// Use verbose mode if --verbose global flag was set
		opts := errors.PrintOptions{
			Verbose: cobra.GetGlobalOpts().Verbose,
		}
		errors.PrintWithOptions(os.Stderr, err, opts)
		os.Exit(errors.ExitCode(err))
	}
}
