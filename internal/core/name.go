// Package core provides foundational utilities for agency.
package core

import (
	"regexp"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// Name validation constants.
const (
	NameMinLen = 2
	NameMaxLen = 40
)

// namePattern validates run names:
// - Must start with a lowercase letter
// - May contain lowercase letters, digits, and hyphens
// - No consecutive hyphens (enforced by pattern structure)
// - No trailing hyphen (enforced by pattern structure)
// Pattern: starts with [a-z], then zero or more groups of (-[a-z0-9]+)
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidateName checks if a name meets all validation requirements.
// Returns nil if valid, or E_INVALID_NAME error with details.
//
// Validation rules:
//   - Length: 2-40 characters
//   - Must start with a lowercase letter
//   - May contain only lowercase letters, digits, and hyphens
//   - No consecutive hyphens
//   - No trailing hyphen
func ValidateName(name string) error {
	if len(name) < NameMinLen {
		return errors.NewWithDetails(
			errors.EInvalidName,
			"name must be at least 2 characters",
			map[string]string{"name": name, "min_length": "2"},
		)
	}
	if len(name) > NameMaxLen {
		return errors.NewWithDetails(
			errors.EInvalidName,
			"name must be at most 40 characters",
			map[string]string{"name": name, "max_length": "40"},
		)
	}
	if !namePattern.MatchString(name) {
		return errors.NewWithDetails(
			errors.EInvalidName,
			"name must contain only lowercase letters, digits, and hyphens; must start with a letter; no consecutive or trailing hyphens",
			map[string]string{"name": name},
		)
	}
	return nil
}
