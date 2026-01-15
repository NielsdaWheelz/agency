package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeRemoveAll_ValidSubpath(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(prefix, "subdir", "target")

	// Create the directories
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	// Create a file in the target
	testFile := filepath.Join(target, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Safe remove should succeed
	err := SafeRemoveAll(target, prefix)
	if err != nil {
		t.Errorf("SafeRemoveAll failed: %v", err)
	}

	// Verify target is gone
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("target directory still exists after SafeRemoveAll")
	}
}

func TestSafeRemoveAll_OutsidePrefix(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two separate directories
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(tmpDir, "outside", "target")

	if err := os.MkdirAll(prefix, 0755); err != nil {
		t.Fatalf("failed to create prefix: %v", err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	// Safe remove should fail
	err := SafeRemoveAll(target, prefix)
	if err == nil {
		t.Error("SafeRemoveAll should have failed for target outside prefix")
	}

	// Check it's the right error type
	if _, ok := err.(*ErrNotUnderPrefix); !ok {
		t.Errorf("expected ErrNotUnderPrefix, got %T: %v", err, err)
	}

	// Verify target still exists
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Error("target was deleted even though it's outside prefix")
	}
}

func TestSafeRemoveAll_TargetEqualsPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "samedir")

	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	// Safe remove should fail when target equals prefix
	err := SafeRemoveAll(target, target)
	if err == nil {
		t.Error("SafeRemoveAll should have failed when target equals prefix")
	}

	// Verify target still exists
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Error("target was deleted when it equals prefix")
	}
}

func TestSafeRemoveAll_TargetDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(prefix, "nonexistent")

	if err := os.MkdirAll(prefix, 0755); err != nil {
		t.Fatalf("failed to create prefix: %v", err)
	}

	// Safe remove should succeed (no-op) for non-existent target
	err := SafeRemoveAll(target, prefix)
	if err != nil {
		t.Errorf("SafeRemoveAll should succeed for non-existent target: %v", err)
	}
}

func TestSafeRemoveAll_ParentTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "prefix")
	target := filepath.Join(prefix, "..", "outside")

	if err := os.MkdirAll(prefix, 0755); err != nil {
		t.Fatalf("failed to create prefix: %v", err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	// Safe remove should fail for parent traversal
	err := SafeRemoveAll(target, prefix)
	if err == nil {
		t.Error("SafeRemoveAll should have failed for parent traversal")
	}

	// Verify target still exists
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Error("target was deleted despite parent traversal attack")
	}
}

func TestSafeRemoveAll_PrefixDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "nonexistent_prefix")
	target := filepath.Join(tmpDir, "some_target")

	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	// Safe remove should fail when prefix doesn't exist
	err := SafeRemoveAll(target, prefix)
	if err == nil {
		t.Error("SafeRemoveAll should have failed when prefix doesn't exist")
	}

	// Verify target still exists (fail closed)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Error("target was deleted when prefix doesn't exist")
	}
}

func TestIsSubpath(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got := IsSubpath(tt.target, tt.prefix)
			if got != tt.want {
				t.Errorf("IsSubpath(%q, %q) = %v, want %v", tt.target, tt.prefix, got, tt.want)
			}
		})
	}
}
