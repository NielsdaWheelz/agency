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
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTempRepo creates a temp repo with one commit on the default branch.
// Returns the repo root path and data dir. Cleanup is handled automatically by t.TempDir().
func setupTempRepo(t *testing.T) (repoRoot, dataDir string) {
	t.Helper()
	testutil.HermeticGitEnv(t)

	// Create temp directories (t.TempDir handles cleanup automatically)
	repoRoot = t.TempDir()
	dataDir = t.TempDir()

	// Initialize git repo
	require.NoError(t, runGit(repoRoot, "init"), "git init failed")

	// Create and commit a file
	readme := filepath.Join(repoRoot, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# Test Repo\n"), 0644), "failed to write README.md")

	require.NoError(t, runGit(repoRoot, "add", "-A"), "git add failed")
	require.NoError(t, runGit(repoRoot, "commit", "-m", "initial commit"), "git commit failed")

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
	require.NoError(t, err, "git branch --show-current failed")
	return strings.TrimSpace(string(output))
}

func TestCreate_Success(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

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
		Name:         "test-run",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	require.NoError(t, err, "Create failed")

	// Verify branch name format
	expectedBranch := "agency/test-run-a1b2"
	assert.Equal(t, expectedBranch, result.Branch)

	// Verify worktree path
	expectedPath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	assert.Equal(t, expectedPath, result.WorktreePath)

	// Verify worktree directory exists
	assert.DirExists(t, result.WorktreePath, "worktree directory does not exist")

	// Verify .agency/ directories exist
	agencyDir := filepath.Join(result.WorktreePath, ".agency")
	assert.DirExists(t, agencyDir, ".agency/ directory does not exist")

	outDir := filepath.Join(agencyDir, "out")
	assert.DirExists(t, outDir, ".agency/out/ directory does not exist")

	tmpDir := filepath.Join(agencyDir, "tmp")
	assert.DirExists(t, tmpDir, ".agency/tmp/ directory does not exist")

	// Verify report.md exists and has title
	reportPath := filepath.Join(agencyDir, "report.md")
	reportContent, err := os.ReadFile(reportPath)
	require.NoError(t, err, "failed to read report.md")

	assert.True(t, strings.HasPrefix(string(reportContent), "# test-run\n"), "report.md should start with '# test-run\\n', got: %q", string(reportContent)[:min(50, len(reportContent))])

	// Verify report.md references INSTRUCTIONS.md (per S7 spec4)
	assert.Contains(t, string(reportContent), "runner: read `.agency/INSTRUCTIONS.md` before starting.", "report.md should reference INSTRUCTIONS.md")

	// Verify INSTRUCTIONS.md exists and has expected content
	instructionsPath := filepath.Join(agencyDir, "INSTRUCTIONS.md")
	instructionsContent, err := os.ReadFile(instructionsPath)
	require.NoError(t, err, "failed to read INSTRUCTIONS.md")

	assert.Contains(t, string(instructionsContent), "# Agency Runner Instructions", "INSTRUCTIONS.md should have runner instructions title")

	// Verify git worktree list shows the new worktree
	cmd := exec.Command("git", "-C", resolvedRepoRoot, "worktree", "list")
	output, err := cmd.Output()
	require.NoError(t, err, "git worktree list failed")
	assert.Contains(t, string(output), runID, "git worktree list should contain run_id %s", runID)
}

// Note: TestCreate_EmptyTitle was removed because name is now required
// and validated at the CLI/pipeline level. The worktree package expects
// a valid, pre-validated name to be passed.

func TestCreate_Collision_ReturnsError(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

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
		Name:         "collision-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	}

	// First creation should succeed
	_, err = Create(ctx, cr, fsys, opts)
	require.NoError(t, err, "first Create failed")

	// Second creation with same run_id should fail
	_, err = Create(ctx, cr, fsys, opts)
	require.Error(t, err, "expected error for collision")

	// Verify error code
	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeCreateFailed, code)

	// Verify error has details
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError")
	require.NotNil(t, ae.Details, "expected Details to be set")
	assert.NotEmpty(t, ae.Details["command"], "expected command in details")
	// Should have stderr (git error message)
	assert.NotEmpty(t, ae.Details["stderr"], "expected stderr in details")
}

func TestCreate_MissingParentBranch_ReturnsError(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-dead"
	repoID := "abcd1234ef567890"

	_, err = Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Name:         "Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: "nonexistent-branch",
		DataDir:      dataDir,
	})

	require.Error(t, err, "expected error for nonexistent parent branch")

	// Verify error code
	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeCreateFailed, code)
}

