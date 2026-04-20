package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRepo creates a temporary git repo with agency.json and executable scripts.
// Returns the repo root path. Cleanup is handled automatically by t.TempDir().
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0o755), "failed to create .git dir")

	scriptsDir := filepath.Join(tmpDir, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755), "failed to create scripts dir")

	// Create agency.json
	agencyJSON := `{
  "version": 2,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agency.json"), []byte(agencyJSON), 0o644), "failed to write agency.json")

	stubScript := "#!/usr/bin/env bash\nexit 0\n"
	scripts := []string{"agency_setup.sh", "agency_verify.sh", "agency_archive.sh"}
	for _, script := range scripts {
		path := filepath.Join(scriptsDir, script)
		require.NoError(t, os.WriteFile(path, []byte(stubScript), 0o755), "failed to write script %s", script)
	}

	return tmpDir
}

func writeUserConfig(t *testing.T, configDir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(configDir, 0o755), "failed to create config dir")
	cfg := `{
  "version": 2,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  },
  "runner_defaults": {
    "claude-code": {
      "model": "user-opus",
      "effort": "max"
    }
  },
  "runners": {
    "claude-code": "claude",
    "codex": "codex"
  },
  "editors": {
    "code": "code"
  }
	}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644), "failed to write config.json")
}

func writeLocalAgencyConfig(t *testing.T, agencyJSONPath string) {
	t.Helper()

	root := filepath.Dir(agencyJSONPath)
	scriptsDir := filepath.Join(root, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755), "failed to create local scripts dir")
	require.NoError(t, os.MkdirAll(root, 0o755), "failed to create local config dir")

	agencyJSON := `{
  "version": 2,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  },
  "runner_defaults": {
    "claude-code": {
      "model": "local-opus",
      "effort": "high"
    }
  }
}`
	require.NoError(t, os.WriteFile(agencyJSONPath, []byte(agencyJSON), 0o644), "failed to write local agency.json")

	stubScript := "#!/usr/bin/env bash\nexit 0\n"
	for _, script := range []string{"agency_setup.sh", "agency_verify.sh", "agency_archive.sh"} {
		require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, script), []byte(stubScript), 0o755), "failed to write script %s", script)
	}
}

func newDoctorRunner(repoRoot string) *testutil.FakeCommandRunner {
	runner := testutil.NewFakeCommandRunner()
	runner.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{
		Stdout:   repoRoot + "\n",
		ExitCode: 0,
	}
	runner.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{
		Stdout:   "git@github.com:testowner/testrepo.git\n",
		ExitCode: 0,
	}
	runner.Responses["git --version"] = testutil.FakeResponse{
		Stdout:   "git version 2.40.0\n",
		ExitCode: 0,
	}
	runner.Responses["git branch --show-current"] = testutil.FakeResponse{
		Stdout:   "main\n",
		ExitCode: 0,
	}
	runner.Responses["tmux -V"] = testutil.FakeResponse{
		Stdout:   "tmux 3.3a\n",
		ExitCode: 0,
	}
	runner.Responses["gh --version"] = testutil.FakeResponse{
		Stdout:   "gh version 2.40.0 (2024-01-15)\nhttps://github.com/cli/cli/releases/tag/v2.40.0\n",
		ExitCode: 0,
	}
	runner.Responses["gh auth status"] = testutil.FakeResponse{
		ExitCode: 0,
	}
	return runner
}

