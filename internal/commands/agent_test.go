// Package commands implements agency CLI commands.
// This file tests agent commands for headed execution (Slice 8 PR-03).
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
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

func TestAgentAttach_AmbiguityUsesEAmbiguous_NoDispatch(t *testing.T) {
	env := setupAgentNavEnv(t, "attach-ambig", store.RunnerModeHeaded)

	secondID := "20260131130000-zzzz"
	secondInvDir := filepath.Join(env.DataDir, "repos", env.RepoID, "invocations", secondID)
	require.NoError(t, os.MkdirAll(secondInvDir, 0o755))

	secondSandbox := filepath.Join(env.DataDir, "repos", env.RepoID, "sandboxes", secondID, "tree")
	require.NoError(t, os.MkdirAll(secondSandbox, 0o755))

	secondMeta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          secondID,
		IntegrationWorktreeID: env.WorktreeID,
		SandboxPath:           secondSandbox,
		SandboxBranch:         "agency/sandbox-" + secondID,
		BaseCommit:            "abc123def456",
		Runner:                "claude",
		Mode:                  store.RunnerModeHeaded,
		StartedAt:             "2026-01-31T13:10:00Z",
		Status:                store.InvocationStatusRunning,
	}
	secondMetaBytes, _ := json.MarshalIndent(secondMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(secondInvDir, "meta.json"), secondMetaBytes, 0o644))

	fakeTmux := testutil.NewFakeTmuxClient()
	var stdout, stderr bytes.Buffer
	err := AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentAttachOpts{
			InvocationRef:   "20260131130000",
			RepoFlag:        env.RepoID,
			TmuxClient:      fakeTmux,
			IsInteractive:   func() bool { return true },
			DataDirOverride: env.DataDir,
		}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err),
		"compatibility attach should use shared navigation ambiguity semantics")
	assert.Len(t, fakeTmux.HasSessionCalls, 0, "tmux preflight must not run for ambiguous targets")

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "invocation", ae.Details["target_kind"])
	assert.Equal(t, "2", ae.Details["candidate_count"])
}

// ---------------------------------------------------------------------------
// S2-PR04: Agent navigation convergence — setup helper
// ---------------------------------------------------------------------------

type agentNavTestEnv struct {
	DataDir      string
	RepoID       string
	WorktreeID   string
	InvocationID string
	SandboxPath  string
}

// setupAgentNavEnv creates a test environment with a daemon, one integration
// worktree, and one invocation. Uses t.Setenv for AGENCY_DATA_DIR / AGENCY_CONFIG_DIR
// so tests must NOT be marked t.Parallel().
func setupAgentNavEnv(t *testing.T, name string, mode store.RunnerMode) agentNavTestEnv {
	t.Helper()

	dataTmp, err := os.MkdirTemp("", "an")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID := "20260131120000-abcd"
	invID := "20260131130000-efgh"

	// Create integration worktree in store
	wtDir := filepath.Join(dataTmp, "repos", repoID, "integration_worktrees", wtID)
	treePath := filepath.Join(wtDir, "tree")
	require.NoError(t, os.MkdirAll(treePath, 0755))

	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0644))

	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    wtID,
		Name:          name,
		RepoID:        repoID,
		Branch:        "agency/" + name + "-abcd",
		ParentBranch:  "main",
		TreePath:      treePath,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	metaBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), metaBytes, 0644))

	// Create invocation
	invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", invID)
	require.NoError(t, os.MkdirAll(invDir, 0755))

	sandboxDir := filepath.Join(dataTmp, "repos", repoID, "sandboxes", invID)
	sandboxTreeDir := filepath.Join(sandboxDir, "tree")
	require.NoError(t, os.MkdirAll(sandboxTreeDir, 0755))

	invMeta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invID,
		IntegrationWorktreeID: wtID,
		SandboxPath:           sandboxTreeDir,
		SandboxBranch:         "agency/sandbox-" + invID,
		BaseCommit:            "abc123def456",
		Runner:                "claude",
		Mode:                  mode,
		StartedAt:             "2026-01-31T13:00:00Z",
		Status:                store.InvocationStatusRunning,
	}
	if mode == store.RunnerModeHeaded {
		invMeta.TmuxSession = tmux.SessionName(invID)
	}
	imBytes, _ := json.MarshalIndent(invMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imBytes, 0644))

	// Start daemon
	fsys := fs.NewRealFS()
	cr := testutil.NewFakeCommandRunner()
	st := store.NewStore(fsys, dataTmp, time.Now)
	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	srv := daemon.NewServer(st, cr, fsys, configDir)

	socketPath := st.DaemonSocketPath()
	listener, listenErr := net.Listen("unix", socketPath)
	require.NoError(t, listenErr)

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

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	return agentNavTestEnv{
		DataDir:      dataTmp,
		RepoID:       repoID,
		WorktreeID:   wtID,
		InvocationID: invID,
		SandboxPath:  sandboxTreeDir,
	}
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 1: agent ls/show daemon-of-record read behavior
// ---------------------------------------------------------------------------

func TestAgentLS_DaemonOfRecord_RendersDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "ls-test", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, env.InvocationID)
	assert.Contains(t, out, "claude")
	assert.Contains(t, out, "headed")
}

func TestAgentShow_DaemonOfRecord_RendersDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "show-test", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShowOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "invocation_id:          "+env.InvocationID)
	assert.Contains(t, out, "worktree_id:            "+env.WorktreeID)
	assert.Contains(t, out, "runner:                 claude")
	assert.Contains(t, out, "mode:                   headed")
	assert.Contains(t, out, "sandbox_path:           "+env.SandboxPath)
}