func TestScaffoldWorkspaceOnly_ReportNotOverwritten(t *testing.T) {
	t.Parallel()
	// Create temp directory (t.TempDir handles cleanup automatically)
	dir := t.TempDir()

	fsys := fs.NewRealFS()

	// First call creates everything
	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir, "First Title"), "first scaffold failed")

	// Verify report.md has first title
	reportPath := filepath.Join(dir, ".agency", "report.md")
	content1, err := os.ReadFile(reportPath)
	require.NoError(t, err, "failed to read report.md")
	assert.True(t, strings.HasPrefix(string(content1), "# First Title\n"), "report.md should have 'First Title', got: %q", string(content1)[:min(50, len(content1))])

	// Write a sentinel to report.md
	sentinel := "# First Title\n\n## SENTINEL\nThis should not be overwritten\n"
	require.NoError(t, os.WriteFile(reportPath, []byte(sentinel), 0644), "failed to write sentinel")

	// Second call should NOT overwrite report.md
	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir, "Second Title"), "second scaffold failed")

	// Verify sentinel is preserved
	content2, err := os.ReadFile(reportPath)
	require.NoError(t, err, "failed to read report.md")
	assert.Equal(t, sentinel, string(content2), "report.md was overwritten")
}

func TestScaffoldWorkspaceOnly_InstructionsAlwaysOverwritten(t *testing.T) {
	t.Parallel()
	// Per S7 spec4: INSTRUCTIONS.md should be unconditionally overwritten on every run
	dir := t.TempDir()
	fsys := fs.NewRealFS()

	// First call creates INSTRUCTIONS.md
	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir, "First Title"), "first scaffold failed")

	instructionsPath := filepath.Join(dir, ".agency", "INSTRUCTIONS.md")
	content1, err := os.ReadFile(instructionsPath)
	require.NoError(t, err, "failed to read INSTRUCTIONS.md")

	// Verify initial content
	assert.Contains(t, string(content1), "# Agency Runner Instructions", "INSTRUCTIONS.md should have standard content")

	// Write custom content to INSTRUCTIONS.md
	customContent := "# Custom Instructions\nThis should be overwritten.\n"
	require.NoError(t, os.WriteFile(instructionsPath, []byte(customContent), 0644), "failed to write custom content")

	// Second call SHOULD overwrite INSTRUCTIONS.md
	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir, "Second Title"), "second scaffold failed")

	content2, err := os.ReadFile(instructionsPath)
	require.NoError(t, err, "failed to read INSTRUCTIONS.md after second scaffold")

	// Verify INSTRUCTIONS.md was overwritten with standard content
	assert.Contains(t, string(content2), "# Agency Runner Instructions", "INSTRUCTIONS.md should be overwritten with standard content")
	assert.NotContains(t, string(content2), "Custom Instructions", "INSTRUCTIONS.md should NOT contain custom content after second scaffold")
}

func TestWorktreePath(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := WorktreePath(tt.dataDir, tt.repoID, tt.runID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReportTemplate(t *testing.T) {
	t.Parallel()
	template := ReportTemplate("My Test Run")

	// Check title line
	assert.True(t, strings.HasPrefix(template, "# My Test Run\n"), "template should start with '# My Test Run\\n'")

	// Check for instructions reference (per S7 spec4)
	assert.Contains(t, template, "runner: read `.agency/INSTRUCTIONS.md` before starting.", "template should reference INSTRUCTIONS.md after title")

	// Check required sections exist (updated per constitution)
	requiredSections := []string{
		"## summary",
		"## scope",
		"## decisions",
		"## deviations",
		"## problems encountered",
		"## how to test",
		"## review notes",
		"## follow-ups",
	}
	for _, section := range requiredSections {
		assert.Contains(t, template, section, "template should contain %q", section)
	}
}

func TestInstructionsTemplate(t *testing.T) {
	t.Parallel()
	template := InstructionsTemplate()

	// Check title
	assert.Contains(t, template, "# Agency Runner Instructions", "template should have Agency Runner Instructions title")

	// Check required content per spec
	requiredContent := []string{
		"Make incremental, focused commits",
		"Keep commits buildable",
		"Update `.agency/report.md` before finishing",
		"`## summary`",
		"`## how to test`",
		"runner_status.json",
		"This file is advisory only",
	}
	for _, content := range requiredContent {
		assert.Contains(t, template, content, "template should contain %q", content)
	}
}

func TestCreate_IgnoreWarning(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

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
		Name:         "Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	require.NoError(t, err, "Create failed")

	// Should have a warning about .agency/ not being ignored
	require.NotEmpty(t, result.Warnings, "expected warning about .agency/ not being ignored")
	assert.Equal(t, "W_AGENCY_NOT_IGNORED", result.Warnings[0].Code)
}

func TestCreate_IgnoreWarning_NotPresentWhenIgnored(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

	parentBranch := getCurrentBranch(t, repoRoot)
	if parentBranch == "" {
		parentBranch = "master"
	}

	// Add .agency/ to .gitignore BEFORE creating worktree
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	require.NoError(t, os.WriteFile(gitignorePath, []byte(".agency/\n"), 0644), "failed to write .gitignore")
	require.NoError(t, runGit(repoRoot, "add", ".gitignore"), "failed to add .gitignore")
	require.NoError(t, runGit(repoRoot, "commit", "-m", "add gitignore"), "failed to commit .gitignore")

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-safe"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:        runID,
		Name:         "Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		ParentBranch: parentBranch,
		DataDir:      dataDir,
	})

	require.NoError(t, err, "Create failed")

	// Should NOT have a warning when .agency/ is properly ignored
	assert.Empty(t, result.Warnings, "expected no warnings when .agency/ is ignored")
}
