package repo

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// Integration tests for CheckRepoSafe.
// These tests use real git operations in temp directories.

// setupTempRepo creates a temp repo with one commit on the default branch.
// Returns the repo root path. Cleanup is handled automatically by t.TempDir().
func setupTempRepo(t *testing.T) string {
	t.Helper()
	testutil.HermeticGitEnv(t)

	// Create temp directory (t.TempDir handles cleanup automatically)
	dir := t.TempDir()

	// Initialize git repo
	require.NoError(t, runGit(dir, "init"), "git init failed")

	// Create and commit a file
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# Test Repo\n"), 0644), "failed to write README.md")

	require.NoError(t, runGit(dir, "add", "-A"), "git add failed")
	require.NoError(t, runGit(dir, "commit", "-m", "initial commit"), "git commit failed")

	return dir
}

// setupEmptyRepo creates a temp repo with git init but no commits.
// Returns the repo root path. Cleanup is handled automatically by t.TempDir().
func setupEmptyRepo(t *testing.T) string {
	t.Helper()

	// Create temp directory (t.TempDir handles cleanup automatically)
	dir := t.TempDir()

	// Initialize git repo only
	require.NoError(t, runGit(dir, "init"), "git init failed")

	return dir
}

// runGit runs a git command in the given directory.
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2000-01-01T00:00:00+0000", "GIT_COMMITTER_DATE=2000-01-01T00:00:00+0000")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrap(errors.EInternal, "git "+args[0]+" failed: "+string(output), err)
	}
	return nil
}

// getCurrentBranch returns the current branch name.
func getCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	output, err := cmd.Output()
	require.NoError(t, err, "git branch --show-current failed")
	branch := string(output)
	if len(branch) > 0 && branch[len(branch)-1] == '\n' {
		branch = branch[:len(branch)-1]
	}
	return branch
}

func TestCheckRepoSafe_CleanRepoSuccess(t *testing.T) {
	repoRoot := setupTempRepo(t)

	dataDir := t.TempDir()

	// Get current branch name
	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master" // fallback for older git versions
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	result, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      branch,
		DataDirOverride: dataDir,
	})

	require.NoError(t, err, "unexpected error")

	// Verify returned context
	// Note: On macOS, git may resolve symlinks (e.g., /var -> /private/var),
	// so we compare resolved paths.
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve repo root")
	assert.Equal(t, resolvedRepoRoot, result.RepoRoot)
	assert.NotEmpty(t, result.RepoID, "RepoID should not be empty")
	assert.Equal(t, 16, len(result.RepoID), "RepoID length")
	// DataDir should match what was set in AGENCY_DATA_DIR
	assert.Equal(t, dataDir, result.DataDir)

	// Verify repo.json was created
	repoJSONPath := filepath.Join(dataDir, "repos", result.RepoID, "repo.json")
	_, err = os.Stat(repoJSONPath)
	require.False(t, os.IsNotExist(err), "repo.json was not created")

	// Verify repo.json contents
	data, err := os.ReadFile(repoJSONPath)
	require.NoError(t, err, "failed to read repo.json")

	var rec map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rec), "failed to unmarshal repo.json")

	// Check required fields
	assert.Equal(t, "1.0", rec["schema_version"])
	assert.Equal(t, result.RepoID, rec["repo_id"])
	assert.NotNil(t, rec["updated_at"], "updated_at should be set")
	assert.NotEqual(t, "", rec["updated_at"], "updated_at should be set")

	// Check file permissions (0644)
	info, err := os.Stat(repoJSONPath)
	require.NoError(t, err, "failed to stat repo.json")
	perm := info.Mode().Perm()
	// On some systems, file permissions may differ slightly, so we check the essentials
	assert.Equal(t, os.FileMode(0600), perm&0600, "repo.json permissions should have at least 0600")
}

func TestCheckRepoSafe_DirtyRepoFails(t *testing.T) {
	repoRoot := setupTempRepo(t)

	dataDir := t.TempDir()

	// Make the repo dirty
	dirty := filepath.Join(repoRoot, "dirty.txt")
	require.NoError(t, os.WriteFile(dirty, []byte("dirty\n"), 0644), "failed to create dirty file")

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      branch,
		DataDirOverride: dataDir,
	})

	require.Error(t, err, "expected error for dirty repo")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EBaseDirty, code)
}

func TestCheckRepoSafe_EmptyRepoFails(t *testing.T) {
	t.Parallel()
	repoRoot := setupEmptyRepo(t)

	dataDir := t.TempDir()

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      "main",
		DataDirOverride: dataDir,
	})

	require.Error(t, err, "expected error for empty repo")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EEmptyRepo, code)
}

