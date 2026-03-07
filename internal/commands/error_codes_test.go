package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/verifyservice"
)

// --- ENotInteractive tests ---

func TestAgentAttach_NotInteractive_ReturnsENotInteractive(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.AlwaysHasSession = true

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef:   invocationID,
		TmuxClient:      fakeTmux,
		IsInteractive:   func() bool { return false }, // NOT interactive
		DataDirOverride: dataDir,
	}

	err := AgentAttach(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))
	assert.Contains(t, err.Error(), "interactive terminal")
}

// --- EInvalidRepoPath tests ---

func TestRun_InvalidRepoPath_NotExist_ReturnsEInvalidRepoPath(t *testing.T) {
	t.Parallel()
	cr := testutil.NewFakeCommandRunner()
	fsys := fs.NewRealFS()

	var stdout, stderr bytes.Buffer
	opts := RunOpts{
		Name:     "test",
		RepoPath: "/nonexistent/path/to/nowhere",
	}

	err := Run(context.Background(), cr, fsys, t.TempDir(), opts, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "does not exist")
}

func TestRun_InvalidRepoPath_NotDir_ReturnsEInvalidRepoPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a file (not a directory)
	filePath := filepath.Join(tmpDir, "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0644))

	cr := testutil.NewFakeCommandRunner()
	fsys := fs.NewRealFS()

	var stdout, stderr bytes.Buffer
	opts := RunOpts{
		Name:     "test",
		RepoPath: filePath,
	}

	err := Run(context.Background(), cr, fsys, tmpDir, opts, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not a directory")
}

func TestRun_InvalidRepoPath_NotGitRepo_ReturnsEInvalidRepoPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a directory that is NOT a git repo
	notRepoDir := filepath.Join(tmpDir, "not-a-repo")
	require.NoError(t, os.MkdirAll(notRepoDir, 0755))

	cr := testutil.NewFakeCommandRunner()
	// Simulate git rev-parse failing (not a git repo)
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{
		ExitCode: 128,
		Stderr:   "fatal: not a git repository",
		Err:      assert.AnError,
	}
	fsys := fs.NewRealFS()

	var stdout, stderr bytes.Buffer
	opts := RunOpts{
		Name:     "test",
		RepoPath: notRepoDir,
	}

	err := Run(context.Background(), cr, fsys, tmpDir, opts, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not inside a git repository")
}

func TestInit_InvalidRepoPath_ReturnsEInvalidRepoPath(t *testing.T) {
	t.Parallel()

	cr := &stubRunner{repoRoot: "", exitCode: 128}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{
		RepoPath: "/nonexistent/repo/path",
	}
	err := Init(ctx, cr, fsys, t.TempDir(), opts, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not inside a git repository")
}

// --- ERunBroken tests ---

func TestOpen_BrokenMeta_ReturnsERunBroken(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	configDir := t.TempDir()
	writeUserConfigForOpen(t, configDir, "code")

	fsys := fs.NewRealFS()

	// Create a run directory with invalid meta.json
	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))

	// Write invalid JSON to meta.json
	metaPath := filepath.Join(runDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, []byte("not valid json"), 0644))

	var stdout, stderr bytes.Buffer
	err := Open(context.Background(), testutil.NewFakeCommandRunner(), fsys, "", OpenOpts{
		RunID:             runID,
		DataDirOverride:   dataDir,
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.ERunBroken, errors.GetCode(err))
	assert.Contains(t, err.Error(), "unreadable or invalid")
}

// --- ERunRefAmbiguous tests ---

func TestResolveRun_AmbiguousName_ReturnsERunRefAmbiguous(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()

	// Create two runs with the same name in different repos
	now := time.Now()
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	repoID1 := "repo-aaa111222333"
	runID1 := "20260115120000-a3f2"
	_, err := st.EnsureRunDir(repoID1, runID1)
	require.NoError(t, err)
	meta1 := store.NewRunMeta(runID1, repoID1, "my-run", "claude", "claude", "main", "agency/my-run-a3f2", "/tmp/wt1", now)
	require.NoError(t, st.WriteInitialMeta(repoID1, runID1, meta1))

	repoID2 := "repo-bbb444555666"
	runID2 := "20260115130000-b4c5"
	_, err = st.EnsureRunDir(repoID2, runID2)
	require.NoError(t, err)
	meta2 := store.NewRunMeta(runID2, repoID2, "my-run", "claude", "claude", "main", "agency/my-run-b4c5", "/tmp/wt2", now)
	require.NoError(t, st.WriteInitialMeta(repoID2, runID2, meta2))

	// Resolve by name "my-run" which exists in both repos - should be ambiguous
	rctx := &RunResolutionContext{
		DataDir: dataDir,
	}

	_, resolveErr := ResolveRun(rctx, "my-run")
	require.Error(t, resolveErr)
	assert.Equal(t, errors.ERunRefAmbiguous, errors.GetCode(resolveErr))
	assert.Contains(t, resolveErr.Error(), "ambiguous")
}