func TestDoctor_Success(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	// Create temp data dir (t.TempDir handles cleanup automatically)
	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	// Setup mock
	m := newDoctorRunner(repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.NoError(t, err, "doctor failed")

	output := stdout.String()

	// Check key output lines
	expectedLines := []string{
		"repo_root: " + repoRoot,
		"agency_data_dir: " + dataDir,
		"agency_config_dir: " + configDir,
		"user_config_path: " + filepath.Join(configDir, "config.json"),
		"agency_json_path: " + filepath.Join(repoRoot, "agency.json"),
		"agency_json_source: repo",
		"repo_key: github:testowner/testrepo",
		"origin_present: true",
		"origin_url: git@github.com:testowner/testrepo.git",
		"origin_host: github.com",
		"github_flow_available: true",
		"git_version: git version 2.40.0",
		"tmux_version: tmux 3.3a",
		"gh_version: gh version 2.40.0 (2024-01-15)",
		"gh_authenticated: true",
		"defaults_base_branch: main",
		"defaults_runner: claude-code",
		"defaults_runner_model: user-opus",
		"defaults_runner_model_source: user",
		"defaults_runner_effort: max",
		"defaults_runner_effort_source: user",
		"defaults_editor: code",
		"runner_cmd: /usr/bin/claude",
		"status: ok",
	}

	for _, line := range expectedLines {
		assert.Contains(t, output, line, "output missing expected line")
	}

	entries, err := os.ReadDir(dataDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "doctor should not write files on success")
}

func TestDoctor_MissingUserConfig(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()
	configDir := t.TempDir()

	m := newDoctorRunner(repoRoot)
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.ENoUserConfig, errors.GetCode(err))
	assert.Empty(t, stdout.String())

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(configDir, "config.json"), ae.Details["path"])
	assert.Equal(t, "run `agency config init`", ae.Details["hint"])

	entries, readErr := os.ReadDir(dataDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "doctor should stay read-only when config is missing")
}

func TestDoctor_UsesLocalAgencyConfigWhenRepoHasNone(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)
	require.NoError(t, os.Remove(filepath.Join(repoRoot, "agency.json")))

	dataDir := t.TempDir()
	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	repoID := identity.DeriveRepoIdentity(repoRoot, "git@github.com:testowner/testrepo.git").RepoID
	agencyJSONPath := config.LocalAgencyConfigPath(configDir, repoID)
	writeLocalAgencyConfig(t, agencyJSONPath)

	m := newDoctorRunner(repoRoot)
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "agency_json_path: "+agencyJSONPath)
	assert.Contains(t, output, "agency_json_source: local")
	assert.Contains(t, output, "defaults_runner_model: local-opus")
	assert.Contains(t, output, "defaults_runner_model_source: local")
	assert.Contains(t, output, "defaults_runner_effort: high")
	assert.Contains(t, output, "defaults_runner_effort_source: local")
	assert.Contains(t, output, "script_setup: "+filepath.Join(filepath.Dir(agencyJSONPath), "scripts", "agency_setup.sh"))
	assert.Contains(t, output, "script_verify: "+filepath.Join(filepath.Dir(agencyJSONPath), "scripts", "agency_verify.sh"))
	assert.Contains(t, output, "script_archive: "+filepath.Join(filepath.Dir(agencyJSONPath), "scripts", "agency_archive.sh"))
}

func TestDoctor_InvalidAgencyConfigIncludesPathSourceAndHint(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(`{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh"
    },
    "verify": {
      "path": "scripts/agency_verify.sh"
    },
    "archive": {
      "path": "scripts/agency_archive.sh"
    }
  }
}`), 0o644))

	dataDir := t.TempDir()
	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)
	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
	assert.Empty(t, stdout.String())

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(repoRoot, "agency.json"), ae.Details["path"])
	assert.Equal(t, "repo", ae.Details["source"])
	assert.Contains(t, ae.Details["hint"], "agency init --path "+repoRoot+" --repo-config --force")
}

func TestDoctor_GhNotAuthenticated(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	// Create temp data dir (t.TempDir handles cleanup automatically)
	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)
	m.Responses["gh auth status"] = testutil.FakeResponse{
		Stderr:   "You are not logged in",
		ExitCode: 1,
	}

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.Error(t, err, "expected error for unauthenticated gh")

	assert.Contains(t, err.Error(), "E_GH_NOT_AUTHENTICATED")

	// stdout should be empty on failure
	assert.Empty(t, stdout.String(), "stdout should be empty on failure")

	// Persistence files should NOT be created on failure
	repoIndexPath := filepath.Join(dataDir, "repo_index.json")
	assert.NoFileExists(t, repoIndexPath, "repo_index.json should not be created on failure")
}

