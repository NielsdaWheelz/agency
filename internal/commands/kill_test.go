package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKill_SessionExists(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID, DataDirOverride: dataDir}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.NoError(t, err, "Kill()")

	// Verify KillSession was called
	require.Len(t, fakeTmux.KillSessionCalls, 1, "expected 1 KillSession call")
	expectedSession := tmux.SessionName(runID)
	assert.Equal(t, expectedSession, fakeTmux.KillSessionCalls[0], "KillSession session")

	// Verify event was appended
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"kill_session"`, "expected kill_session event")
	assert.Contains(t, string(eventsData), expectedSession, "expected session_name in kill_session event")
}

func TestKill_SessionMissing(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID, DataDirOverride: dataDir}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.NoError(t, err, "Kill() should be no-op")

	// Verify stderr message
	assert.Contains(t, stderr.String(), "no session for")

	// Verify KillSession was NOT called
	assert.Empty(t, fakeTmux.KillSessionCalls, "expected 0 KillSession calls")

	// Verify NO event was appended
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	assert.NoFileExists(t, eventsPath, "expected events.jsonl to not exist for no-op kill")
}

func TestKill_RunNotFound(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys := setupStopTestEnv(t, runID, false) // no meta

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID, DataDirOverride: dataDir}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.Error(t, err, "Kill() error = nil, want E_RUN_NOT_FOUND")
	assert.Equal(t, errors.ERunNotFound, errors.GetCode(err))
}

func TestKill_KillSessionError(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true
	fakeTmux.KillSessionErr = errors.New(errors.ETmuxFailed, "kill failed")

	var stdout, stderr bytes.Buffer
	opts := KillOpts{RunID: runID, DataDirOverride: dataDir}

	err := KillWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.Error(t, err, "Kill() error = nil, want error")
	assert.Equal(t, errors.ETmuxFailed, errors.GetCode(err))
}
