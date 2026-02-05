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
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupResumeTestEnv creates a temporary test environment for resume tests.
// If createWorktree is true, also creates the worktree directory.
func setupResumeTestEnv(t *testing.T, runID string, setupMeta, createWorktree bool, archived bool) (string, string, string, *testutil.FakeCommandRunner, fs.FS) {
	t.Helper()

	// Create temp directories
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	// Initialize git repo
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0755))

	// Create agency.json
	agencyJSON := `{
		"version": 1,
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
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "agency.json"), []byte(agencyJSON), 0644))

	originURL := "git@github.com:test/repo.git"

	// Create fake command runner
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: originURL + "\n"}

	fsys := fs.NewRealFS()

	// Compute repo_id the same way the real code does
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)

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
		require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))
	}

	if createWorktree {
		require.NoError(t, os.MkdirAll(worktreePath, 0755))
	}

	return repoDir, dataDir, repoID, cr, fsys
}

func TestResume_SessionExists_NotDetached(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: false, DataDirOverride: dataDir}

	// Note: Attach will fail because it uses real exec.Command
	// but we can verify the event was written
	_ = ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)

	// Verify HasSession was called
	require.GreaterOrEqual(t, len(fakeTmux.HasSessionCalls), 1, "expected at least 1 HasSession call")

	// Verify NewSession was NOT called (session exists)
	assert.Len(t, fakeTmux.NewSessionCalls, 0)

	// Verify resume_attach event was written
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"resume_attach"`)
}

func TestResume_SessionExists_Detached(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: true, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.NoError(t, err)

	// Verify stdout contains success message
	assert.Contains(t, stdout.String(), "ok: session")

	// Verify Attach was NOT called (detached mode)
	assert.Len(t, fakeTmux.AttachCalls, 0, "expected 0 Attach calls in detached mode")

	// Verify resume_attach event was written with detached=true
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"detached":true`)
}

func TestResume_SessionMissing_CreateSession(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false (both calls)

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: true, DataDirOverride: dataDir} // detached to avoid attach call

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.NoError(t, err)

	// Verify NewSession was called
	require.Len(t, fakeTmux.NewSessionCalls, 1)

	call := fakeTmux.NewSessionCalls[0]
	expectedSession := tmux.SessionName(runID)
	assert.Equal(t, expectedSession, call.Name)
	require.Len(t, call.Argv, 1)
	assert.Equal(t, "claude", filepath.Base(call.Argv[0]))

	// Verify resume_create event was written
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"resume_create"`)
}

func TestResume_Restart_WithYes(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Restart: true, Yes: true, Detached: true, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.NoError(t, err)

	// Verify KillSession was called
	require.Len(t, fakeTmux.KillSessionCalls, 1)

	// Verify NewSession was called after kill
	require.Len(t, fakeTmux.NewSessionCalls, 1)

	// Verify resume_restart event was written
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"resume_restart"`)
	assert.Contains(t, string(eventsData), `"restart":true`)
}

func TestResume_Restart_NonInteractive_NoYes(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	// Override isInteractive to return false (non-interactive)
	originalIsInteractive := isInteractive
	isInteractive = func() bool { return false }
	defer func() { isInteractive = originalIsInteractive }()

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Restart: true, Yes: false, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.EConfirmationRequired, code)

	// Verify no tmux mutations occurred
	assert.Len(t, fakeTmux.KillSessionCalls, 0)
	assert.Len(t, fakeTmux.NewSessionCalls, 0)
}

func TestResume_Restart_Interactive_UserDeclinesNo(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	// Override isInteractive to return true
	originalIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	defer func() { isInteractive = originalIsInteractive }()

	// User enters "n" (decline)
	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Restart: true, Yes: false, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader("n\n"), &stdout, &stderr)
	require.NoError(t, err, "user canceled should not return error")

	// Verify "canceled" message
	assert.Contains(t, stderr.String(), "canceled")

	// Verify no tmux mutations occurred
	assert.Len(t, fakeTmux.KillSessionCalls, 0)
	assert.Len(t, fakeTmux.NewSessionCalls, 0)

	// Verify NO event was written (user canceled)
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	_, err = os.ReadFile(eventsPath)
	if err == nil {
		// File should not exist or be empty for canceled operation
		data, _ := os.ReadFile(eventsPath)
		assert.Empty(t, data, "expected no events for canceled restart")
	}
}

func TestResume_WorktreeMissing_Archived(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, false, true) // meta exists, worktree missing, archived

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeMissing, code)

	// Verify error message mentions archived
	assert.Contains(t, err.Error(), "archived")

	// Verify resume_failed event was written with reason=archived
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"resume_failed"`)
	assert.Contains(t, string(eventsData), `"reason":"archived"`)
}

func TestResume_WorktreeMissing_Corrupted(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, false, false) // meta exists, worktree missing, NOT archived

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeMissing, code)

	// Verify error message mentions corrupted
	assert.Contains(t, err.Error(), "corrupted")

	// Verify resume_failed event was written with reason=missing
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"reason":"missing"`)
}

func TestResume_RunNotFound(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys := setupResumeTestEnv(t, runID, false, false, false) // no meta

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.ERunNotFound, code)
}

func TestResume_MissingRunID(t *testing.T) {
	repoDir, dataDir, _, cr, fsys := setupResumeTestEnv(t, "dummy", false, false, false)

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: "", DataDirOverride: dataDir} // empty run_id

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.EUsage, code)
}

func TestResume_SessionRace_CreateBecomesAttach(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys := setupResumeTestEnv(t, runID, true, true, false)

	// Simulate race: session missing initially, exists under lock (another process created it)
	fakeTmux := testutil.NewFakeTmuxClient()
	hsCount := 0
	fakeTmux.HasSessionFunc = func(name string) (bool, error) {
		hsCount++
		return hsCount > 1, nil // false first, then true
	}

	var stdout, stderr bytes.Buffer
	opts := ResumeOpts{RunID: runID, Detached: true, DataDirOverride: dataDir}

	err := ResumeWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, strings.NewReader(""), &stdout, &stderr)
	require.NoError(t, err)

	// Verify NewSession was NOT called (race detected, session exists)
	assert.Len(t, fakeTmux.NewSessionCalls, 0, "expected 0 NewSession calls due to race")

	// Verify resume_attach event was written (race path)
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"resume_attach"`)
}
