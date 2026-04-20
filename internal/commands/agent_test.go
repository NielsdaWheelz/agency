// Package commands implements agency CLI commands.
// This file tests agent commands for headed execution (Slice 8 PR-03).
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		Runner:                "claude-code",
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
	st := store.NewStore(fsys, dataDir, time.Now)

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
		BaseBranch:    "main",
		TreePath:      worktreeTreeDir,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	metaBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	metaPath := filepath.Join(worktreeDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))

	// Register the repo through the canonical registry before starting the daemon.
	repoIndex := store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoIdentity.RepoKey: {
				RepoID:     repoID,
				Paths:      []string{repoDir},
				LastSeenAt: "2026-01-31T12:00:00Z",
			},
		},
	}
	repoRecord := store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoID,
		RepoRootLastSeen: repoDir,
		PreferredRoot:    repoDir,
		AgencyJSONPath:   filepath.Join(repoDir, "agency.json"),
		OriginPresent:    true,
		OriginURL:        originURL,
		OriginHost:       "github.com",
		CreatedAt:        "2026-01-31T12:00:00Z",
		UpdatedAt:        "2026-01-31T12:00:00Z",
	}
	require.NoError(t, st.SaveRepoIndex(repoIndex))
	require.NoError(t, st.SaveRepoRecord(repoRecord))
	configDir := filepath.Join(dataDir, "config")
	srv := daemon.NewServer(st, cr, fsys, configDir)
	srv.TmuxClient = testutil.NewFakeTmuxClient()

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

type agentStartHeadedTestEnv struct {
	RepoDir      string
	DataDir      string
	RepoID       string
	WorktreeID   string
	WorktreePath string
	Runner       exec.CommandRunner
	FS           fs.FS
	RecordFile   string
}

func setupAgentStartHeadedTestEnv(t *testing.T, worktreeName string, tmuxExitCode int) agentStartHeadedTestEnv {
	t.Helper()

	repoDir, dataDir, repoID, worktreeID, fakeRunner, fsys := setupAgentTestEnvShort(t, worktreeName)
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	cfg := map[string]any{
		"version": 2,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude-code": "fake-runner",
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644))
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	shimDir := t.TempDir()
	recordFile := filepath.Join(shimDir, "record.txt")
	shimPath := filepath.Join(shimDir, "tmux")
	script := fmt.Sprintf("#!/bin/sh\npwd > '%s'\necho \"$@\" >> '%s'\nexit %d\n", recordFile, recordFile, tmuxExitCode)
	require.NoError(t, os.WriteFile(shimPath, []byte(script), 0o755))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return agentStartHeadedTestEnv{
		RepoDir:      repoDir,
		DataDir:      dataDir,
		RepoID:       repoID,
		WorktreeID:   worktreeID,
		WorktreePath: filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree"),
		Runner:       fakeRunner,
		FS:           fsys,
		RecordFile:   recordFile,
	}
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
		BaseBranch:    "main",
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
		Runner:                "claude-code",
		Mode:                  mode,
		StartedAt:             "2026-01-31T13:00:00Z",
		Status:                store.InvocationStatusRunning,
	}
	if mode == store.RunnerModeHeaded {
		invMeta.TmuxSession = tmux.SessionName(invID)
	}
	imBytes, _ := json.MarshalIndent(invMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(invDir, "meta.json"), imBytes, 0644))

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataTmp, time.Now)
	repoRoot := filepath.Join(dataTmp, "repos", repoID, "root")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))
	repoIndex := store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"path:" + repoID: {
				RepoID:     repoID,
				Paths:      []string{repoRoot},
				LastSeenAt: "2026-01-31T12:00:00Z",
			},
		},
	}
	require.NoError(t, st.SaveRepoIndex(repoIndex))
	repoRecord := store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "path:" + repoID,
		RepoID:           repoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    false,
		CreatedAt:        "2026-01-31T12:00:00Z",
		UpdatedAt:        "2026-01-31T12:00:00Z",
	}
	require.NoError(t, st.SaveRepoRecord(repoRecord))

	// Start daemon
	cr := testutil.NewFakeCommandRunner()
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

func seedRepoIndexForNavigationTests(t *testing.T, dataDir, repoID string) {
	t.Helper()

	repoRoot := filepath.Join(dataDir, "repos", repoID, "root")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"path:" + repoID: {
				RepoID:     repoID,
				Paths:      []string{repoRoot},
				LastSeenAt: "2026-01-31T12:00:00Z",
			},
		},
	}))
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "path:" + repoID,
		RepoID:           repoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    false,
		CreatedAt:        "2026-01-31T12:00:00Z",
		UpdatedAt:        "2026-01-31T12:00:00Z",
	}))
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 1: agent ls/show daemon-of-record read behavior
// ---------------------------------------------------------------------------

func TestAgentLS_DaemonOfRecord_RendersDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "ls-test", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, env.InvocationID)
	assert.Contains(t, out, "claude-code")
	assert.Contains(t, out, "headed")
}

func TestAgentLS_DefaultRequestsUnresolvedState(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "ag")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0o755))

	var requestedState string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(daemon.APIResponse{
				OK:         true,
				APIVersion: daemon.APIVersion,
			})
		case "/repos/repo-1":
			_ = json.NewEncoder(w).Encode(daemon.APIResponse{
				OK: true,
				Data: daemon.RepoDTO{
					RepoID: "repo-1",
				},
			})
		case "/invocations":
			requestedState = r.URL.Query().Get("state")
			_ = json.NewEncoder(w).Encode(daemon.APIResponse{
				OK: true,
				Data: daemon.ListInvocationsData{
					Invocations: []daemon.InvocationDTO{},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	srv := &http.Server{Handler: handler}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	var stdout, stderr bytes.Buffer
	err = AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoRef: "repo-1"}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, "unresolved", requestedState)
}

