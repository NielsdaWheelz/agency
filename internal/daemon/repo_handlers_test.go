package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// TestRepoRegister_Success verifies POST /repos/register with a real git repo.
func TestRepoRegister_Success(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)
	repoDir := setupTestGitRepoParallel(t)

	// Resolve symlinks (macOS /var → /private/var) for comparison
	resolvedRepoDir, err := filepath.EvalSymlinks(repoDir)
	require.NoError(t, err)

	ctx := context.Background()

	result, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	assert.NotEmpty(t, result.Data.RepoID)
	assert.NotEmpty(t, result.Data.RepoKey)
	assert.Contains(t, result.Data.Paths, resolvedRepoDir)
	assert.Equal(t, resolvedRepoDir, result.Data.PreferredRoot)
	assert.True(t, result.Data.PreferredRootAccessible)
}

// TestRepoRegister_Idempotent verifies registering the same repo twice returns consistent data.
func TestRepoRegister_Idempotent(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)
	repoDir := setupTestGitRepoParallel(t)

	ctx := context.Background()

	r1, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	r2, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	assert.Equal(t, r1.Data.RepoID, r2.Data.RepoID, "repo_id must be stable")
	assert.Equal(t, r1.Data.RepoKey, r2.Data.RepoKey, "repo_key must be stable")
	assert.Equal(t, r1.Data.PreferredRoot, r2.Data.PreferredRoot, "preferred_root must be stable")
}

// TestRepoRegister_NonGitDir verifies E_REPO_NOT_A_GIT_REPO for a non-git directory.
func TestRepoRegister_NonGitDir(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	ctx := context.Background()

	_, err := env.Client.RegisterRepo(ctx, t.TempDir())
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.ERepoNotAGitRepo, ae.Code)
}

// TestRepoRegister_InaccessiblePath verifies E_REPO_ROOT_INACCESSIBLE for a nonexistent path.
func TestRepoRegister_InaccessiblePath(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	ctx := context.Background()

	_, err := env.Client.RegisterRepo(ctx, "/nonexistent/path/that/does/not/exist")
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.ERepoRootInaccessible, ae.Code)
}

// TestRepoRegister_EmptyRepoRoot verifies E_INVALID_REQUEST for empty repo_root.
func TestRepoRegister_EmptyRepoRoot(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	ctx := context.Background()

	_, err := env.Client.RegisterRepo(ctx, "")
	require.Error(t, err)
	// Server returns E_INVALID_REQUEST for missing repo_root
	assert.Contains(t, err.Error(), "repo_root is required")
}

// TestListRepos_Empty verifies GET /repos returns empty list initially.
func TestListRepos_Empty(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	ctx := context.Background()

	result, err := env.Client.ListRepos(ctx)
	require.NoError(t, err)

	assert.Empty(t, result.Data.Repos)
}

// TestListRepos_AfterRegister verifies GET /repos includes registered repos.
func TestListRepos_AfterRegister(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)
	repoDir := setupTestGitRepoParallel(t)

	resolvedRepoDir, err := filepath.EvalSymlinks(repoDir)
	require.NoError(t, err)

	ctx := context.Background()

	reg, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	result, err := env.Client.ListRepos(ctx)
	require.NoError(t, err)

	require.Len(t, result.Data.Repos, 1)
	assert.Equal(t, reg.Data.RepoID, result.Data.Repos[0].RepoID)
	assert.Equal(t, resolvedRepoDir, result.Data.Repos[0].PreferredRoot)
}

// TestGetRepo_Success verifies GET /repos/{id} returns repo data.
func TestGetRepo_Success(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)
	repoDir := setupTestGitRepoParallel(t)

	resolvedRepoDir, err := filepath.EvalSymlinks(repoDir)
	require.NoError(t, err)

	ctx := context.Background()

	reg, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	result, err := env.Client.GetRepo(ctx, reg.Data.RepoID)
	require.NoError(t, err)

	assert.Equal(t, reg.Data.RepoID, result.Data.RepoID)
	assert.Equal(t, reg.Data.RepoKey, result.Data.RepoKey)
	assert.Contains(t, result.Data.Paths, resolvedRepoDir)
	assert.Equal(t, resolvedRepoDir, result.Data.PreferredRoot)
}

// TestGetRepo_NotFound verifies E_REPO_NOT_FOUND for unknown repo.
func TestGetRepo_NotFound(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	ctx := context.Background()

	_, err := env.Client.GetRepo(ctx, "nonexistent")
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.ERepoNotFound, ae.Code)
}

// TestRepoRegister_PreferredRootPersistence verifies PreferredRoot stays consistent.
func TestRepoRegister_PreferredRootPersistence(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)
	repoDir := setupTestGitRepoParallel(t)

	resolvedRepoDir, err := filepath.EvalSymlinks(repoDir)
	require.NoError(t, err)

	ctx := context.Background()

	// Register
	r1, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)
	assert.Equal(t, resolvedRepoDir, r1.Data.PreferredRoot)

	// Get should return same preferred root
	got, err := env.Client.GetRepo(ctx, r1.Data.RepoID)
	require.NoError(t, err)
	assert.Equal(t, resolvedRepoDir, got.Data.PreferredRoot)
	assert.True(t, got.Data.PreferredRootAccessible)
}

// TestRepoRegister_InaccessiblePreferredRoot verifies fallback when preferred_root disappears.
func TestRepoRegister_InaccessiblePreferredRoot(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)
	repoDir := setupTestGitRepoParallel(t)

	ctx := context.Background()

	// Register
	r1, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	// Remove the repo directory to make preferred_root inaccessible
	require.NoError(t, os.RemoveAll(repoDir))

	// Get should show preferred_root as inaccessible
	got, err := env.Client.GetRepo(ctx, r1.Data.RepoID)
	require.NoError(t, err)
	assert.False(t, got.Data.PreferredRootAccessible)
}
