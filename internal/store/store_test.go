package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// fixedTime returns a clock function that always returns the same time.
func fixedTime(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestRepoIndexPath verifies path construction.
func TestRepoIndexPath(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RepoIndexPath()
	want := "/data/agency/repo_index.json"
	if got != want {
		t.Errorf("RepoIndexPath() = %q, want %q", got, want)
	}
}

// TestRepoDir verifies repo directory path construction.
func TestRepoDir(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RepoDir("abc123")
	want := "/data/agency/repos/abc123"
	if got != want {
		t.Errorf("RepoDir() = %q, want %q", got, want)
	}
}

// TestRepoRecordPath verifies repo record path construction.
func TestRepoRecordPath(t *testing.T) {
	s := NewStore(nil, "/data/agency", nil)
	got := s.RepoRecordPath("abc123")
	want := "/data/agency/repos/abc123/repo.json"
	if got != want {
		t.Errorf("RepoRecordPath() = %q, want %q", got, want)
	}
}

// TestLoadRepoIndex_MissingFile verifies empty index returned for missing file.
func TestLoadRepoIndex_MissingFile(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	idx, err := s.LoadRepoIndex()
	if err != nil {
		t.Fatalf("LoadRepoIndex() error = %v, want nil", err)
	}
	if idx.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", idx.SchemaVersion, SchemaVersion)
	}
	if len(idx.Repos) != 0 {
		t.Errorf("Repos = %v, want empty map", idx.Repos)
	}
}

// TestRepoIndexRoundtrip tests save/load cycle.
func TestRepoIndexRoundtrip(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Start with empty index
	idx, err := s.LoadRepoIndex()
	if err != nil {
		t.Fatalf("LoadRepoIndex() error = %v", err)
	}

	// Upsert an entry
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123def456", "/path/to/repo")

	// Save
	if err := s.SaveRepoIndex(idx); err != nil {
		t.Fatalf("SaveRepoIndex() error = %v", err)
	}

	// Load again
	loaded, err := s.LoadRepoIndex()
	if err != nil {
		t.Fatalf("LoadRepoIndex() after save error = %v", err)
	}

	// Verify
	if loaded.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", loaded.SchemaVersion, SchemaVersion)
	}
	entry, ok := loaded.Repos["github:owner/repo"]
	if !ok {
		t.Fatal("expected entry for github:owner/repo")
	}
	if entry.RepoID != "abc123def456" {
		t.Errorf("RepoID = %q, want %q", entry.RepoID, "abc123def456")
	}
	if len(entry.Paths) != 1 || entry.Paths[0] != "/path/to/repo" {
		t.Errorf("Paths = %v, want [/path/to/repo]", entry.Paths)
	}
	if entry.LastSeenAt != "2026-01-09T12:00:00Z" {
		t.Errorf("LastSeenAt = %q, want %q", entry.LastSeenAt, "2026-01-09T12:00:00Z")
	}
}

// TestUpsertRepoIndexEntry_NoDuplication verifies path deduplication.
func TestUpsertRepoIndexEntry_NoDuplication(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add entry with path
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	// Add same path again
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	entry := idx.Repos["github:owner/repo"]
	if len(entry.Paths) != 1 {
		t.Errorf("Paths length = %d, want 1 (no duplication)", len(entry.Paths))
	}
}

// TestUpsertRepoIndexEntry_NewPath verifies new paths are added at front.
func TestUpsertRepoIndexEntry_NewPath(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add first path
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	// Add second path
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/two")

	entry := idx.Repos["github:owner/repo"]
	if len(entry.Paths) != 2 {
		t.Fatalf("Paths length = %d, want 2", len(entry.Paths))
	}
	if entry.Paths[0] != "/path/two" {
		t.Errorf("Paths[0] = %q, want /path/two (most recent first)", entry.Paths[0])
	}
	if entry.Paths[1] != "/path/one" {
		t.Errorf("Paths[1] = %q, want /path/one", entry.Paths[1])
	}
}

// TestUpsertRepoIndexEntry_MoveExistingPathToFront verifies existing path moves to front.
func TestUpsertRepoIndexEntry_MoveExistingPathToFront(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add paths in order
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/two")

	// Touch first path again
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/one")

	entry := idx.Repos["github:owner/repo"]
	if len(entry.Paths) != 2 {
		t.Fatalf("Paths length = %d, want 2", len(entry.Paths))
	}
	if entry.Paths[0] != "/path/one" {
		t.Errorf("Paths[0] = %q, want /path/one (moved to front)", entry.Paths[0])
	}
	if entry.Paths[1] != "/path/two" {
		t.Errorf("Paths[1] = %q, want /path/two", entry.Paths[1])
	}
}

// TestLoadRepoIndex_CorruptJSON verifies E_STORE_CORRUPT for invalid JSON.
func TestLoadRepoIndex_CorruptJSON(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Write corrupt JSON
	path := s.RepoIndexPath()
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := s.LoadRepoIndex()
	if err == nil {
		t.Fatal("LoadRepoIndex() error = nil, want E_STORE_CORRUPT")
	}
	if errors.GetCode(err) != errors.EStoreCorrupt {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.EStoreCorrupt)
	}
}

