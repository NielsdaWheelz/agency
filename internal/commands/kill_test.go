package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func TestKill_SessionExists(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: true,
	}

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Kill() error = %v, want nil", err)
	}

	// Verify KillSession was called
	if len(fakeTmux.killCalls) != 1 {
		t.Fatalf("expected 1 KillSession call, got %d", len(fakeTmux.killCalls))
	}
	expectedSession := tmux.SessionName(runID)
	if fakeTmux.killCalls[0] != expectedSession {
		t.Errorf("KillSession session = %q, want %q", fakeTmux.killCalls[0], expectedSession)
	}

	// Verify event was appended
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"kill_session"`) {
		t.Error("expected kill_session event in events.jsonl")
	}
	if !strings.Contains(string(eventsData), expectedSession) {
		t.Error("expected session_name in kill_session event")
	}
}

func TestKill_SessionMissing(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Kill() error = %v, want nil (no-op)", err)
	}

	// Verify stderr message
	if !strings.Contains(stderr.String(), "no session for") {
		t.Errorf("stderr = %q, want contains 'no session for'", stderr.String())
	}

	// Verify KillSession was NOT called
	if len(fakeTmux.killCalls) != 0 {
		t.Errorf("expected 0 KillSession calls, got %d", len(fakeTmux.killCalls))
	}

	// Verify NO event was appended
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	_, err = os.ReadFile(eventsPath)
	if err == nil {
		t.Error("expected events.jsonl to not exist for no-op kill")
	}
}

func TestKill_RunNotFound(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys := setupStopTestEnv(t, runID, false) // no meta

	fakeTmux := &fakeTmuxClient{}

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("Kill() error = nil, want E_RUN_NOT_FOUND")
	}

	code := errors.GetCode(err)
	if code != errors.ERunNotFound {
		t.Errorf("error code = %q, want %q", code, errors.ERunNotFound)
	}
}

func TestKill_KillSessionError(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: true,
		killSessionErr:   errors.New(errors.ETmuxFailed, "kill failed"),
	}

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("Kill() error = nil, want error")
	}

	code := errors.GetCode(err)
	if code != errors.ETmuxFailed {
		t.Errorf("error code = %q, want %q", code, errors.ETmuxFailed)
	}
}
