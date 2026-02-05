package archive

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestArchive_HappyPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create data directory structure
	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(worktreePath, 0755), "failed to create worktree dir")
	require.NoError(t, os.MkdirAll(logsDir, 0755), "failed to create logs dir")

	// Create a test file in worktree to verify deletion
	testFile := filepath.Join(worktreePath, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644), "failed to create test file")

	// Create archive script that exits 0
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755), "failed to create archive script")

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
		Name:         "Test Run",
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

	deps := Deps{
		CR:         testutil.NewFakeCommandRunner(),
		TmuxClient: testutil.NewFakeTmuxClient(),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	assert.True(t, result.Success(),
		"Archive failed: ScriptOK=%v DeleteOK=%v TmuxOK=%v\nReasons: script=%q delete=%q tmux=%q",
		result.ScriptOK, result.DeleteOK, result.TmuxOK,
		result.ScriptReason, result.DeleteReason, result.TmuxReason)

	// Verify worktree is deleted
	_, err := os.Stat(worktreePath)
	assert.True(t, os.IsNotExist(err), "worktree still exists after archive")
}

func TestArchive_TmuxMissingSessionIsOK(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(worktreePath, 0755), "failed to create worktree dir")
	require.NoError(t, os.MkdirAll(logsDir, 0755), "failed to create logs dir")

	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755), "failed to create archive script")

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
	fakeTmux := testutil.NewFakeTmuxClient()
	fakeTmux.KillSessionErr = &noSessionError{}

	deps := Deps{
		CR:         testutil.NewFakeCommandRunner(),
		TmuxClient: fakeTmux,
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	assert.True(t, result.TmuxOK, "TmuxOK should be true when session doesn't exist")
}

// noSessionError simulates a tmux "no session" error
type noSessionError struct{}

func (e *noSessionError) Error() string {
	return "can't find session: nonexistent"
}

func TestArchive_ScriptFailure(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(worktreePath, 0755), "failed to create worktree dir")
	require.NoError(t, os.MkdirAll(logsDir, 0755), "failed to create logs dir")

	// Create archive script that fails
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1"), 0755), "failed to create archive script")

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

	deps := Deps{
		CR:         testutil.NewFakeCommandRunner(),
		TmuxClient: testutil.NewFakeTmuxClient(),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	assert.False(t, result.ScriptOK, "ScriptOK should be false when script exits non-zero")
	assert.False(t, result.Success(), "Archive should not succeed when script fails")

	// Delete should still be attempted and succeed
	assert.True(t, result.DeleteOK, "DeleteOK should be true even when script fails: %s", result.DeleteReason)
}

func TestArchive_DeleteOutsidePrefix(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	// Create worktree OUTSIDE the expected location
	outsidePath := filepath.Join(tmpDir, "outside", "worktree")
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(outsidePath, 0755), "failed to create outside dir")
	require.NoError(t, os.MkdirAll(logsDir, 0755), "failed to create logs dir")

	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755), "failed to create archive script")

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

	deps := Deps{
		CR:         testutil.NewFakeCommandRunner(),
		TmuxClient: testutil.NewFakeTmuxClient(),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	assert.False(t, result.DeleteOK, "DeleteOK should be false when worktree is outside allowed prefix")

	// Verify the outside path still exists (wasn't deleted)
	_, err := os.Stat(outsidePath)
	assert.False(t, os.IsNotExist(err), "path outside prefix was deleted")
}

func TestResult_Success(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result Result
		wantOK bool
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.result.Success()
			assert.Equal(t, tt.wantOK, got)
		})
	}
}
