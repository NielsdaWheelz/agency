package cobra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigInitCmd_RunsOutsideRepoThroughRootCommand(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "agency-config")
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755), "failed to create fake bin dir")
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755), "failed to create fake codex")
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "code"), []byte("#!/bin/sh\nexit 0\n"), 0o755), "failed to create fake code")

	t.Setenv("PATH", binDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get cwd")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWd), "failed to restore cwd")
	})
	require.NoError(t, os.Chdir(tmpDir), "failed to chdir")

	stdout, _, err := executeCmd("config", "init")
	require.NoError(t, err, "expected config init to succeed outside a repo")
	if !strings.Contains(stdout, "user_config: created") {
		t.Fatalf("config init stdout missing created message:\n%s", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(configDir, "config.json")); statErr != nil {
		t.Fatalf("config file should exist: %v", statErr)
	}
}
