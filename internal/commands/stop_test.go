package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupStopTestEnv creates a temporary test environment for stop/kill tests.
func setupStopTestEnv(t *testing.T, runID string, setupMeta bool) (string, string, string, *testutil.FakeCommandRunner, fs.FS) {
	t.Helper()

	// Create temp directories
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	// Initialize git repo
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0755))

	originURL := "git@github.com:test/repo.git"

	// Create fake command runner
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: originURL + "\n"}

	fsys := fs.NewRealFS()

	// Compute repo_id the same way the real code does
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	if setupMeta {
		// Create store directories
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		require.NoError(t, os.MkdirAll(runDir, 0755))

		// Write meta.json
		meta := &store.RunMeta{
			SchemaVersion:   "1.0",
			RunID:           runID,
			RepoID:          repoID,
			Name:            "test run",
			Runner:          "claude-code",
			RunnerCmd:       "claude",
			ParentBranch:    "main",
			Branch:          "agency/test-run-" + runID[:4],
			WorktreePath:    filepath.Join(dataDir, "repos", repoID, "worktrees", runID),
			CreatedAt:       "2026-01-10T12:00:00Z",
			TmuxSessionName: tmux.SessionName(runID),
		}
		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		metaPath := filepath.Join(runDir, "meta.json")
		require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))
	}

	return repoDir, dataDir, repoID, cr, fsys
}

func TestStop_SessionExists(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	var stdout, stderr bytes.Buffer
	opts := StopOpts{RunID: runID, DataDirOverride: dataDir}

	err := StopWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.NoError(t, err)

	// Verify SendKeys was called with C-c
	require.Len(t, fakeTmux.SendKeysCalls, 1)
	call := fakeTmux.SendKeysCalls[0]
	expectedSession := tmux.SessionName(runID)
	assert.Equal(t, expectedSession, call.Name)
	require.Len(t, call.Keys, 1)
	assert.Equal(t, tmux.KeyCtrlC, call.Keys[0])

	// Verify meta.json was updated with needs_attention flag
	st := store.NewStore(fsys, dataDir, nil)
	meta, err := st.ReadMeta(repoID, runID)
	require.NoError(t, err)
	require.NotNil(t, meta.Flags, "expected flags.needs_attention = true")
	assert.True(t, meta.Flags.NeedsAttention, "expected flags.needs_attention = true")

	// Verify event was appended
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"stop"`)
	assert.Contains(t, string(eventsData), `"keys":["C-c"]`)
}

func TestStop_SessionMissing(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupStopTestEnv(t, runID, true)

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	var stdout, stderr bytes.Buffer
	opts := StopOpts{RunID: runID, DataDirOverride: dataDir}

	err := StopWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.NoError(t, err)

	// Verify stderr message
	assert.Contains(t, stderr.String(), "no session for")

	// Verify SendKeys was NOT called
	assert.Len(t, fakeTmux.SendKeysCalls, 0)

	// Verify meta.json was NOT updated
	st := store.NewStore(fsys, dataDir, nil)
	meta, err := st.ReadMeta(repoID, runID)
	require.NoError(t, err)
	if meta.Flags != nil {
		assert.False(t, meta.Flags.NeedsAttention, "expected flags.needs_attention to NOT be set for missing session")
	}

	// Verify NO event was appended
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	_, err = os.ReadFile(eventsPath)
	assert.Error(t, err, "expected events.jsonl to not exist for no-op stop")
}

func TestStop_RunNotFound(t *testing.T) {
	t.Parallel()
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys := setupStopTestEnv(t, runID, false) // no meta

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := StopOpts{RunID: runID, DataDirOverride: dataDir}

	err := StopWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, &stdout, &stderr)
	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.ERunNotFound, code)
}
