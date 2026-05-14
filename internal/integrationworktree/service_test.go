package integrationworktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateAndRemove tests the full lifecycle of creating and removing a worktree.
// This is an integration test that requires git to be installed.
func TestCreateAndRemove(t *testing.T) {
	// Hermetic git environment: blocks system/global config, provides test identity.
	testutil.HermeticGitEnv(t)

	// Skip if git not available
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"--version"}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		t.Skip("git not available")
	}

	// Create temp directories
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	dataDir := filepath.Join(tmpDir, "data")

	// Initialize git repo with explicit initial branch name
	require.NoError(t, os.Mkdir(repoDir, 0o755), "failed to create repo dir")

	result, err = cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		require.Fail(t, "git init failed", "err=%v, exit %d", err, result.ExitCode)
	}

	// Create initial commit
	readme := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# Test\n"), 0o644), "failed to write readme")

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		require.Fail(t, "git add failed", "err=%v, exit %d", err, result.ExitCode)
	}
	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "Initial commit"}, exec.RunOpts{Dir: repoDir})
	if err != nil || result.ExitCode != 0 {
		require.Fail(t, "git commit failed", "err=%v, exit %d, stderr: %s", err, result.ExitCode, result.Stderr)
	}

	// Create service
	fsys := fs.NewRealFS()
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })
	svc := NewService(st, cr, fsys, func() time.Time { return now })

	repoID := "abc123def456"
	checkoutRoot := filepath.Join(tmpDir, "checkouts", repoID)

	var createdWorktreeID string

	// Test Create
	t.Run("Create", func(t *testing.T) {
		result, err := svc.Create(ctx, CreateOpts{
			Name:             "test-feature",
			RepoRoot:         repoDir,
			RepoID:           repoID,
			BaseBranch:       "main",
			CheckoutRoot:     checkoutRoot,
			ExecutionProfile: "work",
		})

		require.NoError(t, err)

		// Verify result
		assert.NotEmpty(t, result.WorktreeID, "WorktreeID is empty")
		assert.NotEmpty(t, result.Branch, "Branch is empty")
		assert.NotEmpty(t, result.TreePath, "TreePath is empty")

		// Verify tree directory exists
		_, err = os.Stat(result.TreePath)
		assert.False(t, os.IsNotExist(err), "tree directory not created")

		// Verify INTEGRATION_MARKER exists
		markerPath := filepath.Join(result.TreePath, ".agency", IntegrationMarkerFileName)
		_, err = os.Stat(markerPath)
		assert.False(t, os.IsNotExist(err), "INTEGRATION_MARKER not created")

		// Verify meta.json exists
		metaPath := st.IntegrationWorktreeMetaPath(repoID, result.WorktreeID)
		_, err = os.Stat(metaPath)
		assert.False(t, os.IsNotExist(err), "meta.json not created")

		// Verify HasIntegrationMarker
		assert.True(t, HasIntegrationMarker(result.TreePath), "HasIntegrationMarker returned false")

		// Store the worktree ID for later tests
		createdWorktreeID = result.WorktreeID
	})

	// Test Resolve
	t.Run("Resolve", func(t *testing.T) {
		if createdWorktreeID == "" {
			t.Skip("Create test failed, skipping")
		}
		record, err := svc.Resolve(repoID, "test-feature", false)
		require.NoError(t, err)
		assert.Equal(t, "test-feature", record.Name)
	})

	// Test name uniqueness
	t.Run("NameUniqueness", func(t *testing.T) {
		if createdWorktreeID == "" {
			t.Skip("Create test failed, skipping")
		}
		_, err := svc.Create(ctx, CreateOpts{
			Name:             "test-feature",
			RepoRoot:         repoDir,
			RepoID:           repoID,
			BaseBranch:       "main",
			CheckoutRoot:     checkoutRoot,
			ExecutionProfile: "work",
		})
		require.Error(t, err, "expected error for duplicate name")
	})

	// Test Remove
	t.Run("Remove", func(t *testing.T) {
		if createdWorktreeID == "" {
			t.Skip("Create test failed, skipping")
		}
		record, err := svc.Resolve(repoID, "test-feature", false)
		require.NoError(t, err)
		treePath := record.Meta.TreePath

		err = svc.Remove(ctx, repoID, record.WorktreeID, RemoveOpts{
			RepoRoot: repoDir,
			Force:    true,
		})
		require.NoError(t, err)

		// Verify tree directory is gone
		_, err = os.Stat(treePath)
		assert.True(t, os.IsNotExist(err), "tree directory still exists after remove")

		// Verify meta.json still exists with archived state
		meta, err := st.ReadIntegrationWorktreeMeta(repoID, record.WorktreeID)
		require.NoError(t, err)
		assert.Equal(t, store.WorktreeStateArchived, meta.State)
	})
}