func TestAgentShow_DaemonOfRecord_RendersDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "show-test", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShowOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "invocation_id:          "+env.InvocationID)
	assert.Contains(t, out, "worktree_id:            "+env.WorktreeID)
	assert.Contains(t, out, "runner:                 claude-code")
	assert.Contains(t, out, "mode:                   headed")
	assert.Contains(t, out, "sandbox_path:           "+env.SandboxPath)
}

func TestAgentLS_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "lsjson", store.RunnerModeHeadless)

	var stdout, stderr bytes.Buffer
	err := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoRef: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dtos []daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dtos))
	require.Len(t, dtos, 1)

	assert.Equal(t, env.InvocationID, dtos[0].InvocationID)
	assert.Equal(t, env.RepoID, dtos[0].RepoID)
	assert.Equal(t, "claude-code", dtos[0].Runner)
	assert.Equal(t, "headless", dtos[0].Mode)
	assert.Equal(t, env.SandboxPath, dtos[0].SandboxPath)
}

func TestAgentShow_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupAgentNavEnv(t, "showjson", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShowOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dto daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dto))

	assert.Equal(t, env.InvocationID, dto.InvocationID)
	assert.Equal(t, env.RepoID, dto.RepoID)
	assert.Equal(t, "claude-code", dto.Runner)
	assert.Equal(t, env.SandboxPath, dto.SandboxPath)
}

