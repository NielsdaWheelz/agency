package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

func TestIntegrationWorktreePaths(t *testing.T) {
	st := NewStore(fs.NewRealFS(), "/data", time.Now)

	tests := []struct {
		name     string
		repoID   string
		wtID     string
		wantDir  string
		wantMeta string
		wantTree string
	}{
		{
			name:     "basic paths",
			repoID:   "abc123",
			wtID:     "20260131120000-a1b2",
			wantDir:  "/data/repos/abc123/integration_worktrees/20260131120000-a1b2",
			wantMeta: "/data/repos/abc123/integration_worktrees/20260131120000-a1b2/meta.json",
			wantTree: "/data/repos/abc123/integration_worktrees/20260131120000-a1b2/tree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := st.IntegrationWorktreeDir(tt.repoID, tt.wtID); got != tt.wantDir {
				t.Errorf("IntegrationWorktreeDir() = %v, want %v", got, tt.wantDir)
			}
			if got := st.IntegrationWorktreeMetaPath(tt.repoID, tt.wtID); got != tt.wantMeta {
				t.Errorf("IntegrationWorktreeMetaPath() = %v, want %v", got, tt.wantMeta)
			}
			if got := st.IntegrationWorktreeTreePath(tt.repoID, tt.wtID); got != tt.wantTree {
				t.Errorf("IntegrationWorktreeTreePath() = %v, want %v", got, tt.wantTree)
			}
		})
	}
}

func TestNewIntegrationWorktreeMeta(t *testing.T) {
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	meta := NewIntegrationWorktreeMeta(
		"20260131120000-a1b2",
		"my-feature",
		"abc123",
		"agency/my-feature-a1b2",
		"main",
		"/path/to/tree",
		now,
	)

	if meta.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %v, want 1.0", meta.SchemaVersion)
	}
	if meta.WorktreeID != "20260131120000-a1b2" {
		t.Errorf("WorktreeID = %v, want 20260131120000-a1b2", meta.WorktreeID)
	}
	if meta.Name != "my-feature" {
		t.Errorf("Name = %v, want my-feature", meta.Name)
	}
	if meta.State != WorktreeStatePresent {
		t.Errorf("State = %v, want %v", meta.State, WorktreeStatePresent)
	}
	if meta.CreatedAt != "2026-01-31T12:00:00Z" {
		t.Errorf("CreatedAt = %v, want 2026-01-31T12:00:00Z", meta.CreatedAt)
	}
}

func TestEnsureIntegrationWorktreeDir(t *testing.T) {
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// First call should succeed
	dir, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	if err != nil {
		t.Fatalf("EnsureIntegrationWorktreeDir() error = %v", err)
	}
	if dir != st.IntegrationWorktreeDir(repoID, wtID) {
		t.Errorf("returned dir = %v, want %v", dir, st.IntegrationWorktreeDir(repoID, wtID))
	}

	// Directory should exist
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("directory was not created")
	}

	// Second call should fail (exclusive)
	_, err = st.EnsureIntegrationWorktreeDir(repoID, wtID)
	if err == nil {
		t.Errorf("expected error on duplicate, got nil")
	}
}

func TestWriteAndReadIntegrationWorktreeMeta(t *testing.T) {
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// Create the directory first
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	if err != nil {
		t.Fatalf("EnsureIntegrationWorktreeDir() error = %v", err)
	}

	// Write meta
	meta := NewIntegrationWorktreeMeta(
		wtID,
		"my-feature",
		repoID,
		"agency/my-feature-a1b2",
		"main",
		"/path/to/tree",
		time.Now(),
	)

	if err := st.WriteIntegrationWorktreeMeta(repoID, wtID, meta); err != nil {
		t.Fatalf("WriteIntegrationWorktreeMeta() error = %v", err)
	}

	// Read back
	read, err := st.ReadIntegrationWorktreeMeta(repoID, wtID)
	if err != nil {
		t.Fatalf("ReadIntegrationWorktreeMeta() error = %v", err)
	}

	if read.WorktreeID != meta.WorktreeID {
		t.Errorf("WorktreeID = %v, want %v", read.WorktreeID, meta.WorktreeID)
	}
	if read.Name != meta.Name {
		t.Errorf("Name = %v, want %v", read.Name, meta.Name)
	}
}

