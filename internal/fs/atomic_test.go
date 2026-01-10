package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomic_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	fs := NewRealFS()

	// Write initial content
	data := []byte(`{"version": 1}`)
	if err := WriteFileAtomic(fs, path, data, 0644); err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	// Verify content
	got, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", string(got), string(data))
	}

	// Verify no temp files left behind
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomic_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	fs := NewRealFS()

	// Write initial content
	initial := []byte(`{"old": true}`)
	if err := WriteFileAtomic(fs, path, initial, 0644); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// Overwrite with new content
	updated := []byte(`{"new": true, "version": 2}`)
	if err := WriteFileAtomic(fs, path, updated, 0644); err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}

	// Verify content is exactly the new bytes
	got, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(updated) {
		t.Errorf("content = %q, want %q", string(got), string(updated))
	}

	// Verify no temp files left behind
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomic_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	fs := NewRealFS()

	// Write with specific permissions
	if err := WriteFileAtomic(fs, path, []byte("test"), 0600); err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	info, err := fs.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	// Check permissions (mask out type bits)
	got := info.Mode().Perm()
	if got != 0600 {
		t.Errorf("permissions = %o, want %o", got, 0600)
	}
}

func TestWriteFileAtomic_RenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial content that should be preserved on failure
	realFS := NewRealFS()
	initial := []byte(`{"initial": true}`)
	if err := realFS.WriteFile(path, initial, 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Use a stubbed FS that fails on rename
	stubFS := &failingRenameFS{FS: realFS, dir: dir}

	// Attempt write that will fail on rename
	err := WriteFileAtomic(stubFS, path, []byte(`{"new": true}`), 0644)
	if err == nil {
		t.Fatal("expected error on rename failure")
	}

	// Verify original file is unchanged
	got, err := realFS.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(initial) {
		t.Errorf("original content changed: got %q, want %q", string(got), string(initial))
	}

	// Verify temp file was cleaned up
	assertNoTempFiles(t, dir)
}

// failingRenameFS wraps an FS and fails on Rename operations.
type failingRenameFS struct {
	FS
	dir     string
	tmpPath string // track temp file path for verification
}

func (f *failingRenameFS) Rename(oldpath, newpath string) error {
	f.tmpPath = oldpath
	return os.ErrPermission
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agency-tmp-") || strings.HasPrefix(e.Name(), ".agency-atomic-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteJSONAtomic_ReplacesAndIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial file
	initial := map[string]string{"key": "initial"}
	if err := WriteJSONAtomic(path, initial, 0o644); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// Verify initial content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("initial JSON is invalid: %v", err)
	}
	if got["key"] != "initial" {
		t.Errorf("initial content = %v, want key=initial", got)
	}

	// Overwrite with new content
	updated := map[string]string{"key": "updated", "new": "value"}
	if err := WriteJSONAtomic(path, updated, 0o644); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Verify updated content
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	got = make(map[string]string)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("updated JSON is invalid: %v", err)
	}
	if got["key"] != "updated" || got["new"] != "value" {
		t.Errorf("updated content = %v, want key=updated, new=value", got)
	}

	assertNoTempFiles(t, dir)
}

func TestWriteJSONAtomic_PermApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_perm.json")

	data := map[string]int{"num": 42}
	if err := WriteJSONAtomic(path, data, 0o600); err != nil {
		t.Fatalf("WriteJSONAtomic failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("permission = %o, want 0600", perm)
	}
}

func TestWriteJSONAtomic_PrettyFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pretty.json")

	data := map[string]any{
		"name": "test",
		"nested": map[string]int{
			"a": 1,
		},
	}

	if err := WriteJSONAtomic(path, data, 0o644); err != nil {
		t.Fatalf("WriteJSONAtomic failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	s := string(content)
	// Should have trailing newline
	if s[len(s)-1] != '\n' {
		t.Error("JSON should end with newline")
	}
	// Should contain indented content (more chars than compact)
	if len(content) < 20 {
		t.Error("pretty JSON should have more characters than compact")
	}
}

func TestWriteJSONAtomic_StructType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "struct.json")

	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	data := TestStruct{Name: "test", Value: 123}
	if err := WriteJSONAtomic(path, data, 0o644); err != nil {
		t.Fatalf("WriteJSONAtomic failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var got TestStruct
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if got.Name != "test" || got.Value != 123 {
		t.Errorf("got %+v, want Name=test, Value=123", got)
	}
}

func TestWriteJSONAtomic_ParentDirMustExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "test.json")

	err := WriteJSONAtomic(path, "test", 0o644)
	if err == nil {
		t.Error("WriteJSONAtomic should fail when parent dir doesn't exist")
	}
}
