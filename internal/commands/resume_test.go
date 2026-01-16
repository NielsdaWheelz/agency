package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// resumeFakeTmuxClient is a test double for tmux.Client that tracks all calls.
type resumeFakeTmuxClient struct {
	hasSessionResults []bool // sequence of results for HasSession calls
	hasSessionErr     error
	hasSessionCalls   []string

	newSessionCalls []newSessionCall
	newSessionErr   error

	attachCalls []string
	attachErr   error

	killCalls []string
	killErr   error

	callIndex int // tracks which hasSessionResult to return
}

type newSessionCall struct {
	Name string
	Cwd  string
	Argv []string
}

func (f *resumeFakeTmuxClient) HasSession(ctx context.Context, name string) (bool, error) {
	f.hasSessionCalls = append(f.hasSessionCalls, name)
	if f.hasSessionErr != nil {
		return false, f.hasSessionErr
	}
	result := false
	if f.callIndex < len(f.hasSessionResults) {
		result = f.hasSessionResults[f.callIndex]
		f.callIndex++
	}
	return result, nil
}

func (f *resumeFakeTmuxClient) NewSession(ctx context.Context, name, cwd string, argv []string) error {
	f.newSessionCalls = append(f.newSessionCalls, newSessionCall{Name: name, Cwd: cwd, Argv: argv})
	return f.newSessionErr
}

func (f *resumeFakeTmuxClient) Attach(ctx context.Context, name string) error {
	f.attachCalls = append(f.attachCalls, name)
	return f.attachErr
}

func (f *resumeFakeTmuxClient) KillSession(ctx context.Context, name string) error {
	f.killCalls = append(f.killCalls, name)
	return f.killErr
}

func (f *resumeFakeTmuxClient) SendKeys(ctx context.Context, name string, keys []tmux.Key) error {
	return nil
}

// setupResumeTestEnv creates a temporary test environment for resume tests.
// If createWorktree is true, also creates the worktree directory.
func setupResumeTestEnv(t *testing.T, runID string, setupMeta, createWorktree bool, archived bool) (string, string, string, *fakeCommandRunner, fs.FS) {
	t.Helper()

	// Create temp directories
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create agency.json
	agencyJSON := `{
		"version": 1,
		"defaults": {
			"parent_branch": "main",
			"runner": "claude"
		},
		"scripts": {
			"setup": "scripts/agency_setup.sh",
			"verify": "scripts/agency_verify.sh",
			"archive": "scripts/agency_archive.sh"
		}
	}`
	if err := os.WriteFile(filepath.Join(repoDir, "agency.json"), []byte(agencyJSON), 0644); err != nil {
		t.Fatal(err)
	}

	originURL := "git@github.com:test/repo.git"

	// Create fake command runner
	cr := &fakeCommandRunner{
		responses: map[string]fakeResponse{
			"git rev-parse --show-toplevel":      {stdout: repoDir + "\n"},
			"git config --get remote.origin.url": {stdout: originURL + "\n"},
		},
	}

	fsys := fs.NewRealFS()

	// Compute repo_id the same way the real code does
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)

	if setupMeta {
		// Create store directories
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write meta.json
		meta := &store.RunMeta{
			SchemaVersion:   "1.0",
			RunID:           runID,
			RepoID:          repoID,
			Name:           "test run",
			Runner:          "claude",
			RunnerCmd:       "claude",
			ParentBranch:    "main",
			Branch:          "agency/test-run-" + runID[:4],
			WorktreePath:    worktreePath,
			CreatedAt:       "2026-01-10T12:00:00Z",
			TmuxSessionName: tmux.SessionName(runID),
		}

		if archived {
			meta.Archive = &store.RunMetaArchive{
				ArchivedAt: "2026-01-11T12:00:00Z",
			}
		}

		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		metaPath := filepath.Join(runDir, "meta.json")
		if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if createWorktree {
		if err := os.MkdirAll(worktreePath, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Set environment for data dir resolution
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	return repoDir, dataDir, repoID, cr, fsys
}

func TestResume_SessionExists_NotDetached(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{true}, // session exists
	}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: false}

	// Note: Attach will fail because it uses real exec.Command
	// but we can verify the event was written
	_ = ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)

	// Verify HasSession was called
	if len(fakeTmux.hasSessionCalls) < 1 {
		t.Fatal("expected at least 1 HasSession call")
	}

	// Verify NewSession was NOT called (session exists)
	if len(fakeTmux.newSessionCalls) != 0 {
		t.Errorf("expected 0 NewSession calls, got %d", len(fakeTmux.newSessionCalls))
	}

	// Verify resume_attach event was written
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"resume_attach"`) {
		t.Error("expected resume_attach event in events.jsonl")
	}
}

func TestResume_SessionExists_Detached(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{true}, // session exists
	}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: true}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Resume() error = %v, want nil", err)
	}

	// Verify stdout contains success message
	if !strings.Contains(stdout.String(), "ok: session") {
		t.Errorf("stdout = %q, want contains 'ok: session'", stdout.String())
	}

	// Verify Attach was NOT called (detached mode)
	if len(fakeTmux.attachCalls) != 0 {
		t.Errorf("expected 0 Attach calls in detached mode, got %d", len(fakeTmux.attachCalls))
	}

	// Verify resume_attach event was written with detached=true
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"detached":true`) {
		t.Error("expected detached=true in resume_attach event")
	}
}

