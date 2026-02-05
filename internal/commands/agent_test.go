// Package commands implements agency CLI commands.
// This file tests agent commands for headed execution (Slice 8 PR-03).
package commands

import (
	"bytes"
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
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAgentTestEnv creates a test environment with integration worktree for agent tests.
func setupAgentTestEnv(t *testing.T, worktreeName string) (string, string, string, string, *testutil.FakeCommandRunner, fs.FS) {
	t.Helper()

	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	// Initialize git repo (minimal)
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0755))

	originURL := "git@github.com:test/agent-repo.git"
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	// Create fake command runner
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: originURL + "\n"}

	fsys := fs.NewRealFS()

	// Create store directories
	repoStoreDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoStoreDir, 0755))

	// Create integration worktree
	worktreeID := "20260131120000-abcd"
	worktreeDir := filepath.Join(repoStoreDir, "integration_worktrees", worktreeID)
	worktreeTreeDir := filepath.Join(worktreeDir, "tree")
	require.NoError(t, os.MkdirAll(worktreeTreeDir, 0755))

	// Write integration marker
	agencyDir := filepath.Join(worktreeTreeDir, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	markerPath := filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName)
	require.NoError(t, os.WriteFile(markerPath, []byte("# Integration worktree\n"), 0644))

	// Write integration worktree meta.json
	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    worktreeID,
		Name:          worktreeName,
		RepoID:        repoID,
		Branch:        "agency/" + worktreeName + "-abcd",
		ParentBranch:  "main",
		TreePath:      worktreeTreeDir,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	metaBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	metaPath := filepath.Join(worktreeDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))

	return repoDir, dataDir, repoID, worktreeID, cr, fsys
}

// createTestInvocation creates a test invocation for testing attach/stop/kill.
func createTestInvocation(t *testing.T, dataDir, repoID, worktreeID, invocationID string, mode store.RunnerMode, status store.InvocationStatus) {
	t.Helper()

	invDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	require.NoError(t, os.MkdirAll(invDir, 0755))

	sandboxDir := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID)
	sandboxTreeDir := filepath.Join(sandboxDir, "tree")
	require.NoError(t, os.MkdirAll(sandboxTreeDir, 0755))

	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: worktreeID,
		SandboxPath:           sandboxTreeDir,
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123def456",
		Runner:                "claude",
		Mode:                  mode,
		StartedAt:             time.Now().UTC().Format(time.RFC3339),
		Status:                status,
	}
	if mode == store.RunnerModeHeaded {
		meta.TmuxSession = tmux.SessionName(invocationID)
	}

	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	metaPath := filepath.Join(invDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))
}

// setupAgentTestEnvShort creates a test environment using a short dataDir (for socket
// path length safety on macOS) and starts a test daemon. Returns all the same values as
// setupAgentTestEnv plus the daemon socket path.
//
// Why os.MkdirTemp instead of t.TempDir(): Unix domain sockets on macOS have a
// ~104-byte path limit. t.TempDir() embeds the full test name, easily exceeding
// that. We use short prefixes ("ar"/"ad") and clean up via t.Cleanup.
func setupAgentTestEnvShort(t *testing.T, worktreeName string) (string, string, string, string, *testutil.FakeCommandRunner, fs.FS) {
	t.Helper()

	repoTmp, err := os.MkdirTemp("", "ar")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(repoTmp) })

	dataTmp, err := os.MkdirTemp("", "ad")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoDir := filepath.Join(repoTmp, "r")
	dataDir := dataTmp

	require.NoError(t, os.MkdirAll(repoDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0755))

	originURL := "git@github.com:test/agent-repo.git"
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: originURL + "\n"}

	fsys := fs.NewRealFS()

	// Create store directories
	repoStoreDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoStoreDir, 0755))

	// Create integration worktree
	worktreeID := "20260131120000-abcd"
	worktreeDir := filepath.Join(repoStoreDir, "integration_worktrees", worktreeID)
	worktreeTreeDir := filepath.Join(worktreeDir, "tree")
	require.NoError(t, os.MkdirAll(worktreeTreeDir, 0755))

	agencyDir := filepath.Join(worktreeTreeDir, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	markerPath := filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName)
	require.NoError(t, os.WriteFile(markerPath, []byte("# Integration worktree\n"), 0644))

	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    worktreeID,
		Name:          worktreeName,
		RepoID:        repoID,
		Branch:        "agency/" + worktreeName + "-abcd",
		ParentBranch:  "main",
		TreePath:      worktreeTreeDir,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	metaBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	metaPath := filepath.Join(worktreeDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))

	// Start test daemon
	st := store.NewStore(fsys, dataDir, time.Now)
	configDir := filepath.Join(dataDir, "config")
	srv := daemon.NewServer(st, cr, fsys, configDir)

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

	// Wait for daemon readiness using the client's WaitForReady instead of raw time.Sleep.
	client := daemonclient.NewClient(socketPath)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second), "daemon not ready")

	return repoDir, dataDir, repoID, worktreeID, cr, fsys
}

func TestAgentAttach_HeadlessInvocation_ReturnsInvalidMode(t *testing.T) {
	t.Parallel()
	// Use short paths to stay under macOS socket 104-byte limit
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headless invocation (after daemon is started)
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	// Need a separate FakeCommandRunner for the client call (daemon has its own)
	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef:   invocationID,
		TmuxClient:      fakeTmux,
		IsInteractive:   func() bool { return true },
		DataDirOverride: dataDir,
	}

	err := AgentAttach(context.Background(), cr2, fsys, repoDir, opts, &stdout, &stderr)
	require.Error(t, err, "AgentAttach error = nil, want E_INVOCATION_INVALID_MODE")

	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))
}

func TestAgentAttach_HeadedInvocation_SessionMissing(t *testing.T) {
	t.Parallel()
	// Use short paths to stay under macOS socket 104-byte limit
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed invocation (after daemon is started)
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef:   invocationID,
		TmuxClient:      fakeTmux,
		IsInteractive:   func() bool { return true },
		DataDirOverride: dataDir,
	}

	err := AgentAttach(context.Background(), cr2, fsys, repoDir, opts, &stdout, &stderr)
	require.Error(t, err, "AgentAttach error = nil, want E_SESSION_ENDED")

	assert.Equal(t, errors.ESessionEnded, errors.GetCode(err))
}