func TestCreateInvalidName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, filepath.Join(tmpDir, "data"), time.Now)
	svc := NewService(st, exec.NewRealRunner(), fsys, time.Now)

	tests := []struct {
		name     string
		worktree string
	}{
		{name: "too short", worktree: "a"},
		{name: "uppercase", worktree: "MyFeature"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := svc.Create(context.Background(), CreateOpts{
				Name:             tc.worktree,
				RepoRoot:         filepath.Join(tmpDir, "repo"),
				RepoID:           "repo-1",
				BaseBranch:       "main",
				CheckoutRoot:     filepath.Join(tmpDir, "checkouts", "repo-1"),
				ExecutionProfile: "work",
			})

			require.Error(t, err)
			assert.Nil(t, result)
			assert.Equal(t, errors.EInvalidName, errors.GetCode(err))
		})
	}
}

func TestHasIntegrationMarker(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Test without marker
	assert.False(t, HasIntegrationMarker(tmpDir), "should return false for dir without marker")

	// Create marker
	agencyDir := filepath.Join(tmpDir, ".agency")
	require.NoError(t, os.Mkdir(agencyDir, 0o755), "failed to create .agency dir")
	markerPath := filepath.Join(agencyDir, IntegrationMarkerFileName)
	require.NoError(t, os.WriteFile(markerPath, []byte("marker"), 0o644), "failed to write marker")

	// Test with marker
	assert.True(t, HasIntegrationMarker(tmpDir), "should return true for dir with marker")
}

// TestRemove_GitFails verifies that Remove returns E_WORKTREE_REMOVE_FAILED
// when the git worktree remove command fails with a non-zero exit code.
func TestRemove_GitFails(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	repoID := "abc123"
	wtID := "20260201120000-f1a2"

	// Create worktree record directory and write valid meta.json
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	checkoutRoot := filepath.Join(dataDir, "checkouts", repoID)
	treePath := filepath.Join(checkoutRoot, "worktrees", "rm-fail-test-f1a2")
	meta := store.NewIntegrationWorktreeMeta(
		wtID, "rm-fail-test", repoID,
		"agency/rm-fail-test-f1a2", "main",
		treePath, checkoutRoot, "work", now,
	)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))

	// Set up fake runner that returns failure for git worktree remove
	cr := testutil.NewFakeCommandRunner()
	repoRoot := "/fake/repo"
	gitKey := fmt.Sprintf("git -C %s worktree remove %s", repoRoot, treePath)
	cr.Responses[gitKey] = testutil.FakeResponse{
		Stderr:   "fatal: some git error",
		ExitCode: 128,
	}

	svc := NewService(st, cr, fsys, func() time.Time { return now })
	ctx := context.Background()

	err = svc.Remove(ctx, repoID, wtID, RemoveOpts{
		RepoRoot: repoRoot,
		Force:    false,
	})
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeRemoveFailed, errors.GetCode(err),
		"expected E_WORKTREE_REMOVE_FAILED when git worktree remove exits non-zero")
}

func TestRemove_DirtyWorktree(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	repoID := "abc123"
	wtID := "20260201120000-f1a2"

	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	checkoutRoot := filepath.Join(dataDir, "checkouts", repoID)
	treePath := filepath.Join(checkoutRoot, "worktrees", "dirty-test-f1a2")
	meta := store.NewIntegrationWorktreeMeta(
		wtID, "dirty-test", repoID,
		"agency/dirty-test-f1a2", "main",
		treePath, checkoutRoot, "work", now,
	)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git status --porcelain"] = testutil.FakeResponse{
		Stdout:   " M README.md\n",
		ExitCode: 0,
	}

	svc := NewService(st, cr, fsys, func() time.Time { return now })

	err = svc.Remove(context.Background(), repoID, wtID, RemoveOpts{
		RepoRoot: "/fake/repo",
		Force:    false,
	})
	require.Error(t, err)
	assert.Equal(t, errors.EDirtyWorktree, errors.GetCode(err))
	assert.NotContains(t, cr.Calls, "git -C /fake/repo worktree remove "+treePath)
}

// TestRemove_GitRunError verifies that Remove returns E_WORKTREE_REMOVE_FAILED
// when the git worktree remove command itself fails to execute (e.g., binary not found).
func TestRemove_GitRunError(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	repoID := "abc123"
	wtID := "20260201120000-f1a2"

	// Create worktree record directory and write valid meta.json
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	checkoutRoot := filepath.Join(dataDir, "checkouts", repoID)
	treePath := filepath.Join(checkoutRoot, "worktrees", "rm-runerr-test-f1a2")
	meta := store.NewIntegrationWorktreeMeta(
		wtID, "rm-runerr-test", repoID,
		"agency/rm-runerr-test-f1a2", "main",
		treePath, checkoutRoot, "work", now,
	)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))

	// Set up fake runner that returns an error (not a non-zero exit, but a real error)
	cr := testutil.NewFakeCommandRunner()
	repoRoot := "/fake/repo"
	gitKey := fmt.Sprintf("git -C %s worktree remove %s", repoRoot, treePath)
	cr.Responses[gitKey] = testutil.FakeResponse{
		Err: fmt.Errorf("exec: git not found"),
	}

	svc := NewService(st, cr, fsys, func() time.Time { return now })
	ctx := context.Background()

	err = svc.Remove(ctx, repoID, wtID, RemoveOpts{
		RepoRoot: repoRoot,
		Force:    false,
	})
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeRemoveFailed, errors.GetCode(err),
		"expected E_WORKTREE_REMOVE_FAILED when git command runner returns error")
}

