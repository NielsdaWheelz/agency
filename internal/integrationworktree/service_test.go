package integrationworktree

import (
	"context"
	"os"
	"path/filepath"
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

// TestCreate tests creating and resolving a worktree.
// This is an integration test that requires git to be installed.
func TestCreate(t *testing.T) {
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
		assert.DirExists(t, result.TreePath, "tree directory not created")

		// Verify INTEGRATION_MARKER exists
		markerPath := filepath.Join(result.TreePath, ".agency", IntegrationMarkerFileName)
		assert.FileExists(t, markerPath, "INTEGRATION_MARKER not created")

		// Verify meta.json exists
		metaPath := st.IntegrationWorktreeMetaPath(repoID, result.WorktreeID)
		assert.FileExists(t, metaPath, "meta.json not created")

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
	assert.Contains(t, err.Error(), "ambiguous")
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
