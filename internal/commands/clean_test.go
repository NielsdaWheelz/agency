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

// setupCleanTestEnv creates a temporary test environment for clean tests.
func setupCleanTestEnv(t *testing.T, runID string, setupMeta bool, setupWorktree bool) (string, string, string, *testutil.FakeCommandRunner, fs.FS, *store.RunMeta) {
	t.Helper()

	// Create temp directories
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	// Initialize git repo
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0755))

	originURL := "git@github.com:test/repo.git"

	// Compute repo_id the same way the real code does
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	// Create fake command runner
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: originURL + "\n"}

	fsys := fs.NewRealFS()

	var meta *store.RunMeta
	if setupMeta {
		// Create store directories
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		require.NoError(t, os.MkdirAll(runDir, 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(runDir, "logs"), 0755))

		worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)

		// Create worktree directory if needed
		if setupWorktree {
			require.NoError(t, os.MkdirAll(worktreePath, 0755))
			// Create .agency directory
			require.NoError(t, os.MkdirAll(filepath.Join(worktreePath, ".agency"), 0755))
		}

		// Write meta.json
		meta = &store.RunMeta{
			SchemaVersion:   "1.0",
			RunID:           runID,
			RepoID:          repoID,
			Name:            "test-run",
			Runner:          "claude",
			RunnerCmd:       "claude",
			ParentBranch:    "main",
			Branch:          "agency/test-run-" + runID[:4],
			WorktreePath:    worktreePath,
			CreatedAt:       "2026-01-10T12:00:00Z",
			TmuxSessionName: tmux.SessionName(runID),
			PRNumber:        123,
			PRURL:           "https://github.com/test/repo/pull/123",
		}
		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		metaPath := filepath.Join(runDir, "meta.json")
		require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))
	}

	return repoDir, dataDir, repoID, cr, fsys, meta
}

