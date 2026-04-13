package runservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/pipeline"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTempRepo creates a temp repo with agency.json and one commit.
// Returns repo root, data dir, and config dir. Cleanup is handled automatically by t.TempDir().
func setupTempRepo(t *testing.T) (repoRoot, dataDir, configDir string) {
	t.Helper()
	testutil.HermeticGitEnv(t)

	// Create temp directories (t.TempDir handles cleanup automatically)
	repoRoot = t.TempDir()
	dataDir = t.TempDir()

	// Initialize git repo
	require.NoError(t, runGit(repoRoot, "init"), "git init failed")

	// Create agency.json
	agencyJSON := `{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0644), "failed to write agency.json")

	configDir = t.TempDir()
	userConfig := `{
  "version": 1,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  },
  "runners": {
    "claude-code": "sh"
  },
  "editors": {
    "code": "code"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userConfig), 0644), "failed to write config.json")

	// Create scripts directory and setup script
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := "#!/bin/bash\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Create and commit files
	readme := filepath.Join(repoRoot, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# Test\n"), 0644), "failed to write README.md")

	require.NoError(t, runGit(repoRoot, "add", "-A"), "git add failed")
	require.NoError(t, runGit(repoRoot, "commit", "-m", "initial commit"), "git commit failed")

	// Rename branch to main if it's not already
	branch := getCurrentBranch(t, repoRoot)
	if branch != "main" {
		_ = runGit(repoRoot, "branch", "-m", branch, "main") // Best-effort rename
	}

	return repoRoot, dataDir, configDir
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
	require.NoError(t, err, "git branch --show-current failed")
	return strings.TrimSpace(string(output))
}

func TestService_CreateWorktree(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	// Setup pipeline state
	st := &pipeline.PipelineState{
		RunID:    "20260110120000-test",
		Name:     "service-test",
		RepoRoot: resolvedRepoRoot,
		RepoID:   "abcd1234ef567890",
		DataDir:  dataDir,
	}

	// First, simulate CheckRepoSafe and LoadAgencyConfig by populating state
	st.ParentBranch = "main"
	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Now test CreateWorktree
	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	// Verify state was populated
	assert.NotEmpty(t, st.Branch, "Branch should be set")
	assert.True(t, strings.HasPrefix(st.Branch, "agency/"), "Branch should start with 'agency/'")

	assert.NotEmpty(t, st.WorktreePath, "WorktreePath should be set")
	_, err = os.Stat(st.WorktreePath)
	assert.False(t, os.IsNotExist(err), "WorktreePath should exist")

	// Verify .agency/ directories
	agencyDir := filepath.Join(st.WorktreePath, ".agency")
	_, err = os.Stat(agencyDir)
	assert.False(t, os.IsNotExist(err), ".agency/ should exist")

	// Verify report.md
	reportPath := filepath.Join(agencyDir, "report.md")
	_, err = os.Stat(reportPath)
	assert.False(t, os.IsNotExist(err), "report.md should exist")
}

func TestService_CheckRepoSafe_DirtyRepo(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Make repo dirty
	dirty := filepath.Join(repoRoot, "dirty.txt")
	require.NoError(t, os.WriteFile(dirty, []byte("dirty\n"), 0644), "failed to create dirty file")

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	svc := New()
	svc.DataDirOverride = dataDir
	ctx := context.Background()

	st := &pipeline.PipelineState{
		Parent: "main",
	}

	err = svc.CheckRepoSafe(ctx, st)
	require.Error(t, err, "expected error for dirty repo")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EParentDirty, code)
}

func TestService_CheckRepoSafe_UsesWorkingDirOverride(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	svc := NewWithDeps(agencyexec.NewRealRunner(), fs.NewRealFS())
	svc.DataDirOverride = dataDir
	svc.SetWorkingDir(repoRoot)
	ctx := context.Background()

	st := &pipeline.PipelineState{
		Name:   "svc-working-dir",
		Parent: "main",
	}

	err := svc.CheckRepoSafe(ctx, st)
	require.NoError(t, err, "CheckRepoSafe should succeed using explicit working dir override")
	assert.NotEmpty(t, st.RepoRoot)
	assert.NotEmpty(t, st.RepoID)
}

