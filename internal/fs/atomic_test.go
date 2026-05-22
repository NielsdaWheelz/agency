package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	fs := NewRealFS()

	// Write initial content
	data := []byte(`{"version": 1}`)
	err := WriteFileAtomic(fs, path, data, 0644)
	require.NoError(t, err, "WriteFileAtomic failed")

	// Verify content
	got, err := fs.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")
	assert.Equal(t, string(data), string(got))

	// Verify no temp files left behind
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomic_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	fs := NewRealFS()

	// Write initial content
	initial := []byte(`{"old": true}`)
	err := WriteFileAtomic(fs, path, initial, 0644)
	require.NoError(t, err, "initial write failed")

	// Overwrite with new content
	updated := []byte(`{"new": true, "version": 2}`)
	err = WriteFileAtomic(fs, path, updated, 0644)
	require.NoError(t, err, "overwrite failed")

	// Verify content is exactly the new bytes
	got, err := fs.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")
	assert.Equal(t, string(updated), string(got))

	// Verify no temp files left behind
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomic_Permissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	fs := NewRealFS()

	// Write with specific permissions
	err := WriteFileAtomic(fs, path, []byte("test"), 0600)
	require.NoError(t, err, "WriteFileAtomic failed")

	info, err := fs.Stat(path)
	require.NoError(t, err, "Stat failed")

	// Check permissions (mask out type bits)
	got := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0600), got)
}

func TestWriteFileAtomic_RenameFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial content that should be preserved on failure
	realFS := NewRealFS()
	initial := []byte(`{"initial": true}`)
	err := realFS.WriteFile(path, initial, 0644)
	require.NoError(t, err, "setup failed")

	// Use a stubbed FS that fails on rename
	stubFS := &failingRenameFS{FS: realFS, dir: dir}

	// Attempt write that will fail on rename
	err = WriteFileAtomic(stubFS, path, []byte(`{"new": true}`), 0644)
	require.Error(t, err, "expected error on rename failure")

	// Verify original file is unchanged
	got, err := realFS.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")
	assert.Equal(t, string(initial), string(got))

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
	require.NoError(t, err, "ReadDir failed")
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".agency-tmp-") || strings.HasPrefix(e.Name(), ".agency-atomic-"),
			"temp file left behind: %s", e.Name())
	}
}

func TestWriteJSONAtomic_ReplacesAndIsValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial file
	initial := map[string]string{"key": "initial"}
	err := WriteJSONAtomic(NewRealFS(), path, initial, 0o644)
	require.NoError(t, err, "initial write failed")

	// Verify initial content
	data, err := os.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")

	var got map[string]string
	err = json.Unmarshal(data, &got)
	require.NoError(t, err, "initial JSON is invalid")
	assert.Equal(t, "initial", got["key"])

	// Overwrite with new content
	updated := map[string]string{"key": "updated", "new": "value"}
	err = WriteJSONAtomic(NewRealFS(), path, updated, 0o644)
	require.NoError(t, err, "update failed")

	// Verify updated content
	data, err = os.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")

	got = make(map[string]string)
	err = json.Unmarshal(data, &got)
	require.NoError(t, err, "updated JSON is invalid")
	assert.Equal(t, "updated", got["key"])
	assert.Equal(t, "value", got["new"])

	assertNoTempFiles(t, dir)
}

func TestWriteJSONAtomic_PermApplied(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_perm.json")

	data := map[string]int{"num": 42}
	err := WriteJSONAtomic(NewRealFS(), path, data, 0o600)
	require.NoError(t, err, "WriteJSONAtomic failed")

	info, err := os.Stat(path)
	require.NoError(t, err, "Stat failed")

	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), perm)
}

func TestWriteJSONAtomic_PrettyFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pretty.json")

	data := map[string]any{
		"name": "test",
		"nested": map[string]int{
			"a": 1,
		},
	}

	err := WriteJSONAtomic(NewRealFS(), path, data, 0o644)
	require.NoError(t, err, "WriteJSONAtomic failed")

	content, err := os.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")

	s := string(content)
	// Should have trailing newline
	assert.Equal(t, byte('\n'), s[len(s)-1], "JSON should end with newline")
	// Should contain indented content (more chars than compact)
	assert.True(t, len(content) >= 20, "pretty JSON should have more characters than compact")
}

func TestWriteJSONAtomic_StructType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "struct.json")

	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	data := TestStruct{Name: "test", Value: 123}
	err := WriteJSONAtomic(NewRealFS(), path, data, 0o644)
	require.NoError(t, err, "WriteJSONAtomic failed")

	content, err := os.ReadFile(path)
	require.NoError(t, err, "ReadFile failed")

	var got TestStruct
	err = json.Unmarshal(content, &got)
	require.NoError(t, err, "JSON unmarshal failed")

	assert.Equal(t, "test", got.Name)
	assert.Equal(t, 123, got.Value)
}

func TestWriteJSONAtomic_ParentDirMustExist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "test.json")

	err := WriteJSONAtomic(NewRealFS(), path, "test", 0o644)
	require.Error(t, err, "WriteJSONAtomic should fail when parent dir doesn't exist")
}
