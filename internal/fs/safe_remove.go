// Package fs provides filesystem utilities for agency.
// This file implements safe RemoveAll with allowed-prefix guards.
package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotUnderPrefix is returned when a target path is not under the allowed prefix.
type ErrNotUnderPrefix struct {
	Target string
	Prefix string
}

func (e *ErrNotUnderPrefix) Error() string {
	return fmt.Sprintf("target %q is not under allowed prefix %q", e.Target, e.Prefix)
}

// SafeRemoveAll removes a directory only if it is under the allowed prefix.
// This is a safety guard to prevent accidental deletion of arbitrary paths.
//
// Safety checks:
//   - Both target and prefix are cleaned via filepath.Clean
//   - Both are resolved via filepath.EvalSymlinks to prevent symlink trickery
//   - Target must be a true subpath of prefix (not equal, not outside)
//
// Returns ErrNotUnderPrefix if the target is not under the allowed prefix.
// Returns nil if removal succeeds.
// Returns other errors for filesystem failures.
func SafeRemoveAll(target, allowedPrefix string) error {
	// Clean both paths
	cleanTarget := filepath.Clean(target)
	cleanPrefix := filepath.Clean(allowedPrefix)

	// Resolve symlinks for both paths
	resolvedTarget, err := filepath.EvalSymlinks(cleanTarget)
	if err != nil {
		// If target doesn't exist, that's ok - nothing to remove
		if os.IsNotExist(err) {
			return nil
		}
		// For other errors (e.g., permission denied), fail closed
		return &ErrNotUnderPrefix{Target: target, Prefix: allowedPrefix}
	}

	resolvedPrefix, err := filepath.EvalSymlinks(cleanPrefix)
	if err != nil {
		// If prefix doesn't exist or can't be resolved, fail closed
		return &ErrNotUnderPrefix{Target: target, Prefix: allowedPrefix}
	}

	// Check if target is under prefix (true subpath, not equal)
	if !IsSubpath(resolvedTarget, resolvedPrefix) {
		return &ErrNotUnderPrefix{Target: target, Prefix: allowedPrefix}
	}

	// Safe to remove
	return os.RemoveAll(cleanTarget)
}

// IsSubpath returns true if target is a proper subpath of prefix.
// Both paths should already be cleaned and resolved.
// Returns false if target equals prefix or is outside prefix.
func IsSubpath(target, prefix string) bool {
	// Ensure prefix ends with separator for proper matching
	prefixWithSep := prefix
	if !strings.HasSuffix(prefixWithSep, string(filepath.Separator)) {
		prefixWithSep = prefix + string(filepath.Separator)
	}

	// Target must start with prefix+separator AND be longer than prefix
	// This ensures target is a proper subpath, not equal to prefix
	return strings.HasPrefix(target, prefixWithSep) && len(target) > len(prefix)
}