func TestAgentLS_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "lsjson", store.RunnerModeHeadless)

	var stdout, stderr bytes.Buffer
	err := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoFlag: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dtos []daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dtos))
	require.Len(t, dtos, 1)

	assert.Equal(t, env.InvocationID, dtos[0].InvocationID)
	assert.Equal(t, env.RepoID, dtos[0].RepoID)
	assert.Equal(t, "claude", dtos[0].Runner)
	assert.Equal(t, "headless", dtos[0].Mode)
	assert.Equal(t, env.SandboxPath, dtos[0].SandboxPath)
}

func TestAgentShow_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "showjson", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShowOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dto daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dto))

	assert.Equal(t, env.InvocationID, dto.InvocationID)
	assert.Equal(t, env.RepoID, dto.RepoID)
	assert.Equal(t, "claude", dto.Runner)
	assert.Equal(t, env.SandboxPath, dto.SandboxPath)
}

func TestAgentShow_AmbiguousPreservesCandidates(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "an")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID := "20260131120000-abcd"

	// Create worktree
	wtDir := filepath.Join(dataTmp, "repos", repoID, "integration_worktrees", wtID)
	treePath := filepath.Join(wtDir, "tree")
	require.NoError(t, os.MkdirAll(treePath, 0755))
	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0644))
	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0", WorktreeID: wtID, Name: "ambig",
		RepoID: repoID, Branch: "agency/ambig", ParentBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))

	// Two invocations with shared prefix
	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude", Mode: store.RunnerModeHeaded,
			StartedAt: "2026-02-01T00:00:00Z", Status: store.InvocationStatusRunning,
		}
		imB, _ := json.MarshalIndent(im, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imB, 0644))
	}

	// Start daemon
	fsys2 := fs.NewRealFS()
	cr2 := testutil.NewFakeCommandRunner()
	st2 := store.NewStore(fsys2, dataTmp, time.Now)
	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	srv := daemon.NewServer(st2, cr2, fsys2, configDir)
	socketPath := st2.DaemonSocketPath()
	listener, listenErr := net.Listen("unix", socketPath)
	require.NoError(t, listenErr)
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
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second))

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	var stdout, stderr bytes.Buffer
	showErr := AgentShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShowOpts{InvocationRef: "20260201000000", RepoFlag: repoID}, &stdout, &stderr)

	require.Error(t, showErr)
	assert.Equal(t, errors.EInvocationIDAmbiguous, errors.GetCode(showErr),
		"agent show must return entity-specific ambiguity code, not E_AMBIGUOUS")

	dre, ok := daemonclient.AsDaemonReadError(showErr)
	require.True(t, ok, "error must be DaemonReadError with rich details")
	candidates := dre.Candidates()
	assert.Len(t, candidates, 2, "daemon should return both candidate IDs")
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 2: canonical agent path/open/shell/enter daemon-first navigation
// ---------------------------------------------------------------------------

func TestAgentPath_UsesNavigationKernelDaemonResolution(t *testing.T) {
	env := setupAgentNavEnv(t, "path-test", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentPath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentPathOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, env.SandboxPath+"\n", stdout.String(),
		"stdout must be exactly the daemon-resolved sandbox_path plus newline")
}

func TestAgentOpen_UsesNavigationKernelDaemonPath_NoLocalResolve(t *testing.T) {
	env := setupAgentNavEnv(t, "open-test", store.RunnerModeHeaded)
	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	err := AgentOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentOpenOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID, Editor: shimPath}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.SandboxPath, cwd,
		"editor dispatch cwd must equal daemon-resolved sandbox_path")
	assert.Contains(t, args, env.SandboxPath,
		"editor must receive daemon-resolved sandbox_path as argument")
}

func TestAgentShell_UsesNavigationKernelDaemonPath_NoLocalResolve(t *testing.T) {
	env := setupAgentNavEnv(t, "shell-test", store.RunnerModeHeaded)
	shimPath, recordFile := createShimScript(t)
	t.Setenv("SHELL", shimPath)

	var stdout, stderr bytes.Buffer
	err := AgentShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShellOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.SandboxPath, cwd,
		"shell cwd must equal daemon-resolved sandbox_path")
	assert.Equal(t, "-l", args,
		"shell should be invoked with -l (login)")
}

func TestAgentEnter_UsesNavigationKernelInvocationResolution_HeadedOnly(t *testing.T) {
	env := setupAgentNavEnv(t, "enter-test", store.RunnerModeHeaded)

	fakeTmux := testutil.NewFakeTmuxClient()
	sessionName := tmux.SessionName(env.InvocationID)
	fakeTmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	var attachCalled bool
	var attachedSession string

	var stdout, stderr bytes.Buffer
	err := AgentEnter(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentEnterOpts{
			InvocationRef:   env.InvocationID,
			RepoFlag:        env.RepoID,
			IsInteractive:   func() bool { return true },
			TmuxClient:      fakeTmux,
			DataDirOverride: env.DataDir,
			TmuxAttachFn: func(sess string) error {
				attachCalled = true
				attachedSession = sess
				return nil
			},
		}, &stdout, &stderr)
	require.NoError(t, err)

	assert.True(t, attachCalled, "tmux attach must be called")
	assert.Equal(t, sessionName, attachedSession,
		"tmux session target must be derived from daemon-resolved invocation_id via tmux.SessionName")
}

