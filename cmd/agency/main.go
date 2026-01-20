// Command agency is a local-first runner manager for AI coding sessions.
package main

import (
	"os"

	"github.com/NielsdaWheelz/agency/internal/cli"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func main() {
	err := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		// Use verbose mode if --verbose global flag was set
		opts := errors.PrintOptions{
			Verbose: cli.GetGlobalOpts().Verbose,
		}
		errors.PrintWithOptions(os.Stderr, err, opts)
		os.Exit(errors.ExitCode(err))
	}
}
