package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEffectiveRunnerArgs_ClaudeAppendsTypedFlagsAndKeepsPassthrough(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"haiku",
		"high",
		"auto",
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "haiku", "--effort", "high", "--permission-mode", "auto"}, got)
}

func TestResolveEffectiveRunnerArgs_ClaudeLeavesTypedFlagsEmptyWhenUnset(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"",
		"",
		"",
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra"}, got)
}

func TestResolveEffectiveRunnerArgs_CodexAppendsConfigFlag(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"codex",
		[]string{"--allowed-extra"},
		"gpt-5-codex",
		"high",
		"",
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "gpt-5-codex", "--config", "model_reasoning_effort=high"}, got)
}

func TestResolveEffectiveRunnerArgs_CursorAppendsModelOnly(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"sonnet-4.6-thinking",
		"",
		"",
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--model", "sonnet-4.6-thinking"}, got)
}

func TestResolveEffectiveRunnerArgs_NonTypedRunnerRejectsTypedFlags(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"amp",
		[]string{"--allowed-extra"},
		"amp-fast",
		"",
		"",
		false,
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "supported for runners")
}

func TestResolveEffectiveRunnerArgs_CursorRejectsEffort(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"cursor",
		[]string{"--allowed-extra"},
		"sonnet-4.6-thinking",
		"high",
		"",
		false,
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not supported for runner cursor")
}

func TestResolveEffectiveRunnerArgs_ClaudeRejectsOwnedPassthroughFlags(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--model=opus", "--foo", "bar"},
		"",
		"",
		"",
		false,
	)
	require.Error(t, err)
	assert.Equal(t, errors.ERunnerArgConflict, errors.GetCode(err))
	assert.Contains(t, err.Error(), "reserved flag '--model'")
}

func TestResolveEffectiveRunnerArgs_ClaudeHeadlessDefaultsToBypassPermissions(t *testing.T) {
	t.Parallel()

	got, err := resolveEffectiveRunnerArgs(
		"claude-code",
		[]string{"--allowed-extra"},
		"",
		"",
		"",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"--allowed-extra", "--permission-mode", "bypassPermissions"}, got)
}

func TestResolveEffectiveRunnerArgs_ClaudeHeadlessRejectsPromptingPermissionModes(t *testing.T) {
	t.Parallel()

	_, err := resolveEffectiveRunnerArgs(
		"claude-code",
		nil,
		"",
		"",
		"default",
		true,
	)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "headless Claude requires an autonomous permission mode")
}

func TestResolveStartRunnerAndArgs_UsesSharedDefaultPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	repoRoot := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.MkdirAll(repoRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{
  "version": 3,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  },
  "runner_defaults": {
    "claude-code": {
      "model": "user-opus",
      "effort": "low",
      "permission_mode": "auto"
    }
  },
  "runners": {
    "claude-code": "claude"
  },
  "editors": {
    "code": "code"
  }
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(`{
  "version": 3,
  "scripts": {
    "setup": {
      "path": "scripts/setup.sh"
    },
    "verify": {
      "path": "scripts/verify.sh"
    },
    "archive": {
      "path": "scripts/archive.sh"
    }
  },
  "runner_defaults": {
    "claude-code": {
      "model": "agency-opus",
      "effort": "max"
    }
  }
}`), 0o644))

	runner, args, err := resolveStartRunnerAndArgs(context.Background(), fs.NewRealFS(), repoRoot, &daemonNavSetup{
		dirs: paths.Dirs{ConfigDir: configDir},
	}, repoRoot, "repo-1", startRunnerConfigOpts{
		Model: "cli-opus",
	})
	require.NoError(t, err)
	assert.Equal(t, "claude-code", runner)
	assert.Equal(t, []string{"--model", "cli-opus", "--effort", "max", "--permission-mode", "auto"}, args)
}
