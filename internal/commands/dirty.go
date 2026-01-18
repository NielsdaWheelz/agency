// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

const (
	dirtyWarningMessage = "warning: worktree has uncommitted changes; proceeding due to --allow-dirty"
	dirtyErrorMessage   = "worktree has uncommitted changes; use --allow-dirty to proceed"
)

// getDirtyStatus runs git status with deterministic untracked handling.
// Returns clean, status output (trimmed only for trailing newline), or error.
func getDirtyStatus(ctx context.Context, cr exec.CommandRunner, workDir string) (bool, string, error) {
	result, err := cr.Run(ctx, "git", []string{"status", "--porcelain", "--untracked-files=all"}, exec.RunOpts{
		Dir: workDir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		return false, "", errors.Wrap(errors.EInternal, "git status --porcelain failed to start", err)
	}
	if result.ExitCode != 0 {
		return false, "", errors.NewWithDetails(
			errors.EInternal,
			fmt.Sprintf("git status --porcelain failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{"exit_code": fmt.Sprintf("%d", result.ExitCode)},
		)
	}

	status := strings.TrimRight(result.Stdout, "\n")
	clean := strings.TrimSpace(status) == ""
	return clean, status, nil
}

func dirtyErrorWithContext(status string) error {
	msg := dirtyErrorMessage + "\n" + formatDirtyContext(status)
	return errors.New(errors.EDirtyWorktree, msg)
}

func printDirtyWarning(w io.Writer, status string) {
	_, _ = fmt.Fprintln(w, dirtyWarningMessage)
	printDirtyContext(w, status)
}

func printDirtyContext(w io.Writer, status string) {
	_, _ = fmt.Fprintln(w, "dirty_status:")
	if status != "" {
		_, _ = fmt.Fprintln(w, status)
	}
}

func formatDirtyContext(status string) string {
	if status == "" {
		return "dirty_status:"
	}
	return "dirty_status:\n" + status
}
