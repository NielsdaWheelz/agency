// Package config handles loading and validation of agency configuration files.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// UserConfig represents the parsed and validated user configuration.
type UserConfig struct {
	Version           int                         `json:"version"`
	Defaults          UserDefaults                `json:"defaults"`
	RunnerDefaults    map[string]RunnerDefaults   `json:"runner_defaults,omitempty"`
	Runners           map[string]string           `json:"runners,omitempty"`
	Editors           map[string]string           `json:"editors,omitempty"`
	ExecutionProfiles map[string]ExecutionProfile `json:"execution_profiles"`
}

// UserDefaults contains default values for user-scoped operations.
type UserDefaults struct {
	Runner           string `json:"runner"`
	Editor           string `json:"editor"`
	BaseBranch       string `json:"base_branch,omitempty"`
	ExecutionProfile string `json:"execution_profile"`
}

// RunnerDefaults contains typed runner defaults for one canonical runner id.
type RunnerDefaults struct {
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

// ExecutionProfile contains user-scoped runner/account environment.
type ExecutionProfile struct {
	Env map[string]string `json:"env"`
}

// UserConfigPath returns the full path to the user config file.
func UserConfigPath(configDir string) string {
	return filepath.Join(configDir, "config.json")
}

// LoadUserConfig loads and validates the user config.
// Missing config returns E_NO_USER_CONFIG. Invalid config returns E_INVALID_USER_CONFIG.
func LoadUserConfig(filesystem fs.FS, configDir string) (UserConfig, error) {
	path := UserConfigPath(configDir)

	data, err := filesystem.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UserConfig{}, errors.NewWithDetails(
				errors.ENoUserConfig,
				"user config not found: "+path,
				map[string]string{
					"path": path,
					"hint": "run `agency config init`",
				},
			)
		}
		return UserConfig{}, errors.Wrap(errors.EInvalidUserConfig, "failed to read user config", err)
	}

	// First, unmarshal into raw map for type checking
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return UserConfig{}, errors.New(errors.EInvalidUserConfig, "invalid json: "+err.Error())
	}

	cfg, err := parseUserConfigStrict(raw)
	if err != nil {
		return UserConfig{}, err
	}

	cfg, err = ValidateUserConfig(cfg)
	if err != nil {
		return UserConfig{}, err
	}

	return cfg, nil
}

func parseUserConfigStrict(raw map[string]json.RawMessage) (UserConfig, error) {
	var cfg UserConfig
	const code = errors.EInvalidUserConfig
	if err := rejectUnknownKeys(raw, "", code, map[string]bool{
		"version": true, "defaults": true, "runner_defaults": true,
		"runners": true, "editors": true, "execution_profiles": true,
	}); err != nil {
		return UserConfig{}, err
	}

	if err := parseStrictInt(raw, "version", "version", code, &cfg.Version); err != nil {
		return UserConfig{}, err
	}

	if rawDefaults, ok := raw["defaults"]; ok {
		defaultsMap, err := parseStrictObject(rawDefaults, "defaults", code)
		if err != nil {
			return UserConfig{}, err
		}
		if err := rejectUnknownKeys(defaultsMap, "defaults", code, map[string]bool{
			"runner": true, "editor": true, "base_branch": true, "execution_profile": true,
		}); err != nil {
			return UserConfig{}, err
		}
		if err := parseStrictString(defaultsMap, "runner", "defaults.runner", code, &cfg.Defaults.Runner); err != nil {
			return UserConfig{}, err
		}
		if err := parseStrictString(defaultsMap, "editor", "defaults.editor", code, &cfg.Defaults.Editor); err != nil {
			return UserConfig{}, err
		}
		if err := parseStrictString(defaultsMap, "base_branch", "defaults.base_branch", code, &cfg.Defaults.BaseBranch); err != nil {
			return UserConfig{}, err
		}
		if err := parseStrictString(defaultsMap, "execution_profile", "defaults.execution_profile", code, &cfg.Defaults.ExecutionProfile); err != nil {
			return UserConfig{}, err
		}
	}

	if rawRunnerDefaults, ok := raw["runner_defaults"]; ok {
		defaultsMap, err := parseStrictObject(rawRunnerDefaults, "runner_defaults", code)
		if err != nil {
			return UserConfig{}, err
		}
		cfg.RunnerDefaults = make(map[string]RunnerDefaults, len(defaultsMap))
		for runnerName, rawRunnerDefaults := range defaultsMap {
			path := "runner_defaults." + runnerName
			runnerMap, err := parseStrictObject(rawRunnerDefaults, path, code)
			if err != nil {
				return UserConfig{}, err
			}
			if err := rejectUnknownKeys(runnerMap, path, code, map[string]bool{
				"model": true, "effort": true, "permission_mode": true,
			}); err != nil {
				return UserConfig{}, err
			}
			var rd RunnerDefaults
			if err := parseStrictString(runnerMap, "model", path+".model", code, &rd.Model); err != nil {
				return UserConfig{}, err
			}
			if err := parseStrictString(runnerMap, "effort", path+".effort", code, &rd.Effort); err != nil {
				return UserConfig{}, err
			}
			if err := parseStrictString(runnerMap, "permission_mode", path+".permission_mode", code, &rd.PermissionMode); err != nil {
				return UserConfig{}, err
			}
			cfg.RunnerDefaults[runnerName] = rd
		}
	}

	if err := parseStrictStringMap(raw, "runners", "runners", code, &cfg.Runners); err != nil {
		return UserConfig{}, err
	}
	if err := parseStrictStringMap(raw, "editors", "editors", code, &cfg.Editors); err != nil {
		return UserConfig{}, err
	}

	if rawProfiles, ok := raw["execution_profiles"]; ok {
		profilesMap, err := parseStrictObject(rawProfiles, "execution_profiles", code)
		if err != nil {
			return UserConfig{}, err
		}
		cfg.ExecutionProfiles = make(map[string]ExecutionProfile, len(profilesMap))
		for name, rawProfile := range profilesMap {
			path := "execution_profiles." + name
			profileMap, err := parseStrictObject(rawProfile, path, code)
			if err != nil {
				return UserConfig{}, err
			}
			if err := rejectUnknownKeys(profileMap, path, code, map[string]bool{"env": true}); err != nil {
				return UserConfig{}, err
			}
			if _, ok := profileMap["env"]; !ok {
				return UserConfig{}, errors.New(code, path+".env is required")
			}
			var env map[string]string
			if err := parseStrictStringMap(profileMap, "env", path+".env", code, &env); err != nil {
				return UserConfig{}, err
			}
			cfg.ExecutionProfiles[name] = ExecutionProfile{Env: env}
		}
	}

	return cfg, nil
}

