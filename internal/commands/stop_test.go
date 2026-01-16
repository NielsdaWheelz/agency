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
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// fakeTmuxClient is a test double for tmux.Client.
type fakeTmuxClient struct {
	hasSessionResult bool
	hasSessionErr    error
	sendKeysErr      error
	killSessionErr   error

	// Track calls
	hasSessionCalls []string
	sendKeysCalls   []sendKeysCall
	killCalls       []string
}

type sendKeysCall struct {
	Name string
	Keys []tmux.Key
}

func (f *fakeTmuxClient) HasSession(ctx context.Context, name string) (bool, error) {
	f.hasSessionCalls = append(f.hasSessionCalls, name)
	return f.hasSessionResult, f.hasSessionErr
}

func (f *fakeTmuxClient) NewSession(ctx context.Context, name, cwd string, argv []string) error {
	return nil
}

func (f *fakeTmuxClient) Attach(ctx context.Context, name string) error {
	return nil
}

func (f *fakeTmuxClient) KillSession(ctx context.Context, name string) error {
	f.killCalls = append(f.killCalls, name)
	return f.killSessionErr
}

func (f *fakeTmuxClient) SendKeys(ctx context.Context, name string, keys []tmux.Key) error {
	f.sendKeysCalls = append(f.sendKeysCalls, sendKeysCall{Name: name, Keys: keys})
	return f.sendKeysErr
}

// setupStopTestEnv creates a temporary test environment for stop/kill tests.
func setupStopTestEnv(t *testing.T, runID string, setupMeta bool) (string, string, string, *fakeCommandRunner, fs.FS) {
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
			Title:           "test run",
			Runner:          "claude",
			RunnerCmd:       "claude",
			ParentBranch:    "main",
			Branch:          "agency/test-run-" + runID[:4],
			WorktreePath:    filepath.Join(dataDir, "repos", repoID, "worktrees", runID),
			CreatedAt:       "2026-01-10T12:00:00Z",
			TmuxSessionName: tmux.SessionName(runID),
		}
		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		metaPath := filepath.Join(runDir, "meta.json")
		if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Set environment for data dir resolution
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	return repoDir, dataDir, repoID, cr, fsys
}

// fakeCommandRunner for tests (simple version).
type fakeCommandRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)

	if resp, ok := f.responses[key]; ok {
		if resp.err != nil {
			return exec.CmdResult{}, resp.err
		}
		return exec.CmdResult{
			Stdout:   resp.stdout,
			Stderr:   resp.stderr,
			ExitCode: resp.exitCode,
		}, nil
	}

	// Default response for git commands
	if name == "git" {
		return exec.CmdResult{Stdout: "", ExitCode: 0}, nil
	}

	return exec.CmdResult{}, nil
}

func (f *fakeCommandRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

func TestStop_SessionExists(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: true,
	}

	var stdout, stderr bytes.Buffer
	opts := StopOpts{RunID: runID}

	err := StopWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}

	// Verify SendKeys was called with C-c
	if len(fakeTmux.sendKeysCalls) != 1 {
		t.Fatalf("expected 1 SendKeys call, got %d", len(fakeTmux.sendKeysCalls))
	}
	call := fakeTmux.sendKeysCalls[0]
	expectedSession := tmux.SessionName(runID)
	if call.Name != expectedSession {
		t.Errorf("SendKeys session = %q, want %q", call.Name, expectedSession)
	}
	if len(call.Keys) != 1 || call.Keys[0] != tmux.KeyCtrlC {
		t.Errorf("SendKeys keys = %v, want [C-c]", call.Keys)
	}

	// Verify meta.json was updated with needs_attention flag
	st := store.NewStore(fsys, dataDir, nil)
	meta, err := st.ReadMeta(repoID, runID)
	if err != nil {
		t.Fatalf("ReadMeta() error = %v", err)
	}
	if meta.Flags == nil || !meta.Flags.NeedsAttention {
		t.Error("expected flags.needs_attention = true")
	}

	// Verify event was appended
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"stop"`) {
		t.Error("expected stop event in events.jsonl")
	}
	if !strings.Contains(string(eventsData), `"keys":["C-c"]`) {
		t.Error("expected keys [C-c] in stop event")
	}
}

func TestStop_SessionMissing(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	var stdout, stderr bytes.Buffer
	opts := StopOpts{RunID: runID}

	err := StopWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Stop() error = %v, want nil (no-op)", err)
	}

	// Verify stderr message
	if !strings.Contains(stderr.String(), "no session for") {
		t.Errorf("stderr = %q, want contains 'no session for'", stderr.String())
	}

	// Verify SendKeys was NOT called
	if len(fakeTmux.sendKeysCalls) != 0 {
		t.Errorf("expected 0 SendKeys calls, got %d", len(fakeTmux.sendKeysCalls))
	}

	// Verify meta.json was NOT updated
	st := store.NewStore(fsys, dataDir, nil)
	meta, err := st.ReadMeta(repoID, runID)
	if err != nil {
		t.Fatalf("ReadMeta() error = %v", err)
	}
	if meta.Flags != nil && meta.Flags.NeedsAttention {
		t.Error("expected flags.needs_attention to NOT be set for missing session")
	}

	// Verify NO event was appended
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	_, err = os.ReadFile(eventsPath)
	if err == nil {
		t.Error("expected events.jsonl to not exist for no-op stop")
	}
}

func TestStop_RunNotFound(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys := setupStopTestEnv(t, runID, false) // no meta

	fakeTmux := &fakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := StopOpts{RunID: runID}

	err := StopWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("Stop() error = nil, want E_RUN_NOT_FOUND")
	}

	code := errors.GetCode(err)
	if code != errors.ERunNotFound {
		t.Errorf("error code = %q, want %q", code, errors.ERunNotFound)
	}
}
