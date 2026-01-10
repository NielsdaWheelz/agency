package repo

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Integration tests for CheckRepoSafe.
// These tests use real git operations in temp directories.

// setupTempRepo creates a temp repo with one commit on the default branch.
// Returns the repo root path and a cleanup function.
func setupTempRepo(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "agency-gates-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(dir)
	}

	// Initialize git repo
	if err := runGit(dir, "init"); err != nil {
		cleanup()
		t.Fatalf("git init failed: %v", err)
	}

	// Configure git user for commits
	if err := runGit(dir, "config", "user.email", "test@example.com"); err != nil {
		cleanup()
		t.Fatalf("git config user.email failed: %v", err)
	}
	if err := runGit(dir, "config", "user.name", "Test User"); err != nil {
		cleanup()
		t.Fatalf("git config user.name failed: %v", err)
	}

	// Create and commit a file
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo\n"), 0644); err != nil {
		cleanup()
		t.Fatalf("failed to write README.md: %v", err)
	}

	if err := runGit(dir, "add", "-A"); err != nil {
		cleanup()
		t.Fatalf("git add failed: %v", err)
	}
	if err := runGit(dir, "commit", "-m", "initial commit"); err != nil {
		cleanup()
		t.Fatalf("git commit failed: %v", err)
	}

	return dir, cleanup
}

// setupEmptyRepo creates a temp repo with git init but no commits.
func setupEmptyRepo(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "agency-gates-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(dir)
	}

	// Initialize git repo only
	if err := runGit(dir, "init"); err != nil {
		cleanup()
		t.Fatalf("git init failed: %v", err)
	}

	return dir, cleanup
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
	if err != nil {
		t.Fatalf("git branch --show-current failed: %v", err)
	}
	branch := string(output)
	if len(branch) > 0 && branch[len(branch)-1] == '\n' {
		branch = branch[:len(branch)-1]
	}
	return branch
}

func TestCheckRepoSafe_CleanRepoSuccess(t *testing.T) {
	repoRoot, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR to a temp directory
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	// Get current branch name
	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master" // fallback for older git versions
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	result, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: branch,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify returned context
	// Note: On macOS, git may resolve symlinks (e.g., /var -> /private/var),
	// so we compare resolved paths.
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	if result.RepoRoot != resolvedRepoRoot {
		t.Errorf("RepoRoot = %q, want %q", result.RepoRoot, resolvedRepoRoot)
	}
	if result.RepoID == "" {
		t.Error("RepoID should not be empty")
	}
	if len(result.RepoID) != 16 {
		t.Errorf("RepoID length = %d, want 16", len(result.RepoID))
	}
	// DataDir should match what was set in AGENCY_DATA_DIR
	if result.DataDir != dataDir {
		t.Errorf("DataDir = %q, want %q", result.DataDir, dataDir)
	}

	// Verify repo.json was created
	repoJSONPath := filepath.Join(dataDir, "repos", result.RepoID, "repo.json")
	if _, err := os.Stat(repoJSONPath); os.IsNotExist(err) {
		t.Fatal("repo.json was not created")
	}

	// Verify repo.json contents
	data, err := os.ReadFile(repoJSONPath)
	if err != nil {
		t.Fatalf("failed to read repo.json: %v", err)
	}

	var rec map[string]interface{}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("failed to unmarshal repo.json: %v", err)
	}

	// Check required fields
	if rec["schema_version"] != "1.0" {
		t.Errorf("schema_version = %v, want 1.0", rec["schema_version"])
	}
	if rec["repo_id"] != result.RepoID {
		t.Errorf("repo_id = %v, want %s", rec["repo_id"], result.RepoID)
	}
	if rec["updated_at"] == nil || rec["updated_at"] == "" {
		t.Error("updated_at should be set")
	}

	// Check file permissions (0644)
	info, err := os.Stat(repoJSONPath)
	if err != nil {
		t.Fatalf("failed to stat repo.json: %v", err)
	}
	perm := info.Mode().Perm()
	// On some systems, file permissions may differ slightly, so we check the essentials
	if perm&0600 != 0600 {
		t.Errorf("repo.json permissions = %o, want at least 0600", perm)
	}
}

func TestCheckRepoSafe_DirtyRepoFails(t *testing.T) {
	repoRoot, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	// Make the repo dirty
	dirty := filepath.Join(repoRoot, "dirty.txt")
	if err := os.WriteFile(dirty, []byte("dirty\n"), 0644); err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err = CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: branch,
	})

	if err == nil {
		t.Fatal("expected error for dirty repo")
	}

	code := errors.GetCode(err)
	if code != errors.EParentDirty {
		t.Errorf("error code = %q, want %q", code, errors.EParentDirty)
	}
}

func TestCheckRepoSafe_EmptyRepoFails(t *testing.T) {
	repoRoot, cleanup := setupEmptyRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err = CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: "main",
	})

	if err == nil {
		t.Fatal("expected error for empty repo")
	}

	code := errors.GetCode(err)
	if code != errors.EEmptyRepo {
		t.Errorf("error code = %q, want %q", code, errors.EEmptyRepo)
	}
}

