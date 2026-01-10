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

func TestService_WriteMeta_Success(t *testing.T) {
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

	runID := "20260110120000-test"
	repoID := "abcd1234ef567890"

	// First create the worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Test Run",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Populate remaining fields needed for WriteMeta
	st.ResolvedRunnerCmd = "claude"

	// Now test WriteMeta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Verify run directory was created
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		t.Error("run directory should exist")
	}

	// Verify logs directory was created
	logsDir := filepath.Join(runDir, "logs")
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		t.Error("logs directory should exist")
	}

	// Verify meta.json was created
	metaPath := filepath.Join(runDir, "meta.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("meta.json should exist")
	}

	// Read and verify meta.json content
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}

	// Basic content checks
	content := string(data)
	if !strings.Contains(content, `"schema_version": "1.0"`) {
		t.Error("meta.json should contain schema_version 1.0")
	}
	if !strings.Contains(content, `"run_id": "20260110120000-test"`) {
		t.Error("meta.json should contain correct run_id")
	}
	if !strings.Contains(content, `"repo_id": "abcd1234ef567890"`) {
		t.Error("meta.json should contain correct repo_id")
	}
	if !strings.Contains(content, `"title": "Test Run"`) {
		t.Error("meta.json should contain correct title")
	}
	if !strings.Contains(content, `"runner": "claude"`) {
		t.Error("meta.json should contain correct runner")
	}
	if !strings.Contains(content, `"runner_cmd": "claude"`) {
		t.Error("meta.json should contain correct runner_cmd")
	}
	if !strings.Contains(content, `"parent_branch": "main"`) {
		t.Error("meta.json should contain correct parent_branch")
	}
	if !strings.Contains(content, `"created_at"`) {
		t.Error("meta.json should contain created_at")
	}
	// tmux_session_name should NOT be present
	if strings.Contains(content, `"tmux_session_name"`) {
		t.Error("meta.json should not contain tmux_session_name")
	}
}

func TestService_WriteMeta_WorktreeMissing(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "agency-data-*")
	if err != nil {
		t.Fatalf("failed to create temp data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	svc := New()
	ctx := context.Background()

	st := &pipeline.PipelineState{
		RunID:        "20260110120000-test",
		RepoID:       "abcd1234ef567890",
		DataDir:      dataDir,
		WorktreePath: "/nonexistent/path",
		Runner:       "claude",
	}

	err = svc.WriteMeta(ctx, st)
	if err == nil {
		t.Fatal("expected error for missing worktree")
	}

	code := errors.GetCode(err)
	if code != errors.EInternal {
		t.Errorf("error code = %q, want %q", code, errors.EInternal)
	}
}

func TestService_WriteMeta_RunDirCollision(t *testing.T) {
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

	runID := "20260110120000-coll"
	repoID := "abcd1234ef567890"

	// Create worktree first
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Collision Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "claude"

	// First WriteMeta should succeed
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("first WriteMeta failed: %v", err)
	}

	// Second WriteMeta should fail with E_RUN_DIR_EXISTS
	err = svc.WriteMeta(ctx, st)
	if err == nil {
		t.Fatal("expected error for run dir collision")
	}

	code := errors.GetCode(err)
	if code != errors.ERunDirExists {
		t.Errorf("error code = %q, want %q", code, errors.ERunDirExists)
	}
}

func TestService_RunSetup_Success(t *testing.T) {
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

	runID := "20260110120000-setup"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Setup Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script in worktree that writes a sentinel file
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := `#!/bin/bash
set -euo pipefail
echo "setup running"
touch "$AGENCY_DOTAGENCY_DIR/tmp/sentinel"
exit 0
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup
	err = svc.RunSetup(ctx, st)
	if err != nil {
		t.Fatalf("RunSetup failed: %v", err)
	}

	// Verify sentinel file was created
	sentinelPath := filepath.Join(st.WorktreePath, ".agency", "tmp", "sentinel")
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Error("sentinel file should exist after setup")
	}

	// Verify log file exists
	logPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs", "setup.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("setup.log should exist")
	}

	// Read and verify log contains expected content
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read setup.log: %v", err)
	}
	if !strings.Contains(string(logContent), "setup running") {
		t.Error("setup.log should contain script output")
	}

	// Verify meta was updated
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	if !strings.Contains(string(metaContent), `"exit_code": 0`) {
		t.Error("meta.json should contain exit_code 0")
	}
	if !strings.Contains(string(metaContent), `"command"`) {
		t.Error("meta.json should contain command field")
	}
}

func TestService_RunSetup_ScriptFailed(t *testing.T) {
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

	runID := "20260110120000-fail"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Setup Fail Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script that fails with exit code 7
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := `#!/bin/bash
echo "setup failing"
exit 7
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup - should fail
	err = svc.RunSetup(ctx, st)
	if err == nil {
		t.Fatal("expected error for failed setup")
	}

	code := errors.GetCode(err)
	if code != errors.EScriptFailed {
		t.Errorf("error code = %q, want %q", code, errors.EScriptFailed)
	}

	// Verify meta was updated with failure
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	if !strings.Contains(string(metaContent), `"setup_failed": true`) {
		t.Error("meta.json should contain setup_failed: true")
	}
	if !strings.Contains(string(metaContent), `"exit_code": 7`) {
		t.Error("meta.json should contain exit_code 7")
	}
}

