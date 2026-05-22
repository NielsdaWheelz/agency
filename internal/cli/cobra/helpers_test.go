package cobra

import (
	"bytes"
	"path/filepath"
	"testing"
)

func executeCmd(args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	rootCmd := newRootCmd()
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func setIsolatedCompletionEnv(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "config")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	return dataDir, configDir
}