func TestService_LoadAgencyConfig(t *testing.T) {
	repoRoot, dataDir, configDir := setupTempRepo(t)

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err, "failed to resolve symlinks")

	svc := NewWithDeps(agencyexec.NewRealRunner(), fs.NewRealFS())
	svc.ConfigDirOverride = configDir
	ctx := context.Background()

	st := &pipeline.PipelineState{
		RepoRoot: resolvedRepoRoot,
		DataDir:  dataDir,
		Parent:   "main",
	}

	err = svc.LoadAgencyConfig(ctx, st)
	require.NoError(t, err, "LoadAgencyConfig failed")

	// Verify state was populated
	assert.Equal(t, "sh", filepath.Base(st.ResolvedRunnerCmd))
	assert.Equal(t, "scripts/agency_setup.sh", st.SetupScript)
	assert.Equal(t, "main", st.ParentBranch)
}

func TestService_WriteMeta_Success(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-test"
	repoID := "abcd1234ef567890"

	// First create the worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "test-run",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude-code",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	// Populate remaining fields needed for WriteMeta
	st.ResolvedRunnerCmd = "claude"

	// Now test WriteMeta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Verify run directory was created
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	_, err = os.Stat(runDir)
	assert.False(t, os.IsNotExist(err), "run directory should exist")

	// Verify logs directory was created
	logsDir := filepath.Join(runDir, "logs")
	_, err = os.Stat(logsDir)
	assert.False(t, os.IsNotExist(err), "logs directory should exist")

	// Verify meta.json was created
	metaPath := filepath.Join(runDir, "meta.json")
	_, err = os.Stat(metaPath)
	assert.False(t, os.IsNotExist(err), "meta.json should exist")

	// Read and verify meta.json content
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err, "failed to read meta.json")

	// Basic content checks
	content := string(data)
	assert.Contains(t, content, `"schema_version": "1.0"`, "meta.json should contain schema_version 1.0")
	assert.Contains(t, content, `"run_id": "20260110120000-test"`, "meta.json should contain correct run_id")
	assert.Contains(t, content, `"repo_id": "abcd1234ef567890"`, "meta.json should contain correct repo_id")
	assert.Contains(t, content, `"name": "test-run"`, "meta.json should contain correct name")
	assert.Contains(t, content, `"runner": "claude-code"`, "meta.json should contain correct runner")
	assert.Contains(t, content, `"runner_cmd": "claude"`, "meta.json should contain correct runner_cmd")
	assert.Contains(t, content, `"parent_branch": "main"`, "meta.json should contain correct parent_branch")
	assert.Contains(t, content, `"created_at"`, "meta.json should contain created_at")
	// tmux_session_name should NOT be present
	assert.NotContains(t, content, `"tmux_session_name"`, "meta.json should not contain tmux_session_name")
}

func TestService_WriteMeta_WorktreeMissing(t *testing.T) {
	// Create temp data dir (t.TempDir handles cleanup automatically)
	dataDir := t.TempDir()

	svc := New()
	ctx := context.Background()

	st := &pipeline.PipelineState{
		RunID:        "20260110120000-test",
		RepoID:       "abcd1234ef567890",
		DataDir:      dataDir,
		WorktreePath: "/nonexistent/path",
		Runner:       "claude-code",
	}

	err := svc.WriteMeta(ctx, st)
	require.Error(t, err, "expected error for missing worktree")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EInternal, code)
}