func TestAgentPath_AmbiguityUsesEAmbiguous(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "an")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID := "20260131120000-abcd"
	// Create worktree
	wtDir := filepath.Join(dataTmp, "repos", repoID, "integration_worktrees", wtID)
	treePath := filepath.Join(wtDir, "tree")
	require.NoError(t, os.MkdirAll(treePath, 0755))
	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0644))
	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0", WorktreeID: wtID, Name: "ambig",
		RepoID: repoID, Branch: "agency/ambig", ParentBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))

	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude", Mode: store.RunnerModeHeaded,
			StartedAt: "2026-02-01T00:00:00Z", Status: store.InvocationStatusRunning,
		}
		imB, _ := json.MarshalIndent(im, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imB, 0644))
	}

	fsys2 := fs.NewRealFS()
	cr2 := testutil.NewFakeCommandRunner()
	st2 := store.NewStore(fsys2, dataTmp, time.Now)
	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	srv := daemon.NewServer(st2, cr2, fsys2, configDir)
	socketPath := st2.DaemonSocketPath()
	listener, listenErr := net.Listen("unix", socketPath)
	require.NoError(t, listenErr)
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
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second))

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	var stdout, stderr bytes.Buffer
	pathErr := AgentPath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentPathOpts{InvocationRef: "20260201000000", RepoFlag: repoID}, &stdout, &stderr)

	require.Error(t, pathErr)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(pathErr),
		"navigation ambiguity must return E_AMBIGUOUS, not entity-specific code")

	ae, ok := errors.AsAgencyError(pathErr)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.Equal(t, "invocation", ae.Details["target_kind"])
	assert.Equal(t, "2", ae.Details["candidate_count"])
}

func TestAgentOpen_AmbiguityUsesEAmbiguous_NoDispatch(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "an")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID := "20260131120000-abcd"
	wtDir := filepath.Join(dataTmp, "repos", repoID, "integration_worktrees", wtID)
	treePath := filepath.Join(wtDir, "tree")
	require.NoError(t, os.MkdirAll(treePath, 0755))
	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0644))
	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0", WorktreeID: wtID, Name: "ambig",
		RepoID: repoID, Branch: "agency/ambig", ParentBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))

	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude", Mode: store.RunnerModeHeaded,
			StartedAt: "2026-02-01T00:00:00Z", Status: store.InvocationStatusRunning,
		}
		imB, _ := json.MarshalIndent(im, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imB, 0644))
	}

	fsys2 := fs.NewRealFS()
	st2 := store.NewStore(fsys2, dataTmp, time.Now)
	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	srv := daemon.NewServer(st2, testutil.NewFakeCommandRunner(), fsys2, configDir)
	socketPath := st2.DaemonSocketPath()
	listener, listenErr := net.Listen("unix", socketPath)
	require.NoError(t, listenErr)
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
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second))

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	openErr := AgentOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentOpenOpts{InvocationRef: "20260201000000", RepoFlag: repoID, Editor: shimPath}, &stdout, &stderr)

	require.Error(t, openErr)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(openErr),
		"navigation ambiguity must return E_AMBIGUOUS")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr),
		"editor shim must NOT be executed on ambiguous target")
}

func TestAgentEnter_AmbiguityUsesEAmbiguous_NoDispatch(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "an")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID := "20260131120000-abcd"
	wtDir := filepath.Join(dataTmp, "repos", repoID, "integration_worktrees", wtID)
	treePath := filepath.Join(wtDir, "tree")
	require.NoError(t, os.MkdirAll(treePath, 0755))
	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0644))
	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0", WorktreeID: wtID, Name: "ambig",
		RepoID: repoID, Branch: "agency/ambig", ParentBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))

	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude", Mode: store.RunnerModeHeaded,
			StartedAt: "2026-02-01T00:00:00Z", Status: store.InvocationStatusRunning,
		}
		imB, _ := json.MarshalIndent(im, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imB, 0644))
	}

	fsys2 := fs.NewRealFS()
	st2 := store.NewStore(fsys2, dataTmp, time.Now)
	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	srv := daemon.NewServer(st2, testutil.NewFakeCommandRunner(), fsys2, configDir)
	socketPath := st2.DaemonSocketPath()
	listener, listenErr := net.Listen("unix", socketPath)
	require.NoError(t, listenErr)
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
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second))

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	var attachCalled bool
	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	enterErr := AgentEnter(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentEnterOpts{
			InvocationRef:   "20260201000000",
			RepoFlag:        repoID,
			IsInteractive:   func() bool { return true },
			TmuxClient:      fakeTmux,
			DataDirOverride: dataTmp,
			TmuxAttachFn: func(sess string) error {
				attachCalled = true
				return nil
			},
		}, &stdout, &stderr)

	require.Error(t, enterErr)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(enterErr),
		"navigation ambiguity must return E_AMBIGUOUS")
	assert.False(t, attachCalled, "tmux attach must NOT be invoked on ambiguous target")
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 3: command-family policy + deterministic target selection
// ---------------------------------------------------------------------------

