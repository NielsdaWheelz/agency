// Command agency is a local-first runner manager for AI coding sessions.
package main

import (
	"os"

	"github.com/NielsdaWheelz/agency/internal/cli/cobra"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func main() {
	verbose, err := cobra.Execute(os.Stdout, os.Stderr)
	if err != nil {
		opts := errors.PrintOptions{
			Verbose: verbose,
		}
		errors.PrintWithOptions(os.Stderr, err, opts)
		os.Exit(errors.ExitCode(err))
	}
}
