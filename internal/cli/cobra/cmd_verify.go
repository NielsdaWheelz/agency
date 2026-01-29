package cobra

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newVerifyCmd() *cobra.Command {
	var repoPath string
	var timeoutStr string

	cmd := &cobra.Command{
		Use:   "verify <run>",
		Short: "Run scripts.verify and record results",
		Long: `Run the repo's scripts.verify for a run and record results.
Works from any directory; resolves runs globally.

Arguments:
  run    run name, run_id, or unique run_id prefix

Behavior:
  - writes verify_record.json and verify.log
  - updates run flags (needs_attention on failure)
  - does NOT affect push or merge behavior`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get working directory", err)
			}

			// Parse timeout: if empty, use 0 (service will use config default)
			var timeout time.Duration
			if timeoutStr != "" {
				timeout, err = time.ParseDuration(timeoutStr)
				if err != nil {
					_ = cmd.Help()
					return errors.New(errors.EUsage, fmt.Sprintf("invalid timeout: %s", timeoutStr))
				}
				if timeout <= 0 {
					_ = cmd.Help()
					return errors.New(errors.EUsage, "timeout must be positive")
				}
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()

			// Set up cancellation context for user SIGINT
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle SIGINT for cancellation
			go func() {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, os.Interrupt)
				<-sigCh
				cancel()
			}()

			opts := commands.VerifyOpts{
				RunID:    args[0],
				RepoPath: repoPath,
				Timeout:  timeout,
			}

			return commands.Verify(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "scope name resolution to a specific repo")
	cmd.Flags().StringVar(&timeoutStr, "timeout", "", "script timeout override (Go duration format, e.g., '30m'); defaults to agency.json config")

	return cmd
}
