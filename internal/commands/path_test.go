package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/require"
)

func TestPath_RunNotFound(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: "missing", DataDirOverride: dataDir}, &stdout, &stderr)
	require.Error(t, err, "expected error for missing run")
	require.Equal(t, errors.ERunNotFound, errors.GetCode(err))
}

func TestPath_WorktreeMissing(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"

	_, err := st.EnsureRunDir(repoID, runID)
	require.NoError(t, err, "EnsureRunDir")

	meta := store.NewRunMeta(runID, repoID, "test", "claude-code", "claude", "main", "agency/test-a3f2", "/missing/worktree", time.Now())
	require.NoError(t, st.WriteInitialMeta(repoID, runID, meta), "WriteInitialMeta")

	var stdout, stderr bytes.Buffer
	err = Path(context.Background(), PathOpts{RunRef: runID, DataDirOverride: dataDir}, &stdout, &stderr)
	require.Error(t, err, "expected error for missing worktree")
	require.Equal(t, errors.EWorktreeMissing, errors.GetCode(err))
}

func TestPath_Success(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	worktreePath := filepath.Join(dataDir, "worktree")
	require.NoError(t, os.MkdirAll(worktreePath, 0o755), "failed to create worktree")

	_, err := st.EnsureRunDir(repoID, runID)
	require.NoError(t, err, "EnsureRunDir")

	meta := store.NewRunMeta(runID, repoID, "test", "claude-code", "claude", "main", "agency/test-a3f2", worktreePath, time.Now())
	require.NoError(t, st.WriteInitialMeta(repoID, runID, meta), "WriteInitialMeta")

	var stdout, stderr bytes.Buffer
	err = Path(context.Background(), PathOpts{RunRef: runID, DataDirOverride: dataDir}, &stdout, &stderr)
	require.NoError(t, err)

	// Check output is exactly the worktree path with a newline
	got := stdout.String()
	want := worktreePath + "\n"
	require.Equal(t, want, got)
}

func TestPath_ByName(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	runName := "my-feature"
	worktreePath := filepath.Join(dataDir, "worktree")
	require.NoError(t, os.MkdirAll(worktreePath, 0o755), "failed to create worktree")

	_, err := st.EnsureRunDir(repoID, runID)
	require.NoError(t, err, "EnsureRunDir")

	meta := store.NewRunMeta(runID, repoID, runName, "claude-code", "claude", "main", "agency/my-feature-a3f2", worktreePath, time.Now())
	require.NoError(t, st.WriteInitialMeta(repoID, runID, meta), "WriteInitialMeta")

	var stdout, stderr bytes.Buffer
	err = Path(context.Background(), PathOpts{RunRef: runName, DataDirOverride: dataDir}, &stdout, &stderr)
	require.NoError(t, err)

	got := strings.TrimSpace(stdout.String())
	require.Equal(t, worktreePath, got)
}

func TestPath_ByPrefix(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	worktreePath := filepath.Join(dataDir, "worktree")
	require.NoError(t, os.MkdirAll(worktreePath, 0o755), "failed to create worktree")

	_, err := st.EnsureRunDir(repoID, runID)
	require.NoError(t, err, "EnsureRunDir")

	meta := store.NewRunMeta(runID, repoID, "test", "claude-code", "claude", "main", "agency/test-a3f2", worktreePath, time.Now())
	require.NoError(t, st.WriteInitialMeta(repoID, runID, meta), "WriteInitialMeta")

	// Use a prefix of the run_id
	var stdout, stderr bytes.Buffer
	err = Path(context.Background(), PathOpts{RunRef: "20260115", DataDirOverride: dataDir}, &stdout, &stderr)
	require.NoError(t, err)

	got := strings.TrimSpace(stdout.String())
	require.Equal(t, worktreePath, got)
}

func TestPath_EmptyRunRef(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: "", DataDirOverride: dataDir}, &stdout, &stderr)
	require.Error(t, err, "expected error for empty run ref")
	require.Equal(t, errors.EUsage, errors.GetCode(err))
}