func TestClean_DeleteBranch_LocalBranchDeleted(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses for branch deletion
	cr.Responses["git -C "+repoDir+" branch -D "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["git -C "+repoDir+" config --get remote.origin.url"] = testutil.FakeResponse{
		Stdout: "git@github.com:test/repo.git\n",
	}
	cr.Responses["git -C "+repoDir+" push origin --delete "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = testutil.FakeResponse{ExitCode: 0}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	// Create input for confirmation
	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	// Force interactive mode for testing by using CleanWithTmux directly
	// and temporarily modifying tty detection
	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.NoError(t, err, "Clean() error")

	// Verify local branch deletion was called
	foundLocalDelete := false
	for _, call := range cr.Calls {
		if strings.Contains(call, "branch -D "+branch) {
			foundLocalDelete = true
			break
		}
	}
	assert.True(t, foundLocalDelete, "expected local branch deletion call, got calls: %v", cr.Calls)

	// Verify output shows branch was deleted
	assert.Contains(t, stdout.String(), "local_branch: deleted")

	// Verify event was logged
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"branch_deleted"`)
}

func TestClean_DeleteBranch_RemoteBranchDeleted(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses for branch deletion
	cr.Responses["git -C "+repoDir+" branch -D "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["git -C "+repoDir+" config --get remote.origin.url"] = testutil.FakeResponse{
		Stdout: "git@github.com:test/repo.git\n",
	}
	cr.Responses["git -C "+repoDir+" push origin --delete "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = testutil.FakeResponse{ExitCode: 0}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.NoError(t, err, "Clean() error")

	// Verify remote branch deletion was called
	foundRemoteDelete := false
	for _, call := range cr.Calls {
		if strings.Contains(call, "push origin --delete "+branch) {
			foundRemoteDelete = true
			break
		}
	}
	assert.True(t, foundRemoteDelete, "expected remote branch deletion call, got calls: %v", cr.Calls)

	// Verify output shows remote branch was deleted
	assert.Contains(t, stdout.String(), "remote_branch: deleted")

	// Verify event was logged
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	// Should have two branch_deleted events (local and remote)
	assert.GreaterOrEqual(t, strings.Count(string(eventsData), `"event":"branch_deleted"`), 2, "expected two branch_deleted events")
}

func TestClean_DeleteBranch_PRClosed(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses for branch deletion and PR close
	cr.Responses["git -C "+repoDir+" branch -D "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["git -C "+repoDir+" config --get remote.origin.url"] = testutil.FakeResponse{
		Stdout: "git@github.com:test/repo.git\n",
	}
	cr.Responses["git -C "+repoDir+" push origin --delete "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = testutil.FakeResponse{ExitCode: 0}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.NoError(t, err, "Clean() error")

	// Verify PR close was called
	foundPRClose := false
	for _, call := range cr.Calls {
		if strings.Contains(call, "gh pr close 123") {
			foundPRClose = true
			break
		}
	}
	assert.True(t, foundPRClose, "expected gh pr close call, got calls: %v", cr.Calls)

	// Verify output shows PR was closed
	assert.Contains(t, stdout.String(), "pr: closed #123")

	// Verify event was logged
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "failed to read events.jsonl")
	assert.Contains(t, string(eventsData), `"event":"pr_closed"`)
}

func TestClean_WithoutDeleteBranch_NoBranchDeletion(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses but they should NOT be called
	cr.Responses["git -C "+repoDir+" branch -D "+branch] = testutil.FakeResponse{ExitCode: 0}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    false, // Explicitly false
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.NoError(t, err, "Clean() error")

	// Verify NO branch deletion was called
	for _, call := range cr.Calls {
		assert.NotContains(t, call, "branch -D", "unexpected branch deletion call")
		assert.NotContains(t, call, "push origin --delete", "unexpected remote branch deletion call")
		assert.NotContains(t, call, "gh pr close", "unexpected PR close call")
	}

	// Verify output does NOT show branch deletion
	assert.NotContains(t, stdout.String(), "local_branch:")
}

func TestClean_DeleteBranch_LocalBranchFailure_NonFatal(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Local branch deletion fails
	cr.Responses["git -C "+repoDir+" branch -D "+branch] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "error: branch 'agency/test-run-2026' not found.",
	}
	cr.Responses["git -C "+repoDir+" config --get remote.origin.url"] = testutil.FakeResponse{
		Stdout: "git@github.com:test/repo.git\n",
	}
	// Remote should still be attempted
	cr.Responses["git -C "+repoDir+" push origin --delete "+branch] = testutil.FakeResponse{ExitCode: 0}
	cr.Responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = testutil.FakeResponse{ExitCode: 0}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	// Should still succeed despite local branch deletion failure
	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.NoError(t, err, "Clean() should succeed (branch failure is non-fatal)")

	// Verify warning was logged
	assert.Contains(t, stderr.String(), "warning: failed to delete local branch")

	// Output should still show clean succeeded
	assert.Contains(t, stdout.String(), "cleaned:")
}

func TestClean_DeleteBranch_NonGitHubOrigin_SkipsRemote(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Local branch deletion succeeds
	cr.Responses["git -C "+repoDir+" branch -D "+branch] = testutil.FakeResponse{ExitCode: 0}
	// Non-GitHub origin
	cr.Responses["git -C "+repoDir+" config --get remote.origin.url"] = testutil.FakeResponse{
		Stdout: "git@gitlab.com:test/repo.git\n",
	}

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.NoError(t, err, "Clean() error")

	// Verify remote branch deletion was NOT called (non-GitHub origin)
	for _, call := range cr.Calls {
		assert.NotContains(t, call, "push origin --delete", "unexpected remote branch deletion for non-GitHub origin")
		assert.NotContains(t, call, "gh pr close", "unexpected PR close for non-GitHub origin")
	}

	// Output should show local branch deleted but not remote
	output := stdout.String()
	assert.Contains(t, output, "local_branch: deleted")
	assert.NotContains(t, output, "remote_branch:")
}

func TestClean_RunNotFound(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, _ := setupCleanTestEnv(t, runID, false, false) // no meta

	fakeTmux := testutil.NewFakeTmuxClient()

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.Error(t, err, "Clean() error = nil, want E_RUN_NOT_FOUND")

	assert.Equal(t, errors.ERunNotFound, errors.GetCode(err))
}

func TestClean_ConfirmationRequired(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, _ := setupCleanTestEnv(t, runID, true, true)

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	// Wrong confirmation
	stdin := strings.NewReader("no\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:           runID,
		DeleteBranch:    true,
		DataDirOverride: dataDir,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	require.Error(t, err, "Clean() error = nil, want E_ABORTED")

	assert.Equal(t, errors.EAborted, errors.GetCode(err))
}

func TestClean_NonInteractiveWithoutYes_ReturnsEConfirmationRequired(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, _ := setupCleanTestEnv(t, runID, true, true)

	fakeTmux := testutil.NewFakeTmuxClient()

	origIsInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	var stdout, stderr bytes.Buffer
	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, CleanOpts{
		RunID:           runID,
		DataDirOverride: dataDir,
	}, strings.NewReader(""), &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))
}

func TestClean_NonInteractiveWithYes_ProceedsWithoutPrompt(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, _, cr, fsys, _ := setupCleanTestEnv(t, runID, true, true)

	fakeTmux := testutil.NewFakeTmuxClient()

	origIsInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	var stdout, stderr bytes.Buffer
	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, CleanOpts{
		RunID:           runID,
		DataDirOverride: dataDir,
		Yes:             true,
	}, strings.NewReader(""), &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "cleaned:")
}
