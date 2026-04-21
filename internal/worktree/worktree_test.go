package worktree

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
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

	baseBranch := getCurrentBranch(t, repoRoot)
	if baseBranch == "" {
		baseBranch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-a1b2"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:      runID,
		Name:       "test-run",
		RepoRoot:   resolvedRepoRoot,
		RepoID:     repoID,
		BaseBranch: baseBranch,
		DataDir:    dataDir,
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

	// Verify INSTRUCTIONS.md exists and has expected content
	instructionsPath := filepath.Join(agencyDir, "INSTRUCTIONS.md")
	instructionsContent, err := os.ReadFile(instructionsPath)
	require.NoError(t, err, "failed to read INSTRUCTIONS.md")

	assert.Contains(t, string(instructionsContent), "# Agency Runner Instructions", "INSTRUCTIONS.md should have runner instructions title")
	assert.Contains(t, string(instructionsContent), ".agency/state/runner_status.json", "INSTRUCTIONS.md should point runners at runner_status.json")
	assert.NotContains(t, string(instructionsContent), ".agency/report.md", "INSTRUCTIONS.md should not mention report.md")

	// Verify runner_status.json exists and starts in a valid working state.
	status, err := runnerstatus.Load(result.WorktreePath)
	require.NoError(t, err, "failed to load runner_status.json")
	require.NotNil(t, status, "runner_status.json should exist")
	assert.Equal(t, runnerstatus.StateRunning, status.State)
	assert.NoError(t, status.Validate(), "initial runner status should be valid")

	// Verify report.md is not scaffolded.
	_, err = os.Stat(filepath.Join(agencyDir, "report.md"))
	assert.True(t, os.IsNotExist(err), "report.md should not be scaffolded")

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

	baseBranch := getCurrentBranch(t, repoRoot)
	if baseBranch == "" {
		baseBranch = "master"
	}

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-c0de"
	repoID := "abcd1234ef567890"

	opts := CreateOpts{
		RunID:      runID,
		Name:       "collision-test",
		RepoRoot:   resolvedRepoRoot,
		RepoID:     repoID,
		BaseBranch: baseBranch,
		DataDir:    dataDir,
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

func TestCreate_MissingBaseBranch_ReturnsError(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-dead"
	repoID := "abcd1234ef567890"

	_, err = Create(ctx, cr, fsys, CreateOpts{
		RunID:      runID,
		Name:       "Test",
		RepoRoot:   resolvedRepoRoot,
		RepoID:     repoID,
		BaseBranch: "nonexistent-branch",
		DataDir:    dataDir,
	})

	require.Error(t, err, "expected error for nonexistent base branch")

	// Verify error code
	code := errors.GetCode(err)
	assert.Equal(t, errors.EWorktreeCreateFailed, code)
}

func TestScaffoldWorkspaceOnly_DoesNotCreateReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fsys := fs.NewRealFS()

	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir), "scaffold failed")

	_, err := os.Stat(filepath.Join(dir, ".agency", "report.md"))
	assert.True(t, os.IsNotExist(err), "report.md should not be created")
}

func TestScaffoldWorkspaceOnly_RunnerStatusNotOverwritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fsys := fs.NewRealFS()

	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir), "first scaffold failed")

	status, err := runnerstatus.Load(dir)
	require.NoError(t, err, "failed to load initial runner status")
	require.NotNil(t, status, "runner_status.json should exist after scaffold")
	assert.Equal(t, runnerstatus.StateRunning, status.State)

	custom := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		State:         runnerstatus.StateSucceeded,
		UpdatedAt:     "2026-04-20T18:00:00Z",
		Summary:       "Finished the task",
		Questions:     []string{},
		HowToTest:     "go test ./internal/worktree",
		Risks:         []string{"none"},
	}
	data, err := json.MarshalIndent(custom, "", "  ")
	require.NoError(t, err, "failed to marshal custom runner status")
	data = append(data, '\n')
	require.NoError(t, os.WriteFile(runnerstatus.StatusPath(dir), data, 0644), "failed to write custom runner status")

	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir), "second scaffold failed")

	status, err = runnerstatus.Load(dir)
	require.NoError(t, err, "failed to load runner status after second scaffold")
	require.NotNil(t, status, "runner_status.json should still exist after second scaffold")
	assert.Equal(t, custom, *status, "runner_status.json should not be overwritten by scaffold")
}

func TestScaffoldWorkspaceOnly_InstructionsAlwaysOverwritten(t *testing.T) {
	t.Parallel()
	// INSTRUCTIONS.md should be unconditionally overwritten on every run.
	dir := t.TempDir()
	fsys := fs.NewRealFS()

	// First call creates INSTRUCTIONS.md
	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir), "first scaffold failed")

	instructionsPath := filepath.Join(dir, ".agency", "INSTRUCTIONS.md")
	content1, err := os.ReadFile(instructionsPath)
	require.NoError(t, err, "failed to read INSTRUCTIONS.md")

	// Verify initial content
	assert.Contains(t, string(content1), "# Agency Runner Instructions", "INSTRUCTIONS.md should have standard content")

	// Write custom content to INSTRUCTIONS.md
	customContent := "# Custom Instructions\nThis should be overwritten.\n"
	require.NoError(t, os.WriteFile(instructionsPath, []byte(customContent), 0644), "failed to write custom content")

	// Second call SHOULD overwrite INSTRUCTIONS.md
	require.NoError(t, ScaffoldWorkspaceOnly(fsys, dir), "second scaffold failed")

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

func TestInstructionsTemplate(t *testing.T) {
	t.Parallel()
	template := InstructionsTemplate()

	// Check title
	assert.Contains(t, template, "# Agency Runner Instructions", "template should have Agency Runner Instructions title")

	// Check required content per spec
	requiredContent := []string{
		"Make incremental, focused commits",
		"Keep commits buildable",
		"Keep `.agency/state/runner_status.json` current while you work",
		"Set status to `ready` with `summary` and `how_to_test` before finishing",
		"runner_status.json",
		"only runner contract",
		"This file is advisory only",
	}
	for _, content := range requiredContent {
		assert.Contains(t, template, content, "template should contain %q", content)
	}
	assert.NotContains(t, template, ".agency/report.md", "template should not mention report.md")
}

func TestCreate_IgnoreWarning(t *testing.T) {
	repoRoot, dataDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

	baseBranch := getCurrentBranch(t, repoRoot)
	if baseBranch == "" {
		baseBranch = "master"
	}

	// Note: We don't add .agency/ to .gitignore, so we should get a warning

	ctx := context.Background()
	cr := agencyexec.NewRealRunner()
	fsys := fs.NewRealFS()

	runID := "20260110120000-warn"
	repoID := "abcd1234ef567890"

	result, err := Create(ctx, cr, fsys, CreateOpts{
		RunID:      runID,
		Name:       "Test",
		RepoRoot:   resolvedRepoRoot,
		RepoID:     repoID,
		BaseBranch: baseBranch,
		DataDir:    dataDir,
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

	baseBranch := getCurrentBranch(t, repoRoot)
	if baseBranch == "" {
		baseBranch = "master"
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
		RunID:      runID,
		Name:       "Test",
		RepoRoot:   resolvedRepoRoot,
		RepoID:     repoID,
		BaseBranch: baseBranch,
		DataDir:    dataDir,
	})

	require.NoError(t, err, "Create failed")

	// Should NOT have a warning when .agency/ is properly ignored
	assert.Empty(t, result.Warnings, "expected no warnings when .agency/ is ignored")
}
