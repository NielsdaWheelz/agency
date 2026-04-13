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

// RealFS is the production implementation of FS using the os package.
type RealFS struct{}

// NewRealFS creates a new RealFS.
func NewRealFS() *RealFS {
	return &RealFS{}
}

func (r *RealFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (r *RealFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (r *RealFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (r *RealFS) Stat(path string) (iofs.FileInfo, error) {
	return os.Stat(path)
}

func (r *RealFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (r *RealFS) Remove(path string) error {
	return os.Remove(path)
}

func (r *RealFS) Chmod(path string, perm os.FileMode) error {
	return os.Chmod(path, perm)
}

func (r *RealFS) CreateTemp(dir, pattern string) (string, io.WriteCloser, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", nil, err
	}
	return f.Name(), f, nil
}
