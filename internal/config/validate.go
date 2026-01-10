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

// ValidateAgencyConfig validates the configuration and resolves the runner command.
// Returns the config with ResolvedRunnerCmd populated on success.
// Returns E_INVALID_AGENCY_JSON for schema/required-field errors.
// Returns E_RUNNER_NOT_CONFIGURED if runner cannot be resolved.
func ValidateAgencyConfig(cfg AgencyConfig) (AgencyConfig, error) {
	// Validate version
	if cfg.Version != 1 {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version must be 1")
	}

	// Validate required fields in defaults
	if cfg.Defaults.ParentBranch == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field defaults.parent_branch")
	}
	if cfg.Defaults.Runner == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field defaults.runner")
	}

	// Validate required fields in scripts
	if cfg.Scripts.Setup == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.setup")
	}
	if cfg.Scripts.Verify == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.verify")
	}
	if cfg.Scripts.Archive == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.archive")
	}

	// Validate runners entries (if present)
	for name, cmd := range cfg.Runners {
		if cmd == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runners."+name+" must be a non-empty string")
		}
		if containsWhitespace(cmd) {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runners."+name+" must be a single executable (no args); use a wrapper script")
		}
	}

	// Resolve runner command
	resolved, err := resolveRunner(cfg)
	if err != nil {
		return cfg, err
	}
	cfg.ResolvedRunnerCmd = resolved

	return cfg, nil
}

// resolveRunner determines the runner command based on config.
// Returns E_RUNNER_NOT_CONFIGURED if resolution fails.
func resolveRunner(cfg AgencyConfig) (string, error) {
	name := cfg.Defaults.Runner

	// If runners map has an entry for this name, use it
	if cfg.Runners != nil {
		if cmd, ok := cfg.Runners[name]; ok {
			// Already validated non-empty and no whitespace in ValidateAgencyConfig
			return cmd, nil
		}
	}

	// PATH fallback for standard runners
	if name == "claude" || name == "codex" {
		return name, nil
	}

	// Runner not configured
	return "", errors.New(errors.ERunnerNotConfigured,
		"runner \""+name+"\" not configured; set runners."+name+" or choose claude/codex")
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
// This is the primary entry point for callers.
func LoadAndValidate(filesystem fs.FS, repoRoot string) (AgencyConfig, error) {
	cfg, err := LoadAgencyConfig(filesystem, repoRoot)
	if err != nil {
		return AgencyConfig{}, err
	}
	return ValidateAgencyConfig(cfg)
}
