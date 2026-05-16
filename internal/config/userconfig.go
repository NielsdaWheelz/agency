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
	allowedKeys := map[string]bool{
		"version":            true,
		"defaults":           true,
		"runner_defaults":    true,
		"runners":            true,
		"editors":            true,
		"execution_profiles": true,
	}
	for key := range raw {
		if !allowedKeys[key] {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "unknown field: "+key)
		}
	}

	// Parse version
	if rawVersion, ok := raw["version"]; ok {
		if isJSONNull(rawVersion) {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "version must be an integer")
		}
		var version int
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "version must be an integer")
		}
		cfg.Version = version
	}

	// Parse defaults
	if rawDefaults, ok := raw["defaults"]; ok {
		var defaultsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawDefaults, &defaultsMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults must be an object")
		}
		if defaultsMap == nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults must be an object")
		}
		allowedDefaultKeys := map[string]bool{
			"runner":            true,
			"editor":            true,
			"base_branch":       true,
			"execution_profile": true,
		}
		for key := range defaultsMap {
			switch key {
			case "model":
				return UserConfig{}, errors.NewWithDetails(
					errors.EInvalidUserConfig,
					"defaults.model is not supported; use runner_defaults.<runner>.model",
					map[string]string{"hint": "move the model under runner_defaults.<runner>.model"},
				)
			case "effort":
				return UserConfig{}, errors.NewWithDetails(
					errors.EInvalidUserConfig,
					"defaults.effort is not supported; use runner_defaults.<runner>.effort",
					map[string]string{"hint": "move the effort under runner_defaults.<runner>.effort"},
				)
			case "thinking":
				return UserConfig{}, errors.NewWithDetails(
					errors.EInvalidUserConfig,
					"defaults.thinking is not supported; select a thinking-capable model via runner_defaults.<runner>.model",
					map[string]string{"hint": "pick a thinking-capable model in runner_defaults.<runner>.model"},
				)
			}
			if !allowedDefaultKeys[key] {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults contains unknown field: "+key)
			}
		}
		if rawRunner, ok := defaultsMap["runner"]; ok {
			if isJSONNull(rawRunner) {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.runner must be a string")
			}
			var runner string
			if err := json.Unmarshal(rawRunner, &runner); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.runner must be a string")
			}
			cfg.Defaults.Runner = runner
		}
		if rawEditor, ok := defaultsMap["editor"]; ok {
			if isJSONNull(rawEditor) {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.editor must be a string")
			}
			var editor string
			if err := json.Unmarshal(rawEditor, &editor); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.editor must be a string")
			}
			cfg.Defaults.Editor = editor
		}
		if rawBaseBranch, ok := defaultsMap["base_branch"]; ok {
			if isJSONNull(rawBaseBranch) {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.base_branch must be a string")
			}
			var baseBranch string
			if err := json.Unmarshal(rawBaseBranch, &baseBranch); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.base_branch must be a string")
			}
			cfg.Defaults.BaseBranch = baseBranch
		}
		if rawExecutionProfile, ok := defaultsMap["execution_profile"]; ok {
			if isJSONNull(rawExecutionProfile) {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.execution_profile must be a string")
			}
			var executionProfile string
			if err := json.Unmarshal(rawExecutionProfile, &executionProfile); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.execution_profile must be a string")
			}
			cfg.Defaults.ExecutionProfile = executionProfile
		}
	}

	if rawRunnerDefaults, ok := raw["runner_defaults"]; ok {
		var defaultsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawRunnerDefaults, &defaultsMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults must be an object")
		}
		if defaultsMap == nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults must be an object")
		}

		cfg.RunnerDefaults = make(map[string]RunnerDefaults, len(defaultsMap))
		for runnerName, rawRunnerDefaults := range defaultsMap {
			var runnerDefaultsMap map[string]json.RawMessage
			if err := json.Unmarshal(rawRunnerDefaults, &runnerDefaultsMap); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+" must be an object")
			}
			if runnerDefaultsMap == nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+" must be an object")
			}

			allowedRunnerDefaultsKeys := map[string]bool{
				"model":           true,
				"effort":          true,
				"permission_mode": true,
			}
			for key := range runnerDefaultsMap {
				if !allowedRunnerDefaultsKeys[key] {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+" contains unknown field: "+key)
				}
			}

			var runnerDefaults RunnerDefaults
			if rawModel, ok := runnerDefaultsMap["model"]; ok {
				if isJSONNull(rawModel) {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".model must be a string")
				}
				var model string
				if err := json.Unmarshal(rawModel, &model); err != nil {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".model must be a string")
				}
				runnerDefaults.Model = model
			}
			if rawEffort, ok := runnerDefaultsMap["effort"]; ok {
				if isJSONNull(rawEffort) {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".effort must be a string")
				}
				var effort string
				if err := json.Unmarshal(rawEffort, &effort); err != nil {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".effort must be a string")
				}
				runnerDefaults.Effort = effort
			}
			if rawPermissionMode, ok := runnerDefaultsMap["permission_mode"]; ok {
				if isJSONNull(rawPermissionMode) {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".permission_mode must be a string")
				}
				var permissionMode string
				if err := json.Unmarshal(rawPermissionMode, &permissionMode); err != nil {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".permission_mode must be a string")
				}
				runnerDefaults.PermissionMode = permissionMode
			}

			cfg.RunnerDefaults[runnerName] = runnerDefaults
		}
	}

	// Parse runners
	if rawRunners, ok := raw["runners"]; ok {
		var runnersMap map[string]json.RawMessage
		if err := json.Unmarshal(rawRunners, &runnersMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runners must be an object")
		}
		if runnersMap == nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runners must be an object")
		}
		cfg.Runners = make(map[string]string)
		for key, rawVal := range runnersMap {
			if isJSONNull(rawVal) {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runners."+key+" must be a string")
			}
			var val string
			if err := json.Unmarshal(rawVal, &val); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runners."+key+" must be a string")
			}
			cfg.Runners[key] = val
		}
	}

	// Parse editors
	if rawEditors, ok := raw["editors"]; ok {
		var editorsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawEditors, &editorsMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "editors must be an object")
		}
		if editorsMap == nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "editors must be an object")
		}
		cfg.Editors = make(map[string]string)
		for key, rawVal := range editorsMap {
			if isJSONNull(rawVal) {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "editors."+key+" must be a string")
			}
			var val string
			if err := json.Unmarshal(rawVal, &val); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "editors."+key+" must be a string")
			}
			cfg.Editors[key] = val
		}
	}

	if rawProfiles, ok := raw["execution_profiles"]; ok {
		var profilesMap map[string]json.RawMessage
		if err := json.Unmarshal(rawProfiles, &profilesMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles must be an object")
		}
		if profilesMap == nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles must be an object")
		}
		cfg.ExecutionProfiles = make(map[string]ExecutionProfile, len(profilesMap))
		for name, rawProfile := range profilesMap {
			var profileMap map[string]json.RawMessage
			if err := json.Unmarshal(rawProfile, &profileMap); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+" must be an object")
			}
			if profileMap == nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+" must be an object")
			}
			for key := range profileMap {
				if key != "env" {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+" contains unknown field: "+key)
				}
			}
			rawEnv, ok := profileMap["env"]
			if !ok {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+".env is required")
			}
			var envMap map[string]json.RawMessage
			if err := json.Unmarshal(rawEnv, &envMap); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+".env must be an object")
			}
			if envMap == nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+".env must be an object")
			}
			profile := ExecutionProfile{Env: make(map[string]string, len(envMap))}
			for key, rawValue := range envMap {
				if isJSONNull(rawValue) {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+".env."+key+" must be a string")
				}
				var value string
				if err := json.Unmarshal(rawValue, &value); err != nil {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "execution_profiles."+name+".env."+key+" must be a string")
				}
				profile.Env[key] = value
			}
			cfg.ExecutionProfiles[name] = profile
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