func TestResolveRun_AmbiguousName_ErrorMessageContainsCandidates(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()

	now := time.Now()
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	// Create two runs with same name in different repos
	repoID1 := "repo-aaa111222333"
	runID1 := "20260115120000-a3f2"
	_, err := st.EnsureRunDir(repoID1, runID1)
	require.NoError(t, err)
	meta1 := store.NewRunMeta(runID1, repoID1, "dup-name", "claude", "claude", "main", "b1", "/tmp/w1", now)
	require.NoError(t, st.WriteInitialMeta(repoID1, runID1, meta1))

	repoID2 := "repo-bbb444555666"
	runID2 := "20260115130000-b4c5"
	_, err = st.EnsureRunDir(repoID2, runID2)
	require.NoError(t, err)
	meta2 := store.NewRunMeta(runID2, repoID2, "dup-name", "claude", "claude", "main", "b2", "/tmp/w2", now)
	require.NoError(t, st.WriteInitialMeta(repoID2, runID2, meta2))

	rctx := &RunResolutionContext{DataDir: dataDir}
	_, resolveErr := ResolveRun(rctx, "dup-name")
	require.Error(t, resolveErr)

	ae, ok := errors.AsAgencyError(resolveErr)
	require.True(t, ok, "expected AgencyError")
	assert.Equal(t, errors.ERunRefAmbiguous, ae.Code)
	// Error message should contain both run IDs for disambiguation
	assert.Contains(t, ae.Msg, runID1)
	assert.Contains(t, ae.Msg, runID2)
	assert.Contains(t, ae.Msg, "hint")
}

func TestResolveRun_ThreeWayAmbiguous_ReturnsERunRefAmbiguous(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()

	now := time.Now()
	st := store.NewStore(fsys, dataDir, func() time.Time { return now })

	// Create three runs with same name in three repos
	repos := []struct {
		repoID string
		runID  string
	}{
		{"repo-aaa111111111", "20260115120000-a3f2"},
		{"repo-bbb222222222", "20260115130000-b4c5"},
		{"repo-ccc333333333", "20260115140000-c6d7"},
	}

	for _, r := range repos {
		_, err := st.EnsureRunDir(r.repoID, r.runID)
		require.NoError(t, err)
		meta := store.NewRunMeta(r.runID, r.repoID, "shared-name", "claude", "claude", "main", "b", "/tmp/w", now)
		require.NoError(t, st.WriteInitialMeta(r.repoID, r.runID, meta))
	}

	rctx := &RunResolutionContext{DataDir: dataDir}
	_, err := ResolveRun(rctx, "shared-name")
	require.Error(t, err)
	assert.Equal(t, errors.ERunRefAmbiguous, errors.GetCode(err))

	// Verify the details contain the input
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.NotNil(t, ae.Details)
	assert.Equal(t, "shared-name", ae.Details["input"])
}

// --- EScriptNotFound tests ---

