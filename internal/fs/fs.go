// Package fs provides a stub-friendly interface for filesystem operations.
package fs

import (
	"io"
	iofs "io/fs"
	"os"
)

// FS is the interface for filesystem operations.
// Implementations must be safe for stubbing in tests.
type FS interface {
	MkdirAll(path string, perm os.FileMode) error
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Stat(path string) (iofs.FileInfo, error)
	Rename(oldpath, newpath string) error
	Remove(path string) error
	Chmod(path string, perm os.FileMode) error
	// CreateTemp creates a temp file and returns the path and a WriteCloser.
	// The caller is responsible for closing the writer and removing the file.
	CreateTemp(dir, pattern string) (path string, w io.WriteCloser, err error)
}

type realFS struct{}

// NewRealFS creates a production filesystem implementation.
func NewRealFS() FS {
	return &realFS{}
}

func (r *realFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (r *realFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (r *realFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (r *realFS) Stat(path string) (iofs.FileInfo, error) {
	return os.Stat(path)
}

func (r *realFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (r *realFS) Remove(path string) error {
	return os.Remove(path)
}

func (r *realFS) Chmod(path string, perm os.FileMode) error {
	return os.Chmod(path, perm)
}

func (r *realFS) CreateTemp(dir, pattern string) (string, io.WriteCloser, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", nil, err
	}
	return f.Name(), f, nil
}