func TestAgentLS_JSONOutput_PreservesRepoScopedIDs(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "an")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repo1, repo2 := "r1", "r2"
	wtID1, wtID2 := "20260131000000-aaaa", "20260131000000-bbbb"
	invID1, invID2 := "20260131100000-aaaa", "20260131100000-bbbb"

	for _, r := range []struct{ repoID, wtID, invID string }{
		{repo1, wtID1, invID1}, {repo2, wtID2, invID2},
	} {
		wtDir := filepath.Join(dataTmp, "repos", r.repoID, "integration_worktrees", r.wtID)
		tp := filepath.Join(wtDir, "tree")
		require.NoError(t, os.MkdirAll(tp, 0755))
		ad := filepath.Join(tp, ".agency")
		require.NoError(t, os.MkdirAll(ad, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(ad, integrationworktree.IntegrationMarkerFileName),
			[]byte("# Integration worktree\n"), 0644))
		wm := &store.IntegrationWorktreeMeta{
			SchemaVersion: "1.0", WorktreeID: r.wtID, Name: "wt-" + r.repoID,
			RepoID: r.repoID, Branch: "agency/b", ParentBranch: "main",
			TreePath: tp, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
		}
		wmb, _ := json.MarshalIndent(wm, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), wmb, 0644))

		invDir := filepath.Join(dataTmp, "repos", r.repoID, "invocations", r.invID)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sp := filepath.Join(dataTmp, "repos", r.repoID, "sandboxes", r.invID, "tree")
		require.NoError(t, os.MkdirAll(sp, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: r.invID,
			IntegrationWorktreeID: r.wtID, SandboxPath: sp,
			SandboxBranch: "agency/sandbox-" + r.invID, BaseCommit: "abc",
			Runner: "claude", Mode: store.RunnerModeHeaded,
			StartedAt: "2026-01-31T10:00:00Z", Status: store.InvocationStatusRunning,
		}
		imb, _ := json.MarshalIndent(im, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imb, 0644))
	}

	repoIndex := store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			"k1": {RepoID: repo1, Paths: []string{"/r1"}, LastSeenAt: "2026-01-31T12:00:00Z"},
			"k2": {RepoID: repo2, Paths: []string{"/r2"}, LastSeenAt: "2026-01-31T12:00:00Z"},
		},
	}
	idxBytes, _ := json.MarshalIndent(repoIndex, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dataTmp, "repo_index.json"), idxBytes, 0644))

	fsys2 := fs.NewRealFS()
	st2 := store.NewStore(fsys2, dataTmp, time.Now)
	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	srv := daemon.NewServer(st2, testutil.NewFakeCommandRunner(), fsys2, configDir)
	socketPath := st2.DaemonSocketPath()
	listener, listenErr := net.Listen("unix", socketPath)
	require.NoError(t, listenErr)
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
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second))

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	var stdout, stderr bytes.Buffer
	lsErr := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{AllRepos: true, JSON: true}, &stdout, &stderr)
	require.NoError(t, lsErr)

	var dtos []daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dtos))
	require.Len(t, dtos, 2)

	repoIDs := map[string]bool{}
	for _, dto := range dtos {
		repoIDs[dto.RepoID] = true
		assert.NotEmpty(t, dto.InvocationID, "each row must preserve invocation_id")
	}
	assert.True(t, repoIDs[repo1], "repo1 must appear in JSON output")
	assert.True(t, repoIDs[repo2], "repo2 must appear in JSON output")
}

func TestAgentPath_OutputsDaemonResolvedPath(t *testing.T) {
	env := setupAgentNavEnv(t, "pathout", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentPath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentPathOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	printedPath := strings.TrimSpace(stdout.String())
	assert.Equal(t, env.SandboxPath, printedPath,
		"printed path must equal daemon DTO sandbox_path (not re-derived)")
}

func TestAgentHumanOutput_RemainsHumanOriented_ScriptContractViaJSON(t *testing.T) {
	env := setupAgentNavEnv(t, "human", store.RunnerModeHeaded)

	var humanOut, jsonOut, stderr bytes.Buffer

	err := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoFlag: env.RepoID}, &humanOut, &stderr)
	require.NoError(t, err)

	err = AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoFlag: env.RepoID, JSON: true}, &jsonOut, &stderr)
	require.NoError(t, err)

	humanStr := humanOut.String()
	assert.NotContains(t, humanStr, `"invocation_id"`,
		"human output must not introduce JSON machine token grammar")
	assert.Contains(t, humanStr, env.InvocationID,
		"human output must still include invocation ID for readability")

	var dtos []daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &dtos),
		"JSON output must decode to daemon DTO slice (canonical script-safe format)")
	require.Len(t, dtos, 1)
	assert.Equal(t, env.RepoID, dtos[0].RepoID)
	assert.Equal(t, env.InvocationID, dtos[0].InvocationID)
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 4: invocation-mode validity + E_INVOCATION_INVALID_MODE
// ---------------------------------------------------------------------------

func TestAgentEnter_HeadlessInvocation_ReturnsInvalidMode(t *testing.T) {
	env := setupAgentNavEnv(t, "headless-enter", store.RunnerModeHeadless)

	fakeTmux := testutil.NewFakeTmuxClient()
	var attachCalled bool

	var stdout, stderr bytes.Buffer
	err := AgentEnter(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentEnterOpts{
			InvocationRef:   env.InvocationID,
			RepoFlag:        env.RepoID,
			IsInteractive:   func() bool { return true },
			TmuxClient:      fakeTmux,
			DataDirOverride: env.DataDir,
			TmuxAttachFn: func(sess string) error {
				attachCalled = true
				return nil
			},
		}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))
	assert.False(t, attachCalled, "tmux attach must NOT be called for headless invocation")

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "logs",
		"error hint should suggest alternative for headless")
}