func TestService_WriteMeta_RunDirCollision(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-coll"
	repoID := "abcd1234ef567890"

	// Create worktree first
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "collision-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude-code",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "claude"

	// First WriteMeta should succeed
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "first WriteMeta failed")

	// Second WriteMeta should fail with E_RUN_DIR_EXISTS
	err = svc.WriteMeta(ctx, st)
	require.Error(t, err, "expected error for run dir collision")

	code := errors.GetCode(err)
	assert.Equal(t, errors.ERunDirExists, code)
}

func TestService_RunSetup_Success(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-setup"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "setup-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude-code",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script in worktree that writes a sentinel file
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := `#!/bin/bash
set -euo pipefail
echo "setup running"
touch "$AGENCY_DOTAGENCY_DIR/tmp/sentinel"
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup
	err = svc.RunSetup(ctx, st)
	require.NoError(t, err, "RunSetup failed")

	// Verify sentinel file was created
	sentinelPath := filepath.Join(st.WorktreePath, ".agency", "tmp", "sentinel")
	_, err = os.Stat(sentinelPath)
	assert.False(t, os.IsNotExist(err), "sentinel file should exist after setup")

	// Verify log file exists
	logPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs", "setup.log")
	_, err = os.Stat(logPath)
	assert.False(t, os.IsNotExist(err), "setup.log should exist")

	// Read and verify log contains expected content
	logContent, err := os.ReadFile(logPath)
	require.NoError(t, err, "failed to read setup.log")
	assert.Contains(t, string(logContent), "setup running", "setup.log should contain script output")

	// Verify meta was updated
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	require.NoError(t, err, "failed to read meta.json")
	assert.Contains(t, string(metaContent), `"exit_code": 0`, "meta.json should contain exit_code 0")
	assert.Contains(t, string(metaContent), `"command"`, "meta.json should contain command field")
}

func TestService_RunSetup_ScriptFailed(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-fail"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "setup-fail-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude-code",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script that fails with exit code 7
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := `#!/bin/bash
echo "setup failing"
exit 7
`
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup - should fail
	err = svc.RunSetup(ctx, st)
	require.Error(t, err, "expected error for failed setup")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EScriptFailed, code)

	// Verify meta was updated with failure
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	require.NoError(t, err, "failed to read meta.json")
	assert.Contains(t, string(metaContent), `"setup_failed": true`, "meta.json should contain setup_failed: true")
	assert.Contains(t, string(metaContent), `"exit_code": 7`, "meta.json should contain exit_code 7")
}

func TestService_RunSetup_SetupJsonOkFalse(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-json"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "setup-json-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude-code",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script that exits 0 but writes ok=false to setup.json
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := `#!/bin/bash
echo '{"schema_version": "1.0", "ok": false, "summary": "test failure"}' > "$AGENCY_OUTPUT_DIR/setup.json"
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup - should fail due to setup.json ok=false
	err = svc.RunSetup(ctx, st)
	require.Error(t, err, "expected error for setup.json ok=false")

	code := errors.GetCode(err)
	assert.Equal(t, errors.EScriptFailed, code)

	// Verify meta was updated with failure and structured output
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	require.NoError(t, err, "failed to read meta.json")
	assert.Contains(t, string(metaContent), `"setup_failed": true`, "meta.json should contain setup_failed: true")
	assert.Contains(t, string(metaContent), `"output_ok": false`, "meta.json should contain output_ok: false")
	assert.Contains(t, string(metaContent), `"output_summary": "test failure"`, "meta.json should contain output_summary")
}