func TestAgentActivitySurfaces_ConvergeLatestActivityStatusSummaryAndNavigation(t *testing.T) {
	env := setupAgentNavEnv(t, "activity-converge", store.RunnerModeHeadless)

	stateDir := filepath.Join(env.SandboxPath, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))
	runner := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusWorking,
		UpdatedAt:     "2026-02-05T11:59:30Z",
		Summary:       "waiting on api contract",
		Questions:     []string{},
		Blockers:      []string{},
		Risks:         []string{},
	}
	runnerBytes, err := json.Marshal(runner)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), runnerBytes, 0o600))

	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	logsDir := st.InvocationLogsDir(env.RepoID, env.InvocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	streamLine := `{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:59:00Z","invocation_id":"` + env.InvocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"latest activity summary"}}`
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(env.RepoID, env.InvocationID), []byte(streamLine+"\n"), 0o644))

	var lsJSON, showJSON, checkJSON, stderr bytes.Buffer

	err = AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoRef: env.RepoID, JSON: true}, &lsJSON, &stderr)
	require.NoError(t, err)

	var listedRows []daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(lsJSON.Bytes(), &listedRows))
	require.Len(t, listedRows, 1)
	listed := listedRows[0]
	require.NotNil(t, listed.LatestActivity)
	require.NotNil(t, listed.Navigation)

	err = AgentShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShowOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID, JSON: true}, &showJSON, &stderr)
	require.NoError(t, err)
	var shown daemon.InvocationDTO
	require.NoError(t, json.Unmarshal(showJSON.Bytes(), &shown))
	require.NotNil(t, shown.LatestActivity)
	require.NotNil(t, shown.Navigation)

	err = AgentCheck(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentCheckOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID, JSON: true, DataDirOverride: env.DataDir}, &checkJSON, &stderr)
	require.NoError(t, err)
	var check daemon.InvocationCheckData
	require.NoError(t, json.Unmarshal(checkJSON.Bytes(), &check))
	require.NotNil(t, check.LatestActivity)

	assert.Equal(t, listed.DisplayStatus, shown.DisplayStatus)
	assert.Equal(t, shown.DisplayStatus, check.DisplayStatus)

	assert.Equal(t, listed.StatusSummary, shown.StatusSummary)
	assert.Equal(t, shown.StatusSummary, check.StatusSummary)
	assert.Equal(t, "latest activity summary", check.StatusSummary)

	assert.Equal(t, listed.LatestActivity.TurnID, shown.LatestActivity.TurnID)
	assert.Equal(t, shown.LatestActivity.TurnID, check.LatestActivity.TurnID)
	assert.Equal(t, listed.LatestActivity.Summary, shown.LatestActivity.Summary)
	assert.Equal(t, shown.LatestActivity.Summary, check.LatestActivity.Summary)
	assert.Equal(t, "stream:1", check.LatestActivity.TurnID)
	assert.Equal(t, "latest activity summary", check.LatestActivity.Summary)

	assert.Equal(t, listed.Navigation.HistoryCommand, shown.Navigation.HistoryCommand)
	assert.Equal(t, shown.Navigation.HistoryCommand, check.Navigation.HistoryCommand)
	assert.Equal(t, listed.Navigation.DiffCommand, shown.Navigation.DiffCommand)
	assert.Equal(t, shown.Navigation.DiffCommand, check.Navigation.DiffCommand)
	assert.Equal(t, listed.Navigation.LatestTurnID, shown.Navigation.LatestTurnID)
	assert.Equal(t, shown.Navigation.LatestTurnID, check.Navigation.LatestTurnID)
}

func TestWriteAgentLSHumanFromDTO_IncludesLatestActivityMetadata(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := writeAgentLSHumanFromDTO(&out, []daemon.InvocationDTO{
		{
			InvocationID:  "inv-1",
			Runner:        "claude-code",
			Mode:          "headless",
			Status:        "running",
			DisplayStatus: "working",
			LatestActivity: &daemon.InvocationLatestActivity{
				TurnID:        "stream:9",
				Kind:          "assistant",
				Summary:       "applied migration",
				ToolCallCount: 2,
				CheckpointID:  4,
				Restorable:    true,
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "latest[stream:9]: [assistant] applied migration (tools=2, checkpoint=4)")
}

func TestWriteAgentShowHumanFromDTO_IncludesLatestActivityMetadata(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := writeAgentShowHumanFromDTO(&out, &daemon.InvocationDTO{
		InvocationID:  "inv-1",
		WorktreeID:    "wt-1",
		Runner:        "claude-code",
		Mode:          "headless",
		Status:        "running",
		DisplayStatus: "working",
		StartedAt:     "2026-02-05T11:50:00Z",
		SandboxPath:   "/tmp/sandbox/inv-1",
		LatestActivity: &daemon.InvocationLatestActivity{
			TurnID:                 "stream:9",
			Kind:                   "assistant",
			Summary:                "applied migration",
			ToolCallCount:          1,
			ToolCalls:              []daemon.InvocationActivityToolCall{{Name: "Bash", Command: "go test ./...", HasExit: true, ExitCode: 1}},
			CheckpointID:           4,
			Restorable:             true,
			CheckpointDescription:  "checkpoint after migration",
			CheckpointDiffstat:     "2 files changed, 10 insertions(+), 2 deletions(-)",
			CheckpointChangedPaths: []string{"internal/apply.go", "internal/apply_test.go"},
			CheckpointChangedCount: 2,
		},
	})
	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "latest_activity:        [assistant] applied migration (tools=1, checkpoint=4)")
	assert.Contains(t, output, "latest_activity_tool:   ▶ Bash go test ./... (exit=1)")
	assert.Contains(t, output, "latest_activity_checkpoint: 4")
	assert.Contains(t, output, "latest_activity_checkpoint_description: checkpoint after migration")
	assert.Contains(t, output, "latest_activity_checkpoint_diffstat: 2 files changed, 10 insertions(+), 2 deletions(-)")
	assert.Contains(t, output, "latest_activity_checkpoint_paths: internal/apply.go, internal/apply_test.go")
}

func TestWriteAgentCheckHumanFromDTO_IncludesLatestActivityMetadata(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	check := &daemon.InvocationCheckData{
		InvocationID:  "inv-1",
		RepoID:        "repo-1",
		Status:        "running",
		DisplayStatus: "working",
		Navigation: daemon.InvocationCheckNavigation{
			HistoryCommand: "agency agent inv-1 history --repo repo-1",
		},
		LatestActivity: &daemon.InvocationLatestActivity{
			TurnID:                 "stream:9",
			Kind:                   "assistant",
			Summary:                "applied migration",
			ToolCallCount:          1,
			ToolCalls:              []daemon.InvocationActivityToolCall{{Name: "Bash", Command: "go test ./...", HasExit: true, ExitCode: 1}},
			CheckpointID:           4,
			Restorable:             true,
			CheckpointDescription:  "checkpoint after migration",
			CheckpointDiffstat:     "2 files changed, 10 insertions(+), 2 deletions(-)",
			CheckpointChangedPaths: []string{"internal/apply.go", "internal/apply_test.go"},
			CheckpointChangedCount: 2,
		},
	}
	err := writeAgentCheckHumanFromDTO(&out, check)
	require.NoError(t, err)
	output := out.String()
	assert.Contains(t, output, "latest_activity:      [assistant] applied migration (tools=1, checkpoint=4)")
	assert.Contains(t, output, "latest_activity_tool: ▶ Bash go test ./... (exit=1)")
	assert.Contains(t, output, "latest_activity_checkpoint: 4")
	assert.Contains(t, output, "latest_activity_checkpoint_description: checkpoint after migration")
	assert.Contains(t, output, "latest_activity_checkpoint_diffstat: 2 files changed, 10 insertions(+), 2 deletions(-)")
	assert.Contains(t, output, "latest_activity_checkpoint_paths: internal/apply.go, internal/apply_test.go")
}

func TestWriteAgentCheckHumanFromDTO_ReadinessFallbackRendersReadyVerdict(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	check := &daemon.InvocationCheckData{
		InvocationID: "inv-1",
		RepoID:       "repo-1",
		Status:       "finished",
		Readiness:    "ready",
		Navigation: daemon.InvocationCheckNavigation{
			HistoryCommand: "agency agent inv-1 history --repo repo-1",
		},
	}

	err := writeAgentCheckHumanFromDTO(&out, check)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Readiness:            READY")
}

func TestWriteAgentCheckHumanFromDTO_NilCheckReturnsInternalError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := writeAgentCheckHumanFromDTO(&out, nil)
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
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
		RepoID: repoID, Branch: "agency/ambig", BaseBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))
	seedRepoIndexForNavigationTests(t, dataTmp, repoID)

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
			Runner: "claude-code", Mode: store.RunnerModeHeaded,
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
		AgentShowOpts{InvocationRef: "20260201000000", RepoRef: repoID}, &stdout, &stderr)

	require.Error(t, showErr)
	assert.Equal(t, errors.EInvocationIDAmbiguous, errors.GetCode(showErr),
		"agent show must return entity-specific ambiguity code, not E_AMBIGUOUS")

	dre, ok := daemonclient.AsDaemonReadError(showErr)
	require.True(t, ok, "error must be DaemonReadError with rich details")
	candidates := dre.Candidates()
	assert.Len(t, candidates, 2, "daemon should return both candidate IDs")
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 2: canonical agent path/open/shell/attach daemon-first navigation
// ---------------------------------------------------------------------------

func TestAgentPath_UsesDaemonResolution(t *testing.T) {
	env := setupAgentNavEnv(t, "path-test", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentPath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentPathOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, env.SandboxPath+"\n", stdout.String(),
		"stdout must be exactly the daemon-resolved sandbox_path plus newline")
}

func TestAgentOpen_UsesDaemonResolution_NoLocalResolve(t *testing.T) {
	env := setupAgentNavEnv(t, "open-test", store.RunnerModeHeaded)
	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	err := AgentOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentOpenOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID, Editor: shimPath}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.SandboxPath, cwd,
		"editor dispatch cwd must equal daemon-resolved sandbox_path")
	assert.Contains(t, args, env.SandboxPath,
		"editor must receive daemon-resolved sandbox_path as argument")
}

func TestAgentShell_UsesDaemonResolution_NoLocalResolve(t *testing.T) {
	env := setupAgentNavEnv(t, "shell-test", store.RunnerModeHeaded)
	shimPath, recordFile := createShimScript(t)
	t.Setenv("SHELL", shimPath)

	var stdout, stderr bytes.Buffer
	err := AgentShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentShellOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.SandboxPath, cwd,
		"shell cwd must equal daemon-resolved sandbox_path")
	assert.Equal(t, "-l", args,
		"shell should be invoked with -l (login)")
}

// ---------------------------------------------------------------------------
// S2-PR04 Acceptance 2.5: headed start attach cutover
// ---------------------------------------------------------------------------

func TestAgentStart_Headed_NonInteractiveFailsFast(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-noterm", 1)

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-noterm",
		Runner:        "claude-code",
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))

	invocationsDir := filepath.Join(env.DataDir, "repos", env.RepoID, "invocations")
	_, statErr := os.Stat(invocationsDir)
	assert.True(t, os.IsNotExist(statErr), "headed start must not create an invocation before the interactive gate")

	_, recordErr := os.Stat(env.RecordFile)
	assert.True(t, os.IsNotExist(recordErr), "tmux attach shim must not be invoked")
}

func TestAgentStart_NormalRepoRequiresWorktree(t *testing.T) {
	repoDir, dataDir, _, _, cr, fsys := setupAgentTestEnvShort(t, "start-missing-worktree")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataDir, "config"))

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), cr, fsys, repoDir, AgentStartOpts{
		Headless: true,
		Prompt:   "hello",
	}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestAgentStart_DefaultsRepoAndWorktreeFromIntegrationCWD(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-infer", 1)

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), env.Runner, env.FS, env.WorktreePath, AgentStartOpts{
		Runner:        "claude-code",
		Detached:      true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Session started in detached mode.")
	assert.Contains(t, stdout.String(), "  worktree:       "+env.WorktreeID)
	assert.Empty(t, stderr.String())
}

func TestAgentStart_Headed_DetachedSkipsAttach(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-detached", 1)

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-detached",
		Runner:        "claude-code",
		Detached:      true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Session started in detached mode.")
	assert.Regexp(t, regexp.MustCompile(`Use 'agency agent [^ ]+ attach' to attach\.`), stdout.String())
	assert.NotContains(t, stdout.String(), "Use 'agency agent attach ")
	assert.Empty(t, stderr.String())

	invocationsDir := filepath.Join(env.DataDir, "repos", env.RepoID, "invocations")
	entries, readErr := os.ReadDir(invocationsDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "headed start should create exactly one invocation")

	_, recordErr := os.Stat(env.RecordFile)
	assert.True(t, os.IsNotExist(recordErr), "detached headed start must not attach")
}

func TestAgentStart_UsesUserRunnerDefaultsWhenCLIUnset(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-user-runner-defaults", 1)

	cfg := map[string]any{
		"version": 2,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runner_defaults": map[string]map[string]string{
			"claude-code": {
				"model":  "user-opus",
				"effort": "max",
			},
		},
		"runners": map[string]string{
			"claude-code": "fake-runner",
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(env.DataDir, "config", "config.json"), cfgBytes, 0o644))

	var stdout, stderr bytes.Buffer
	err = AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-user-runner-defaults",
		Runner:        "claude-code",
		Detached:      true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(filepath.Join(env.DataDir, "repos", env.RepoID, "invocations"))
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	meta, err := st.ReadInvocationMeta(env.RepoID, entries[0].Name())
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "user-opus", "--effort", "max"}, meta.RunnerArgs)
}

func TestAgentStart_AgencyConfigRunnerDefaultsOverrideUserConfig(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-agency-runner-defaults", 1)

	cfg := map[string]any{
		"version": 2,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runner_defaults": map[string]map[string]string{
			"claude-code": {
				"model":  "user-opus",
				"effort": "high",
			},
		},
		"runners": map[string]string{
			"claude-code": "fake-runner",
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(env.DataDir, "config", "config.json"), cfgBytes, 0o644))

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
      "model": "agency-opus",
      "effort": "max"
    }
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(env.RepoDir, "agency.json"), []byte(agencyJSON), 0o644))

	var stdout, stderr bytes.Buffer
	err = AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-agency-runner-defaults",
		Runner:        "claude-code",
		Detached:      true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(filepath.Join(env.DataDir, "repos", env.RepoID, "invocations"))
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	meta, err := st.ReadInvocationMeta(env.RepoID, entries[0].Name())
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "agency-opus", "--effort", "max"}, meta.RunnerArgs)
}

func TestAgentStart_CLIOverridesAgencyAndUserRunnerDefaults(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-cli-runner-defaults", 1)

	cfg := map[string]any{
		"version": 2,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runner_defaults": map[string]map[string]string{
			"claude-code": {
				"model":  "user-opus",
				"effort": "high",
			},
		},
		"runners": map[string]string{
			"claude-code": "fake-runner",
		},
	}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(env.DataDir, "config", "config.json"), cfgBytes, 0o644))

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
      "model": "agency-opus",
      "effort": "max"
    }
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(env.RepoDir, "agency.json"), []byte(agencyJSON), 0o644))

	var stdout, stderr bytes.Buffer
	err = AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-cli-runner-defaults",
		Runner:        "claude-code",
		Model:         "cli-opus",
		Effort:        "medium",
		Detached:      true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(filepath.Join(env.DataDir, "repos", env.RepoID, "invocations"))
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	meta, err := st.ReadInvocationMeta(env.RepoID, entries[0].Name())
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "cli-opus", "--effort", "medium"}, meta.RunnerArgs)
}

func TestAgentStart_ExplicitMissingAgencyConfigFails(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-missing-agency-config", 1)

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:          env.RepoID,
		WorktreeRef:      "start-missing-agency-config",
		Runner:           "claude-code",
		AgencyConfigPath: filepath.Join(env.RepoDir, "missing-agency.json"),
		Detached:         true,
		IsInteractive:    func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.ENoAgencyJSON, errors.GetCode(err))
}

func TestAgentStart_InvalidRepoAgencyConfigIncludesPathSourceAndHint(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-invalid-agency-config", 1)
	require.NoError(t, os.WriteFile(filepath.Join(env.RepoDir, "agency.json"), []byte(`{
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

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-invalid-agency-config",
		Runner:        "claude-code",
		Detached:      true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(env.RepoDir, "agency.json"), ae.Details["path"])
	assert.Equal(t, "repo", ae.Details["source"])
	assert.Contains(t, ae.Details["hint"], "agency init --path "+env.RepoDir+" --repo-config --force")
}