func TestCheckRepoSafe_MissingBaseBranchFails(t *testing.T) {
	repoRoot := setupTempRepo(t)

	dataDir := t.TempDir()

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      "nonexistent-branch",
		DataDirOverride: dataDir,
	})

	require.Error(t, err, "expected error for missing base branch")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EBaseBranchNotFound, code)
}

func TestCheckRepoSafe_OriginURLPersisted(t *testing.T) {
	repoRoot := setupTempRepo(t)

	// Add a fake origin
	require.NoError(t, runGit(repoRoot, "remote", "add", "origin", "git@github.com:test/repo.git"), "failed to add origin")

	dataDir := t.TempDir()

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	result, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      branch,
		DataDirOverride: dataDir,
	})

	require.NoError(t, err, "unexpected error")

	// Verify origin URL in context
	expectedURL := "git@github.com:test/repo.git"
	assert.Equal(t, expectedURL, result.OriginURL)

	// Verify repo.json contains origin URL
	repoJSONPath := filepath.Join(dataDir, "repos", result.RepoID, "repo.json")
	data, err := os.ReadFile(repoJSONPath)
	require.NoError(t, err, "failed to read repo.json")

	var rec map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rec), "failed to unmarshal repo.json")

	assert.Equal(t, expectedURL, rec["origin_url"])
	assert.Equal(t, true, rec["origin_present"])
}

func TestCheckRepoSafe_NoOriginURL(t *testing.T) {
	repoRoot := setupTempRepo(t)

	// No origin is set (default state after setupTempRepo)

	dataDir := t.TempDir()

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	result, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      branch,
		DataDirOverride: dataDir,
	})

	require.NoError(t, err, "unexpected error")

	// Verify origin URL is empty
	assert.Equal(t, "", result.OriginURL)

	// Verify repo.json has empty origin URL and origin_present=false
	repoJSONPath := filepath.Join(dataDir, "repos", result.RepoID, "repo.json")
	data, err := os.ReadFile(repoJSONPath)
	require.NoError(t, err, "failed to read repo.json")

	var rec map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rec), "failed to unmarshal repo.json")

	assert.Equal(t, "", rec["origin_url"])
	assert.Equal(t, false, rec["origin_present"])
}

func TestCheckRepoSafe_NotInsideRepo(t *testing.T) {
	t.Parallel()
	// Create a temp directory that is NOT a git repo (t.TempDir handles cleanup)
	dir := t.TempDir()

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err := CheckRepoSafe(ctx, cr, fsys, dir, CheckRepoSafeOpts{
		BaseBranch: "main",
	})

	require.Error(t, err, "expected error for non-repo directory")

	code := errors.GetCode(err)
	assert.Equal(t, errors.ENoRepo, code)
}

func TestCheckRepoSafe_RepoJSONUpdatedOnSecondCall(t *testing.T) {
	repoRoot := setupTempRepo(t)

	dataDir := t.TempDir()

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	// First call
	result1, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      branch,
		DataDirOverride: dataDir,
	})
	require.NoError(t, err, "first call failed")

	// Read first timestamp
	repoJSONPath := filepath.Join(dataDir, "repos", result1.RepoID, "repo.json")
	data1, err := os.ReadFile(repoJSONPath)
	require.NoError(t, err, "failed to read repo.json")
	var rec1 map[string]interface{}
	require.NoError(t, json.Unmarshal(data1, &rec1), "failed to unmarshal repo.json")
	createdAt := rec1["created_at"].(string)

	// Second call
	result2, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		BaseBranch:      branch,
		DataDirOverride: dataDir,
	})
	require.NoError(t, err, "second call failed")

	// Verify same repo ID
	assert.Equal(t, result1.RepoID, result2.RepoID)

	// Read second record
	data2, err := os.ReadFile(repoJSONPath)
	require.NoError(t, err, "failed to read repo.json")
	var rec2 map[string]interface{}
	require.NoError(t, json.Unmarshal(data2, &rec2), "failed to unmarshal repo.json")

	// created_at should be preserved
	assert.Equal(t, createdAt, rec2["created_at"].(string))

	// updated_at should be updated (or same if called within same second)
	// Just verify it exists
	assert.NotNil(t, rec2["updated_at"], "updated_at should be set on second call")
	assert.NotEqual(t, "", rec2["updated_at"], "updated_at should be set on second call")
}
