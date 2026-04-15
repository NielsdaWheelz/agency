package config

import (
	"strings"
	"unicode"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ValidateAgencyConfig validates the repo configuration (agency.json).
// Returns E_INVALID_AGENCY_JSON for schema/required-field errors.
func ValidateAgencyConfig(cfg AgencyConfig) (AgencyConfig, error) {
	// Validate version
	if cfg.Version != 1 {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version must be 1")
	}

	// Validate required fields in scripts
	if cfg.Scripts.Setup.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.setup.path")
	}
	if cfg.Scripts.Verify.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.verify.path")
	}
	if cfg.Scripts.Archive.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.archive.path")
	}

	return cfg, nil
}

// containsWhitespace returns true if s contains any whitespace character.
func containsWhitespace(s string) bool {
	return strings.ContainsFunc(s, unicode.IsSpace)
}

// LoadAndValidate is a convenience function that loads and validates agency.json.
// This is the primary entry point for callers that need full validation (e.g., doctor).
func LoadAndValidate(filesystem fs.FS, repoRoot string) (AgencyConfig, error) {
	cfg, err := LoadAgencyConfig(filesystem, repoRoot)
	if err != nil {
		return AgencyConfig{}, err
	}
	return ValidateAgencyConfig(cfg)
}