func TestAgentEnter_NotInteractive_ReturnsENotInteractive(t *testing.T) {
	env := setupAgentNavEnv(t, "noterm-enter", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentEnter(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentEnterOpts{
			InvocationRef:   env.InvocationID,
			RepoFlag:        env.RepoID,
			IsInteractive:   func() bool { return false },
			DataDirOverride: env.DataDir,
		}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.NotEmpty(t, ae.Details["hint"], "error must include recovery hint")
}

// ---------------------------------------------------------------------------
// S2-PR04: D-004 — no E_INVOCATION_BROKEN on canonical navigation surfaces
// ---------------------------------------------------------------------------

func TestAgentNavigation_DoesNotReturnEInvocationBrokenForTargetResolution(t *testing.T) {
	env := setupAgentNavEnv(t, "brk-nav", store.RunnerModeHeaded)

	for _, verb := range []string{"path", "open", "shell", "enter"} {
		t.Run(verb, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			var navErr error

			ref := "nonexistent-invocation"
			switch verb {
			case "path":
				navErr = AgentPath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentPathOpts{InvocationRef: ref, RepoFlag: env.RepoID}, &stdout, &stderr)
			case "open":
				shimPath, _ := createShimScript(t)
				navErr = AgentOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentOpenOpts{InvocationRef: ref, RepoFlag: env.RepoID, Editor: shimPath}, &stdout, &stderr)
			case "shell":
				shimPath, _ := createShimScript(t)
				t.Setenv("SHELL", shimPath)
				navErr = AgentShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentShellOpts{InvocationRef: ref, RepoFlag: env.RepoID}, &stdout, &stderr)
			case "enter":
				navErr = AgentEnter(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentEnterOpts{
						InvocationRef:   ref,
						RepoFlag:        env.RepoID,
						IsInteractive:   func() bool { return true },
						TmuxClient:      testutil.NewFakeTmuxClient(),
						DataDirOverride: env.DataDir,
						TmuxAttachFn:    func(string) error { return nil },
					}, &stdout, &stderr)
			}

			require.Error(t, navErr)
			code := errors.GetCode(navErr)
			assert.NotEqual(t, errors.EInvocationBroken, code,
				"canonical navigation must not return E_INVOCATION_BROKEN after PR-04 migration")
			assert.Equal(t, errors.EInvocationNotFound, code,
				"expected daemon-first E_INVOCATION_NOT_FOUND for missing target")
		})
	}
}

// ---------------------------------------------------------------------------
// S2-PR04: D-005 — sandbox missing uses daemon-resolved path
// ---------------------------------------------------------------------------

func TestAgentOpen_SandboxMissing_UsesDaemonResolvedPath(t *testing.T) {
	env := setupAgentNavEnv(t, "open-missing", store.RunnerModeHeaded)

	require.NoError(t, os.RemoveAll(env.SandboxPath))

	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	err := AgentOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentOpenOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID, Editor: shimPath}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.ESandboxMissing, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, env.SandboxPath, ae.Details["sandbox_path"],
		"error details must include daemon-resolved sandbox_path")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr),
		"editor shim must NOT be executed when sandbox is missing")
}

func TestAgentShell_SandboxMissing_UsesDaemonResolvedPath(t *testing.T) {
	env := setupAgentNavEnv(t, "shell-missing", store.RunnerModeHeaded)

	require.NoError(t, os.RemoveAll(env.SandboxPath))

	shimPath, recordFile := createShimScript(t)
	t.Setenv("SHELL", shimPath)

	var stdout, stderr bytes.Buffer
	err := AgentShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShellOpts{InvocationRef: env.InvocationID, RepoFlag: env.RepoID}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.ESandboxMissing, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, env.SandboxPath, ae.Details["sandbox_path"],
		"error details must include daemon-resolved sandbox_path")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr),
		"shell shim must NOT be executed when sandbox is missing")
}

// ---------------------------------------------------------------------------
// S3 PR-01: AgentHistory integration tests
// ---------------------------------------------------------------------------

