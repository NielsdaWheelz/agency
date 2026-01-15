package archive

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// fakeTmuxClient is a test fake for tmux.Client.
type fakeTmuxClient struct {
	hasSession  bool
	killErr     error
	killCalled  bool
	sessionName string
}

func (f *fakeTmuxClient) HasSession(ctx context.Context, name string) (bool, error) {
	return f.hasSession, nil
}

func (f *fakeTmuxClient) NewSession(ctx context.Context, name, cwd string, argv []string) error {
	return nil
}

func (f *fakeTmuxClient) Attach(ctx context.Context, name string) error {
	return nil
}

func (f *fakeTmuxClient) KillSession(ctx context.Context, name string) error {
	f.killCalled = true
	f.sessionName = name
	return f.killErr
}

func (f *fakeTmuxClient) SendKeys(ctx context.Context, name string, keys []tmux.Key) error {
	return nil
}

// fakeRunner is a test fake for exec.CommandRunner.
type fakeRunner struct {
	results map[string]exec.CmdResult
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	key := name
	if result, ok := f.results[key]; ok {
		return result, nil
	}
	return exec.CmdResult{ExitCode: 0}, nil
}

func TestArchive_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create data directory structure
	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	// Create a test file in worktree to verify deletion
	testFile := filepath.Join(worktreePath, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create archive script that exits 0
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatalf("failed to create archive script: %v", err)
	}

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
		Title:        "Test Run",
		Runner:       "claude",
		Branch:       "agency/test-a3f2",
		ParentBranch: "main",
	}

	cfg := Config{
		Meta:          meta,
		RepoRoot:      "", // empty to skip git worktree remove
		DataDir:       dataDir,
		ArchiveScript: scriptPath,
		Timeout:       5 * time.Second,
	}

	fakeTmux := &fakeTmuxClient{hasSession: false}
	deps := Deps{
		CR:         &fakeRunner{results: map[string]exec.CmdResult{}},
		TmuxClient: fakeTmux,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	if !result.Success() {
		t.Errorf("Archive failed: ScriptOK=%v DeleteOK=%v TmuxOK=%v\nReasons: script=%q delete=%q tmux=%q",
			result.ScriptOK, result.DeleteOK, result.TmuxOK,
			result.ScriptReason, result.DeleteReason, result.TmuxReason)
	}

	// Verify worktree is deleted
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree still exists after archive")
	}
}

func TestArchive_TmuxMissingSessionIsOK(t *testing.T) {
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	scriptPath := filepath.Join(tmpDir, "archive.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatalf("failed to create archive script: %v", err)
	}

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
	}

	cfg := Config{
		Meta:          meta,
		DataDir:       dataDir,
		ArchiveScript: scriptPath,
		Timeout:       5 * time.Second,
	}

	// Tmux kill returns "no sessions" error - simulate via fake
	fakeTmux := &fakeTmuxClient{
		killErr: &noSessionError{},
	}

	deps := Deps{
		CR:         &fakeRunner{results: map[string]exec.CmdResult{}},
		TmuxClient: fakeTmux,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	if !result.TmuxOK {
		t.Error("TmuxOK should be true when session doesn't exist")
	}
}

// noSessionError simulates a tmux "no session" error
type noSessionError struct{}

func (e *noSessionError) Error() string {
	return "can't find session: nonexistent"
}

func TestArchive_ScriptFailure(t *testing.T) {
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	// Create archive script that fails
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1"), 0755); err != nil {
		t.Fatalf("failed to create archive script: %v", err)
	}

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
	}

	cfg := Config{
		Meta:          meta,
		DataDir:       dataDir,
		ArchiveScript: scriptPath,
		Timeout:       5 * time.Second,
	}

	fakeTmux := &fakeTmuxClient{hasSession: false}
	deps := Deps{
		CR:         &fakeRunner{results: map[string]exec.CmdResult{}},
		TmuxClient: fakeTmux,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	if result.ScriptOK {
		t.Error("ScriptOK should be false when script exits non-zero")
	}

	if result.Success() {
		t.Error("Archive should not succeed when script fails")
	}

	// Delete should still be attempted and succeed
	if !result.DeleteOK {
		t.Errorf("DeleteOK should be true even when script fails: %s", result.DeleteReason)
	}
}

func TestArchive_DeleteOutsidePrefix(t *testing.T) {
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	// Create worktree OUTSIDE the expected location
	outsidePath := filepath.Join(tmpDir, "outside", "worktree")
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	if err := os.MkdirAll(outsidePath, 0755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	scriptPath := filepath.Join(tmpDir, "archive.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatalf("failed to create archive script: %v", err)
	}

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: outsidePath, // Point to path outside allowed prefix
	}

	cfg := Config{
		Meta:          meta,
		RepoRoot:      "", // no git worktree remove
		DataDir:       dataDir,
		ArchiveScript: scriptPath,
		Timeout:       5 * time.Second,
	}

	fakeTmux := &fakeTmuxClient{hasSession: false}
	deps := Deps{
		CR:         &fakeRunner{results: map[string]exec.CmdResult{}},
		TmuxClient: fakeTmux,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	if result.DeleteOK {
		t.Error("DeleteOK should be false when worktree is outside allowed prefix")
	}

	// Verify the outside path still exists (wasn't deleted)
	if _, err := os.Stat(outsidePath); os.IsNotExist(err) {
		t.Error("path outside prefix was deleted")
	}
}

func TestResult_Success(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		wantOK   bool
	}{
		{
			name:   "all ok",
			result: Result{ScriptOK: true, TmuxOK: true, DeleteOK: true},
			wantOK: true,
		},
		{
			name:   "script failed",
			result: Result{ScriptOK: false, TmuxOK: true, DeleteOK: true},
			wantOK: false,
		},
		{
			name:   "delete failed",
			result: Result{ScriptOK: true, TmuxOK: true, DeleteOK: false},
			wantOK: false,
		},
		{
			name:   "tmux failed but script and delete ok",
			result: Result{ScriptOK: true, TmuxOK: false, DeleteOK: true},
			wantOK: true, // tmux failure doesn't affect success
		},
		{
			name:   "all failed",
			result: Result{ScriptOK: false, TmuxOK: false, DeleteOK: false},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.Success()
			if got != tt.wantOK {
				t.Errorf("Success() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}
