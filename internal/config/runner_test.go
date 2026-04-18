package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// configTestRunner implements exec.CommandRunner for config runner/editor tests.
type configTestRunner struct {
	lookPathErr map[string]error
}

func (r *configTestRunner) Run(_ context.Context, _ string, _ []string, _ agencyexec.RunOpts) (agencyexec.CmdResult, error) {
	return agencyexec.CmdResult{}, nil
}

func (r *configTestRunner) LookPath(file string) (string, error) {
	if r.lookPathErr != nil {
		if err, ok := r.lookPathErr[file]; ok {
			return "", err
		}
	}
	return "/usr/bin/" + file, nil
}

// Compile-time interface check.
var _ agencyexec.CommandRunner = (*configTestRunner)(nil)

// --- ERunnerNotConfigured tests ---

func TestResolveRunnerCmd_UnknownRunner_ReturnsRunnerNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}

	cfg := UserConfig{
		Version:  2,
		Defaults: UserDefaults{Runner: "claude-code"},
		Runners:  map[string]string{},
	}

	// Unknown runner name that is neither canonical nor custom.
	_, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, "my-custom-runner")
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not configured")
}

func TestResolveRunnerCmd_TargetRunnerSet_RequiresExplicitConfig(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}
	cfg := UserConfig{
		Version:  2,
		Defaults: UserDefaults{Runner: "claude-code"},
		Runners:  map[string]string{},
	}

	for _, runner := range []string{"claude-code", "codex", "amp", "opencode", "cursor", "droid"} {
		runner := runner
		t.Run(runner, func(t *testing.T) {
			t.Parallel()
			_, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, runner)
			require.Error(t, err)
			assert.Equal(t, errors.ERunnerNotConfigured, errors.GetCode(err))
		})
	}
}

func TestResolveRunnerCmd_TargetRunnerSet_ResolvesWhenExplicitlyConfigured(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}
	cfg := UserConfig{
		Version: 2,
		Runners: map[string]string{
			"claude-code": "claude",
			"codex":       "codex",
			"amp":         "amp",
			"opencode":    "opencode",
			"cursor":      "cursor-agent",
			"droid":       "droid",
		},
	}

	for _, runner := range []string{"claude-code", "codex", "amp", "opencode", "cursor", "droid"} {
		runner := runner
		t.Run(runner, func(t *testing.T) {
			t.Parallel()
			cmd, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, runner)
			require.NoError(t, err)
			assert.NotEmpty(t, cmd)
		})
	}
}

func TestResolveRunnerCmd_ClaudeCode_RequiresCanonicalConfig(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}

	cfg := UserConfig{
		Version: 2,
		Runners: map[string]string{
			"claude": "./runner",
		},
	}

	_, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, "claude-code")
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "claude-code")
}

func TestResolveRunnerCmd_NotOnPath_ReturnsRunnerNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{
		lookPathErr: map[string]error{
			"claude-code": fmt.Errorf("executable not found"),
		},
	}

	cfg := UserConfig{
		Version:  2,
		Defaults: UserDefaults{Runner: "claude-code"},
		Runners: map[string]string{
			"claude-code": "claude-code",
		},
	}

	// "claude-code" is a known runner but not on PATH.
	_, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, "claude-code")
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not found on PATH")
}

func TestResolveRunnerCmd_PathNotExecutable_ReturnsRunnerNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}

	// Create a file that is NOT executable
	runnerPath := filepath.Join(configDir, "my-runner")
	require.NoError(t, os.WriteFile(runnerPath, []byte("#!/bin/sh\nexit 0\n"), 0644))

	cfg := UserConfig{
		Version: 2,
		Runners: map[string]string{
			"claude-code": "./my-runner", // relative path triggers file stat check
		},
	}

	_, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, "claude-code")
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not executable")
}

func TestResolveRunnerCmd_PathNotFound_ReturnsRunnerNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}

	cfg := UserConfig{
		Version: 2,
		Runners: map[string]string{
			"claude-code": "./nonexistent-runner",
		},
	}

	_, err := ResolveRunnerCmd(cr, fsys, configDir, cfg, "claude-code")
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not found")
}

// --- EEditorNotConfigured tests ---

func TestResolveEditorCmd_NotOnPath_ReturnsEditorNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{
		lookPathErr: map[string]error{
			"code": fmt.Errorf("executable not found"),
		},
	}

	cfg := UserConfig{
		Version:  2,
		Defaults: UserDefaults{Editor: "code"},
	}

	_, err := ResolveEditorCmd(cr, fsys, configDir, cfg, "code")
	require.Error(t, err)
	assert.Equal(t, errors.EEditorNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not found on PATH")
}

func TestResolveEditorCmd_PathNotExecutable_ReturnsEditorNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}

	// Create a file that is NOT executable
	editorPath := filepath.Join(configDir, "my-editor")
	require.NoError(t, os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 0\n"), 0644))

	cfg := UserConfig{
		Version: 2,
		Editors: map[string]string{
			"custom": "./my-editor",
		},
	}

	_, err := ResolveEditorCmd(cr, fsys, configDir, cfg, "custom")
	require.Error(t, err)
	assert.Equal(t, errors.EEditorNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not executable")
}

func TestResolveEditorCmd_PathNotFound_ReturnsEditorNotConfigured(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	fsys := fs.NewRealFS()
	cr := &configTestRunner{}

	cfg := UserConfig{
		Version: 2,
		Editors: map[string]string{
			"custom": "./nonexistent-editor",
		},
	}

	_, err := ResolveEditorCmd(cr, fsys, configDir, cfg, "custom")
	require.Error(t, err)
	assert.Equal(t, errors.EEditorNotConfigured, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not found")
}