func TestDoctor_ScriptNotExecutable(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	// Make setup script non-executable
	setupScript := filepath.Join(repoRoot, "scripts", "agency_setup.sh")
	require.NoError(t, os.Chmod(setupScript, 0644), "failed to chmod script")

	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.Error(t, err, "expected error for non-executable script")

	assert.Contains(t, err.Error(), "E_SCRIPT_NOT_EXECUTABLE")
	assert.Contains(t, err.Error(), "chmod +x", "expected chmod hint in error")
}

func TestDoctor_ScriptMissing(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	// Remove setup script
	setupScript := filepath.Join(repoRoot, "scripts", "agency_setup.sh")
	require.NoError(t, os.Remove(setupScript), "failed to remove script")

	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.Error(t, err, "expected error for missing script")

	assert.Contains(t, err.Error(), "E_SCRIPT_NOT_FOUND")
}

func TestDoctor_NoGitHubOrigin(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)
	m.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{
		ExitCode: 1,
	}

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	// Doctor should still succeed with missing origin
	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.NoError(t, err, "doctor should succeed without GitHub origin")

	output := stdout.String()

	assert.Contains(t, output, "github_flow_available: false")
	assert.Contains(t, output, "origin_present: false")
	assert.Contains(t, output, "repo_key: path:", "expected path-based repo_key")
	assert.Contains(t, output, "status: ok")
}

func TestDoctor_IsReadOnly(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)

	fsys := fs.NewRealFS()

	// Run doctor twice
	var stdout1, stderr1 bytes.Buffer
	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout1, &stderr1)
	require.NoError(t, err, "first doctor run failed")

	var stdout2, stderr2 bytes.Buffer
	err = Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout2, &stderr2)
	require.NoError(t, err, "second doctor run failed")

	entries, readErr := os.ReadDir(dataDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "doctor should not persist repo data")
}

func TestDoctor_OutputOrder(t *testing.T) {
	t.Parallel()
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()

	configDir := t.TempDir()
	writeUserConfig(t, configDir)

	m := newDoctorRunner(repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{DataDirOverride: dataDir, ConfigDirOverride: configDir}, &stdout, &stderr)
	require.NoError(t, err, "doctor failed")

	output := stdout.String()
	lines := strings.Split(output, "\n")

	// Verify key order per spec
	expectedKeyOrder := []string{
		"repo_root:",
		"agency_data_dir:",
		"agency_config_dir:",
		"user_config_path:",
		"agency_json_path:",
		"agency_json_source:",
		"agency_cache_dir:",
		"repo_key:",
		"repo_id:",
		"origin_present:",
		"origin_url:",
		"origin_host:",
		"github_flow_available:",
		"git_version:",
		"tmux_version:",
		"gh_version:",
		"gh_authenticated:",
		"defaults_base_branch:",
		"defaults_runner:",
		"defaults_runner_model:",
		"defaults_runner_model_source:",
		"defaults_runner_effort:",
		"defaults_runner_effort_source:",
		"defaults_editor:",
		"runner_cmd:",
		"script_setup:",
		"script_verify:",
		"script_archive:",
		"status:",
	}

	keyIndex := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		if !assert.Less(t, keyIndex, len(expectedKeyOrder), "unexpected extra line: %s", line) {
			continue
		}
		assert.True(t, strings.HasPrefix(line, expectedKeyOrder[keyIndex]),
			"line %d: expected prefix %q, got %q", keyIndex, expectedKeyOrder[keyIndex], line)
		keyIndex++
	}

	assert.Equal(t, len(expectedKeyOrder), keyIndex, "expected %d lines, got %d", len(expectedKeyOrder), keyIndex)
}