func TestCheckScript_MissingScript_ReturnsEScriptNotFound(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	fsys := fs.NewRealFS()

	_, err := checkScript(fsys, "scripts/nonexistent.sh", repoRoot, "verify")
	require.Error(t, err)
	assert.Equal(t, errors.EScriptNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not found")
}

func TestCheckScript_AbsolutePathMissing_ReturnsEScriptNotFound(t *testing.T) {
	t.Parallel()
	fsys := fs.NewRealFS()

	_, err := checkScript(fsys, "/nonexistent/path/script.sh", t.TempDir(), "setup")
	require.Error(t, err)
	assert.Equal(t, errors.EScriptNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not found")
}

// --- EScriptNotExecutable tests ---

func TestCheckScript_NotExecutable_ReturnsEScriptNotExecutable(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	fsys := fs.NewRealFS()

	// Create script directory
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755))

	// Create script file with no execute permission
	scriptPath := filepath.Join(scriptsDir, "agency_verify.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0644)) // 0644 = no execute

	_, err := checkScript(fsys, "scripts/agency_verify.sh", repoRoot, "verify")
	require.Error(t, err)
	assert.Equal(t, errors.EScriptNotExecutable, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not executable")
}

func TestCheckScript_AbsolutePathNotExecutable_ReturnsEScriptNotExecutable(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()

	// Create script file at absolute path with no execute permission
	scriptPath := filepath.Join(tmpDir, "verify.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0644))

	_, err := checkScript(fsys, scriptPath, t.TempDir(), "verify")
	require.Error(t, err)
	assert.Equal(t, errors.EScriptNotExecutable, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not executable")
}

// --- EScriptTimeout tests ---

func TestFormatVerifyOutput_TimedOut_ReturnsEScriptTimeout(t *testing.T) {
	t.Parallel()
	record := &store.VerifyRecord{
		RunID:    "20260115120000-a3f2",
		OK:       false,
		TimedOut: true,
		LogPath:  "/data/repos/abc123/runs/20260115120000-a3f2/logs/verify.log",
	}
	result := &verifyservice.VerifyRunResult{Record: record}

	var stdout, stderr bytes.Buffer
	err := formatVerifyOutput(result, nil, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EScriptTimeout, errors.GetCode(err))
	assert.Contains(t, err.Error(), "verify timed out")
	// Error details should be carried in the error, not printed to stderr
	assert.Empty(t, stderr.String(), "errors must not be printed to stderr; main.go handles error printing")
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["log"], "verify.log")
	assert.Contains(t, ae.Details["record"], "verify_record.json")
}

// --- EScriptFailed (companion test for completeness) ---

func TestFormatVerifyOutput_ScriptFailed_ReturnsEScriptFailed(t *testing.T) {
	t.Parallel()
	exitCode := 1
	record := &store.VerifyRecord{
		RunID:    "20260115120000-a3f2",
		OK:       false,
		TimedOut: false,
		ExitCode: &exitCode,
		LogPath:  "/data/repos/abc123/runs/20260115120000-a3f2/logs/verify.log",
	}
	result := &verifyservice.VerifyRunResult{Record: record}

	var stdout, stderr bytes.Buffer
	err := formatVerifyOutput(result, nil, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EScriptFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "verify failed")
	// Error details should be carried in the error, not printed to stderr
	assert.Empty(t, stderr.String(), "errors must not be printed to stderr; main.go handles error printing")
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["log"], "verify.log")
	assert.Contains(t, ae.Details["record"], "verify_record.json")
}

// --- EInvalidRepoPath via ResolveRunContext (runresolver.go) ---

func TestResolveRunContext_InvalidRepoPath_NotExist(t *testing.T) {
	t.Parallel()
	cr := testutil.NewFakeCommandRunner()
	cwd := t.TempDir()

	_, err := ResolveRunContext(context.Background(), cr, cwd, "/nonexistent/path", "")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "does not exist")
}

func TestResolveRunContext_InvalidRepoPath_NotGitRepo(t *testing.T) {
	t.Parallel()
	notRepoDir := t.TempDir()

	cr := testutil.NewFakeCommandRunner()
	// git rev-parse fails for this dir
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{
		ExitCode: 128,
		Stderr:   "fatal: not a git repository",
		Err:      assert.AnError,
	}

	_, err := ResolveRunContext(context.Background(), cr, t.TempDir(), notRepoDir, "")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not inside a git repository")
}

// --- EResolveRepoContext with --repo path validation ---

func TestResolveRepoContext_InvalidRepoPath_NotExist(t *testing.T) {
	t.Parallel()
	cr := testutil.NewFakeCommandRunner()

	_, _, err := ResolveRepoContext(context.Background(), cr, t.TempDir(), "/nonexistent/repo/path")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
}

func TestResolveRepoContext_InvalidRepoPath_NotDir(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "afile")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	cr := testutil.NewFakeCommandRunner()

	_, _, err := ResolveRepoContext(context.Background(), cr, tmpDir, filePath)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidRepoPath, errors.GetCode(err))
	assert.Contains(t, err.Error(), "not a directory")
}

// --- ERunBroken via resolveRunGlobal (used by show, path, open, push, resolve) ---

func TestResolveRunGlobal_BrokenMeta_ReturnsBrokenRef(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	// Create a run directory with invalid meta.json
	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(runDir, 0755))

	// Write invalid JSON
	metaPath := filepath.Join(runDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, []byte("{invalid json"), 0644))

	ref, record, err := resolveRunGlobal(runID, dataDir)
	// resolveRunGlobal returns the broken ref (not an error) - command layer checks Broken flag
	require.NoError(t, err)
	assert.True(t, ref.Broken)
	assert.Nil(t, record.Meta)
}

