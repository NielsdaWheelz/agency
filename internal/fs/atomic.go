package fs

import (
	"encoding/json"
	"io/fs"
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
			fs.Remove(tmpPath)
		}
	}()

	// Write data to temp file
	_, err = w.Write(data)
	if err != nil {
		w.Close()
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
// Steps:
// - create temp file in same dir
// - write bytes
// - best-effort file.Sync()
// - close
// - chmod(perm) best-effort before rename
// - rename over target
// perm is applied on create (e.g. 0o644).
// Parent dir must exist; do not mkdir here.
func WriteJSONAtomic(path string, v any, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	pattern := ".agency-atomic-*"

	// Marshal to JSON with pretty formatting
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// Add trailing newline
	data = append(data, '\n')

	// Create temp file in the same directory for atomic rename
	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on any error path
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}

	// Best-effort sync
	_ = tmpFile.Sync()

	// Close before rename
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Best-effort chmod before rename
	_ = os.Chmod(tmpPath, perm)

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	success = true
	return nil
}
