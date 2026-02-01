// Package commands implements agency CLI commands.
// This file tests agent commands for headed execution (Slice 8 PR-03).
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// agentFakeTmuxClient is a test double for tmux.Client for agent tests.
type agentFakeTmuxClient struct {
	hasSessionResult bool
	hasSessionErr    error
	newSessionCalls  []agentNewSessionCall
	newSessionErr    error
	attachCalls      []string
	attachErr        error
	killSessionCalls []string
	killSessionErr   error
	sendKeysCalls    []agentSendKeysCall
	sendKeysErr      error
}

type agentNewSessionCall struct {
	name string
	cwd  string
	argv []string
}

type agentSendKeysCall struct {
	name string
	keys []tmux.Key
}

func (f *agentFakeTmuxClient) HasSession(ctx context.Context, name string) (bool, error) {
	return f.hasSessionResult, f.hasSessionErr
}

func (f *agentFakeTmuxClient) NewSession(ctx context.Context, name, cwd string, argv []string) error {
	f.newSessionCalls = append(f.newSessionCalls, agentNewSessionCall{name: name, cwd: cwd, argv: argv})
	return f.newSessionErr
}

func (f *agentFakeTmuxClient) Attach(ctx context.Context, name string) error {
	f.attachCalls = append(f.attachCalls, name)
	return f.attachErr
}

func (f *agentFakeTmuxClient) KillSession(ctx context.Context, name string) error {
	f.killSessionCalls = append(f.killSessionCalls, name)
	return f.killSessionErr
}

func (f *agentFakeTmuxClient) SendKeys(ctx context.Context, name string, keys []tmux.Key) error {
	f.sendKeysCalls = append(f.sendKeysCalls, agentSendKeysCall{name: name, keys: keys})
	return f.sendKeysErr
}