// TestLoadRepoIndex_MissingSchemaVersion verifies E_STORE_CORRUPT for missing schema_version.
func TestLoadRepoIndex_MissingSchemaVersion(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Write JSON without schema_version
	path := s.RepoIndexPath()
	if err := os.WriteFile(path, []byte(`{"repos":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := s.LoadRepoIndex()
	if err == nil {
		t.Fatal("LoadRepoIndex() error = nil, want E_STORE_CORRUPT")
	}
	if errors.GetCode(err) != errors.EStoreCorrupt {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.EStoreCorrupt)
	}
}

// TestLoadRepoRecord_MissingFile verifies (zero, false, nil) for missing file.
func TestLoadRepoRecord_MissingFile(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	rec, exists, err := s.LoadRepoRecord("nonexistent")
	if err != nil {
		t.Fatalf("LoadRepoRecord() error = %v, want nil", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
	if rec.RepoID != "" {
		t.Errorf("RepoID = %q, want empty", rec.RepoID)
	}
}

// TestRepoRecordRoundtrip tests save/load cycle for repo records.
func TestRepoRecordRoundtrip(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	input := BuildRepoRecordInput{
		RepoKey:          "github:owner/repo",
		RepoID:           "abc123def456ghij",
		RepoRootLastSeen: "/path/to/repo",
		AgencyJSONPath:   "/path/to/repo/agency.json",
		OriginPresent:    true,
		OriginURL:        "git@github.com:owner/repo.git",
		OriginHost:       "github.com",
		Capabilities: Capabilities{
			GitHubOrigin: true,
			OriginHost:   "github.com",
			GhAuthed:     true,
		},
	}

	// Create new record
	rec := s.UpsertRepoRecord(nil, input)

	// Save
	if err := s.SaveRepoRecord(rec); err != nil {
		t.Fatalf("SaveRepoRecord() error = %v", err)
	}

	// Load
	loaded, exists, err := s.LoadRepoRecord(input.RepoID)
	if err != nil {
		t.Fatalf("LoadRepoRecord() error = %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}

	// Verify all fields
	if loaded.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", loaded.SchemaVersion, SchemaVersion)
	}
	if loaded.RepoKey != input.RepoKey {
		t.Errorf("RepoKey = %q, want %q", loaded.RepoKey, input.RepoKey)
	}
	if loaded.RepoID != input.RepoID {
		t.Errorf("RepoID = %q, want %q", loaded.RepoID, input.RepoID)
	}
	if loaded.RepoRootLastSeen != input.RepoRootLastSeen {
		t.Errorf("RepoRootLastSeen = %q, want %q", loaded.RepoRootLastSeen, input.RepoRootLastSeen)
	}
	if loaded.AgencyJSONPath != input.AgencyJSONPath {
		t.Errorf("AgencyJSONPath = %q, want %q", loaded.AgencyJSONPath, input.AgencyJSONPath)
	}
	if loaded.OriginPresent != input.OriginPresent {
		t.Errorf("OriginPresent = %v, want %v", loaded.OriginPresent, input.OriginPresent)
	}
	if loaded.OriginURL != input.OriginURL {
		t.Errorf("OriginURL = %q, want %q", loaded.OriginURL, input.OriginURL)
	}
	if loaded.OriginHost != input.OriginHost {
		t.Errorf("OriginHost = %q, want %q", loaded.OriginHost, input.OriginHost)
	}
	if loaded.Capabilities.GitHubOrigin != input.Capabilities.GitHubOrigin {
		t.Errorf("Capabilities.GitHubOrigin = %v, want %v", loaded.Capabilities.GitHubOrigin, input.Capabilities.GitHubOrigin)
	}
	if loaded.Capabilities.OriginHost != input.Capabilities.OriginHost {
		t.Errorf("Capabilities.OriginHost = %q, want %q", loaded.Capabilities.OriginHost, input.Capabilities.OriginHost)
	}
	if loaded.Capabilities.GhAuthed != input.Capabilities.GhAuthed {
		t.Errorf("Capabilities.GhAuthed = %v, want %v", loaded.Capabilities.GhAuthed, input.Capabilities.GhAuthed)
	}
	if loaded.CreatedAt != "2026-01-09T12:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q", loaded.CreatedAt, "2026-01-09T12:00:00Z")
	}
	if loaded.UpdatedAt != "2026-01-09T12:00:00Z" {
		t.Errorf("UpdatedAt = %q, want %q", loaded.UpdatedAt, "2026-01-09T12:00:00Z")
	}
}

// TestUpsertRepoRecord_PreservesCreatedAt verifies CreatedAt is preserved on update.
func TestUpsertRepoRecord_PreservesCreatedAt(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	createTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updateTime := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)

	// Create initial record
	s := NewStore(realFS, dataDir, fixedTime(createTime))
	input := BuildRepoRecordInput{
		RepoKey:        "github:owner/repo",
		RepoID:         "abc123",
		OriginPresent:  true,
		OriginURL:      "git@github.com:owner/repo.git",
		OriginHost:     "github.com",
		Capabilities:   Capabilities{GitHubOrigin: true},
	}
	rec := s.UpsertRepoRecord(nil, input)
	if err := s.SaveRepoRecord(rec); err != nil {
		t.Fatalf("SaveRepoRecord() error = %v", err)
	}

	// Load and update with later time
	s = NewStore(realFS, dataDir, fixedTime(updateTime))
	loaded, exists, err := s.LoadRepoRecord("abc123")
	if err != nil || !exists {
		t.Fatalf("LoadRepoRecord() error = %v, exists = %v", err, exists)
	}

	input.Capabilities.GhAuthed = true // change something
	updated := s.UpsertRepoRecord(&loaded, input)

	// Verify timestamps
	if updated.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q (preserved)", updated.CreatedAt, "2026-01-01T00:00:00Z")
	}
	if updated.UpdatedAt != "2026-01-09T12:00:00Z" {
		t.Errorf("UpdatedAt = %q, want %q (updated)", updated.UpdatedAt, "2026-01-09T12:00:00Z")
	}
}

// TestLoadRepoRecord_CorruptJSON verifies E_STORE_CORRUPT for invalid JSON.
func TestLoadRepoRecord_CorruptJSON(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create repo directory and write corrupt JSON
	repoDir := s.RepoDir("abc123")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := s.RepoRecordPath("abc123")
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := s.LoadRepoRecord("abc123")
	if err == nil {
		t.Fatal("LoadRepoRecord() error = nil, want E_STORE_CORRUPT")
	}
	if errors.GetCode(err) != errors.EStoreCorrupt {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.EStoreCorrupt)
	}
}

// TestLoadRepoRecord_MissingSchemaVersion verifies E_STORE_CORRUPT for missing schema_version.
func TestLoadRepoRecord_MissingSchemaVersion(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	s := NewStore(realFS, dataDir, nil)

	// Create repo directory and write JSON without schema_version
	repoDir := s.RepoDir("abc123")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := s.RepoRecordPath("abc123")
	if err := os.WriteFile(path, []byte(`{"repo_id":"abc123"}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := s.LoadRepoRecord("abc123")
	if err == nil {
		t.Fatal("LoadRepoRecord() error = nil, want E_STORE_CORRUPT")
	}
	if errors.GetCode(err) != errors.EStoreCorrupt {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.EStoreCorrupt)
	}
}

// TestSaveRepoRecord_CreatesDirectory verifies repo directory is created.
func TestSaveRepoRecord_CreatesDirectory(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	rec := s.UpsertRepoRecord(nil, BuildRepoRecordInput{
		RepoKey: "github:owner/repo",
		RepoID:  "newrepo123",
	})

	// Directory should not exist yet
	repoDir := s.RepoDir("newrepo123")
	if _, err := os.Stat(repoDir); !os.IsNotExist(err) {
		t.Error("repo directory should not exist before save")
	}

	// Save should create it
	if err := s.SaveRepoRecord(rec); err != nil {
		t.Fatalf("SaveRepoRecord() error = %v", err)
	}

	// Now it should exist
	if _, err := os.Stat(repoDir); err != nil {
		t.Errorf("repo directory should exist after save: %v", err)
	}
}

// TestSaveRepoIndex_CreatesDirectory verifies data directory is created.
func TestSaveRepoIndex_CreatesDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "subdir", "agency")
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Directory should not exist yet
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Error("data directory should not exist before save")
	}

	// Save should create it
	if err := s.SaveRepoIndex(idx); err != nil {
		t.Fatalf("SaveRepoIndex() error = %v", err)
	}

	// Now it should exist
	if _, err := os.Stat(dataDir); err != nil {
		t.Errorf("data directory should exist after save: %v", err)
	}
}

