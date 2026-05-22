package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// runEditorAt resolves the configured editor (with optional override) and opens dir in it.
func runEditorAt(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, configDir, editorOverride, dir string) error {
	editorCmd, err := resolveEditorCmdWithOptionalOverride(cr, fsys, configDir, editorOverride)
	if err != nil {
		return err
	}
	return runAttachedAt(ctx, editorCmd, []string{dir}, dir, "editor")
}

// runShellAt opens an interactive login shell with cwd=dir.
func runShellAt(ctx context.Context, dir string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return runAttachedAt(ctx, shell, []string{"-l"}, dir, "shell")
}

func runAttachedAt(ctx context.Context, command string, args []string, dir, label string) error {
	result, err := exec.RunAttached(ctx, command, args, exec.AttachedRunOpts{
		Dir:    dir,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to run "+label, err)
	}
	if result.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("%s exited with code %d", label, result.ExitCode)),
			result.ExitCode,
		)
	}
	return nil
}

func resolveEditorCmdWithOptionalOverride(cr exec.CommandRunner, fsys fs.FS, configDir string, editorOverride string) (string, error) {
	editorName := strings.TrimSpace(editorOverride)
	if editorName != "" {
		return config.ResolveEditorCmd(cr, fsys, configDir, config.UserConfig{}, editorName)
	}
	userCfg, err := config.LoadUserConfig(fsys, configDir)
	if err != nil {
		return "", err
	}
	return config.ResolveEditorCmd(cr, fsys, configDir, userCfg, userCfg.Defaults.Editor)
}
