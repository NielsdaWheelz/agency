package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

type stubRunner struct {
	paths map[string]string
}

func (s stubRunner) Run(_ context.Context, _ string, _ []string, _ exec.RunOpts) (exec.CmdResult, error) {
	return exec.CmdResult{}, nil
}

func (s stubRunner) LookPath(file string) (string, error) {
	if p, ok := s.paths[file]; ok {
		return p, nil
	}
	return "", os.ErrNotExist
}

func TestLoadUserConfig_MissingFile(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	cfg, found, err := LoadUserConfig(stub, "/cfg")
	require.NoError(t, err)
	assert.False(t, found, "expected found=false for missing config")
	assert.Equal(t, 2, cfg.Version)
	assert.Equal(t, "claude-code", cfg.Defaults.Runner)
	assert.Equal(t, "code", cfg.Defaults.Editor)
	assert.Equal(t, "main", cfg.Defaults.BaseBranch)
}

func TestLoadUserConfig_InvalidJSON(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{"version": 2, "defaults": {`)
	_, _, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err, "expected error for invalid JSON")
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
}

func TestLoadUserConfig_UnknownKeys(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 2,
	  "defaults": { "runner": "claude-code", "editor": "code" },
  "extra": "nope"
}`)
	_, _, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err, "expected error for unknown keys")
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
}

func TestLoadUserConfig_UnknownDefaultsKeys(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 2,
	  "defaults": { "runner": "claude-code", "editor": "code", "unknown": "nope" }
}`)
	_, _, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err, "expected error for unknown defaults keys")
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults contains unknown field")
}

func TestLoadUserConfig_RemovedDefaultsModelRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	data, err := os.ReadFile("testdata/user_removed_defaults_model.json")
	require.NoError(t, err, "failed to read fixture")
	stub.files["/cfg/config.json"] = data

	_, _, err = LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults contains unknown field: model")
}

func TestLoadUserConfig_RemovedDefaultsEffortRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	data, err := os.ReadFile("testdata/user_removed_defaults_effort.json")
	require.NoError(t, err, "failed to read fixture")
	stub.files["/cfg/config.json"] = data

	_, _, err = LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults contains unknown field: effort")
}

func TestLoadUserConfig_DefaultsThinkingRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 2,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "thinking": "high"
  }
}`)
	_, _, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults contains unknown field")
}

func TestLoadUserConfig_ValidRunnerDefaults(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	data, err := os.ReadFile("testdata/user_valid_runner_defaults.json")
	require.NoError(t, err, "failed to read fixture")
	stub.files["/cfg/config.json"] = data

	cfg, found, err := LoadUserConfig(stub, "/cfg")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 2, cfg.Version)
	assert.Equal(t, "claude-code", cfg.Defaults.Runner)
	assert.Equal(t, "code", cfg.Defaults.Editor)
	assert.Equal(t, "main", cfg.Defaults.BaseBranch)
}

func TestLoadUserConfig_RunnerDefaultsWrongTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fixture string
		wantMsg string
	}{
		{"runner_defaults as array", "user_wrong_types_runner_defaults.json", "runner_defaults must be an object"},
		{"runner_defaults entry as string", "user_wrong_types_runner_defaults_entry.json", "runner_defaults.claude-code must be an object"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			require.NoError(t, err, "failed to read fixture")
			stub := newStubFS()
			stub.files["/cfg/config.json"] = data

			_, _, err = LoadUserConfig(stub, "/cfg")
			require.Error(t, err)
			assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestLoadUserConfig_RunnerDefaultsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fixture string
		wantMsg string
	}{
		{"unknown runner", "user_runner_defaults_unknown_runner.json", "runner_defaults.amp is not supported"},
		{"cursor effort unsupported", "user_runner_defaults_cursor_effort.json", "runner_defaults.cursor.effort is not supported"},
		{"missing model and effort", "user_runner_defaults_empty_entry.json", "runner_defaults.codex requires at least one of model or effort"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			require.NoError(t, err, "failed to read fixture")
			stub := newStubFS()
			stub.files["/cfg/config.json"] = data

			_, _, err = LoadUserConfig(stub, "/cfg")
			require.Error(t, err)
			assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateUserConfig_RequiredFields(t *testing.T) {
	t.Parallel()
	cfg := UserConfig{Version: 2}
	_, err := ValidateUserConfig(cfg)
	require.Error(t, err, "expected validation error")
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
}

func TestValidateUserConfig_WrongVersion(t *testing.T) {
	t.Parallel()
	cfg := UserConfig{
		Version: 1,
		Defaults: UserDefaults{
			Runner: "claude-code",
			Editor: "code",
		},
	}

	_, err := ValidateUserConfig(cfg)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version must be 2")
}

func TestResolveRunnerCmd_Path(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "bin", "runner")
	err := os.MkdirAll(filepath.Dir(binPath), 0o755)
	require.NoError(t, err, "failed to create dir")
	err = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755)
	require.NoError(t, err, "failed to write file")

	cfg := UserConfig{
		Version: 2,
		Defaults: UserDefaults{
			Runner: "claude-code",
			Editor: "code",
		},
		Runners: map[string]string{
			"claude-code": "bin/runner",
		},
	}

	cmd, err := ResolveRunnerCmd(stubRunner{}, fs.NewRealFS(), tmpDir, cfg, "claude-code")
	require.NoError(t, err)
	assert.Equal(t, binPath, cmd)
}

func TestResolveEditorCmd_Path(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "bin", "editor")
	err := os.MkdirAll(filepath.Dir(binPath), 0o755)
	require.NoError(t, err, "failed to create dir")
	err = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755)
	require.NoError(t, err, "failed to write file")

	cfg := UserConfig{
		Version: 2,
		Defaults: UserDefaults{
			Runner: "claude-code",
			Editor: "custom",
		},
		Editors: map[string]string{
			"custom": "bin/editor",
		},
	}

	cmd, err := ResolveEditorCmd(stubRunner{}, fs.NewRealFS(), tmpDir, cfg, "custom")
	require.NoError(t, err)
	assert.Equal(t, binPath, cmd)
}