func TestAgentHistory_JSONIncludesTypedEntries(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-typed")
	invocationID := "20260131180000-hist"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	promptPath := st.InvocationPromptPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(promptPath, []byte("seed prompt: investigate failure"), 0o600))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	logsDir := st.SandboxLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.SandboxRawLogPath(repoID, invocationID), []byte("{\"raw\":1}\n"), 0o644))

	streamPath := st.SandboxStreamLogPath(repoID, invocationID)
	streamBytes := "" +
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude","kind":"message","data":{"role":"assistant","text":"checking"}}` + "\n" +
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"` + invocationID + `","runner":"claude","kind":"tool_start","data":{"name":"shell","command":"go test ./..."}}` + "\n"
	require.NoError(t, os.WriteFile(streamPath, []byte(streamBytes), 0o644))

	eventsPath := st.InvocationEventsPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(eventsPath, []byte(
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:30Z","invocation_id":"`+invocationID+`","kind":"agency.checkpoint_applied","data":{"checkpoint_id":1}}`+"\n",
	), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		JSON:            true,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	var payload struct {
		Entries []struct {
			EntryID string `json:"entry_id"`
			Kind    string `json:"kind"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	require.NotEmpty(t, payload.Entries)

	seenKinds := map[string]bool{}
	for _, entry := range payload.Entries {
		seenKinds[entry.Kind] = true
	}
	assert.True(t, seenKinds["prompt_seed"])
	assert.True(t, seenKinds["message"])
	assert.True(t, seenKinds["tool_use"])
	assert.True(t, seenKinds["raw_log_coverage"])
}

func TestAgentHistory_PaginationStableContinuation(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-page")
	invocationID := "20260131190000-page"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	promptPath := st.InvocationPromptPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(promptPath, []byte("seed prompt"), 0o600))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))
	require.NoError(t, os.MkdirAll(st.SandboxLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, os.WriteFile(st.SandboxRawLogPath(repoID, invocationID), []byte("raw\n"), 0o644))

	streamPath := st.SandboxStreamLogPath(repoID, invocationID)
	streamBytes := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude","kind":"message","data":{"role":"assistant","text":"one"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude","kind":"message","data":{"role":"assistant","text":"two"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:12Z","invocation_id":"` + invocationID + `","runner":"claude","kind":"tool_start","data":{"name":"shell","command":"echo hi"}}`,
		`{"schema_version":"1.0","seq":4,"timestamp":"2026-02-05T11:50:13Z","invocation_id":"` + invocationID + `","runner":"claude","kind":"tool_end","data":{"name":"shell","command":"echo hi","exit_code":0}}`,
	}
	require.NoError(t, os.WriteFile(streamPath, []byte(strings.Join(streamBytes, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	readPage := func(limit int, cursor string) ([]string, string) {
		var out bytes.Buffer
		var errOut bytes.Buffer
		err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
			InvocationRef:   invocationID,
			JSON:            true,
			Limit:           limit,
			Cursor:          cursor,
			DataDirOverride: dataDir,
		}, &out, &errOut)
		require.NoError(t, err)

		var payload struct {
			Entries []struct {
				EntryID string `json:"entry_id"`
			} `json:"entries"`
			NextCursor string `json:"next_cursor"`
		}
		require.NoError(t, json.Unmarshal(out.Bytes(), &payload))

		ids := make([]string, 0, len(payload.Entries))
		for _, entry := range payload.Entries {
			ids = append(ids, entry.EntryID)
		}
		return ids, payload.NextCursor
	}

	allIDs, _ := readPage(100, "")
	require.NotEmpty(t, allIDs)

	pagedIDs := make([]string, 0)
	cursor := ""
	for {
		ids, next := readPage(2, cursor)
		pagedIDs = append(pagedIDs, ids...)
		if next == "" {
			break
		}
		cursor = next
	}
	assert.Equal(t, allIDs, pagedIDs)
}

// ---------------------------------------------------------------------------
// PR-B: AgentLogs integration tests
// ---------------------------------------------------------------------------

func TestAgentLogs_PageToEOF(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "logs-test")
	invocationID := "20260131140000-logs"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	// Seed a raw log file with known content
	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.SandboxLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.SandboxRawLogPath(repoID, invocationID), []byte("hello world\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentLogs(context.Background(), cr2, fsys, repoDir, AgentLogsOpts{
		InvocationRef:   invocationID,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, "hello world\n", stdout.String())
}

func TestAgentLogs_FollowMode(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "follow-test")
	invocationID := "20260131150000-foll"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.SandboxLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	logPath := st.SandboxRawLogPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(logPath, []byte("line1\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	appendCalls := 0
	sleepFn := func(d time.Duration) {
		appendCalls++
		// Simulate new data appearing after first poll
		if appendCalls == 1 {
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				_, _ = f.WriteString("line2\n")
				_ = f.Close()
			}
		}
	}

	var stdout, stderr bytes.Buffer
	err := AgentLogs(context.Background(), cr2, fsys, repoDir, AgentLogsOpts{
		InvocationRef:   invocationID,
		Follow:          true,
		MaxIterations:   2,
		SleepFn:         sleepFn,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "line1\n")
	assert.Contains(t, stdout.String(), "line2\n")
}

func TestAgentLogs_ContextCancellation(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "ctx-test")
	invocationID := "20260131160000-cctx"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.SandboxLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.SandboxRawLogPath(repoID, invocationID), []byte("data\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	ctx, cancel := context.WithCancel(context.Background())

	sleepFn := func(d time.Duration) {
		cancel() // cancel on first poll sleep
	}

	var stdout, stderr bytes.Buffer
	err := AgentLogs(ctx, cr2, fsys, repoDir, AgentLogsOpts{
		InvocationRef:   invocationID,
		Follow:          true,
		SleepFn:         sleepFn,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	// Should have read initial data before cancellation
	assert.Contains(t, stdout.String(), "data\n")
}

func TestAgentLogs_StderrKind(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "stderr-test")
	invocationID := "20260131170000-stde"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.SandboxLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.SandboxStderrLogPath(repoID, invocationID), []byte("error output\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentLogs(context.Background(), cr2, fsys, repoDir, AgentLogsOpts{
		InvocationRef:   invocationID,
		Kind:            "stderr",
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, "error output\n", stdout.String())
}

func TestAgentHistory_InvalidLimitReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-invalid-limit")
	invocationID := "20260131180000-hlim"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		Limit:           0,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestAgentChat_PromptFileOverLimitReturnsEPromptTooLarge(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "chat-file-limit")
	invocationID := "20260131181000-chat"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	oversizedPromptPath := filepath.Join(t.TempDir(), "prompt.txt")
	require.NoError(t, os.WriteFile(oversizedPromptPath, bytes.Repeat([]byte("x"), daemon.MaxPromptSize+1), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentChat(context.Background(), cr2, fsys, repoDir, AgentChatOpts{
		InvocationRef:   invocationID,
		PromptFile:      oversizedPromptPath,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EPromptTooLarge, errors.GetCode(err))
}

func TestAgentChat_HumanAndJSONAligned(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "chat-output")
	invocationID := "20260131182000-cout"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var humanOut, jsonOut, stderr bytes.Buffer
	err := AgentChat(context.Background(), cr2, fsys, repoDir, AgentChatOpts{
		InvocationRef:   invocationID,
		Prompt:          "continue with regression analysis",
		DataDirOverride: dataDir,
	}, &humanOut, &stderr)
	require.NoError(t, err)

	err = AgentChat(context.Background(), cr2, fsys, repoDir, AgentChatOpts{
		InvocationRef:   invocationID,
		Prompt:          "second follow-up",
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &stderr)
	require.NoError(t, err)

	assert.Contains(t, humanOut.String(), invocationID)
	assert.Contains(t, strings.ToLower(humanOut.String()), "accepted")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
}

func TestAgentRestart_InvalidCheckpointIDReturnsUsage(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", AgentRestartOpts{
		InvocationRef: "inv-123",
		CheckpointID:  0,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentRestart_NegativeCheckpointIDReturnsUsage(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", AgentRestartOpts{
		InvocationRef: "inv-123",
		CheckpointID:  -1,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
	assert.Contains(t, err.Error(), "--checkpoint must be a positive integer")
}

func TestAgentRestart_HumanAndJSONAligned(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "restart-output")
	invocationID := "20260131183000-rout"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFailed)

	st := store.NewStore(fsys, dataDir, time.Now)
	promptPath := st.InvocationPromptPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(promptPath, []byte("restart prompt"), 0o600))
	require.NoError(t, os.MkdirAll(st.SandboxLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    "deadbeef",
				SandboxHeadSHA:    "deadbeef",
				CreatedAt:         "2026-02-05T11:50:30Z",
				IncludesUntracked: true,
				Diffstat:          "+0 -0 in 0 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(st.SandboxDir(repoID, invocationID), "checkpoints.json"),
		cpBytes,
		0o644,
	))

	runnerDir := t.TempDir()
	runnerPath := filepath.Join(runnerDir, "restart-runner.sh")
	require.NoError(t, os.WriteFile(runnerPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	cfg := map[string]any{
		"version": 1,
		"defaults": map[string]string{
			"runner": "claude",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude": runnerPath,
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var humanOut, jsonOut, stderr bytes.Buffer
	err = AgentRestart(context.Background(), cr2, fsys, repoDir, AgentRestartOpts{
		InvocationRef:   invocationID,
		CheckpointID:    1,
		Env:             map[string]string{"FAKE_RUNNER_MODE": "exit-ok"},
		DataDirOverride: dataDir,
	}, &humanOut, &stderr)
	require.NoError(t, err)

	err = AgentRestart(context.Background(), cr2, fsys, repoDir, AgentRestartOpts{
		InvocationRef:   invocationID,
		CheckpointID:    1,
		Env:             map[string]string{"FAKE_RUNNER_MODE": "exit-ok"},
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &stderr)
	require.NoError(t, err)

	assert.Contains(t, humanOut.String(), invocationID)
	assert.Contains(t, strings.ToLower(humanOut.String()), "checkpoint")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
	assert.Equal(t, float64(1), payload["checkpoint_id"])
}

func TestAgentRestart_RequiresExplicitEnvReplayWhenProfilePresent(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "restart-env-required")
	invocationID := "20260131184000-renv"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFailed)

	st := store.NewStore(fsys, dataDir, time.Now)
	promptPath := st.InvocationPromptPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(promptPath, []byte("restart prompt"), 0o600))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.CustomEnvKeys = []string{"API_TOKEN"}
	}))

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), cr, fsys, repoDir, AgentRestartOpts{
		InvocationRef:   invocationID,
		CheckpointID:    1,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.Code("E_INVALID_REQUEST"), errors.GetCode(err))
	assert.Contains(t, err.Error(), "explicit env values")
}

func TestResolveBoundedPromptInput_MissingPromptUsesContextMessage(t *testing.T) {
	t.Parallel()

	_, err := resolveBoundedPromptInput("", "", 64, "custom missing prompt message", "custom empty prompt message")
	require.Error(t, err)
	assert.Equal(t, errors.EPromptRequired, errors.GetCode(err))
	assert.Contains(t, err.Error(), "custom missing prompt message")
}

func TestResolveBoundedPromptInput_RejectsPromptAndFileTogether(t *testing.T) {
	t.Parallel()

	_, err := resolveBoundedPromptInput("inline", "prompt.txt", 64, "unused", "unused")
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestResolveBoundedPromptInput_EmptyFileUsesContextMessage(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	emptyPath := filepath.Join(tempDir, "empty-prompt.txt")
	require.NoError(t, os.WriteFile(emptyPath, nil, 0o600))

	_, err := resolveBoundedPromptInput("", emptyPath, 64, "unused", "context-specific empty file message")
	require.Error(t, err)
	assert.Equal(t, errors.EPromptRequired, errors.GetCode(err))
	assert.Contains(t, err.Error(), "context-specific empty file message")
}

func TestRunInteractiveHistorySelector_ArrowNavigationAndConfirm(t *testing.T) {
	t.Parallel()

	items := []historySelectorItem{
		{Entry: daemon.TimelineEntryDTO{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z"}, Summary: "first"},
		{Entry: daemon.TimelineEntryDTO{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:20Z"}, Summary: "second"},
		{Entry: daemon.TimelineEntryDTO{EntryID: "e-3", Kind: "message", Timestamp: "2026-02-05T11:50:30Z"}, Summary: "third"},
	}

	// Initial selection is newest (last item). Arrow-up then Enter should pick e-2.
	input := bytes.NewBufferString("\x1b[A\r")
	var output bytes.Buffer
	selected, err := runInteractiveHistorySelector(items, input, &output)
	require.NoError(t, err)
	assert.Equal(t, "e-2", selected.Entry.EntryID)
}

func TestRunInteractiveHistorySelector_CancelReturnsAborted(t *testing.T) {
	t.Parallel()

	items := []historySelectorItem{
		{Entry: daemon.TimelineEntryDTO{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:10Z"}, Summary: "first"},
	}

	input := bytes.NewBufferString("q")
	var output bytes.Buffer
	_, err := runInteractiveHistorySelector(items, input, &output)
	require.Error(t, err)
	assert.Equal(t, errors.EAborted, errors.GetCode(err))
}

func TestMapTimelineSelectionToCheckpoint_Deterministic(t *testing.T) {
	t.Parallel()

	entries := []daemon.TimelineEntryDTO{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:05Z"},
		{
			EntryID:   "cp-1",
			Kind:      "checkpoint_event",
			Timestamp: "2026-02-05T11:50:10Z",
			Data: map[string]interface{}{
				"event_kind":    "agency.checkpoint_created",
				"checkpoint_id": 1,
			},
		},
		{EntryID: "e-2", Kind: "followup_prompt", Timestamp: "2026-02-05T11:50:20Z"},
		{
			EntryID:   "cp-2",
			Kind:      "checkpoint_event",
			Timestamp: "2026-02-05T11:50:30Z",
			Data: map[string]interface{}{
				"event_kind":    "agency.checkpoint_created",
				"checkpoint_id": 2,
			},
		},
		{EntryID: "e-3", Kind: "message", Timestamp: "2026-02-05T11:50:40Z"},
	}
	checkpoints := []daemon.CheckpointDTO{
		{ID: 1},
		{ID: 2},
	}

	first, err := mapTimelineSelectionToCheckpoint(entries, checkpoints, "e-2")
	require.NoError(t, err)
	second, err := mapTimelineSelectionToCheckpoint(entries, checkpoints, "e-2")
	require.NoError(t, err)

	assert.Equal(t, 1, first)
	assert.Equal(t, first, second)
}

func TestMapTimelineSelectionToCheckpoint_NoMappingReturnsCheckpointNotFound(t *testing.T) {
	t.Parallel()

	entries := []daemon.TimelineEntryDTO{
		{EntryID: "e-1", Kind: "message", Timestamp: "2026-02-05T11:50:05Z"},
		{EntryID: "e-2", Kind: "message", Timestamp: "2026-02-05T11:50:06Z"},
	}
	checkpoints := []daemon.CheckpointDTO{{ID: 1}}

	_, err := mapTimelineSelectionToCheckpoint(entries, checkpoints, "e-1")
	require.Error(t, err)
	assert.Equal(t, errors.ECheckpointNotFound, errors.GetCode(err))
}

func TestMapTimelineSelectionToCheckpoint_MappedCheckpointUnavailableReturnsCheckpointNotFound(t *testing.T) {
	t.Parallel()

	entries := []daemon.TimelineEntryDTO{
		{
			EntryID:   "cp-1",
			Kind:      "checkpoint_event",
			Timestamp: "2026-02-05T11:50:10Z",
			Data: map[string]interface{}{
				"event_kind":    "agency.checkpoint_created",
				"checkpoint_id": 1,
			},
		},
		{
			EntryID:   "cp-2",
			Kind:      "checkpoint_event",
			Timestamp: "2026-02-05T11:50:30Z",
			Data: map[string]interface{}{
				"event_kind":    "agency.checkpoint_created",
				"checkpoint_id": 2,
			},
		},
		{EntryID: "e-3", Kind: "message", Timestamp: "2026-02-05T11:50:40Z"},
	}
	checkpoints := []daemon.CheckpointDTO{
		{ID: 1},
	}

	_, err := mapTimelineSelectionToCheckpoint(entries, checkpoints, "e-3")
	require.Error(t, err)
	assert.Equal(t, errors.ECheckpointNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "mapped checkpoint 2 is no longer available")
}

func TestAgentRestart_InteractiveHistory_NonInteractiveFailsFast(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := AgentRestart(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", AgentRestartOpts{
		InvocationRef:      "inv-123",
		InteractiveHistory: true,
		IsInteractive:      func() bool { return false },
		DataDirOverride:    t.TempDir(),
		HistorySelectorIn:  bytes.NewBuffer(nil),
		HistorySelectorOut: &bytes.Buffer{},
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
}

func TestAgentRestart_InteractiveHistory_MapsToCheckpointAndUsesCanonicalRestartFlow(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "restart-history")
	invocationID := "20260131185000-rhist"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFailed)

	st := store.NewStore(fsys, dataDir, time.Now)
	promptPath := st.InvocationPromptPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(promptPath, []byte("restart prompt"), 0o600))
	require.NoError(t, os.MkdirAll(st.SandboxLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    "deadbeef",
				SandboxHeadSHA:    "deadbeef",
				CreatedAt:         "2026-02-05T11:50:10Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
			{
				ID:                2,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/2",
				SnapshotCommit:    "feedface",
				SandboxHeadSHA:    "feedface",
				CreatedAt:         "2026-02-05T11:50:30Z",
				IncludesUntracked: true,
				Diffstat:          "+1 -0 in 1 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(st.SandboxDir(repoID, invocationID), "checkpoints.json"), cpBytes, 0o644))

	eventsLines := strings.Join([]string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"` + invocationID + `","kind":"agency.followup_prompt","data":{"text":"continue from cp1"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:30Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":2}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(eventsLines), 0o644))

	runnerDir := t.TempDir()
	runnerPath := filepath.Join(runnerDir, "restart-runner.sh")
	require.NoError(t, os.WriteFile(runnerPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	cfg := map[string]any{
		"version": 1,
		"defaults": map[string]string{
			"runner": "claude",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude": runnerPath,
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var jsonOut, stderr bytes.Buffer
	err = AgentRestart(context.Background(), cr2, fsys, repoDir, AgentRestartOpts{
		InvocationRef:      invocationID,
		InteractiveHistory: true,
		IsInteractive:      func() bool { return true },
		HistorySelector: func(items []historySelectorItem, _ io.Reader, _ io.Writer) (historySelectorItem, error) {
			for _, item := range items {
				if item.Entry.EntryID == "inv_event:2:agency.followup_prompt" {
					return item, nil
				}
			}
			t.Fatalf("expected follow-up timeline entry in selector items")
			return historySelectorItem{}, nil
		},
		Env:             map[string]string{"FAKE_RUNNER_MODE": "exit-ok"},
		JSON:            true,
		DataDirOverride: dataDir,
	}, &jsonOut, &stderr)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &payload))
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
	// Selected follow-up entry is before checkpoint 2, so deterministic mapping must pick checkpoint 1.
	assert.Equal(t, float64(1), payload["checkpoint_id"])
}