func TestAgentStart_Headed_AttachFailureWarnsButSucceeds(t *testing.T) {
	env := setupAgentStartHeadedTestEnv(t, "start-attach-fail", 1)

	var stdout, stderr bytes.Buffer
	err := AgentStart(context.Background(), env.Runner, env.FS, env.RepoDir, AgentStartOpts{
		RepoRef:       env.RepoID,
		WorktreeRef:   "start-attach-fail",
		Runner:        "claude-code",
		IsInteractive: func() bool { return true },
	}, &stdout, &stderr)
	require.NoError(t, err)

	output := stdout.String()
	require.Contains(t, output, "tmux_session:")
	session := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "  tmux_session:") {
			session = strings.TrimSpace(strings.TrimPrefix(line, "  tmux_session:"))
			break
		}
	}
	require.NotEmpty(t, session, "headed start must print the tmux session it attached to")

	cwd, args := readShimRecord(t, env.RecordFile)
	assert.NotEmpty(t, cwd)
	assert.Equal(t, "attach -t "+session, args)
	assert.Contains(t, stderr.String(), "warning: could not attach to tmux session:")
	assert.Regexp(t, regexp.MustCompile(`Use 'agency agent [^ ]+ attach' to attach later\.`), stderr.String())
	assert.NotContains(t, stderr.String(), "Use 'agency agent attach ")
}

