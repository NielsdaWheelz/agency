package cobra

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestCompletionDynamicRepoFlagWithoutDaemonReturnsHelp(t *testing.T) {
	dataDir, configDir := setIsolatedCompletionEnv(t)

	stdout, _, err := executeCmd("__complete", "agent", "start", "--repo", "ag")
	require.NoError(t, err, "expected repo completion without daemon to return active help")
	if !strings.Contains(stdout, "register a repo first with: agency repo add /path/to/repo") {
		t.Fatalf("completion output missing active help:\n%s", stdout)
	}
	require.NoDirExists(t, dataDir)
	require.NoDirExists(t, configDir)
}

func TestCompletionDynamicRepoFlag(t *testing.T) {
	dataDir, configDir, client := startCompletionTestDaemon(t)
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	repoDir := setupCompletionTestRepo(t)
	_, err := client.RegisterRepo(context.Background(), repoDir)
	require.NoError(t, err, "expected repo registration to succeed")

	stdout, _, err := executeCmd("__complete", "agent", "start", "--repo", "ag")
	require.NoError(t, err, "expected dynamic repo completion to succeed")
	if !strings.Contains(stdout, "agency") {
		t.Fatalf("repo completion output missing agency:\n%s", stdout)
	}
}

func startCompletionTestDaemon(t *testing.T) (string, string, *daemonclient.Client) {
	t.Helper()

	dataDir, err := os.MkdirTemp("/tmp", "acd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	cfg := map[string]any{
		"version": 4,
		"defaults": map[string]string{
			"runner":            "claude-code",
			"editor":            "code",
			"execution_profile": "personal",
		},
		"runners": map[string]string{
			"claude-code": "/bin/echo",
		},
		"execution_profiles": map[string]any{
			"personal": map[string]any{
				"env": map[string]string{},
			},
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644))

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)
	srv := daemon.NewServer(st, exec.NewRealRunner(), fsys, configDir)

	socketPath := st.DaemonSocketPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0o755))
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	t.Cleanup(func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
		select {
		case <-serveDone:
		case <-time.After(5 * time.Second):
			t.Error("completion test daemon did not stop")
		}
	})

	client := daemonclient.NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.WaitForReady(ctx, 5*time.Second), "daemon not ready")

	return dataDir, configDir, client
}

func setupCompletionTestRepo(t *testing.T) string {
	t.Helper()
	testutil.HermeticGitEnv(t)

	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git init: %s", result.Stderr)

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0o644))

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "init"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git commit: %s", result.Stderr)

	result, err = cr.Run(ctx, "git", []string{"remote", "add", "origin", "git@github.com:owner/agency.git"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git remote add origin: %s", result.Stderr)

	return repoDir
}
