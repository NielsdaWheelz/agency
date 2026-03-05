package cobra

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type compatibilityAdapterEnv struct {
	DataDir      string
	ConfigDir    string
	RepoID       string
	WorktreeID   string
	InvocationID string
	SandboxPath  string
}

func setupCompatibilityAdapterEnv(t *testing.T, invocationIDs []string) compatibilityAdapterEnv {
	t.Helper()
	require.NotEmpty(t, invocationIDs)

	dataDir, err := os.MkdirTemp("", "ca")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	repoID := "r1"
	worktreeID := "20260131120000-abcd"
	worktreeDir := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID)
	worktreeTreeDir := filepath.Join(worktreeDir, "tree")
	require.NoError(t, os.MkdirAll(worktreeTreeDir, 0o755))

	agencyDir := filepath.Join(worktreeTreeDir, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0o644,
	))

	worktreeMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    worktreeID,
		Name:          "compat-test",
		RepoID:        repoID,
		Branch:        "agency/compat-test-abcd",
		ParentBranch:  "main",
		TreePath:      worktreeTreeDir,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	worktreeMetaBytes, _ := json.MarshalIndent(worktreeMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "meta.json"), worktreeMetaBytes, 0o644))

	sandboxPath := ""
	for i, invocationID := range invocationIDs {
		invocationDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
		require.NoError(t, os.MkdirAll(invocationDir, 0o755))

		sandboxTree := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0o755))
		if i == 0 {
			sandboxPath = sandboxTree
		}

		invMeta := &store.InvocationMeta{
			SchemaVersion:         "1.0",
			InvocationID:          invocationID,
			IntegrationWorktreeID: worktreeID,
			SandboxPath:           sandboxTree,
			SandboxBranch:         "agency/sandbox-" + invocationID,
			BaseCommit:            "abc123def456",
			Runner:                "claude",
			Mode:                  store.RunnerModeHeaded,
			StartedAt:             "2026-01-31T13:00:00Z",
			Status:                store.InvocationStatusRunning,
		}
		invMetaBytes, _ := json.MarshalIndent(invMeta, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(invocationDir, "meta.json"), invMetaBytes, 0o644))
	}

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)
	srv := daemon.NewServer(st, testutil.NewFakeCommandRunner(), fsys, configDir)

	socketPath := st.DaemonSocketPath()
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	client := daemonclient.NewClient(socketPath)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second), "daemon not ready")

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	return compatibilityAdapterEnv{
		DataDir:      dataDir,
		ConfigDir:    configDir,
		RepoID:       repoID,
		WorktreeID:   worktreeID,
		InvocationID: invocationIDs[0],
		SandboxPath:  sandboxPath,
	}
}

func TestLegacyPath_CompatibilityAdapter_DelegatesToAgentPath(t *testing.T) {
	env := setupCompatibilityAdapterEnv(t, []string{"20260131130000-efgh"})

	stdout, _, err := executeCmd("path", "--repo", env.RepoID, env.InvocationID)
	require.NoError(t, err)
	assert.Equal(t, env.SandboxPath+"\n", stdout)
}

func TestLegacyOpen_CompatibilityAdapter_AmbiguityUsesEAmbiguous(t *testing.T) {
	env := setupCompatibilityAdapterEnv(t, []string{"20260131130000-aaaa", "20260131130000-bbbb"})

	_, _, err := executeCmd("open", "--repo", env.RepoID, "20260131130000")
	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err))
}

func TestLegacyAttach_CompatibilityAdapter_EnforcesTTYPreflight(t *testing.T) {
	env := setupCompatibilityAdapterEnv(t, []string{"20260131130000-efgh"})

	_, _, err := executeCmd("attach", "--repo", env.RepoID, env.InvocationID)
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
}

// ---------------------------------------------------------------------------
// open: invocation-first fallback to legacy run resolution
// ---------------------------------------------------------------------------

