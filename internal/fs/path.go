package fs

import (
	"os"
	"path/filepath"
	"strings"
)

// PathContains reports whether child is parent itself or lies beneath it.
// Both paths should already be cleaned and symlink-resolved by the caller.
func PathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// CanonicalizePath returns path cleaned, made absolute, and symlink-resolved.
// Each step is best-effort: a step that fails leaves the path at its last good
// value. Use this to compare paths that already exist; for a path that may not
// exist yet, use ResolveSymlinks.
func CanonicalizePath(path string) string {
	clean := filepath.Clean(path)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	return clean
}

// ResolveSymlinks resolves path through symlinks, tolerating trailing
// components that do not exist yet: it resolves the deepest existing ancestor
// and rejoins the missing tail. It returns an error only for a real filesystem
// failure, never for a missing path.
func ResolveSymlinks(path string) (string, error) {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	current := clean
	var missing []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return clean, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent

		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
}
