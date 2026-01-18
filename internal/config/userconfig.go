// Package config handles loading and validation of agency configuration files.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// UserConfig represents the parsed and validated user configuration.
type UserConfig struct {
	Version  int               `json:"version"`
	Defaults UserDefaults      `json:"defaults"`
	Runners  map[string]string `json:"runners,omitempty"`
	Editors  map[string]string `json:"editors,omitempty"`
}

// UserDefaults contains default values for user-scoped operations.
type UserDefaults struct {
	Runner string `json:"runner"`
	Editor string `json:"editor"`
}

// DefaultUserConfig returns built-in defaults used when config.json is missing.
func DefaultUserConfig() UserConfig {
	return UserConfig{
		Version: 1,
		Defaults: UserDefaults{
			Runner: "claude",
			Editor: "code",
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
// If the file is missing, returns defaults with found=false.
// If the file exists but is invalid, returns E_INVALID_USER_CONFIG.
func LoadUserConfig(filesystem fs.FS, configDir string) (UserConfig, bool, error) {
	path := UserConfigPath(configDir)

	data, err := filesystem.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultUserConfig(), false, nil
		}
		return UserConfig{}, false, errors.Wrap(errors.EInvalidUserConfig, "failed to read user config", err)
	}

	// First, unmarshal into raw map for type checking
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return UserConfig{}, false, errors.New(errors.EInvalidUserConfig, "invalid json: "+err.Error())
	}

	cfg, err := parseUserConfigStrict(raw)
	if err != nil {
		return UserConfig{}, false, err
	}

	if _, err := ValidateUserConfig(cfg); err != nil {
		return UserConfig{}, false, err
	}

	return cfg, true, nil
}

func parseUserConfigStrict(raw map[string]json.RawMessage) (UserConfig, error) {
	var cfg UserConfig
	allowedKeys := map[string]bool{
		"version":  true,
		"defaults": true,
		"runners":  true,
		"editors":  true,
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
		cfg.Version = version
	}

	// Parse defaults
	if rawDefaults, ok := raw["defaults"]; ok {
		var defaultsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawDefaults, &defaultsMap); err != nil {
			return UserConfig{}, errors.New(errors.EInvalidUserConfig, "defaults must be an object")
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
	if cfg.Version != 1 {
		return cfg, errors.New(errors.EInvalidUserConfig, "version must be 1")
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
	return cfg, nil
}
