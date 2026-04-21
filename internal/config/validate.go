package config

import (
	"strings"
	"unicode"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// ValidateAgencyConfig validates the repo configuration (agency.json).
// Returns E_INVALID_AGENCY_JSON for schema/required-field errors.
func ValidateAgencyConfig(cfg AgencyConfig) (AgencyConfig, error) {
	// Validate version
	if cfg.Version == 1 {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version 1 is not supported; agency.json must use version 3")
	}
	if cfg.Version == 2 {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version 2 is not supported; agency.json must use version 3")
	}
	if cfg.Version != 3 {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version must be 3")
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
	for name, runnerDefaults := range cfg.RunnerDefaults {
		canonicalRunner, err := runners.Canonicalize(name)
		if err != nil {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+" is not supported; typed runner defaults are supported for runners claude-code, codex, cursor")
		}
		if canonicalRunner != runners.RunnerClaudeCode && canonicalRunner != runners.RunnerCodex && canonicalRunner != runners.RunnerCursor {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+" is not supported; typed runner defaults are supported for runners claude-code, codex, cursor")
		}

		model := strings.TrimSpace(runnerDefaults.Model)
		effort := strings.TrimSpace(runnerDefaults.Effort)
		permissionMode := strings.TrimSpace(runnerDefaults.PermissionMode)
		if permissionMode != "" || runnerDefaults.PermissionMode != "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+".permission_mode is not supported in agency.json")
		}
		if model == "" && effort == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+" requires at least one of model or effort")
		}
		if runnerDefaults.Model != "" && model == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+".model must be a non-empty string")
		}
		if runnerDefaults.Effort != "" && effort == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+".effort must be a non-empty string")
		}
		if canonicalRunner == runners.RunnerCursor && effort != "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults.cursor.effort is not supported")
		}

		cfg.RunnerDefaults[canonicalRunner] = RunnerDefaults{
			Model:  model,
			Effort: effort,
		}
		if canonicalRunner != name {
			delete(cfg.RunnerDefaults, name)
		}
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
