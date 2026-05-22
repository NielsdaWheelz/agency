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
	return cmd.Context(), exec.NewRealRunner(), fs.NewRealFS(), cwd, nil
}
