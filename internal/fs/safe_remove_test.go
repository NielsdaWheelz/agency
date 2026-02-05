package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeRemoveAll_ValidSubpath(t *testing.T) {
	t.Parallel()
	// Create temp directory structure
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(prefix, "subdir", "target")

	// Create the directories
	err := os.MkdirAll(target, 0755)
	require.NoError(t, err, "failed to create target dir")

	// Create a file in the target
	testFile := filepath.Join(target, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err, "failed to create test file")

	// Safe remove should succeed
	err = SafeRemoveAll(target, prefix)
	require.NoError(t, err, "SafeRemoveAll failed")

	// Verify target is gone
	_, err = os.Stat(target)
	assert.True(t, os.IsNotExist(err), "target directory still exists after SafeRemoveAll")
}

func TestSafeRemoveAll_OutsidePrefix(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create two separate directories
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(tmpDir, "outside", "target")

	err := os.MkdirAll(prefix, 0755)
	require.NoError(t, err, "failed to create prefix")
	err = os.MkdirAll(target, 0755)
	require.NoError(t, err, "failed to create target")

	// Safe remove should fail
	err = SafeRemoveAll(target, prefix)
	require.Error(t, err, "SafeRemoveAll should have failed for target outside prefix")

	// Check it's the right error type
	_, ok := err.(*ErrNotUnderPrefix)
	assert.True(t, ok, "expected ErrNotUnderPrefix, got %T: %v", err, err)

	// Verify target still exists
	_, err = os.Stat(target)
	assert.False(t, os.IsNotExist(err), "target was deleted even though it's outside prefix")
}

func TestSafeRemoveAll_TargetEqualsPrefix(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "samedir")

	err := os.MkdirAll(target, 0755)
	require.NoError(t, err, "failed to create target")

	// Safe remove should fail when target equals prefix
	err = SafeRemoveAll(target, target)
	require.Error(t, err, "SafeRemoveAll should have failed when target equals prefix")

	// Verify target still exists
	_, err = os.Stat(target)
	assert.False(t, os.IsNotExist(err), "target was deleted when it equals prefix")
}

func TestSafeRemoveAll_TargetDoesNotExist(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(prefix, "nonexistent")

	err := os.MkdirAll(prefix, 0755)
	require.NoError(t, err, "failed to create prefix")

	// Safe remove should succeed (no-op) for non-existent target
	err = SafeRemoveAll(target, prefix)
	require.NoError(t, err, "SafeRemoveAll should succeed for non-existent target")
}

func TestSafeRemoveAll_ParentTraversal(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(prefix, "..", "outside")

	err := os.MkdirAll(prefix, 0755)
	require.NoError(t, err, "failed to create prefix")
	err = os.MkdirAll(target, 0755)
	require.NoError(t, err, "failed to create target")

	// Safe remove should fail for parent traversal
	err = SafeRemoveAll(target, prefix)
	require.Error(t, err, "SafeRemoveAll should have failed for parent traversal")

	// Verify target still exists
	_, err = os.Stat(target)
	assert.False(t, os.IsNotExist(err), "target was deleted despite parent traversal attack")
}

func TestSafeRemoveAll_PrefixDoesNotExist(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "nonexistent_prefix")
	target := filepath.Join(tmpDir, "some_target")

	err := os.MkdirAll(target, 0755)
	require.NoError(t, err, "failed to create target")

	// Safe remove should fail when prefix doesn't exist
	err = SafeRemoveAll(target, prefix)
	require.Error(t, err, "SafeRemoveAll should have failed when prefix doesn't exist")

	// Verify target still exists (fail closed)
	_, err = os.Stat(target)
	assert.False(t, os.IsNotExist(err), "target was deleted when prefix doesn't exist")
}

func TestIsSubpath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
		prefix string
		want   bool
	}{
		{
			name:   "valid subpath",
			target: "/a/b/c/d",
			prefix: "/a/b",
			want:   true,
		},
		{
			name:   "equal paths",
			target: "/a/b",
			prefix: "/a/b",
			want:   false,
		},
		{
			name:   "target outside prefix",
			target: "/a/c",
			prefix: "/a/b",
			want:   false,
		},
		{
			name:   "partial name match",
			target: "/a/bcd",
			prefix: "/a/b",
			want:   false,
		},
		{
			name:   "direct child",
			target: "/a/b/c",
			prefix: "/a/b",
			want:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSubpath(tt.target, tt.prefix)
			assert.Equal(t, tt.want, got)
		})
	}
}
