package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/require"
)

func writeUserConfigForOpen(t *testing.T, configDir, editorCmd string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(configDir, 0o755), "failed to create config dir")
	cfg := `{
  "version": 1,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  },
  "editors": {
    "code": "` + editorCmd + `"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644), "failed to write config.json")
}

func TestOpen_RunNotFound(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	configDir := t.TempDir()
	writeUserConfigForOpen(t, configDir, "code")

	err := Open(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), OpenOpts{RunID: "missing", DataDirOverride: dataDir, ConfigDirOverride: configDir})
	require.Error(t, err, "expected error for missing run")
	require.Equal(t, errors.ERunNotFound, errors.GetCode(err))
}

func TestOpen_WorktreeMissing(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	configDir := t.TempDir()
	writeUserConfigForOpen(t, configDir, "code")

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"

	_, err := st.EnsureRunDir(repoID, runID)
	require.NoError(t, err, "EnsureRunDir")

	meta := store.NewRunMeta(runID, repoID, "test", "claude-code", "claude", "main", "agency/test-a3f2", "/missing/worktree", time.Now())
	require.NoError(t, st.WriteInitialMeta(repoID, runID, meta), "WriteInitialMeta")

	err = Open(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), OpenOpts{RunID: runID, DataDirOverride: dataDir, ConfigDirOverride: configDir})
	require.Error(t, err, "expected error for missing worktree")
	require.Equal(t, errors.EWorktreeMissing, errors.GetCode(err))
}

func TestOpen_Success(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	configDir := t.TempDir()

	editorPath := filepath.Join(configDir, "bin", "editor")
	require.NoError(t, os.MkdirAll(filepath.Dir(editorPath), 0o755), "failed to create editor dir")
	require.NoError(t, os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 0\n"), 0o755), "failed to write editor script")

	writeUserConfigForOpen(t, configDir, "bin/editor")

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

	err = Open(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), OpenOpts{RunID: runID, DataDirOverride: dataDir, ConfigDirOverride: configDir})
	require.NoError(t, err)
}
