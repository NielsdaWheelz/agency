package cobra

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func realCommandDeps(ctx context.Context) (context.Context, exec.CommandRunner, fs.FS, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, nil, "", errors.Wrap(errors.EInternal, "failed to get cwd", err)
	}

	return ctx, exec.NewRealRunner(), fs.NewRealFS(), cwd, nil
}

func realCommandDepsFromCmd(cmd *cobra.Command) (context.Context, exec.CommandRunner, fs.FS, string, error) {
	if cmd == nil {
		return nil, nil, nil, "", errors.New(errors.EInternal, "command context is required")
	}
	return realCommandDeps(cmd.Context())
}

func runNestedCommand(parent *cobra.Command, child *cobra.Command, args []string) error {
	if parent == nil || child == nil {
		return errors.New(errors.EInternal, "nested command execution requires parent and child commands")
	}

	child.SetContext(parent.Context())
	child.SetOut(parent.OutOrStdout())
	child.SetErr(parent.ErrOrStderr())
	child.SetArgs(args)
	return child.Execute()
}
