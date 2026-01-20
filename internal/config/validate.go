package config

import (
	"unicode"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ValidationError represents a single validation error with field context.
type ValidationError struct {
	Field string
	Msg   string
}

func (v *ValidationError) Error() string {
	if v.Field != "" {
		return v.Field + ": " + v.Msg
	}
	return v.Msg
}

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
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// FirstValidationError extracts a stable, human-readable error message from an error.
// If the error is an AgencyError from this package, returns the message portion.
// Otherwise returns the error's Error() string.
func FirstValidationError(err error) string {
	if err == nil {
		return ""
	}

	var ae *errors.AgencyError
	if ok := isAgencyError(err, &ae); ok {
		return ae.Msg
	}

	return err.Error()
}

// isAgencyError checks if err is an AgencyError and populates target if so.
func isAgencyError(err error, target **errors.AgencyError) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*errors.AgencyError); ok {
		*target = ae
		return true
	}
	// Check wrapped errors
	if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
		return isAgencyError(unwrapper.Unwrap(), target)
	}
	return false
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

// ValidateForS1 validates the configuration for slice 1 requirements only.
// Unlike ValidateAgencyConfig, this only requires scripts.setup (not verify/archive).
// Returns E_INVALID_AGENCY_JSON for schema/required-field errors.
func ValidateForS1(cfg AgencyConfig) (AgencyConfig, error) {
	// Validate version
	if cfg.Version != 1 {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version must be 1")
	}

	// Validate scripts.setup only (S1 requirement)
	if cfg.Scripts.Setup.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.setup.path")
	}

	return cfg, nil
}

// LoadAndValidateForS1 is a convenience function that loads and validates agency.json
// for slice 1 requirements only. This validates only scripts.setup (not verify/archive).
// This is the primary entry point for S1 commands (e.g., agency run).
func LoadAndValidateForS1(filesystem fs.FS, repoRoot string) (AgencyConfig, error) {
	cfg, err := LoadAgencyConfig(filesystem, repoRoot)
	if err != nil {
		return AgencyConfig{}, err
	}
	return ValidateForS1(cfg)
}