func TestAgentAttach_UsesStoredTmuxSessionWithFallback(t *testing.T) {
	t.Run("stored session wins", func(t *testing.T) {
		env := setupAgentNavEnv(t, "attach-stored", store.RunnerModeHeaded)
		st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
		storedSession := "agency_explicit_attach"
		require.NoError(t, st.UpdateInvocationMeta(env.RepoID, env.InvocationID, func(meta *store.InvocationMeta) {
			meta.TmuxSession = storedSession
		}))

		fakeTmux := testutil.NewFakeTmuxClient()
		fakeTmux.Sessions[storedSession] = testutil.FakeTmuxSession{Name: storedSession}

		var attachCalled bool
		var attachedSession string

		var stdout, stderr bytes.Buffer
		err := AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
			AgentAttachOpts{
				InvocationRef:   env.InvocationID,
				RepoRef:         env.RepoID,
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
		assert.Equal(t, storedSession, attachedSession, "stored tmux_session must win over the derived name")
	})

	t.Run("fallback to derived session", func(t *testing.T) {
		env := setupAgentNavEnv(t, "attach-fallback", store.RunnerModeHeaded)
		st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
		require.NoError(t, st.UpdateInvocationMeta(env.RepoID, env.InvocationID, func(meta *store.InvocationMeta) {
			meta.TmuxSession = ""
		}))

		fakeTmux := testutil.NewFakeTmuxClient()
		fallbackSession := tmux.SessionName(env.InvocationID)
		fakeTmux.Sessions[fallbackSession] = testutil.FakeTmuxSession{Name: fallbackSession}

		var attachCalled bool
		var attachedSession string

		var stdout, stderr bytes.Buffer
		err := AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
			AgentAttachOpts{
				InvocationRef:   env.InvocationID,
				RepoRef:         env.RepoID,
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
		assert.Equal(t, fallbackSession, attachedSession, "agent attach must fall back to tmux.SessionName(invocation_id)")
	})
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
		RepoID: repoID, Branch: "agency/ambig", BaseBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))

	seedRepoIndexForNavigationTests(t, dataTmp, repoID)

	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude-code", Mode: store.RunnerModeHeaded,
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
		AgentPathOpts{InvocationRef: "20260201000000", RepoRef: repoID}, &stdout, &stderr)

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
		RepoID: repoID, Branch: "agency/ambig", BaseBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))

	seedRepoIndexForNavigationTests(t, dataTmp, repoID)

	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude-code", Mode: store.RunnerModeHeaded,
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
		AgentOpenOpts{InvocationRef: "20260201000000", RepoRef: repoID, Editor: shimPath}, &stdout, &stderr)

	require.Error(t, openErr)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(openErr),
		"navigation ambiguity must return E_AMBIGUOUS")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr),
		"editor shim must NOT be executed on ambiguous target")
}

func TestAgentAttach_AmbiguityUsesEAmbiguous_NoDispatch(t *testing.T) {
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
		RepoID: repoID, Branch: "agency/ambig", BaseBranch: "main",
		TreePath: treePath, CreatedAt: "2026-01-31T12:00:00Z", State: store.WorktreeStatePresent,
	}
	mBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), mBytes, 0644))
	seedRepoIndexForNavigationTests(t, dataTmp, repoID)

	for _, id := range []string{"20260201000000-aaaa", "20260201000000-bbbb"} {
		invDir := filepath.Join(dataTmp, "repos", repoID, "invocations", id)
		require.NoError(t, os.MkdirAll(invDir, 0755))
		sandboxTree := filepath.Join(dataTmp, "repos", repoID, "sandboxes", id, "tree")
		require.NoError(t, os.MkdirAll(sandboxTree, 0755))
		im := &store.InvocationMeta{
			SchemaVersion: "1.0", InvocationID: id,
			IntegrationWorktreeID: wtID, SandboxPath: sandboxTree,
			SandboxBranch: "agency/sandbox-" + id, BaseCommit: "abc123",
			Runner: "claude-code", Mode: store.RunnerModeHeaded,
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
	attachErr := AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentAttachOpts{
			InvocationRef:   "20260201000000",
			RepoRef:         repoID,
			IsInteractive:   func() bool { return true },
			TmuxClient:      fakeTmux,
			DataDirOverride: dataTmp,
			TmuxAttachFn: func(sess string) error {
				attachCalled = true
				return nil
			},
		}, &stdout, &stderr)

	require.Error(t, attachErr)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(attachErr),
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
			RepoID: r.repoID, Branch: "agency/b", BaseBranch: "main",
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
			Runner: "claude-code", Mode: store.RunnerModeHeaded,
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
		AgentPathOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	printedPath := strings.TrimSpace(stdout.String())
	assert.Equal(t, env.SandboxPath, printedPath,
		"printed path must equal daemon DTO sandbox_path (not re-derived)")
}

