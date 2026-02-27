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