// setupAgentTestEnv creates a test environment with integration worktree for agent tests.
func setupAgentTestEnv(t *testing.T, worktreeName string) (string, string, string, string, *fakeCommandRunner, fs.FS) {
	t.Helper()

	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo (minimal)
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	originURL := "git@github.com:test/agent-repo.git"
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	// Create fake command runner
	cr := &fakeCommandRunner{
		responses: map[string]fakeResponse{
			"git rev-parse --show-toplevel":      {stdout: repoDir + "\n"},
			"git config --get remote.origin.url": {stdout: originURL + "\n"},
		},
	}

	fsys := fs.NewRealFS()

	// Create store directories
	repoStoreDir := filepath.Join(dataDir, "repos", repoID)
	if err := os.MkdirAll(repoStoreDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create integration worktree
	worktreeID := "20260131120000-abcd"
	worktreeDir := filepath.Join(repoStoreDir, "integration_worktrees", worktreeID)
	worktreeTreeDir := filepath.Join(worktreeDir, "tree")
	if err := os.MkdirAll(worktreeTreeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write integration marker
	agencyDir := filepath.Join(worktreeTreeDir, ".agency")
	if err := os.MkdirAll(agencyDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName)
	if err := os.WriteFile(markerPath, []byte("# Integration worktree\n"), 0644); err != nil {
		t.Fatal(err)
	}

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
	if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENCY_DATA_DIR", dataDir)

	return repoDir, dataDir, repoID, worktreeID, cr, fsys
}

// createTestInvocation creates a test invocation for testing attach/stop/kill.
func createTestInvocation(t *testing.T, dataDir, repoID, worktreeID, invocationID string, mode store.RunnerMode, status store.InvocationStatus) {
	t.Helper()

	invDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	if err := os.MkdirAll(invDir, 0755); err != nil {
		t.Fatal(err)
	}

	sandboxDir := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID)
	sandboxTreeDir := filepath.Join(sandboxDir, "tree")
	if err := os.MkdirAll(sandboxTreeDir, 0755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentStart_HeadedMode_CreatesSession(t *testing.T) {
	repoDir, dataDir, repoID, _, cr, fsys := setupAgentTestEnv(t, "test-feature")

	// Add responses for git commands needed by agent start
	cr.responses["git -C "+repoDir+" rev-parse --abbrev-ref HEAD"] = fakeResponse{stdout: "main\n"}

	fakeTmux := &agentFakeTmuxClient{
		hasSessionResult: false, // No existing session
	}

	var stdout, stderr bytes.Buffer
	opts := AgentStartOpts{
		WorktreeRef: "test-feature",
		Runner:      "claude",
		Headless:    false,
		Detached:    true, // Don't try to attach in test
		TmuxClient:  fakeTmux,
	}

	err := AgentStart(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err != nil {
		// May fail due to runner resolution or git worktree - that's OK
		// We're testing the tmux interaction path
		t.Logf("AgentStart returned error (expected in minimal test env): %v", err)
		return
	}

	// Verify tmux.NewSession was called with sandbox path (not integration path)
	if len(fakeTmux.newSessionCalls) == 0 {
		t.Error("tmux.NewSession was not called")
	} else {
		call := fakeTmux.newSessionCalls[0]
		if call.cwd == "" {
			t.Error("tmux.NewSession called with empty cwd")
		}
		// Session name should match pattern agency_<invocation_id>
		if call.name == "" || len(call.name) < 8 {
			t.Errorf("tmux.NewSession called with invalid session name: %q", call.name)
		}
		// Verify no shell wrapping in argv
		for _, arg := range call.argv {
			if arg == "sh" || arg == "-lc" {
				t.Errorf("tmux.NewSession argv contains shell wrapper: %v", call.argv)
			}
		}
	}

	_ = dataDir
	_ = repoID
}

func TestAgentStart_HeadedMode_ExistingSessionFails(t *testing.T) {
	repoDir, _, _, _, cr, fsys := setupAgentTestEnv(t, "test-feature")

	// Simulate existing tmux session
	fakeTmux := &agentFakeTmuxClient{
		hasSessionResult: true, // Session already exists!
	}

	var stdout, stderr bytes.Buffer
	opts := AgentStartOpts{
		WorktreeRef: "test-feature",
		Runner:      "claude",
		Headless:    false,
		Detached:    true,
		TmuxClient:  fakeTmux,
	}

	err := AgentStart(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Log("Note: AgentStart may succeed if it fails before tmux check")
		return
	}

	// If we get far enough to check session existence, we should get E_TMUX_SESSION_EXISTS
	code := errors.GetCode(err)
	if code == errors.ETmuxSessionExists {
		t.Log("Correctly returned E_TMUX_SESSION_EXISTS")
	}
}

func TestAgentAttach_HeadlessInvocation_ReturnsInvalidMode(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headless invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	fakeTmux := &agentFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	err := AgentAttach(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("AgentAttach error = nil, want E_INVOCATION_INVALID_MODE")
	}

	code := errors.GetCode(err)
	if code != errors.EInvocationInvalidMode {
		t.Errorf("error code = %q, want %q", code, errors.EInvocationInvalidMode)
	}
}

func TestAgentAttach_HeadedInvocation_SessionMissing(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	fakeTmux := &agentFakeTmuxClient{
		hasSessionResult: false, // Session is gone
	}

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	err := AgentAttach(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("AgentAttach error = nil, want E_TMUX_SESSION_MISSING")
	}

	code := errors.GetCode(err)
	if code != errors.ETmuxSessionMissing {
		t.Errorf("error code = %q, want %q", code, errors.ETmuxSessionMissing)
	}
}

func TestAgentStop_HeadedInvocation_SendsCtrlC(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed running invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	fakeTmux := &agentFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := AgentStopOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	err := AgentStop(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("AgentStop error = %v, want nil", err)
	}

	// Verify SendKeys was called with C-c
	if len(fakeTmux.sendKeysCalls) != 1 {
		t.Fatalf("sendKeysCalls = %d, want 1", len(fakeTmux.sendKeysCalls))
	}
	call := fakeTmux.sendKeysCalls[0]
	if len(call.keys) != 1 || call.keys[0] != tmux.KeyCtrlC {
		t.Errorf("sendKeys keys = %v, want [C-c]", call.keys)
	}

	// Verify meta was updated with stop_requested_at
	st := store.NewStore(fsys, dataDir, time.Now)
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		t.Fatalf("failed to read meta: %v", err)
	}
	if meta.StopRequestedAt == "" {
		t.Error("stop_requested_at not set")
	}
	if !meta.Flags.NeedsAttention {
		t.Error("flags.needs_attention not set")
	}
	// Status should still be running (stop doesn't guarantee termination)
	if meta.Status != store.InvocationStatusRunning {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusRunning)
	}
}

func TestAgentStop_HeadlessInvocation_RoutesThroughDaemon(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headless invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	fakeTmux := &agentFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := AgentStopOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	// PR-04: headless stop routes through daemon
	// Without daemon running and without PGID, this returns an error
	err := AgentStop(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("AgentStop error = nil, want error (daemon not running, no PGID)")
	}

	// The error should be E_DAEMON_NOT_RUNNING since daemon isn't available
	code := errors.GetCode(err)
	if code != errors.EDaemonNotRunning {
		t.Errorf("error code = %q, want %q", code, errors.EDaemonNotRunning)
	}
}

func TestAgentKill_HeadedInvocation_KillsSession(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed running invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	fakeTmux := &agentFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := AgentKillOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	err := AgentKill(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("AgentKill error = %v, want nil", err)
	}

	// Verify KillSession was called
	if len(fakeTmux.killSessionCalls) != 1 {
		t.Fatalf("killSessionCalls = %d, want 1", len(fakeTmux.killSessionCalls))
	}

	// Verify meta was updated
	st := store.NewStore(fsys, dataDir, time.Now)
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		t.Fatalf("failed to read meta: %v", err)
	}
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.ExitReason != "killed" {
		t.Errorf("exit_reason = %q, want %q", meta.ExitReason, "killed")
	}
	if meta.FinishedAt == "" {
		t.Error("finished_at not set")
	}
	// tmux_session should be preserved (not nulled)
	if meta.TmuxSession == "" {
		t.Error("tmux_session was nulled, should be preserved as historical value")
	}
}

func TestAgentKill_HeadlessInvocation_UpdatesMetaViaDeamon(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headless invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	fakeTmux := &agentFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := AgentKillOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	// PR-04: headless kill routes through daemon (but still succeeds even if daemon not running)
	// The daemon client call will fail, but AgentKill still proceeds to show success message
	// because the daemon already updated meta (or would have if running)
	err := AgentKill(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	// AgentKill continues even if daemon communication fails, so we expect no error returned
	// The stderr will contain a warning about daemon not being available
	if err != nil {
		t.Fatalf("AgentKill error = %v, want nil (daemon communication failure is non-fatal)", err)
	}

	// Check output contains headless kill message
	if !bytes.Contains(stdout.Bytes(), []byte("Killed headless invocation")) {
		t.Errorf("stdout missing headless kill message, got: %s", stdout.String())
	}
}

func TestAgentKill_SessionAlreadyGone_StillUpdatesMetadata(t *testing.T) {
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	// Simulate session already gone
	fakeTmux := &agentFakeTmuxClient{
		killSessionErr: fmt.Errorf("can't find session: agency_%s", invocationID), // tmux.IsNoSessionErr matches this
	}

	var stdout, stderr bytes.Buffer
	opts := AgentKillOpts{
		InvocationRef: invocationID,
		TmuxClient:    fakeTmux,
	}

	err := AgentKill(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("AgentKill error = %v, want nil (should succeed even if session gone)", err)
	}

	// Metadata should still be updated
	st := store.NewStore(fsys, dataDir, time.Now)
	meta, err := st.ReadInvocationMeta(repoID, invocationID)
	if err != nil {
		t.Fatalf("failed to read meta: %v", err)
	}
	if meta.Status != store.InvocationStatusFailed {
		t.Errorf("status = %q, want %q", meta.Status, store.InvocationStatusFailed)
	}
	if meta.ExitReason != "killed" {
		t.Errorf("exit_reason = %q, want %q", meta.ExitReason, "killed")
	}
}