func TestService_RunSetup_SetupJsonMalformed(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-malj"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "setup-malformed-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "claude-code",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "claude"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script that exits 0 but writes invalid JSON
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := `#!/bin/bash
echo 'not valid json {{{' > "$AGENCY_OUTPUT_DIR/setup.json"
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup - should succeed (malformed JSON is ignored)
	err = svc.RunSetup(ctx, st)
	require.NoError(t, err, "RunSetup failed unexpectedly")

	// Verify meta was updated without structured output fields
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	require.NoError(t, err, "failed to read meta.json")
	// Should not contain output_ok since JSON was malformed
	assert.NotContains(t, string(metaContent), `"output_ok"`, "meta.json should not contain output_ok for malformed JSON")
}

func TestService_StartTmux_Success(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120000-tmux"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "tmux-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "sh", // Use sh as runner for testing
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	// Use 'sh' as the runner command (just opens a shell, which is safe for testing)
	st.ResolvedRunnerCmd = "sh"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script that succeeds
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := "#!/bin/bash\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup
	err = svc.RunSetup(ctx, st)
	require.NoError(t, err, "RunSetup failed")

	// Now test StartTmux
	err = svc.StartTmux(ctx, st)
	if err != nil {
		if errors.GetCode(err) == errors.ETmuxFailed && strings.Contains(err.Error(), "Operation not permitted") {
			t.Skip("tmux is not permitted in this environment")
		}
		require.NoError(t, err, "StartTmux failed")
	}

	// Clean up tmux session (best-effort cleanup)
	sessionName := "agency_" + runID
	_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	// Verify meta was updated with tmux_session_name
	metaPath := filepath.Join(dataDir, "repos", repoID, "runs", runID, "meta.json")
	metaContent, err := os.ReadFile(metaPath)
	require.NoError(t, err, "failed to read meta.json")
	assert.Contains(t, string(metaContent), `"tmux_session_name": "agency_`+runID+`"`, "meta.json should contain tmux_session_name")
}

func TestService_StartTmux_SetupFailed(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

	svc := New()
	ctx := context.Background()

	runID := "20260110120001-fail"
	repoID := "abcd1234ef567890"

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "tmux-fail-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "sh",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "sh"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script that FAILS
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := "#!/bin/bash\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup (should fail)
	err = svc.RunSetup(ctx, st)
	require.Error(t, err, "expected RunSetup to fail")

	// Now test StartTmux - should fail because setup failed
	err = svc.StartTmux(ctx, st)
	require.Error(t, err, "expected StartTmux to fail when setup failed")

	code := errors.GetCode(err)
	assert.Equal(t, errors.ETmuxFailed, code)
}

func TestService_StartTmux_SessionExists(t *testing.T) {
	repoRoot, dataDir, _ := setupTempRepo(t)

	// Change to repo directory with proper cleanup
	oldWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	require.NoError(t, os.Chdir(repoRoot), "failed to chdir to repo")
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWd), "failed to restore working directory")
	})

	resolvedRepoRoot, err2 := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err2, "failed to resolve symlinks")

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
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run() // Best-effort cleanup
	})

	// Create worktree
	st := &pipeline.PipelineState{
		RunID:        runID,
		Name:         "collision-test",
		RepoRoot:     resolvedRepoRoot,
		RepoID:       repoID,
		DataDir:      dataDir,
		ParentBranch: "main",
		Runner:       "sh",
	}

	err = svc.CreateWorktree(ctx, st)
	require.NoError(t, err, "CreateWorktree failed")

	st.ResolvedRunnerCmd = "sh"
	st.SetupScript = "scripts/agency_setup.sh"
	st.SetupTimeout = 10 * time.Minute

	// Write meta
	err = svc.WriteMeta(ctx, st)
	require.NoError(t, err, "WriteMeta failed")

	// Create setup script that succeeds
	scriptsDir := filepath.Join(st.WorktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	setupScript := "#!/bin/bash\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(setupScript), 0755), "failed to write setup script")

	// Run setup
	err = svc.RunSetup(ctx, st)
	require.NoError(t, err, "RunSetup failed")

	// Now test StartTmux - should fail because session already exists
	err = svc.StartTmux(ctx, st)
	require.Error(t, err, "expected StartTmux to fail with session collision")

	code := errors.GetCode(err)
	assert.Equal(t, errors.ETmuxSessionExists, code)
}