// TestLegacyOpen_FallsBackToRunResolution verifies that when the ref does not
// match any invocation the command falls through to legacy run resolution.
// We prove the fallback executed by checking it finds the run and attempts to
// open the editor (which is a shim that exits 0).
func TestLegacyOpen_FallsBackToRunResolution(t *testing.T) {
	// Daemon has one invocation, but NOT matching our run name.
	env := setupCompatibilityAdapterEnv(t, []string{"20260131130000-efgh"})

	// Create a legacy run record that only the fallback path can find.
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, env.DataDir, time.Now)

	runID := "20260115120000-a3f2"
	runName := "my-legacy-run"
	worktreePath := filepath.Join(env.DataDir, "test-worktree")
	require.NoError(t, os.MkdirAll(worktreePath, 0o755))

	_, err := st.EnsureRunDir(env.RepoID, runID)
	require.NoError(t, err)

	meta := store.NewRunMeta(runID, env.RepoID, runName, "claude", "claude",
		"main", "agency/test-a3f2", worktreePath, time.Now())
	require.NoError(t, st.WriteInitialMeta(env.RepoID, runID, meta))

	// Write a no-op editor shim so the editor launch succeeds.
	shimPath := filepath.Join(env.ConfigDir, "bin", "editor")
	require.NoError(t, os.MkdirAll(filepath.Dir(shimPath), 0o755))
	require.NoError(t, os.WriteFile(shimPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	cfg := `{"version": 1, "defaults": {"runner": "claude", "editor": "shim"}, "editors": {"shim": "./bin/editor"}}`
	require.NoError(t, os.WriteFile(filepath.Join(env.ConfigDir, "config.json"), []byte(cfg), 0o644))

	// Pass --repo so the daemon path can resolve repo context (test CWD is
	// a temp dir, not a git repo). The invocation lookup fails with
	// E_INVOCATION_NOT_FOUND, triggering the fallback to run resolution.
	_, _, err = executeCmd("open", "--repo", env.RepoID, runName)
	require.NoError(t, err, "open should fall back to run resolution and succeed")
}

// TestLegacyOpen_FallbackReturnsRunNotFound verifies that when the ref matches
// neither an invocation nor a run, the final error is E_RUN_NOT_FOUND from
// the legacy fallback (not E_INVOCATION_NOT_FOUND from the daemon path).
func TestLegacyOpen_FallbackReturnsRunNotFound(t *testing.T) {
	env := setupCompatibilityAdapterEnv(t, []string{"20260131130000-efgh"})

	// Write editor config so the fallback doesn't fail on config resolution.
	shimPath := filepath.Join(env.ConfigDir, "bin", "editor")
	require.NoError(t, os.MkdirAll(filepath.Dir(shimPath), 0o755))
	require.NoError(t, os.WriteFile(shimPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	cfg := `{"version": 1, "defaults": {"runner": "claude", "editor": "shim"}, "editors": {"shim": "./bin/editor"}}`
	require.NoError(t, os.WriteFile(filepath.Join(env.ConfigDir, "config.json"), []byte(cfg), 0o644))

	_, _, err := executeCmd("open", "--repo", env.RepoID, "nonexistent-thing")
	require.Error(t, err)
	assert.Equal(t, errors.ERunNotFound, errors.GetCode(err),
		"should propagate E_RUN_NOT_FOUND from legacy fallback, not E_INVOCATION_NOT_FOUND")
}

// TestLegacyOpen_AmbiguousInvocationDoesNotFallBack verifies that ambiguous
// invocation matches do NOT fall through to run resolution. Ambiguity is an
// authoritative error from the daemon path.
func TestLegacyOpen_AmbiguousInvocationDoesNotFallBack(t *testing.T) {
	// Create two invocations that share a prefix → ambiguous.
	env := setupCompatibilityAdapterEnv(t, []string{"20260131130000-aaaa", "20260131130000-bbbb"})

	// Also create a legacy run with the same prefix, to prove we DON'T fall back.
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, env.DataDir, time.Now)
	runID := "20260131130000-cccc"
	worktreePath := filepath.Join(env.DataDir, "test-worktree")
	require.NoError(t, os.MkdirAll(worktreePath, 0o755))
	_, err := st.EnsureRunDir(env.RepoID, runID)
	require.NoError(t, err)
	meta := store.NewRunMeta(runID, env.RepoID, "20260131130000", "claude", "claude",
		"main", "agency/test", worktreePath, time.Now())
	require.NoError(t, st.WriteInitialMeta(env.RepoID, runID, meta))

	_, _, err = executeCmd("open", "--repo", env.RepoID, "20260131130000")
	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err),
		"ambiguous invocation must not fall back to run resolution")
}
