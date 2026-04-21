package daemon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
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
	assert.Equal(t, filepath.Base(resolvedRepoDir), result.Data.RepoName)
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

// TestRepoRegister_EmptyRepoRoot verifies a typed error for empty repo_root.
func TestRepoRegister_EmptyRepoRoot(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	ctx := context.Background()

	_, err := env.Client.RegisterRepo(ctx, "")
	require.Error(t, err)
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
	assert.Equal(t, reg.Data.RepoName, result.Data.Repos[0].RepoName)
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
	assert.Equal(t, reg.Data.RepoName, result.Data.RepoName)
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

func seedRepoRmState(t *testing.T, st *store.Store, repoID, repoKey string) {
	t.Helper()

	idx := store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoKey: {
				RepoID:     repoID,
				Paths:      []string{"/tmp/repo"},
				LastSeenAt: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	require.NoError(t, st.SaveRepoIndex(idx))
}

func TestRepoRm_ExactRepoIDResolvesBrokenRepoJSONAndRetainsRepoDir(t *testing.T) {
	t.Parallel()
	env := startTestDaemon(t)

	const repoID = "repo-rm-exact-001"
	const repoKey = "github:owner/agency"

	seedRepoRmState(t, env.Store, repoID, repoKey)
	require.NoError(t, os.MkdirAll(env.Store.RepoDir(repoID), 0o755))
	require.NoError(t, os.WriteFile(env.Store.RepoRecordPath(repoID), []byte("{broken json"), 0o644))

	result, err := env.Client.RepoRm(context.Background(), repoID)
	require.NoError(t, err)

	assert.Equal(t, repoID, result.Data.RepoID)
	assert.Equal(t, "agency", result.Data.RepoName)
	assert.Equal(t, repoKey, result.Data.RepoKey)
	assert.True(t, result.Data.RemovedFromIndex)
	assert.NotEmpty(t, result.RequestID)

	idx, err := env.Store.LoadRepoIndex()
	require.NoError(t, err)
	_, stillPresent := idx.Repos[repoKey]
	assert.False(t, stillPresent, "repo_index entry should be removed")

	_, statErr := os.Stat(env.Store.RepoDir(repoID))
	require.NoError(t, statErr)
}

func TestRepoRm_BlocksWhenRepoHasChildren(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seed     func(t *testing.T, st *store.Store, repoID string)
		wantCode errors.Code
	}{
		{
			name: "integration worktree",
			seed: func(t *testing.T, st *store.Store, repoID string) {
				wtID := "wt-rm-001"
				_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
				require.NoError(t, err)
				require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, wtID, &store.IntegrationWorktreeMeta{
					SchemaVersion: store.SchemaVersion,
					WorktreeID:    wtID,
					Name:          "keep-me",
					RepoID:        repoID,
					Branch:        "agency/keep-me",
					BaseBranch:    "main",
					TreePath:      filepath.Join(t.TempDir(), "tree"),
					CreatedAt:     time.Now().UTC().Format(time.RFC3339),
					LastUsedAt:    time.Now().UTC().Format(time.RFC3339),
					State:         store.WorktreeStatePresent,
				}))
			},
			wantCode: errors.ERepoHasWorktrees,
		},
		{
			name: "running invocation",
			seed: func(t *testing.T, st *store.Store, repoID string) {
				invID := "inv-rm-001"
				_, err := st.EnsureInvocationDir(repoID, invID)
				require.NoError(t, err)
				meta := store.NewInvocationMeta(
					invID,
					"",
					"wt-rm-001",
					filepath.Join(t.TempDir(), "sandbox", invID, "tree"),
					"agency/sandbox-"+invID,
					"basecommit",
					"claude-code",
					store.RunnerModeHeadless,
					time.Now(),
				)
				meta.Status = store.InvocationStatusRunning
				require.NoError(t, st.WriteInvocationMeta(repoID, invID, meta))
			},
			wantCode: errors.ERepoHasInvocations,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := startTestDaemon(t)

			const repoID = "repo-rm-blocked-001"
			const repoKey = "github:owner/agency"

			seedRepoRmState(t, env.Store, repoID, repoKey)
			require.NoError(t, os.MkdirAll(env.Store.RepoDir(repoID), 0o755))
			require.NoError(t, os.WriteFile(env.Store.RepoRecordPath(repoID), []byte(fmt.Sprintf(`{"schema_version":"1.0","repo_id":"%s","repo_key":"%s"}`, repoID, repoKey)), 0o644))
			tc.seed(t, env.Store, repoID)

			_, err := env.Client.RepoRm(context.Background(), repoID)
			require.Error(t, err)

			ae, ok := errors.AsAgencyError(err)
			require.True(t, ok, "expected AgencyError, got %T", err)
			assert.Equal(t, tc.wantCode, ae.Code)
		})
	}
}