func TestReadIntegrationWorktreeMeta_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	_, err := st.ReadIntegrationWorktreeMeta("nonexistent", "notfound")
	if err == nil {
		t.Error("expected error for nonexistent worktree")
	}
}

func TestUpdateIntegrationWorktreeMeta(t *testing.T) {
	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "abc123"
	wtID := "20260131120000-a1b2"

	// Create directory and initial meta
	_, err := st.EnsureIntegrationWorktreeDir(repoID, wtID)
	if err != nil {
		t.Fatalf("EnsureIntegrationWorktreeDir() error = %v", err)
	}

	meta := NewIntegrationWorktreeMeta(
		wtID,
		"my-feature",
		repoID,
		"agency/my-feature-a1b2",
		"main",
		"/path/to/tree",
		time.Now(),
	)
	if err := st.WriteIntegrationWorktreeMeta(repoID, wtID, meta); err != nil {
		t.Fatalf("WriteIntegrationWorktreeMeta() error = %v", err)
	}

	// Update to archived state
	err = st.UpdateIntegrationWorktreeMeta(repoID, wtID, func(m *IntegrationWorktreeMeta) {
		m.State = WorktreeStateArchived
	})
	if err != nil {
		t.Fatalf("UpdateIntegrationWorktreeMeta() error = %v", err)
	}

	// Verify update
	read, err := st.ReadIntegrationWorktreeMeta(repoID, wtID)
	if err != nil {
		t.Fatalf("ReadIntegrationWorktreeMeta() error = %v", err)
	}
	if read.State != WorktreeStateArchived {
		t.Errorf("State = %v, want %v", read.State, WorktreeStateArchived)
	}
}

func TestScanIntegrationWorktreesForRepo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create repo directory structure
	repoID := "abc123"
	wtDir := filepath.Join(tmpDir, "repos", repoID, "integration_worktrees")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Create two worktrees
	wt1Dir := filepath.Join(wtDir, "20260131100000-a1b2")
	wt2Dir := filepath.Join(wtDir, "20260131110000-c3d4")
	if err := os.Mkdir(wt1Dir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.Mkdir(wt2Dir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Write meta for wt1
	meta1 := &IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "20260131100000-a1b2",
		Name:          "feature-a",
		RepoID:        repoID,
		State:         WorktreeStatePresent,
		CreatedAt:     "2026-01-31T10:00:00Z",
	}
	data1, _ := json.MarshalIndent(meta1, "", "  ")
	if err := os.WriteFile(filepath.Join(wt1Dir, "meta.json"), data1, 0o644); err != nil {
		t.Fatalf("failed to write meta: %v", err)
	}

	// Write meta for wt2
	meta2 := &IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    "20260131110000-c3d4",
		Name:          "feature-b",
		RepoID:        repoID,
		State:         WorktreeStateArchived,
		CreatedAt:     "2026-01-31T11:00:00Z",
	}
	data2, _ := json.MarshalIndent(meta2, "", "  ")
	if err := os.WriteFile(filepath.Join(wt2Dir, "meta.json"), data2, 0o644); err != nil {
		t.Fatalf("failed to write meta: %v", err)
	}

	// Create a broken worktree (no meta.json)
	wt3Dir := filepath.Join(wtDir, "20260131120000-e5f6")
	if err := os.Mkdir(wt3Dir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Scan
	records, err := ScanIntegrationWorktreesForRepo(tmpDir, repoID)
	if err != nil {
		t.Fatalf("ScanIntegrationWorktreesForRepo() error = %v", err)
	}

	if len(records) != 3 {
		t.Errorf("got %d records, want 3", len(records))
	}

	// Records should be sorted by created_at (broken last)
	if records[0].Name != "feature-a" {
		t.Errorf("first record name = %v, want feature-a", records[0].Name)
	}
	if records[1].Name != "feature-b" {
		t.Errorf("second record name = %v, want feature-b", records[1].Name)
	}
	if !records[2].Broken {
		t.Error("third record should be broken")
	}
}