func TestAgentHumanOutput_RemainsHumanOriented_ScriptContractViaJSON(t *testing.T) {
	env := setupAgentNavEnv(t, "human", store.RunnerModeHeaded)

	var humanOut, jsonOut, stderr bytes.Buffer

	err := AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoRef: env.RepoID}, &humanOut, &stderr)
	require.NoError(t, err)

	err = AgentLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentLSOpts{RepoRef: env.RepoID, JSON: true}, &jsonOut, &stderr)
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

func TestAgentAttach_HeadlessInvocation_ReturnsInvalidMode(t *testing.T) {
	env := setupAgentNavEnv(t, "headless-attach", store.RunnerModeHeadless)

	fakeTmux := testutil.NewFakeTmuxClient()
	var attachCalled bool

	var stdout, stderr bytes.Buffer
	err := AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentAttachOpts{
			InvocationRef:   env.InvocationID,
			RepoRef:         env.RepoID,
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

func TestAgentAttach_NotInteractive_ReturnsENotInteractive(t *testing.T) {
	env := setupAgentNavEnv(t, "noterm-attach", store.RunnerModeHeaded)

	var stdout, stderr bytes.Buffer
	err := AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		AgentAttachOpts{
			InvocationRef:   env.InvocationID,
			RepoRef:         env.RepoID,
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

	for _, verb := range []string{"path", "open", "shell", "attach"} {
		t.Run(verb, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			var navErr error

			ref := "nonexistent-invocation"
			switch verb {
			case "path":
				navErr = AgentPath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentPathOpts{InvocationRef: ref, RepoRef: env.RepoID}, &stdout, &stderr)
			case "open":
				shimPath, _ := createShimScript(t)
				navErr = AgentOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentOpenOpts{InvocationRef: ref, RepoRef: env.RepoID, Editor: shimPath}, &stdout, &stderr)
			case "shell":
				shimPath, _ := createShimScript(t)
				t.Setenv("SHELL", shimPath)
				navErr = AgentShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentShellOpts{InvocationRef: ref, RepoRef: env.RepoID}, &stdout, &stderr)
			case "attach":
				navErr = AgentAttach(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
					AgentAttachOpts{
						InvocationRef:   ref,
						RepoRef:         env.RepoID,
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
		AgentOpenOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID, Editor: shimPath}, &stdout, &stderr)

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
		AgentShellOpts{InvocationRef: env.InvocationID, RepoRef: env.RepoID}, &stdout, &stderr)

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

	logsDir := st.InvocationLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.InvocationRawLogPath(repoID, invocationID), []byte("{\"raw\":1}\n"), 0o644))

	streamPath := st.InvocationStreamLogPath(repoID, invocationID)
	streamBytes := "" +
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"checking"}}` + "\n" +
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_end","data":{"name":"shell","command":"go test ./...","exit_code":0}}` + "\n"
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
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, os.WriteFile(st.InvocationRawLogPath(repoID, invocationID), []byte("raw\n"), 0o644))

	streamPath := st.InvocationStreamLogPath(repoID, invocationID)
	streamBytes := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"one"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"two"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:12Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_start","data":{"name":"shell","command":"echo hi"}}`,
		`{"schema_version":"1.0","seq":4,"timestamp":"2026-02-05T11:50:13Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_end","data":{"name":"shell","command":"echo hi","exit_code":0}}`,
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

func TestAgentHistory_InteractiveUsesSharedWatchRuntime(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-watch")
	invocationID := "20260131190500-watch"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var captured watch.RunOptions
	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		Limit:           50,
		DataDirOverride: dataDir,
		IsInteractive: func() bool {
			return true
		},
		RunWatch: func(_ context.Context, client *daemonclient.Client, opts watch.RunOptions) error {
			require.NotNil(t, client)
			captured = opts
			return nil
		},
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, watch.InitialPageHistory, captured.InitialPage)
	assert.Equal(t, invocationID, captured.InvocationID)
	assert.Equal(t, repoID, captured.RepoID)
	assert.NotNil(t, captured.Attach)
	assert.NotNil(t, captured.Open)
	assert.NotNil(t, captured.PRSync)
	assert.NotNil(t, captured.Restore)
}

// ---------------------------------------------------------------------------
// PR-B: AgentHistoryLogs integration tests
// ---------------------------------------------------------------------------

func TestAgentHistoryLogs_PageToEOF(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "logs-test")
	invocationID := "20260131140000-logs"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	// Seed a raw log file with known content
	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.InvocationLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.InvocationRawLogPath(repoID, invocationID), []byte("hello world\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistoryLogs(context.Background(), cr2, fsys, repoDir, AgentHistoryLogsOpts{
		InvocationRef:   invocationID,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, "hello world\n", stdout.String())
}

