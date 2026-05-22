package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path atomically using a temp file + rename.
// The temp file is created in the same directory as path to ensure atomic rename on POSIX.
// If the operation fails, the original file (if any) is left unchanged.
// The caller must ensure the parent directory exists.
func WriteFileAtomic(fs FS, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	pattern := ".agency-tmp-*"

	// Create temp file in the same directory
	tmpPath, w, err := fs.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}

	// Ensure cleanup on any error path
	success := false
	defer func() {
		if !success {
			_ = fs.Remove(tmpPath) // Best-effort cleanup
		}
	}()

	// Write data to temp file
	_, err = w.Write(data)
	if err != nil {
		_ = w.Close() // Best-effort close; returning write error
		return err
	}

	// Close the file before rename
	if err := w.Close(); err != nil {
		return err
	}

	// Set permissions on temp file before rename
	if err := fs.Chmod(tmpPath, perm); err != nil {
		return err
	}

	// Atomic rename
	if err := fs.Rename(tmpPath, path); err != nil {
		return err
	}

	success = true
	return nil
}

// WriteJSONAtomic writes v as pretty JSON (2-space indent) to path atomically.
// Parent dir must exist; do not mkdir here.
func WriteJSONAtomic(fsys FS, path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFileAtomic(fsys, path, data, perm)
}
