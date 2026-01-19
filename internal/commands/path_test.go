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
)

func TestPath_RunNotFound(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: "missing"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing run")
	}
	if errors.GetCode(err) != errors.ERunNotFound {
		t.Fatalf("expected E_RUN_NOT_FOUND, got %s", errors.GetCode(err))
	}
}

func TestPath_WorktreeMissing(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

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

	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: runID}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing worktree")
	}
	if errors.GetCode(err) != errors.EWorktreeMissing {
		t.Fatalf("expected E_WORKTREE_MISSING, got %s", errors.GetCode(err))
	}
}

func TestPath_Success(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

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

	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: runID}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check output is exactly the worktree path with a newline
	got := stdout.String()
	want := worktreePath + "\n"
	if got != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestPath_ByName(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)

	repoID := "repo123456789012"
	runID := "20260115120000-a3f2"
	runName := "my-feature"
	worktreePath := filepath.Join(dataDir, "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	if _, err := st.EnsureRunDir(repoID, runID); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}

	meta := store.NewRunMeta(runID, repoID, runName, "claude", "claude", "main", "agency/my-feature-a3f2", worktreePath, time.Now())
	if err := st.WriteInitialMeta(repoID, runID, meta); err != nil {
		t.Fatalf("WriteInitialMeta: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: runName}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := strings.TrimSpace(stdout.String())
	if got != worktreePath {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", got, worktreePath)
	}
}

func TestPath_ByPrefix(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

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

	// Use a prefix of the run_id
	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: "20260115"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := strings.TrimSpace(stdout.String())
	if got != worktreePath {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", got, worktreePath)
	}
}

func TestPath_EmptyRunRef(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	var stdout, stderr bytes.Buffer
	err := Path(context.Background(), PathOpts{RunRef: ""}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for empty run ref")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Fatalf("expected E_USAGE, got %s", errors.GetCode(err))
	}
}