func TestService_RunSetup_SetupJsonOkFalse(t *testing.T) {
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

	runID := "20260110120000-json"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Setup JSON Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script that exits 0 but writes ok=false to setup.json
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := `#!/bin/bash
echo '{"schema_version": "1.0", "ok": false, "summary": "test failure"}' > "$AGENCY_OUTPUT_DIR/setup.json"
exit 0
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup - should fail due to setup.json ok=false
	err = svc.RunSetup(ctx, st)
	if err == nil {
		t.Fatal("expected error for setup.json ok=false")
	}

	code := errors.GetCode(err)
	if code != errors.EScriptFailed {
		t.Errorf("error code = %q, want %q", code, errors.EScriptFailed)
	}

	// Verify meta was updated with failure and structured output
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	if !strings.Contains(string(metaContent), `"setup_failed": true`) {
		t.Error("meta.json should contain setup_failed: true")
	}
	if !strings.Contains(string(metaContent), `"output_ok": false`) {
		t.Error("meta.json should contain output_ok: false")
	}
	if !strings.Contains(string(metaContent), `"output_summary": "test failure"`) {
		t.Error("meta.json should contain output_summary")
	}
}

func TestService_RunSetup_SetupJsonMalformed(t *testing.T) {
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

	runID := "20260110120000-malj"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Setup Malformed JSON Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script that exits 0 but writes invalid JSON
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := `#!/bin/bash
echo 'not valid json {{{' > "$AGENCY_OUTPUT_DIR/setup.json"
exit 0
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup - should succeed (malformed JSON is ignored)
	err = svc.RunSetup(ctx, st)
	if err != nil {
		t.Fatalf("RunSetup failed unexpectedly: %v", err)
	}

	// Verify meta was updated without structured output fields
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	// Should not contain output_ok since JSON was malformed
	if strings.Contains(string(metaContent), `"output_ok"`) {
		t.Error("meta.json should not contain output_ok for malformed JSON")
	}
}

func TestService_StartTmux_Success(t *testing.T) {
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

	runID := "20260110120000-tmux"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Tmux Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "sh", // Use sh as runner for testing
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Use 'sh' as the runner command (just opens a shell, which is safe for testing)
	st.ResolvedRunnerCmd = "sh"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script that succeeds
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := "#!/bin/bash\nexit 0\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup
	err = svc.RunSetup(ctx, st)
	if err != nil {
		t.Fatalf("RunSetup failed: %v", err)
	}

	// Now test StartTmux
	err = svc.StartTmux(ctx, st)
	if err != nil {
		t.Fatalf("StartTmux failed: %v", err)
	}

	// Clean up tmux session
	sessionName := "agency_" + runID
	exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	// Verify meta was updated with tmux_session_name
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	if !strings.Contains(string(metaContent), `"tmux_session_name": "agency_`+runID+`"`) {
		t.Error("meta.json should contain tmux_session_name")
	}
}

func TestService_StartTmux_SetupFailed(t *testing.T) {
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

	runID := "20260110120001-fail"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Tmux Fail Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "sh",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "sh"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script that FAILS
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := "#!/bin/bash\nexit 1\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup (should fail)
	err = svc.RunSetup(ctx, st)
	if err == nil {
		t.Fatal("expected RunSetup to fail")
	}

	// Now test StartTmux - should fail because setup failed
	err = svc.StartTmux(ctx, st)
	if err == nil {
		t.Fatal("expected StartTmux to fail when setup failed")
	}

	code := errors.GetCode(err)
	if code != errors.ETmuxFailed {
		t.Errorf("error code = %q, want %q", code, errors.ETmuxFailed)
	}
}

func TestService_StartTmux_SessionExists(t *testing.T) {
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

	runID := "20260110120002-coll"
	repoID := "abcd1234ef567890"
	sessionName := "agency_" + runID

	// Pre-create a tmux session with this name (collision)
	// Use sleep to keep the session alive
	if err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep", "60").Run(); err != nil {
		t.Skip("tmux not available, skipping test")
	}
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Title:        "Collision Test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "sh",
	}

	err := svc.CreateWorktree(ctx, st)
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	st.ResolvedRunnerCmd = "sh"
	st.SetupScript = "scripts/agency_setup.sh"

	// Write meta
	err = svc.WriteMeta(ctx, st)
	if err != nil {
		t.Fatalf("WriteMeta failed: %v", err)
	}

	// Create setup script that succeeds
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	setupScript := "#!/bin/bash\nexit 0\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}

	// Run setup
	err = svc.RunSetup(ctx, st)
	if err != nil {
		t.Fatalf("RunSetup failed: %v", err)
	}

	// Now test StartTmux - should fail because session already exists
	err = svc.StartTmux(ctx, st)
	if err == nil {
		t.Fatal("expected StartTmux to fail with session collision")
	}

	code := errors.GetCode(err)
	if code != errors.ETmuxSessionExists {
		t.Errorf("error code = %q, want %q", code, errors.ETmuxSessionExists)
	}
}
