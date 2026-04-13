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

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// --- EArchiveFailed tests ---

func TestArchive_ScriptFails_ReturnsEArchiveFailed(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(worktreePath, 0755))
	require.NoError(t, os.MkdirAll(logsDir, 0755))

	// Create archive script that exits non-zero
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1"), 0755))

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
		Name:         "Test Run",
		Runner:       "claude-code",
		Branch:       "agency/test-a3f2",
		ParentBranch: "main",
	}

	cfg := Config{
		Meta:          meta,
		RepoRoot:      "",
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

	// Script failed, so result.Success() should be false
	assert.False(t, result.ScriptOK, "ScriptOK should be false when script exits non-zero")
	assert.False(t, result.Success(), "archive should not succeed when script fails")

	// Convert result to error and verify EArchiveFailed
	archiveErr := result.ToError()
	require.Error(t, archiveErr)
	assert.Equal(t, errors.EArchiveFailed, errors.GetCode(archiveErr))
	assert.Contains(t, archiveErr.Error(), "script failed")
}

func TestArchive_ScriptTimeout_ReturnsEArchiveFailed(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	worktreesDir := filepath.Join(dataDir, "repos", repoID, "worktrees")
	worktreePath := filepath.Join(worktreesDir, runID)
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(worktreePath, 0755))
	require.NoError(t, os.MkdirAll(logsDir, 0755))

	// Create archive script that sleeps longer than timeout
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 30"), 0755))

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
		Name:         "Test Run",
		Runner:       "claude-code",
		Branch:       "agency/test-a3f2",
		ParentBranch: "main",
	}

	cfg := Config{
		Meta:          meta,
		RepoRoot:      "",
		DataDir:       dataDir,
		ArchiveScript: scriptPath,
		Timeout:       100 * time.Millisecond, // very short timeout
	}

	deps := Deps{
		CR:         testutil.NewFakeCommandRunner(),
		TmuxClient: testutil.NewFakeTmuxClient(),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	}

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	result := Archive(context.Background(), cfg, deps, st)

	assert.False(t, result.ScriptOK, "ScriptOK should be false when script times out")
	assert.Contains(t, result.ScriptReason, "timed out")

	archiveErr := result.ToError()
	require.Error(t, archiveErr)
	assert.Equal(t, errors.EArchiveFailed, errors.GetCode(archiveErr))
	assert.Contains(t, archiveErr.Error(), "script failed")
}

func TestArchive_DeleteFails_ReturnsEArchiveFailed(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test-repo-id"
	runID := "20260115-test"

	// Put worktree OUTSIDE the allowed prefix so SafeRemoveAll refuses
	worktreePath := filepath.Join(tmpDir, "outside-worktree")
	logsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID, "logs")

	require.NoError(t, os.MkdirAll(worktreePath, 0755))
	require.NoError(t, os.MkdirAll(logsDir, 0755))

	// Create archive script that exits 0
	scriptPath := filepath.Join(tmpDir, "archive.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0"), 0755))

	meta := &store.RunMeta{
		RunID:        runID,
		RepoID:       repoID,
		WorktreePath: worktreePath,
		Name:         "Test Run",
		Runner:       "claude-code",
		Branch:       "agency/test-a3f2",
		ParentBranch: "main",
	}

	cfg := Config{
		Meta:          meta,
		RepoRoot:      "", // skip git worktree remove
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

	// Script succeeded but delete should fail (outside allowed prefix)
	assert.True(t, result.ScriptOK, "ScriptOK should be true")
	assert.False(t, result.DeleteOK, "DeleteOK should be false when worktree is outside allowed prefix")
	assert.False(t, result.Success(), "archive should fail when delete fails")
	assert.Contains(t, result.DeleteReason, "outside allowed prefix")

	archiveErr := result.ToError()
	require.Error(t, archiveErr)
	assert.Equal(t, errors.EArchiveFailed, errors.GetCode(archiveErr))
	assert.Contains(t, archiveErr.Error(), "worktree deletion failed")
}

func TestResult_ToError_Success_ReturnsNil(t *testing.T) {
	t.Parallel()
	result := &Result{
		ScriptOK: true,
		TmuxOK:   true,
		DeleteOK: true,
	}

	assert.Nil(t, result.ToError(), "successful result should return nil error")
}

func TestResult_ToError_ScriptAndDeleteFailed_CombinesReasons(t *testing.T) {
	t.Parallel()
	result := &Result{
		ScriptOK:     false,
		ScriptReason: "exit 1",
		TmuxOK:       true,
		DeleteOK:     false,
		DeleteReason: "directory still exists",
	}

	archiveErr := result.ToError()
	require.Error(t, archiveErr)
	assert.Equal(t, errors.EArchiveFailed, errors.GetCode(archiveErr))
	// Both failures should be mentioned
	assert.Contains(t, archiveErr.Error(), "script failed")
	assert.Contains(t, archiveErr.Error(), "worktree deletion failed")
}