func TestResume_SessionMissing_CreateSession(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{false, false}, // missing initially, still missing under lock
	}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: true} // detached to avoid attach call

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Resume() error = %v, want nil", err)
	}

	// Verify NewSession was called
	if len(fakeTmux.newSessionCalls) != 1 {
		t.Fatalf("expected 1 NewSession call, got %d", len(fakeTmux.newSessionCalls))
	}

	call := fakeTmux.newSessionCalls[0]
	expectedSession := tmux.SessionName(runID)
	if call.Name != expectedSession {
		t.Errorf("NewSession name = %q, want %q", call.Name, expectedSession)
	}
	if len(call.Argv) != 1 || call.Argv[0] != "claude" {
		t.Errorf("NewSession argv = %v, want [claude]", call.Argv)
	}

	// Verify resume_create event was written
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"resume_create"`) {
		t.Error("expected resume_create event in events.jsonl")
	}
}

func TestResume_Restart_WithYes(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{true, true}, // exists initially, exists under lock
	}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Restart: true, Yes: true, Detached: true}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Resume() error = %v, want nil", err)
	}

	// Verify KillSession was called
	if len(fakeTmux.killCalls) != 1 {
		t.Fatalf("expected 1 KillSession call, got %d", len(fakeTmux.killCalls))
	}

	// Verify NewSession was called after kill
	if len(fakeTmux.newSessionCalls) != 1 {
		t.Fatalf("expected 1 NewSession call, got %d", len(fakeTmux.newSessionCalls))
	}

	// Verify resume_restart event was written
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"resume_restart"`) {
		t.Error("expected resume_restart event in events.jsonl")
	}
	if !strings.Contains(string(eventsData), `"restart":true`) {
		t.Error("expected restart=true in resume_restart event")
	}
}

func TestResume_Restart_NonInteractive_NoYes(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{true}, // session exists
	}

	// Override isInteractive to return false (non-interactive)
	originalIsInteractive := isInteractive
	isInteractive = func() bool { return false }
	defer func() { isInteractive = originalIsInteractive }()

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Restart: true, Yes: false}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("Resume() error = nil, want E_CONFIRMATION_REQUIRED")
	}

	code := errors.GetCode(err)
	if code != errors.EConfirmationRequired {
		t.Errorf("error code = %q, want %q", code, errors.EConfirmationRequired)
	}

	// Verify no tmux mutations occurred
	if len(fakeTmux.killCalls) != 0 {
		t.Errorf("expected 0 KillSession calls, got %d", len(fakeTmux.killCalls))
	}
	if len(fakeTmux.newSessionCalls) != 0 {
		t.Errorf("expected 0 NewSession calls, got %d", len(fakeTmux.newSessionCalls))
	}
}

