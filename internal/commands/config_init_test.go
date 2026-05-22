package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lookupRunner struct {
	paths map[string]string
}

func (r *lookupRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	return exec.CmdResult{ExitCode: 0}, nil
}

func (r *lookupRunner) LookPath(file string) (string, error) {
	if path, ok := r.paths[file]; ok {
		return path, nil
	}
	return "", fmt.Errorf("%s not found", file)
}

func TestConfigInit_WritesOperationalConfig(t *testing.T) {
	t.Parallel()

	configDir := filepath.Join(t.TempDir(), "agency-config")
	cr := &lookupRunner{
		paths: map[string]string{
			"codex": "/usr/bin/codex",
			"amp":   "/usr/bin/amp",
			"zed":   "/usr/bin/zed",
		},
	}
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := ConfigInit(context.Background(), cr, fsys, ConfigInitOpts{
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "ConfigInit failed")

	userConfigPath := config.UserConfigPath(configDir)
	data, err := os.ReadFile(userConfigPath)
	require.NoError(t, err, "failed to read config.json")

	var cfg config.UserConfig
	require.NoError(t, json.Unmarshal(data, &cfg), "failed to unmarshal config.json")
	assert.Equal(t, 4, cfg.Version)
	assert.Equal(t, "codex", cfg.Defaults.Runner)
	assert.Equal(t, "zed", cfg.Defaults.Editor)
	assert.Equal(t, "main", cfg.Defaults.BaseBranch)
	assert.Equal(t, "personal", cfg.Defaults.ExecutionProfile)
	assert.Contains(t, cfg.ExecutionProfiles, "personal")
	assert.Equal(t, map[string]string{
		"codex": "codex",
		"amp":   "amp",
	}, cfg.Runners)

	output := stdout.String()
	assert.Contains(t, output, "user_config_path: "+userConfigPath)
	assert.Contains(t, output, "user_config: created")
	assert.Contains(t, output, "defaults_runner: codex")
	assert.Contains(t, output, "defaults_editor: zed")
	assert.Contains(t, output, "defaults_base_branch: main")
	assert.Contains(t, output, "defaults_execution_profile: personal")
	assert.Contains(t, output, "runners: codex, amp")
}

func TestConfigInit_FailsWithoutSupportedRunnerAndWritesNothing(t *testing.T) {
	t.Parallel()

	configDir := filepath.Join(t.TempDir(), "nested", "agency")
	cr := &lookupRunner{
		paths: map[string]string{
			"code": "/usr/bin/code",
		},
	}
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := ConfigInit(context.Background(), cr, fsys, ConfigInitOpts{
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected ConfigInit to fail without a supported runner")
	assert.Equal(t, errors.ERunnerNotFound, errors.GetCode(err))
	assert.NoFileExists(t, config.UserConfigPath(configDir))
	assert.NoDirExists(t, configDir, "config directory should not be created on failure")
	assert.Empty(t, stdout.String())
}

func TestConfigInit_FailsWithoutSupportedEditorAndWritesNothing(t *testing.T) {
	t.Parallel()

	configDir := filepath.Join(t.TempDir(), "nested", "agency")
	cr := &lookupRunner{
		paths: map[string]string{
			"codex": "/usr/bin/codex",
		},
	}
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := ConfigInit(context.Background(), cr, fsys, ConfigInitOpts{
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected ConfigInit to fail without a supported editor")
	assert.Equal(t, errors.EEditorNotConfigured, errors.GetCode(err))
	assert.NoFileExists(t, config.UserConfigPath(configDir))
	assert.NoDirExists(t, configDir, "config directory should not be created on failure")
	assert.Empty(t, stdout.String())
}

func TestConfigInit_ExistingFileRequiresForce(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	userConfigPath := config.UserConfigPath(configDir)
	original := []byte("{\"version\":2}\n")
	require.NoError(t, os.WriteFile(userConfigPath, original, 0o644), "failed to write existing config")

	cr := &lookupRunner{
		paths: map[string]string{
			"codex": "/usr/bin/codex",
			"code":  "/usr/bin/code",
		},
	}
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := ConfigInit(context.Background(), cr, fsys, ConfigInitOpts{
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)
	require.Error(t, err, "expected ConfigInit to reject existing file without --force")
	assert.Equal(t, errors.EUsage, errors.GetCode(err))

	data, readErr := os.ReadFile(userConfigPath)
	require.NoError(t, readErr, "failed to read existing config")
	assert.Equal(t, string(original), string(data))
}

func TestConfigInit_ForceOverwritesExistingFile(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	userConfigPath := config.UserConfigPath(configDir)
	require.NoError(t, os.WriteFile(userConfigPath, []byte("{\"version\":2}\n"), 0o644), "failed to write existing config")

	cr := &lookupRunner{
		paths: map[string]string{
			"claude": "/usr/bin/claude",
			"codex":  "/usr/bin/codex",
			"code":   "/usr/bin/code",
		},
	}
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := ConfigInit(context.Background(), cr, fsys, ConfigInitOpts{
		Force:             true,
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "ConfigInit with --force failed")

	data, readErr := os.ReadFile(userConfigPath)
	require.NoError(t, readErr, "failed to read overwritten config")

	var cfg config.UserConfig
	require.NoError(t, json.Unmarshal(data, &cfg), "failed to unmarshal config.json")
	assert.Equal(t, "claude-code", cfg.Defaults.Runner)
	assert.Equal(t, "code", cfg.Defaults.Editor)
	assert.Equal(t, map[string]string{
		"claude-code": "claude",
		"codex":       "codex",
	}, cfg.Runners)
	assert.Contains(t, stdout.String(), "user_config: overwritten")
}
