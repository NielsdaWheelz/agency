package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// setupTempRepo creates a temp repo with one commit on the default branch.
// Returns the repo root path and data dir. Cleanup is handled automatically by t.TempDir().
func setupTempRepo(t *testing.T) (repoRoot, dataDir string) {
	t.Helper()

	// Create temp directories (t.TempDir handles cleanup automatically)
	repoRoot = t.TempDir()
	dataDir = t.TempDir()

	// Initialize git repo
	if err := runGit(repoRoot, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure git user for commits
	if err := runGit(repoRoot, "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config user.email failed: %v", err)
	}
	if err := runGit(repoRoot, "config", "user.name", "Test User"); err != nil {
		t.Fatalf("git config user.name failed: %v", err)
	}

	// Create and commit a file
	readme := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	if err := runGit(repoRoot, "add", "-A"); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if err := runGit(repoRoot, "commit", "-m", "initial commit"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	return repoRoot, dataDir
}

// runGit runs a git command in the given directory.
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00+0000",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00+0000",
	)
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
	return strings.TrimSpace(string(output))
}

func TestCreate_Success(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	parentBranch := getCurrentBranch(t, repoRoot)
	if parentBranch == "" {
		parentBranch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-a1b2"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Title:        "Test Run",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify branch name format
	expectedBranch := "agency/test-run-a1b2"
	if result.Branch != expectedBranch {
		t.Errorf("Branch = %q, want %q", result.Branch, expectedBranch)
	}

	// Verify worktree path
	expectedPath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	if result.WorktreePath != expectedPath {
		t.Errorf("WorktreePath = %q, want %q", result.WorktreePath, expectedPath)
	}

	// Verify resolved title
	if result.ResolvedTitle != "Test Run" {
		t.Errorf("ResolvedTitle = %q, want %q", result.ResolvedTitle, "Test Run")
	}

	// Verify worktree directory exists
	if _, err := os.Stat(result.WorktreePath); os.IsNotExist(err) {
		t.Error("worktree directory does not exist")
	}

	// Verify .agency/ directories exist
	agencyDir := filepath.Join(result.WorktreePath, ".agency")
	if _, err := os.Stat(agencyDir); os.IsNotExist(err) {
		t.Error(".agency/ directory does not exist")
	}

	outDir := filepath.Join(agencyDir, "out")
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		t.Error(".agency/out/ directory does not exist")
	}

	tmpDir := filepath.Join(agencyDir, "tmp")
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Error(".agency/tmp/ directory does not exist")
	}

	// Verify report.md exists and has title
	reportPath := filepath.Join(agencyDir, "report.md")
	reportContent, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read report.md: %v", err)
	}

	if !strings.HasPrefix(string(reportContent), "# Test Run\n") {
		t.Errorf("report.md should start with '# Test Run\\n', got: %q", string(reportContent)[:min(50, len(reportContent))])
	}

	// Verify git worktree list shows the new worktree
	cmd := exec.Command("git", "-C", resolvedRepoRoot, "worktree", "list")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git worktree list failed: %v", err)
	}
	if !strings.Contains(string(output), runID) {
		t.Errorf("git worktree list should contain run_id %s, got: %s", runID, output)
	}
}

func TestCreate_EmptyTitle(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	parentBranch := getCurrentBranch(t, repoRoot)
	if parentBranch == "" {
		parentBranch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-beef"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Title:        "", // empty title
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Title should default to "untitled-<shortid>"
	expectedTitle := "untitled-beef"
	if result.ResolvedTitle != expectedTitle {
		t.Errorf("ResolvedTitle = %q, want %q", result.ResolvedTitle, expectedTitle)
	}

	// Branch should use the default title
	expectedBranch := "agency/untitled-beef-beef"
	if result.Branch != expectedBranch {
		t.Errorf("Branch = %q, want %q", result.Branch, expectedBranch)
	}

	// Verify report.md has the default title
	reportPath := filepath.Join(result.WorktreePath, ".agency", "report.md")
	reportContent, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read report.md: %v", err)
	}

	if !strings.HasPrefix(string(reportContent), "# "+expectedTitle+"\n") {
		t.Errorf("report.md should start with '# %s\\n', got: %q", expectedTitle, string(reportContent)[:min(50, len(reportContent))])
	}
}

func TestCreate_Collision_ReturnsError(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	parentBranch := getCurrentBranch(t, repoRoot)
	if parentBranch == "" {
		parentBranch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-c0de"
	repoID := "abcd1234ef567890"

	opts := CreateOpts{
		RunID:        runID,
		Title:        "Collision Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	}

	// First creation should succeed
	_, err := Create(ctx, cr, fsys, opts)
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	// Second creation with same run_id should fail
	_, err = Create(ctx, cr, fsys, opts)
	if err == nil {
		t.Fatal("expected error for collision, got nil")
	}

	// Verify error code
	code := errors.GetCode(err)
	if code != errors.EWorktreeCreateFailed {
		t.Errorf("error code = %q, want %q", code, errors.EWorktreeCreateFailed)
	}

	// Verify error has details
	ae, ok := errors.AsAgencyError(err)
	if !ok {
		t.Fatal("expected AgencyError")
	}
	if ae.Details == nil {
		t.Error("expected Details to be set")
	}
	if ae.Details["command"] == "" {
		t.Error("expected command in details")
	}
	// Should have stderr (git error message)
	if ae.Details["stderr"] == "" {
		t.Error("expected stderr in details")
	}
}

func TestCreate_MissingParentBranch_ReturnsError(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-dead"
	repoID := "abcd1234ef567890"

	_, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Title:        "Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: "nonexistent-branch",
		DataDir:      dataDir,
	})

	if err == nil {
		t.Fatal("expected error for nonexistent parent branch, got nil")
	}

	// Verify error code
	code := errors.GetCode(err)
	if code != errors.EWorktreeCreateFailed {
		t.Errorf("error code = %q, want %q", code, errors.EWorktreeCreateFailed)
	}
}