func TestAgentHistoryLogs_FollowMode(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "follow-test")
	invocationID := "20260131150000-foll"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.InvocationLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	logPath := st.InvocationRawLogPath(repoID, invocationID)
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
	err := AgentHistoryLogs(context.Background(), cr2, fsys, repoDir, AgentHistoryLogsOpts{
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

func TestAgentHistoryLogs_ContextCancellation(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "ctx-test")
	invocationID := "20260131160000-cctx"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.InvocationLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.InvocationRawLogPath(repoID, invocationID), []byte("data\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	ctx, cancel := context.WithCancel(context.Background())

	sleepFn := func(d time.Duration) {
		cancel() // cancel on first poll sleep
	}

	var stdout, stderr bytes.Buffer
	err := AgentHistoryLogs(ctx, cr2, fsys, repoDir, AgentHistoryLogsOpts{
		InvocationRef:   invocationID,
		Follow:          true,
		SleepFn:         sleepFn,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	// Should have read initial data before cancellation
	assert.Contains(t, stdout.String(), "data\n")
}

func TestAgentHistoryLogs_StderrKind(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "stderr-test")
	invocationID := "20260131170000-stde"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	logsDir := st.InvocationLogsDir(repoID, invocationID)
	require.NoError(t, os.MkdirAll(logsDir, 0o700))
	require.NoError(t, os.WriteFile(st.InvocationStderrLogPath(repoID, invocationID), []byte("error output\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistoryLogs(context.Background(), cr2, fsys, repoDir, AgentHistoryLogsOpts{
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

func TestAgentHistory_LastReturnsOnlyLastEntry(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-last")
	invocationID := "20260131180000-last"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))

	// Write 5 stream messages.
	streamPath := st.InvocationStreamLogPath(repoID, invocationID)
	var lines []string
	for i := 1; i <= 5; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"schema_version":"1.0","seq":%d,"timestamp":"2026-02-05T11:50:%02dZ","invocation_id":"%s","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"msg-%d"}}`,
			i, 10+i, invocationID, i))
	}
	require.NoError(t, os.WriteFile(streamPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		JSON:            true,
		Last:            true,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	var payload struct {
		Entries []struct {
			EntryID string                 `json:"entry_id"`
			Kind    string                 `json:"kind"`
			Data    map[string]interface{} `json:"data"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	require.Len(t, payload.Entries, 1, "--last must return exactly one entry")
	assert.Equal(t, "stream:5", payload.Entries[0].EntryID, "--last must return the chronologically last entry")
	assert.Equal(t, "msg-5", payload.Entries[0].Data["text"], "--last must return the last message content")
}

func TestAgentHistory_LastJSONReturnsAllEntriesFromLatestTurn(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-last-turn-entries")
	invocationID := "20260131180001-last-turn"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))

	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"working"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_start","data":{"name":"Bash","command":"echo hi"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:12Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_end","data":{"name":"Bash","command":"echo hi","exit_code":0}}`,
		`{"schema_version":"1.0","seq":4,"timestamp":"2026-02-05T11:50:13Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"final","data":{"duration_ms":1200}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		JSON:            true,
		Last:            true,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	var payload struct {
		Entries []daemon.TimelineEntryDTO `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	entryIDs := make([]string, 0, len(payload.Entries))
	for _, entry := range payload.Entries {
		entryIDs = append(entryIDs, entry.EntryID)
	}
	assert.Contains(t, entryIDs, "stream:1")
	assert.Contains(t, entryIDs, "stream:2")
	assert.Contains(t, entryIDs, "stream:3")
	assert.NotContains(t, entryIDs, "stream:4")
}

func TestAgentHistory_LastWithCursorReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), nil, nil, "/tmp", AgentHistoryOpts{
		InvocationRef: "some-inv",
		Last:          true,
		Cursor:        "some-cursor",
		Limit:         100,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestAgentHistory_HumanTurnOutput_ConvergesWithRestoreTurnSelection(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-turn-converge")
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	invocationID := "20260131200000-hturn"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFailed)

	st := store.NewStore(fsys, dataDir, time.Now)
	promptPath := st.InvocationPromptPath(repoID, invocationID)
	require.NoError(t, os.WriteFile(promptPath, []byte("investigate restore convergence"), 0o600))
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.PromptPath = promptPath
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"first assistant turn"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_end","data":{"name":"Write","command":"internal/service.go","exit_code":0}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:40Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"second assistant turn"}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))

	eventsLines := strings.Join([]string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:15Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":1}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:20Z","invocation_id":"` + invocationID + `","kind":"agency.followup_prompt","data":{"text":"continue from checkpoint one"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:30Z","invocation_id":"` + invocationID + `","kind":"agency.checkpoint_created","data":{"checkpoint_id":2}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(st.InvocationEventsPath(repoID, invocationID), []byte(eventsLines), 0o644))

	cpFile := checkpoint.CheckpointsFile{
		SchemaVersion: checkpoint.SchemaVersion,
		Checkpoints: []checkpoint.Checkpoint{
			{
				ID:                1,
				SnapshotRef:       checkpoint.RefPrefix + invocationID + "/1",
				SnapshotCommit:    "deadbeef",
				SandboxHeadSHA:    "deadbeef",
				CreatedAt:         "2026-02-05T11:50:15Z",
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
				Diffstat:          "+2 -1 in 2 files",
			},
		},
	}
	cpBytes, err := json.Marshal(cpFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(st.InvocationCheckpointsPath(repoID, invocationID), cpBytes, 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var historyOut, historyErr bytes.Buffer
	err = AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &historyOut, &historyErr)
	require.NoError(t, err)

	ns, err := setupDaemonNav(context.Background(), fsys, dataDir)
	require.NoError(t, err)
	entries, err := fetchAllTimelineEntries(context.Background(), ns.client, invocationID, repoID)
	require.NoError(t, err)
	checkpoints, err := fetchAllCheckpoints(context.Background(), ns.client, invocationID, repoID)
	require.NoError(t, err)
	projectedTurns := daemon.ProjectTimelineTurns(entries, checkpoints)
	require.NotEmpty(t, projectedTurns)

	human := historyOut.String()
	for _, turn := range projectedTurns {
		assert.Contains(t, human, turn.EntryID, "history output should expose the same projected turn ids")
	}
	assert.Contains(t, human, "[prompt]")
	assert.Contains(t, human, "[assistant]")
	assert.Contains(t, human, "[follow-up]")
	assert.Contains(t, human, "checkpoint=1")
	assert.Contains(t, human, "checkpoint=2")
}

func TestAgentHistory_DefaultHumanIsConciseWhileJSONRetainsFullPayload(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-concise")
	invocationID := "20260131201000-hconcise"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	payloadMarker := "S8_PR03_LARGE_TOOL_PAYLOAD_" + strings.Repeat("x", 256)
	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"applying edits","content_blocks":[{"type":"tool_use","name":"Edit","input":{"patch":"` + payloadMarker + `"}}]}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"tool_end","data":{"name":"Edit","command":"internal/service.go","exit_code":0}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var humanOut, jsonOut, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &humanOut, &stderr)
	require.NoError(t, err)

	err = AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		JSON:            true,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &jsonOut, &stderr)
	require.NoError(t, err)

	assert.NotContains(t, humanOut.String(), payloadMarker, "default human history should summarize, not dump huge tool payloads")
	assert.Contains(t, jsonOut.String(), payloadMarker, "json output must preserve full payload fidelity")
}

func TestAgentHistory_InvalidCursorReturnsEInvalidArgument(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-invalid-cursor")
	invocationID := "20260131201700-hcursor"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"first"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"second"}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		Cursor:          "missing-turn-id",
		Limit:           50,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestAgentHistory_LastReturnsLatestMeaningfulTurnNotFinalMarker(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-last-meaningful")
	invocationID := "20260131202000-hlast"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))
	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"meaningful assistant turn"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"final","data":{"duration_ms":1200}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		JSON:            true,
		Last:            true,
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
	require.Len(t, payload.Entries, 1)
	assert.Equal(t, "stream:1", payload.Entries[0].EntryID)
	assert.Equal(t, "message", payload.Entries[0].Kind)
}

func TestAgentHistory_HumanIncludesUnknownDiagnosticsWithinTurnProjection(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "history-human-unknown")
	invocationID := "20260131202101-hunknown"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFinished)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, os.MkdirAll(st.InvocationLogsDir(repoID, invocationID), 0o700))
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.StartedAt = "2026-02-05T11:50:00Z"
	}))

	streamLines := []string{
		`{"schema_version":"1.0","seq":1,"timestamp":"2026-02-05T11:50:10Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"message","data":{"role":"assistant","text":"working"}}`,
		`{"schema_version":"1.0","seq":2,"timestamp":"2026-02-05T11:50:11Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"unknown","data":{"runner_event_type":"cursor.weird_event","reason":"unrecognized_event_shape"}}`,
		`{"schema_version":"1.0","seq":3,"timestamp":"2026-02-05T11:50:12Z","invocation_id":"` + invocationID + `","runner":"claude-code","kind":"final","data":{"duration_ms":1200}}`,
	}
	require.NoError(t, os.WriteFile(st.InvocationStreamLogPath(repoID, invocationID), []byte(strings.Join(streamLines, "\n")+"\n"), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr2, fsys, repoDir, AgentHistoryOpts{
		InvocationRef:   invocationID,
		Limit:           100,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	human := stdout.String()
	assert.Contains(t, human, "stream:2", "unknown diagnostics must not disappear from turn-projected human history")
	assert.Contains(t, human, "unknown runner event")
}

func TestAgentFollowup_PromptFileOverLimitReturnsEPromptTooLarge(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "followup-file-limit")
	invocationID := "20260131181000-followup"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	oversizedPromptPath := filepath.Join(t.TempDir(), "prompt.txt")
	require.NoError(t, os.WriteFile(oversizedPromptPath, bytes.Repeat([]byte("x"), daemon.MaxPromptSize+1), 0o644))

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentFollowup(context.Background(), cr2, fsys, repoDir, AgentFollowupOpts{
		InvocationRef:   invocationID,
		PromptFile:      oversizedPromptPath,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EPromptTooLarge, errors.GetCode(err))
}

func TestAgentFollowup_HumanAndJSONAligned(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "followup-output")
	invocationID := "20260131182000-cout"

	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	cr2 := testutil.NewFakeCommandRunner()
	cr2.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr2.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var humanOut, jsonOut, stderr bytes.Buffer
	err := AgentFollowup(context.Background(), cr2, fsys, repoDir, AgentFollowupOpts{
		InvocationRef:   invocationID,
		Prompt:          "continue with regression analysis",
		DataDirOverride: dataDir,
	}, &humanOut, &stderr)
	require.NoError(t, err)

	err = AgentFollowup(context.Background(), cr2, fsys, repoDir, AgentFollowupOpts{
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
	assertMutationEnvelopeShape(t, payload)
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
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

func TestProjectTimelineTurns_SparseAssistantSummary_IncludesCheckpointMetadata(t *testing.T) {
	t.Parallel()

	entries := []daemon.TimelineEntryDTO{
		{
			EntryID:   "stream:1",
			Kind:      "message",
			Source:    "stream",
			Timestamp: "2026-02-05T11:50:10Z",
			Data: map[string]interface{}{
				"role": "assistant",
				"text": "Done.",
			},
		},
		{
			EntryID:   "inv_event:2:agency.checkpoint_created",
			Kind:      "checkpoint_event",
			Source:    "invocation_event",
			Timestamp: "2026-02-05T11:50:11Z",
			Data: map[string]interface{}{
				"event_kind":    "agency.checkpoint_created",
				"checkpoint_id": float64(1),
			},
		},
	}
	checkpoints := []daemon.CheckpointDTO{
		{
			ID:          1,
			Description: "After Edit",
			Diffstat:    "+12 -3 in 2 files",
		},
	}

	turns := daemon.ProjectTimelineTurns(entries, checkpoints)
	require.Len(t, turns, 1)
	require.Equal(t, daemon.TurnAssistant, turns[0].Kind)
	assert.Equal(t, 1, turns[0].CheckpointID)
	assert.True(t, turns[0].Restorable)

	// A sparse summary like "Done." should still be informative by incorporating
	// checkpoint metadata from the selected restore point.
	assert.Contains(t, turns[0].Summary, "After Edit")
	assert.Contains(t, turns[0].Summary, "+12 -3 in 2 files")
}