// --- EScriptNotFound/EScriptNotExecutable via Doctor checkScript ---

func TestDoctor_ScriptNotFound_ReturnsEScriptNotFound(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	fsys := fs.NewRealFS()

	// Create a valid agency.json that references missing scripts
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755))
	agencyJSON := `{
  "version": 1,
  "scripts": {
    "setup": {"path": "scripts/agency_setup.sh"},
    "verify": {"path": "scripts/agency_verify.sh"},
    "archive": {"path": "scripts/agency_archive.sh"}
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0644))

	configDir := t.TempDir()
	dataDir := t.TempDir()

	// Write valid user config
	userCfgJSON := `{"version": 1, "defaults": {"runner": "claude", "editor": "code"}, "runners": {"claude": "claude"}}`
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userCfgJSON), 0644))

	cr := newMockRunner()
	cr.SetResponse("git", []string{"rev-parse", "--show-toplevel"}, agencyexec.CmdResult{Stdout: repoRoot + "\n"}, nil)
	cr.SetResponse("git", []string{"--version"}, agencyexec.CmdResult{Stdout: "git version 2.40.0\n"}, nil)
	cr.SetResponse("tmux", []string{"-V"}, agencyexec.CmdResult{Stdout: "tmux 3.3\n"}, nil)
	cr.SetResponse("gh", []string{"--version"}, agencyexec.CmdResult{Stdout: "gh version 2.40.0\n"}, nil)
	cr.SetResponse("gh", []string{"auth", "status"}, agencyexec.CmdResult{Stdout: "Logged in\n"}, nil)
	cr.SetResponse("git", []string{"branch", "--show-current"}, agencyexec.CmdResult{Stdout: "main\n"}, nil)
	cr.SetResponse("git", []string{"config", "--get", "remote.origin.url"}, agencyexec.CmdResult{Stdout: "git@github.com:test/repo.git\n"}, nil)

	var stdout, stderr bytes.Buffer
	err := Doctor(context.Background(), cr, fsys, repoRoot, DoctorOpts{
		DataDirOverride:   dataDir,
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EScriptNotFound, errors.GetCode(err))
}

func TestDoctor_ScriptNotExecutable_ReturnsEScriptNotExecutable(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	fsys := fs.NewRealFS()

	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755))
	agencyJSON := `{
  "version": 1,
  "scripts": {
    "setup": {"path": "scripts/agency_setup.sh"},
    "verify": {"path": "scripts/agency_verify.sh"},
    "archive": {"path": "scripts/agency_archive.sh"}
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0644))

	// Create scripts but NOT executable
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte("#!/bin/sh\nexit 0"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_verify.sh"), []byte("#!/bin/sh\nexit 0"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_archive.sh"), []byte("#!/bin/sh\nexit 0"), 0644))

	configDir := t.TempDir()
	dataDir := t.TempDir()

	userCfgJSON := `{"version": 1, "defaults": {"runner": "claude", "editor": "code"}, "runners": {"claude": "claude"}}`
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userCfgJSON), 0644))

	cr := newMockRunner()
	cr.SetResponse("git", []string{"rev-parse", "--show-toplevel"}, agencyexec.CmdResult{Stdout: repoRoot + "\n"}, nil)
	cr.SetResponse("git", []string{"--version"}, agencyexec.CmdResult{Stdout: "git version 2.40.0\n"}, nil)
	cr.SetResponse("tmux", []string{"-V"}, agencyexec.CmdResult{Stdout: "tmux 3.3\n"}, nil)
	cr.SetResponse("gh", []string{"--version"}, agencyexec.CmdResult{Stdout: "gh version 2.40.0\n"}, nil)
	cr.SetResponse("gh", []string{"auth", "status"}, agencyexec.CmdResult{Stdout: "Logged in\n"}, nil)
	cr.SetResponse("git", []string{"branch", "--show-current"}, agencyexec.CmdResult{Stdout: "main\n"}, nil)
	cr.SetResponse("git", []string{"config", "--get", "remote.origin.url"}, agencyexec.CmdResult{Stdout: "git@github.com:test/repo.git\n"}, nil)

	var stdout, stderr bytes.Buffer
	err := Doctor(context.Background(), cr, fsys, repoRoot, DoctorOpts{
		DataDirOverride:   dataDir,
		ConfigDirOverride: configDir,
	}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EScriptNotExecutable, errors.GetCode(err))
}