func TestScaffoldWorkspaceOnly_ReportNotOverwritten(t *testing.T) {
	// Create temp directory (t.TempDir handles cleanup automatically)
	dir := t.TempDir()

	fsys := fs.NewRealFS()

	// First call creates everything
	if err := ScaffoldWorkspaceOnly(fsys, dir, "First Title"); err != nil {
		t.Fatalf("first scaffold failed: %v", err)
	}

	// Verify report.md has first title
	reportPath := filepath.Join(dir, ".agency", "report.md")
	content1, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read report.md: %v", err)
	}
	if !strings.HasPrefix(string(content1), "# First Title\n") {
		t.Errorf("report.md should have 'First Title', got: %q", string(content1)[:min(50, len(content1))])
	}

	// Write a sentinel to report.md
	sentinel := "# First Title\n\n## SENTINEL\nThis should not be overwritten\n"
	if err := os.WriteFile(reportPath, []byte(sentinel), 0644); err != nil {
		t.Fatalf("failed to write sentinel: %v", err)
	}

	// Second call should NOT overwrite report.md
	if err := ScaffoldWorkspaceOnly(fsys, dir, "Second Title"); err != nil {
		t.Fatalf("second scaffold failed: %v", err)
	}

	// Verify sentinel is preserved
	content2, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read report.md: %v", err)
	}
	if string(content2) != sentinel {
		t.Errorf("report.md was overwritten, got: %q, want: %q", string(content2), sentinel)
	}
}

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name    string
		dataDir string
		repoID  string
		runID   string
		want    string
	}{
		{
			name:    "basic",
			dataDir: "/home/user/.local/share/agency",
			repoID:  "abcd1234ef567890",
			runID:   "20260110120000-a1b2",
			want:    "/home/user/.local/share/agency/repos/abcd1234ef567890/worktrees/20260110120000-a1b2",
		},
		{
			name:    "macos",
			dataDir: "/Users/dev/Library/Application Support/agency",
			repoID:  "1234567890abcdef",
			runID:   "20260109013207-beef",
			want:    "/Users/dev/Library/Application Support/agency/repos/1234567890abcdef/worktrees/20260109013207-beef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorktreePath(tt.dataDir, tt.repoID, tt.runID)
			if got != tt.want {
				t.Errorf("WorktreePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReportTemplate(t *testing.T) {
	template := ReportTemplate("My Test Run")

	// Check title line
	if !strings.HasPrefix(template, "# My Test Run\n") {
		t.Errorf("template should start with '# My Test Run\\n'")
	}

	// Check required sections exist
	requiredSections := []string{
		"## summary of changes",
		"## problems encountered",
		"## solutions implemented",
		"## decisions made",
		"## deviations from spec",
		"## how to test",
	}
	for _, section := range requiredSections {
		if !strings.Contains(template, section) {
			t.Errorf("template should contain %q", section)
		}
	}
}

func TestCreate_IgnoreWarning(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	parentBranch := getCurrentBranch(t, repoRoot)
	if parentBranch == "" {
		parentBranch = "master"
	}

	// Note: We don't add .agency/ to .gitignore, so we should get a warning

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-warn"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Title:        "Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should have a warning about .agency/ not being ignored
	if len(result.Warnings) == 0 {
		t.Error("expected warning about .agency/ not being ignored")
	}

	if len(result.Warnings) > 0 {
		if result.Warnings[0].Code != "W_AGENCY_NOT_IGNORED" {
			t.Errorf("warning code = %q, want %q", result.Warnings[0].Code, "W_AGENCY_NOT_IGNORED")
		}
	}
}

func TestCreate_IgnoreWarning_NotPresentWhenIgnored(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	parentBranch := getCurrentBranch(t, repoRoot)
	if parentBranch == "" {
		parentBranch = "master"
	}

	// Add .agency/ to .gitignore BEFORE creating worktree
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".agency/\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}
	if err := runGit(repoRoot, "add", ".gitignore"); err != nil {
		t.Fatalf("failed to add .gitignore: %v", err)
	}
	if err := runGit(repoRoot, "commit", "-m", "add gitignore"); err != nil {
		t.Fatalf("failed to commit .gitignore: %v", err)
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-safe"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Title:        "Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should NOT have a warning when .agency/ is properly ignored
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings when .agency/ is ignored, got: %v", result.Warnings)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