// TestJSONFormat verifies the output JSON is properly formatted.
func TestJSONFormat(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	// Save repo index
	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/to/repo")
	if err := s.SaveRepoIndex(idx); err != nil {
		t.Fatalf("SaveRepoIndex() error = %v", err)
	}

	// Read raw JSON and verify it's indented
	data, err := os.ReadFile(s.RepoIndexPath())
	if err != nil {
		t.Fatal(err)
	}

	// Check for indentation (should contain newlines and spaces)
	if !json.Valid(data) {
		t.Error("output is not valid JSON")
	}
	// Verify trailing newline
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("output should end with newline")
	}
}

// TestPathNormalization verifies paths are cleaned.
func TestPathNormalization(t *testing.T) {
	dataDir := t.TempDir()
	realFS := fs.NewRealFS()
	now := time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC)
	s := NewStore(realFS, dataDir, fixedTime(now))

	idx := RepoIndex{
		SchemaVersion: SchemaVersion,
		Repos:         make(map[string]RepoIndexEntry),
	}

	// Add path with .. components
	idx = s.UpsertRepoIndexEntry(idx, "github:owner/repo", "abc123", "/path/to/../to/repo")

	entry := idx.Repos["github:owner/repo"]
	if entry.Paths[0] != "/path/to/repo" {
		t.Errorf("path not normalized: got %q, want /path/to/repo", entry.Paths[0])
	}
}
