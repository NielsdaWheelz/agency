package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func writeUserConfigForOpen(t *testing.T, configDir, editorCmd string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	cfg := `{
  "version": 1,
  "defaults": {
    "runner": "claude",
    "editor": "code"
  },
  "editors": {
    "code": "` + editorCmd + `"
  }
}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}
}

func TestOpen_RunNotFound(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfigForOpen(t, configDir, "code")

	err := Open(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), "", OpenOpts{RunID: "missing"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing run")
	}
	if errors.GetCode(err) != errors.ERunNotFound {
		t.Fatalf("expected E_RUN_NOT_FOUND, got %s", errors.GetCode(err))
	}
}

func TestOpen_WorktreeMissing(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfigForOpen(t, configDir, "code")

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"

	if _, err := st.EnsureRunDir(repoID, runID); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}

	meta := store.NewRunMeta(runID, repoID, "test", "claude", "claude", "main", "agency/test-a3f2", "/missing/worktree", time.Now())
	if err := st.WriteInitialMeta(repoID, runID, meta); err != nil {
		t.Fatalf("WriteInitialMeta: %v", err)
	}

	err := Open(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), "", OpenOpts{RunID: runID}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing worktree")
	}
	if errors.GetCode(err) != errors.EWorktreeMissing {
		t.Fatalf("expected E_WORKTREE_MISSING, got %s", errors.GetCode(err))
	}
}

func TestOpen_Success(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	editorPath := filepath.Join(configDir, "bin", "editor")
	if err := os.MkdirAll(filepath.Dir(editorPath), 0o755); err != nil {
		t.Fatalf("failed to create editor dir: %v", err)
	}
	if err := os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("failed to write editor script: %v", err)
	}

	writeUserConfigForOpen(t, configDir, "bin/editor")

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	worktreePath := filepath.Join(dataDir, "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	if _, err := st.EnsureRunDir(repoID, runID); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}

	meta := store.NewRunMeta(runID, repoID, "test", "claude", "claude", "main", "agency/test-a3f2", worktreePath, time.Now())
	if err := st.WriteInitialMeta(repoID, runID, meta); err != nil {
		t.Fatalf("WriteInitialMeta: %v", err)
	}

	err := Open(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), "", OpenOpts{RunID: runID}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
