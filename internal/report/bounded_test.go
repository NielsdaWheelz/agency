package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFileBounded_MissingFile(t *testing.T) {
	t.Parallel()
	realFS := fs.NewRealFS()
	path := filepath.Join(t.TempDir(), "missing.md")

	data, exists, oversized, err := ReadFileBounded(realFS, path, 128)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.False(t, oversized)
	assert.Nil(t, data)
}

func TestReadFileBounded_OversizedFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "report.md")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), 256), 0o644))

	realFS := fs.NewRealFS()
	data, exists, oversized, err := ReadFileBounded(realFS, path, 128)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.True(t, oversized)
	assert.Nil(t, data)
}

func TestReadFileBounded_WithinLimit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "report.md")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	realFS := fs.NewRealFS()
	data, exists, oversized, err := ReadFileBounded(realFS, path, 128)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.False(t, oversized)
	assert.Equal(t, "hello", string(data))
}

func TestReadFileBounded_DirectoryPathReturnsError(t *testing.T) {
	t.Parallel()
	dirPath := t.TempDir()
	realFS := fs.NewRealFS()

	data, exists, oversized, err := ReadFileBounded(realFS, dirPath, 128)
	require.Error(t, err)
	assert.True(t, exists)
	assert.False(t, oversized)
	assert.Nil(t, data)
}

func TestReadFileBounded_InvalidMaxBytesReturnsError(t *testing.T) {
	t.Parallel()
	realFS := fs.NewRealFS()
	path := filepath.Join(t.TempDir(), "report.md")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	_, _, _, err := ReadFileBounded(realFS, path, 0)
	require.Error(t, err)
	assert.ErrorContains(t, err, "maxBytes")
}