func TestCheckRepoSafe_MissingParentBranchFails(t *testing.T) {
	repoRoot, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err = CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: "nonexistent-branch",
	})

	if err == nil {
		t.Fatal("expected error for missing parent branch")
	}

	code := errors.GetCode(err)
	if code != errors.EParentBranchNotFound {
		t.Errorf("error code = %q, want %q", code, errors.EParentBranchNotFound)
	}
}

func TestCheckRepoSafe_OriginURLPersisted(t *testing.T) {
	repoRoot, cleanup := setupTempRepo(t)
	defer cleanup()

	// Add a fake origin
	if err := runGit(repoRoot, "remote", "add", "origin", "git@github.com:test/repo.git"); err != nil {
		t.Fatalf("failed to add origin: %v", err)
	}

	// Set AGENCY_DATA_DIR
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	result, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: branch,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify origin URL in context
	expectedURL := "git@github.com:test/repo.git"
	if result.OriginURL != expectedURL {
		t.Errorf("OriginURL = %q, want %q", result.OriginURL, expectedURL)
	}

	// Verify repo.json contains origin URL
	repoJSONPath := filepath.Join(dataDir, "repos", result.RepoID, "repo.json")
	data, err := os.ReadFile(repoJSONPath)
	if err != nil {
		t.Fatalf("failed to read repo.json: %v", err)
	}

	var rec map[string]interface{}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("failed to unmarshal repo.json: %v", err)
	}

	if rec["origin_url"] != expectedURL {
		t.Errorf("repo.json origin_url = %v, want %s", rec["origin_url"], expectedURL)
	}
	if rec["origin_present"] != true {
		t.Errorf("repo.json origin_present = %v, want true", rec["origin_present"])
	}
}

func TestCheckRepoSafe_NoOriginURL(t *testing.T) {
	repoRoot, cleanup := setupTempRepo(t)
	defer cleanup()

	// No origin is set (default state after setupTempRepo)

	// Set AGENCY_DATA_DIR
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	result, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: branch,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify origin URL is empty
	if result.OriginURL != "" {
		t.Errorf("OriginURL = %q, want empty", result.OriginURL)
	}

	// Verify repo.json has empty origin URL and origin_present=false
	repoJSONPath := filepath.Join(dataDir, "repos", result.RepoID, "repo.json")
	data, err := os.ReadFile(repoJSONPath)
	if err != nil {
		t.Fatalf("failed to read repo.json: %v", err)
	}

	var rec map[string]interface{}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("failed to unmarshal repo.json: %v", err)
	}

	if rec["origin_url"] != "" {
		t.Errorf("repo.json origin_url = %v, want empty", rec["origin_url"])
	}
	if rec["origin_present"] != false {
		t.Errorf("repo.json origin_present = %v, want false", rec["origin_present"])
	}
}

func TestCheckRepoSafe_NotInsideRepo(t *testing.T) {
	// Create a temp directory that is NOT a git repo
	dir, err := os.MkdirTemp("", "agency-gates-norepo-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	_, err = CheckRepoSafe(ctx, cr, fsys, dir, CheckRepoSafeOpts{
		ParentBranch: "main",
	})

	if err == nil {
		t.Fatal("expected error for non-repo directory")
	}

	code := errors.GetCode(err)
	if code != errors.ENoRepo {
		t.Errorf("error code = %q, want %q", code, errors.ENoRepo)
	}
}

func TestCheckRepoSafe_RepoJSONUpdatedOnSecondCall(t *testing.T) {
	repoRoot, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	branch := getCurrentBranch(t, repoRoot)
	if branch == "" {
		branch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	// First call
	result1, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: branch,
	})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Read first timestamp
	repoJSONPath := filepath.Join(dataDir, "repos", result1.RepoID, "repo.json")
	data1, err := os.ReadFile(repoJSONPath)
	if err != nil {
		t.Fatalf("failed to read repo.json: %v", err)
	}
	var rec1 map[string]interface{}
	if err := json.Unmarshal(data1, &rec1); err != nil {
		t.Fatalf("failed to unmarshal repo.json: %v", err)
	}
	createdAt := rec1["created_at"].(string)

	// Second call
	result2, err := CheckRepoSafe(ctx, cr, fsys, repoRoot, CheckRepoSafeOpts{
		ParentBranch: branch,
	})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// Verify same repo ID
	if result2.RepoID != result1.RepoID {
		t.Errorf("repo ID changed between calls: %q -> %q", result1.RepoID, result2.RepoID)
	}

	// Read second record
	data2, err := os.ReadFile(repoJSONPath)
	if err != nil {
		t.Fatalf("failed to read repo.json: %v", err)
	}
	var rec2 map[string]interface{}
	if err := json.Unmarshal(data2, &rec2); err != nil {
		t.Fatalf("failed to unmarshal repo.json: %v", err)
	}

	// created_at should be preserved
	if rec2["created_at"].(string) != createdAt {
		t.Errorf("created_at changed: %q -> %q", createdAt, rec2["created_at"])
	}

	// updated_at should be updated (or same if called within same second)
	// Just verify it exists
	if rec2["updated_at"] == nil || rec2["updated_at"] == "" {
		t.Error("updated_at should be set on second call")
	}
}
