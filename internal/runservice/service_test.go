package runservice

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
	"github.com/NielsdaWheelz/agency/internal/pipeline"
)

// setupTempRepo creates a temp repo with agency.json and one commit.
// Returns repo root, data dir, and cleanup function.
func setupTempRepo(t *testing.T) (repoRoot, dataDir string, cleanup func()) {
	t.Helper()

	repoRoot, err := os.MkdirTemp("", "agency-svc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp repo dir: %v", err)
	}

	dataDir, err = os.MkdirTemp("", "agency-data-*")
	if err != nil {
		os.RemoveAll(repoRoot)
		t.Fatalf("failed to create temp data dir: %v", err)
	}

	cleanup = func() {
		os.RemoveAll(repoRoot)
		os.RemoveAll(dataDir)
	}

	// Initialize git repo
	if err := runGit(repoRoot, "init"); err != nil {
		cleanup()
		t.Fatalf("git init failed: %v", err)
	}

	if err := runGit(repoRoot, "config", "user.email", "test@example.com"); err != nil {
		cleanup()
		t.Fatalf("git config user.email failed: %v", err)
	}
	if err := runGit(repoRoot, "config", "user.name", "Test User"); err != nil {
		cleanup()
		t.Fatalf("git config user.name failed: %v", err)
	}

	// Create agency.json
	agencyJSON := `{
  "version": 1,
  "defaults": {
    "parent_branch": "main",
    "runner": "claude"
  },
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
  }
}`
	if err := os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0644); err != nil {
		cleanup()
		t.Fatalf("failed to write agency.json: %v", err)
	}

	// Create scripts directory and setup script
	scriptsDir := filepath.Join(repoRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		cleanup()
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := "#!/bin/bash\nexit 0\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		cleanup()
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Create and commit files
	readme := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readme, []byte("# Test\n"), 0644); err != nil {
		cleanup()
		t.Fatalf("failed to write README.md: %v", err)
	}

	if err := runGit(repoRoot, "add", "-A"); err != nil {
		cleanup()
		t.Fatalf("git add failed: %v", err)
	}
	if err := runGit(repoRoot, "commit", "-m", "initial commit"); err != nil {
		cleanup()
		t.Fatalf("git commit failed: %v", err)
	}

	// Rename branch to main if it's not already
	branch := getCurrentBranch(t, repoRoot)
	if branch != "main" {
		runGit(repoRoot, "branch", "-m", branch, "main")
	}

	return repoRoot, dataDir, cleanup
}

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

func TestService_CreateWorktree(t *testing.T) {
	repoRoot, dataDir, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	// Change to repo directory
	oldWd, _ := os.Getwd()
	os.Chdir(repoRoot)
	defer os.Chdir(oldWd)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	svc := New()
	ctx := context.Background()

	// Setup pipeline state
	st := &pipeline.PipelineState{
		RunID:    "20260110120000-test",
		Title:    "Service Test",
		RepoRoot: resolvedRepoRoot,
		RepoID:   "abcd1234ef567890",
		DataDir:  dataDir,
	}

	// First, simulate CheckRepoSafe and LoadAgencyConfig by populating state
	st.ParentBranch = "main"
	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"

	// Now test CreateWorktree
	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify state was populated
	if st.Branch == "" {
		t.Error("Branch should be set")
	}
	if !strings.HasPrefix(st.Branch, "agency/") {
		t.Errorf("Branch should start with 'agency/', got %q", st.Branch)
	}

	if st.WorktreePath == "" {
		t.Error("WorktreePath should be set")
	}
	if _, err := os.Stat(st.WorktreePath); os.IsNotExist(err) {
		t.Error("WorktreePath should exist")
	}

	// Verify .agency/ directories
	agencyDir := filepath.Join(st.WorktreePath, ".agency")
	if _, err := os.Stat(agencyDir); os.IsNotExist(err) {
		t.Error(".agency/ should exist")
	}

	// Verify report.md
	reportPath := filepath.Join(agencyDir, "report.md")
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		t.Error("report.md should exist")
	}
}

func TestService_CheckRepoSafe_DirtyRepo(t *testing.T) {
	repoRoot, dataDir, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	// Make repo dirty
	dirty := filepath.Join(repoRoot, "dirty.txt")
	if err := os.WriteFile(dirty, []byte("dirty\n"), 0644); err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	// Change to repo directory
	oldWd, _ := os.Getwd()
	os.Chdir(repoRoot)
	defer os.Chdir(oldWd)

	svc := New()
	ctx := context.Background()

	st := &pipeline.PipelineState{
		Parent: "main",
	}

	err := svc.CheckRepoSafe(ctx, st)
	if err == nil {
		t.Fatal("expected error for dirty repo")
	}

	code := errors.GetCode(err)
	if code != errors.EParentDirty {
		t.Errorf("error code = %q, want %q", code, errors.EParentDirty)
	}
}

func TestService_LoadAgencyConfig(t *testing.T) {
	repoRoot, dataDir, cleanup := setupTempRepo(t)
	defer cleanup()

	// Set AGENCY_DATA_DIR
	oldDataDir := os.Getenv("AGENCY_DATA_DIR")
	os.Setenv("AGENCY_DATA_DIR", dataDir)
	defer os.Setenv("AGENCY_DATA_DIR", oldDataDir)

	resolvedRepoRoot, _ := filepath.EvalSymlinks(repoRoot)

	svc := NewWithDeps(agencyexec.NewRealRunner(), fs.NewRealFS())
	ctx := context.Background()

	st := &pipeline.PipelineState{
		RepoRoot: resolvedRepoRoot,
		DataDir:  dataDir,
	}

	err := svc.LoadAgencyConfig(ctx, st)
	if err != nil {
		t.Fatalf("LoadAgencyConfig failed: %v", err)
	}

	// Verify state was populated
	if st.ResolvedRunnerCmd != "claude" {
		t.Errorf("ResolvedRunnerCmd = %q, want %q", st.ResolvedRunnerCmd, "claude")
	}
	if st.SetupScript != "scripts/agency_setup.sh" {
		t.Errorf("SetupScript = %q, want %q", st.SetupScript, "scripts/agency_setup.sh")
	}
	if st.ParentBranch != "main" {
		t.Errorf("ParentBranch = %q, want %q", st.ParentBranch, "main")
	}
}

func TestService_WriteMeta_NotImplemented(t *testing.T) {
	svc := New()
	ctx := context.Background()
	st := &pipeline.PipelineState{}

	err := svc.WriteMeta(ctx, st)
	if err == nil {
		t.Fatal("expected error for not implemented")
	}

	code := errors.GetCode(err)
	if code != errors.ENotImplemented {
		t.Errorf("error code = %q, want %q", code, errors.ENotImplemented)
	}
}

func TestService_RunSetup_NotImplemented(t *testing.T) {
	svc := New()
	ctx := context.Background()
	st := &pipeline.PipelineState{}

	err := svc.RunSetup(ctx, st)
	if err == nil {
		t.Fatal("expected error for not implemented")
	}

	code := errors.GetCode(err)
	if code != errors.ENotImplemented {
		t.Errorf("error code = %q, want %q", code, errors.ENotImplemented)
	}
}

func TestService_StartTmux_NotImplemented(t *testing.T) {
	svc := New()
	ctx := context.Background()
	st := &pipeline.PipelineState{}

	err := svc.StartTmux(ctx, st)
	if err == nil {
		t.Fatal("expected error for not implemented")
	}

	code := errors.GetCode(err)
	if code != errors.ENotImplemented {
		t.Errorf("error code = %q, want %q", code, errors.ENotImplemented)
	}
}
