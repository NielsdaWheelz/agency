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
	Version        int                       `json:"version"`
	Defaults       UserDefaults              `json:"defaults"`
	RunnerDefaults map[string]RunnerDefaults `json:"runner_defaults,omitempty"`
	Runners        map[string]string         `json:"runners,omitempty"`
	Editors        map[string]string         `json:"editors,omitempty"`
}

// UserDefaults contains default values for user-scoped operations.
type UserDefaults struct {
	Runner     string `json:"runner"`
	Editor     string `json:"editor"`
	BaseBranch string `json:"base_branch,omitempty"`
}

// RunnerDefaults contains typed runner defaults for one canonical runner id.
type RunnerDefaults struct {
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

// DefaultUserConfig returns scaffold content for creating a new user config.
func DefaultUserConfig() UserConfig {
	return UserConfig{
		Version: 3,
		Defaults: UserDefaults{
			Runner:     "claude-code",
			Editor:     "code",
			BaseBranch: "main",
		},
		Runners: map[string]string{},
		Editors: map[string]string{},
	}
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
		"version":         true,
		"defaults":        true,
		"runner_defaults": true,
		"runners":         true,
		"editors":         true,
	}
	for key := range raw {
		if !allowedKeys[key] {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "unknown field: "+key)
		}
	}

	// Parse version
	if rawVersion, ok := raw["version"]; ok {
		var version int
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			var floatVal float64
			if json.Unmarshal(rawVersion, &floatVal) == nil {
				if floatVal != float64(int(floatVal)) {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "version must be an integer")
				}
				version = int(floatVal)
			} else {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "version must be an integer")
			}
		}
		if version == 1 {
			return UserConfig{}, errors.NewWithDetails(
				errors.EInvalidUserConfig,
				"version 1 is not supported; config.json must use version 3",
				map[string]string{"hint": "run `agency config init --force` to scaffold a fresh version 3 config"},
			)
		}
		if version == 2 {
			return UserConfig{}, errors.NewWithDetails(
				errors.EInvalidUserConfig,
				"version 2 is not supported; config.json must use version 3",
				map[string]string{"hint": "run `agency config init --force` to scaffold a fresh version 3 config"},
			)
		}
		cfg.Version = version
	}

	// Parse defaults
	if rawDefaults, ok := raw["defaults"]; ok {
		var defaultsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawDefaults, &defaultsMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults must be an object")
		}
		allowedDefaultKeys := map[string]bool{
			"runner":      true,
			"editor":      true,
			"base_branch": true,
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
			var runner string
			if err := json.Unmarshal(rawRunner, &runner); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.runner must be a string")
			}
			cfg.Defaults.Runner = runner
		}
		if rawEditor, ok := defaultsMap["editor"]; ok {
			var editor string
			if err := json.Unmarshal(rawEditor, &editor); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.editor must be a string")
			}
			cfg.Defaults.Editor = editor
		}
		if rawBaseBranch, ok := defaultsMap["base_branch"]; ok {
			var baseBranch string
			if err := json.Unmarshal(rawBaseBranch, &baseBranch); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults.base_branch must be a string")
			}
			cfg.Defaults.BaseBranch = baseBranch
		}
	}

	if rawRunnerDefaults, ok := raw["runner_defaults"]; ok {
		var defaultsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawRunnerDefaults, &defaultsMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults must be an object")
		}

		cfg.RunnerDefaults = make(map[string]RunnerDefaults, len(defaultsMap))
		for runnerName, rawRunnerDefaults := range defaultsMap {
			var runnerDefaultsMap map[string]json.RawMessage
			if err := json.Unmarshal(rawRunnerDefaults, &runnerDefaultsMap); err != nil {
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
				var model string
				if err := json.Unmarshal(rawModel, &model); err != nil {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".model must be a string")
				}
				runnerDefaults.Model = model
			}
			if rawEffort, ok := runnerDefaultsMap["effort"]; ok {
				var effort string
				if err := json.Unmarshal(rawEffort, &effort); err != nil {
					return UserConfig{}, errors.New(errors.EInvalidUserConfig, "runner_defaults."+runnerName+".effort must be a string")
				}
				runnerDefaults.Effort = effort
			}
			if rawPermissionMode, ok := runnerDefaultsMap["permission_mode"]; ok {
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
		cfg.Runners = make(map[string]string)
		for key, rawVal := range runnersMap {
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
		cfg.Editors = make(map[string]string)
		for key, rawVal := range editorsMap {
			var val string
			if err := json.Unmarshal(rawVal, &val); err != nil {
				return UserConfig{}, errors.New(errors.EInvalidUserConfig, "editors."+key+" must be a string")
			}
			cfg.Editors[key] = val
		}
	}

	return cfg, nil
}

// ValidateUserConfig validates the user config and returns E_INVALID_USER_CONFIG on failure.
func ValidateUserConfig(cfg UserConfig) (UserConfig, error) {
	if cfg.Version == 1 {
		return cfg, errors.NewWithDetails(
			errors.EInvalidUserConfig,
			"version 1 is not supported; config.json must use version 3",
			map[string]string{"hint": "run `agency config init --force` to scaffold a fresh version 3 config"},
		)
	}
	if cfg.Version == 2 {
		return cfg, errors.NewWithDetails(
			errors.EInvalidUserConfig,
			"version 2 is not supported; config.json must use version 3",
			map[string]string{"hint": "run `agency config init --force` to scaffold a fresh version 3 config"},
		)
	}
	if cfg.Version != 3 {
		return cfg, errors.New(errors.EInvalidUserConfig, "version must be 3")
	}
	if cfg.Defaults.Runner == "" {
		return cfg, errors.New(errors.EInvalidUserConfig, "missing required field defaults.runner")
	}
	if cfg.Defaults.Editor == "" {
		return cfg, errors.New(errors.EInvalidUserConfig, "missing required field defaults.editor")
	}
	for name, cmd := range cfg.Runners {
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
	for name, runnerDefaults := range cfg.RunnerDefaults {
		canonicalRunner, err := runners.Canonicalize(name)
		if err != nil {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+" is not supported; typed runner defaults are supported for runners claude-code, codex, cursor")
		}
		if canonicalRunner != runners.RunnerClaudeCode && canonicalRunner != runners.RunnerCodex && canonicalRunner != runners.RunnerCursor {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+" is not supported; typed runner defaults are supported for runners claude-code, codex, cursor")
		}

		model := strings.TrimSpace(runnerDefaults.Model)
		effort := strings.TrimSpace(runnerDefaults.Effort)
		permissionMode := strings.TrimSpace(runnerDefaults.PermissionMode)
		if model == "" && effort == "" && permissionMode == "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+" requires at least one of model, effort, or permission_mode")
		}
		if runnerDefaults.Model != "" && model == "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+".model must be a non-empty string")
		}
		if runnerDefaults.Effort != "" && effort == "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+".effort must be a non-empty string")
		}
		if runnerDefaults.PermissionMode != "" && permissionMode == "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+".permission_mode must be a non-empty string")
		}
		if canonicalRunner == runners.RunnerCursor && effort != "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults.cursor.effort is not supported")
		}
		if canonicalRunner != runners.RunnerClaudeCode && permissionMode != "" {
			return cfg, errors.New(errors.EInvalidUserConfig, "runner_defaults."+name+".permission_mode is only supported for claude-code")
		}

		cfg.RunnerDefaults[canonicalRunner] = RunnerDefaults{
			Model:          model,
			Effort:         effort,
			PermissionMode: permissionMode,
		}
		if canonicalRunner != name {
			delete(cfg.RunnerDefaults, name)
		}
	}
	return cfg, nil
}