func TestResume_Restart_Interactive_UserDeclinesNo(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{true}, // session exists
	}

	// Override isInteractive to return true
	originalIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	defer func() { isInteractive = originalIsInteractive }()

	// User enters "n" (decline)
	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Restart: true, Yes: false}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader("n\n"), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Resume() error = %v, want nil (user canceled)", err)
	}

	// Verify "canceled" message
	if !strings.Contains(stderr.String(), "canceled") {
		t.Errorf("stderr = %q, want contains 'canceled'", stderr.String())
	}

	// Verify no tmux mutations occurred
	if len(fakeTmux.killCalls) != 0 {
		t.Errorf("expected 0 KillSession calls, got %d", len(fakeTmux.killCalls))
	}
	if len(fakeTmux.newSessionCalls) != 0 {
		t.Errorf("expected 0 NewSession calls, got %d", len(fakeTmux.newSessionCalls))
	}

	// Verify NO event was written (user canceled)
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	_, err = os.ReadFile(eventsPath)
	if err == nil {
		// File should not exist or be empty for canceled operation
		data, _ := os.ReadFile(eventsPath)
		if len(data) > 0 {
			t.Error("expected no events for canceled restart")
		}
	}
}

func TestResume_WorktreeMissing_Archived(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, false, true) // meta exists, worktree missing, archived

	fakeTmux := &resumeFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("Resume() error = nil, want E_WORKTREE_MISSING")
	}

	code := errors.GetCode(err)
	if code != errors.EWorktreeMissing {
		t.Errorf("error code = %q, want %q", code, errors.EWorktreeMissing)
	}

	// Verify error message mentions archived
	if !strings.Contains(err.Error(), "archived") {
		t.Errorf("error = %q, want contains 'archived'", err.Error())
	}

	// Verify resume_failed event was written with reason=archived
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"resume_failed"`) {
		t.Error("expected resume_failed event in events.jsonl")
	}
	if !strings.Contains(string(eventsData), `"reason":"archived"`) {
		t.Error("expected reason=archived in resume_failed event")
	}
}

func TestResume_WorktreeMissing_Corrupted(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, false, false) // meta exists, worktree missing, NOT archived

	fakeTmux := &resumeFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("Resume() error = nil, want E_WORKTREE_MISSING")
	}

	code := errors.GetCode(err)
	if code != errors.EWorktreeMissing {
		t.Errorf("error code = %q, want %q", code, errors.EWorktreeMissing)
	}

	// Verify error message mentions corrupted
	if !strings.Contains(err.Error(), "corrupted") {
		t.Errorf("error = %q, want contains 'corrupted'", err.Error())
	}

	// Verify resume_failed event was written with reason=missing
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"reason":"missing"`) {
		t.Error("expected reason=missing in resume_failed event")
	}
}

func TestResume_RunNotFound(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys := setupResumeTestEnv(t, runID, false, false, false) // no meta

	fakeTmux := &resumeFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("Resume() error = nil, want E_RUN_NOT_FOUND")
	}

	code := errors.GetCode(err)
	if code != errors.ERunNotFound {
		t.Errorf("error code = %q, want %q", code, errors.ERunNotFound)
	}
}

func TestResume_MissingRunID(t *testing.T) {
	repoDir, _, _, cr, fsys := setupResumeTestEnv(t, "dummy", false, false, false)

	fakeTmux := &resumeFakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: ""} // empty run_id

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("Resume() error = nil, want E_USAGE")
	}

	code := errors.GetCode(err)
	if code != errors.EUsage {
		t.Errorf("error code = %q, want %q", code, errors.EUsage)
	}
}

func TestResume_SessionRace_CreateBecomesAttach(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	// Simulate race: session missing initially, exists under lock (another process created it)
	fakeTmux := &resumeFakeTmuxClient{
		hasSessionResults: []bool{false, true}, // missing, then exists (race)
	}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: true}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Resume() error = %v, want nil", err)
	}

	// Verify NewSession was NOT called (race detected, session exists)
	if len(fakeTmux.newSessionCalls) != 0 {
		t.Errorf("expected 0 NewSession calls due to race, got %d", len(fakeTmux.newSessionCalls))
	}

	// Verify resume_attach event was written (race path)
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"resume_attach"`) {
		t.Error("expected resume_attach event for race path")
	}
}
