package cobra

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func realCommandDepsFromCmd(cmd *cobra.Command) (context.Context, exec.CommandRunner, fs.FS, string, error) {
	if cmd == nil {
		return nil, nil, nil, "", errors.New(errors.EInternal, "command context is required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, nil, "", errors.Wrap(errors.EInternal, "failed to get cwd", err)
	}
	ctx, cr, fsys := realCommandDeps(cmd)
	return ctx, cr, fsys, cwd, nil
}

// realCommandDeps returns the runtime deps a command needs when it does not
// require cwd. Use realCommandDepsFromCmd when cwd is needed.
func realCommandDeps(cmd *cobra.Command) (context.Context, exec.CommandRunner, fs.FS) {
	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}
	return ctx, exec.NewRealRunner(), fs.NewRealFS()
}