// ValidateUserConfig validates the user config and returns E_INVALID_USER_CONFIG on failure.
func ValidateUserConfig(cfg UserConfig) (UserConfig, error) {
	if cfg.Version != AgencyConfigVersion {
		return cfg, errors.NewWithDetails(
			errors.EInvalidUserConfig,
			"version must be 4",
			map[string]string{"hint": "run `agency config init --force` to scaffold a fresh version 4 config"},
		)
	}
	if cfg.Defaults.Runner == "" {
		return cfg, errors.New(errors.EInvalidUserConfig, "missing required field defaults.runner")
	}
	canonicalDefaultRunner, err := runners.Canonicalize(cfg.Defaults.Runner)
	if err != nil || canonicalDefaultRunner != cfg.Defaults.Runner {
		return cfg, errors.New(errors.EInvalidUserConfig, "defaults.runner must be a canonical runner id: "+strings.Join(runners.CanonicalIDs(), ", "))
	}
	if cfg.Defaults.Editor == "" {
		return cfg, errors.New(errors.EInvalidUserConfig, "missing required field defaults.editor")
	}
	cfg.Defaults.ExecutionProfile = strings.TrimSpace(cfg.Defaults.ExecutionProfile)
	if cfg.Defaults.ExecutionProfile == "" {
		return cfg, errors.New(errors.EInvalidExecutionProfile, "missing required field defaults.execution_profile")
	}
	if !IsValidExecutionProfileLabel(cfg.Defaults.ExecutionProfile) {
		return cfg, errors.New(errors.EInvalidExecutionProfile, "defaults.execution_profile must contain only lowercase letters, digits, and hyphens")
	}
	if len(cfg.ExecutionProfiles) == 0 {
		return cfg, errors.New(errors.EInvalidExecutionProfile, "missing required field execution_profiles")
	}
	if _, ok := cfg.ExecutionProfiles[cfg.Defaults.ExecutionProfile]; !ok {
		return cfg, errors.NewWithDetails(
			errors.EExecutionProfileNotFound,
			"defaults.execution_profile has no matching execution_profiles entry",
			map[string]string{"execution_profile": cfg.Defaults.ExecutionProfile},
		)
	}
	for name, profile := range cfg.ExecutionProfiles {
		if !IsValidExecutionProfileLabel(name) {
			return cfg, errors.New(errors.EInvalidExecutionProfile, "execution profile name must contain only lowercase letters, digits, and hyphens")
		}
		if profile.Env == nil {
			profile.Env = map[string]string{}
		}
		for key, value := range profile.Env {
			if key == "" || strings.Contains(key, "=") || strings.ContainsRune(key, '\x00') {
				return cfg, errors.New(errors.EInvalidExecutionProfile, "execution_profiles."+name+".env keys must be non-empty and must not contain '=' or NUL")
			}
			if strings.ContainsRune(value, '\x00') {
				return cfg, errors.New(errors.EInvalidExecutionProfile, "execution_profiles."+name+".env."+key+" must not contain NUL")
			}
		}
		cfg.ExecutionProfiles[name] = profile
	}
	for name, cmd := range cfg.Runners {
		canonicalRunner, err := runners.Canonicalize(name)
		if err != nil || canonicalRunner != name {
			return cfg, errors.New(errors.EInvalidUserConfig, "runners."+name+" must use a canonical runner id: "+strings.Join(runners.CanonicalIDs(), ", "))
		}
		if cmd == "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runners."+name+" must be a non-empty string")
		}
		if containsWhitespace(cmd) {
			return cfg, errors.New(errors.EInvalidUserConfig, "runners."+name+" must be a single executable (no args); use a wrapper script")
		}
	}
	for name, cmd := range cfg.Editors {
		if cmd == "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "editors."+name+" must be a non-empty string")
		}
		if containsWhitespace(cmd) {
			return cfg, errors.New(errors.EInvalidUserConfig, "editors."+name+" must be a single executable (no args); use a wrapper script")
		}
	}
	for name, rd := range cfg.RunnerDefaults {
		cleaned, err := validateRunnerDefaults(name, rd, errors.EInvalidUserConfig, true)
		if err != nil {
			return cfg, err
		}
		cfg.RunnerDefaults[name] = cleaned
	}
	return cfg, nil
}