// TestRemove_AlreadyArchived verifies that Remove returns E_WORKTREE_NOT_FOUND
// when the worktree is already archived.
func TestRemove_AlreadyArchived(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	repoID := "abc123"
	wtID := "20260201120000-arch"

	// Create worktree record directory and write meta.json with archived state
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	require.NoError(t, err)

	checkoutRoot := filepath.Join(dataDir, "checkouts", repoID)
	treePath := filepath.Join(checkoutRoot, "worktrees", "archived-test-arch")
	meta := store.NewIntegrationWorktreeMeta(
		wtID, "archived-test", repoID,
		"agency/archived-test-arch", "main",
		treePath, checkoutRoot, "work", now,
	)
	meta.State = store.WorktreeStateArchived
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))

	cr := testutil.NewFakeCommandRunner()
	svc := NewService(st, cr, fsys, func() time.Time { return now })
	ctx := context.Background()

	err = svc.Remove(ctx, repoID, wtID, RemoveOpts{
		RepoRoot: "/fake/repo",
	})
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err),
		"expected E_WORKTREE_NOT_FOUND for already-archived worktree")
}

// TestResolve_WorktreeNotFound verifies that Resolve returns E_WORKTREE_NOT_FOUND
// when no worktree matches the input.
func TestResolve_WorktreeNotFound(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })
	cr := testutil.NewFakeCommandRunner()
	svc := NewService(st, cr, fsys, func() time.Time { return now })

	_, err := svc.Resolve("nonexistent-repo", "no-such-worktree", false)
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err),
		"expected E_WORKTREE_NOT_FOUND for non-existent worktree")
}

// TestResolve_AmbiguousWorktreeID verifies that Resolve returns E_WORKTREE_ID_AMBIGUOUS
// when a prefix matches multiple worktrees.
func TestResolve_AmbiguousWorktreeID(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	repoID := "abc123"

	// Create two worktrees with the same prefix pattern
	for _, wtID := range []string{"20260201120000-a1b2", "20260201120000-a1c3"} {
		_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
		require.NoError(t, err)

		checkoutRoot := filepath.Join(dataDir, "checkouts", repoID)
		treePath := filepath.Join(checkoutRoot, "worktrees", "feature-"+wtID[len(wtID)-4:])
		meta := store.NewIntegrationWorktreeMeta(
			wtID, "feature-"+wtID[len(wtID)-4:], repoID,
			"agency/feature-"+wtID, "main",
			treePath, checkoutRoot, "work", now,
		)
		require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, meta))
	}

	cr := testutil.NewFakeCommandRunner()
	svc := NewService(st, cr, fsys, func() time.Time { return now })

	// Use a prefix that matches both worktrees
	_, err := svc.Resolve(repoID, "20260201120000-a1", false)
	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, errors.GetCode(err),
		"expected E_WORKTREE_ID_AMBIGUOUS when prefix matches multiple worktrees")
	assert.True(t, strings.Contains(err.Error(), "ambiguous"),
		"error message should contain 'ambiguous'")
}

// TestLoad_MissingMarkerFile verifies that HasIntegrationMarker returns false
// when the .agency/INTEGRATION_MARKER file is missing from a directory.
// This is the condition that triggers E_NOT_AN_INTEGRATION_WORKTREE in the daemon.
func TestLoad_MissingMarkerFile(t *testing.T) {
	t.Parallel()

	// Test with empty directory (no .agency/ at all)
	emptyDir := t.TempDir()
	assert.False(t, HasIntegrationMarker(emptyDir),
		"should return false for directory without .agency/")

	// Test with .agency/ directory but no INTEGRATION_MARKER
	dirWithAgency := t.TempDir()
	agencyDir := filepath.Join(dirWithAgency, ".agency")
	require.NoError(t, os.Mkdir(agencyDir, 0o755))
	assert.False(t, HasIntegrationMarker(dirWithAgency),
		"should return false for directory with .agency/ but no INTEGRATION_MARKER")

	// Test with nonexistent directory
	assert.False(t, HasIntegrationMarker("/nonexistent/path"),
		"should return false for non-existent path")
}
