package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestLoadUserConfig_MissingFile(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.ENoUserConfig, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "/cfg/config.json", ae.Details["path"])
	assert.Equal(t, "run `agency config init`", ae.Details["hint"])
}

func TestLoadUserConfig_InvalidJSON(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{"version": 3, "defaults": {`)
	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err, "expected error for invalid JSON")
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
}

func TestLoadUserConfig_UnknownKeys(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 3,
	  "defaults": { "runner": "claude-code", "editor": "code" },
  "extra": "nope"
}`)
	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err, "expected error for unknown keys")
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
}

func TestLoadUserConfig_UnknownDefaultsKeys(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 3,
	  "defaults": { "runner": "claude-code", "editor": "code", "unknown": "nope" }
}`)
	_, err := LoadUserConfig(stub, "/cfg")
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

	_, err = LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults.model is not supported")
	assert.Contains(t, err.Error(), "runner_defaults.<runner>.model")
}

func TestLoadUserConfig_RemovedDefaultsEffortRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	data, err := os.ReadFile("testdata/user_removed_defaults_effort.json")
	require.NoError(t, err, "failed to read fixture")
	stub.files["/cfg/config.json"] = data

	_, err = LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults.effort is not supported")
	assert.Contains(t, err.Error(), "runner_defaults.<runner>.effort")
}

func TestLoadUserConfig_DefaultsThinkingRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 3,
  "defaults": {
    "runner": "claude-code",
    "editor": "code",
    "thinking": "high"
  }
}`)
	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "defaults.thinking is not supported")
	assert.Contains(t, err.Error(), "runner_defaults.<runner>.model")
}

func TestLoadUserConfig_Version1Rejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 1,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  }
}`)

	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version 1 is not supported")
}

func TestLoadUserConfig_Version2Rejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 2,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  }
}`)

	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version 2 is not supported")
	assert.Contains(t, err.Error(), "version 3")
}

func TestLoadUserConfig_ValidRunnerDefaults(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	data, err := os.ReadFile("testdata/user_valid_runner_defaults.json")
	require.NoError(t, err, "failed to read fixture")
	stub.files["/cfg/config.json"] = data

	cfg, err := LoadUserConfig(stub, "/cfg")
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Version)
	assert.Equal(t, "claude-code", cfg.Defaults.Runner)
	assert.Equal(t, "code", cfg.Defaults.Editor)
	assert.Equal(t, "main", cfg.Defaults.BaseBranch)
	assert.Equal(t, "acceptEdits", cfg.RunnerDefaults["claude-code"].PermissionMode)
}

func TestLoadUserConfig_RunnerDefaultsPermissionModeRequiresClaudeCode(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 3,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  },
  "runner_defaults": {
    "codex": {
      "permission_mode": "default"
    }
  }
}`)

	_, err := LoadUserConfig(stub, "/cfg")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "runner_defaults.codex.permission_mode is only supported for claude-code")
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

			_, err = LoadUserConfig(stub, "/cfg")
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
		{"missing model, effort, and permission_mode", "user_runner_defaults_empty_entry.json", "runner_defaults.codex requires at least one of model, effort, or permission_mode"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			require.NoError(t, err, "failed to read fixture")
			stub := newStubFS()
			stub.files["/cfg/config.json"] = data

			_, err = LoadUserConfig(stub, "/cfg")
			require.Error(t, err)
			assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateUserConfig_RequiredFields(t *testing.T) {
	t.Parallel()
	cfg := UserConfig{Version: 3}
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
	assert.Contains(t, err.Error(), "version 1 is not supported")
}

func TestValidateUserConfig_Version2Rejected(t *testing.T) {
	t.Parallel()
	cfg := UserConfig{
		Version: 2,
		Defaults: UserDefaults{
			Runner: "claude-code",
			Editor: "code",
		},
	}

	_, err := ValidateUserConfig(cfg)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version 2 is not supported")
}

func TestValidateUserConfig_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	cfg := UserConfig{
		Version: 4,
		Defaults: UserDefaults{
			Runner: "claude-code",
			Editor: "code",
		},
	}

	_, err := ValidateUserConfig(cfg)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidUserConfig, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version must be 3")
}
