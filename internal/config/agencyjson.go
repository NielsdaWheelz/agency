// Package config handles loading and validation of agency.json configuration files.
package config

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Default timeouts for scripts.
const (
	AgencyConfigVersion   = 4
	CheckoutRootSibling   = "repo-sibling"
	DefaultSetupTimeout   = 10 * time.Minute
	DefaultVerifyTimeout  = 30 * time.Minute
	DefaultArchiveTimeout = 5 * time.Minute
	minTimeout            = 1 * time.Minute
	maxTimeout            = 24 * time.Hour
)

// AgencyConfig represents the parsed and validated agency.json configuration.
type AgencyConfig struct {
	Version        int                       `json:"version"`
	Scripts        Scripts                   `json:"scripts"`
	RunnerDefaults map[string]RunnerDefaults `json:"runner_defaults,omitempty"`
	Execution      AgencyExecutionConfig     `json:"execution,omitempty"`
}

// AgencyExecutionConfig contains repo-scoped execution policy.
type AgencyExecutionConfig struct {
	Profile      string `json:"profile,omitempty"`
	CheckoutRoot string `json:"checkout_root,omitempty"`
}

// Scripts contains configuration for the required agency scripts.
type Scripts struct {
	Setup   ScriptConfig `json:"setup"`
	Verify  ScriptConfig `json:"verify"`
	Archive ScriptConfig `json:"archive"`
}

// ScriptConfig contains the path and timeout for a script.
type ScriptConfig struct {
	Path    string        `json:"path"`
	Timeout time.Duration `json:"-"` // Parsed from "timeout" string field
}

func loadAgencyConfigPath(filesystem fs.FS, path string) (AgencyConfig, error) {
	data, err := filesystem.ReadFile(path)
	if err != nil {
		return AgencyConfig{}, err
	}

	// First, unmarshal into raw map for type checking
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "invalid json: "+err.Error())
	}

	// Perform strict type validation during parsing
	cfg, err := parseWithStrictTypes(raw)
	if err != nil {
		return AgencyConfig{}, err
	}

	return cfg, nil
}

// parseWithStrictTypes parses the raw JSON map with strict type checking.
// This catches type mismatches that Go's json.Unmarshal would silently accept or default.
func parseWithStrictTypes(raw map[string]json.RawMessage) (AgencyConfig, error) {
	var cfg AgencyConfig
	const code = errors.EInvalidAgencyJSON
	if err := rejectUnknownKeys(raw, "", code, map[string]bool{
		"version": true, "scripts": true, "runner_defaults": true, "execution": true,
	}); err != nil {
		return AgencyConfig{}, err
	}

	if err := parseStrictInt(raw, "version", "version", code, &cfg.Version); err != nil {
		return AgencyConfig{}, err
	}

	if rawScripts, ok := raw["scripts"]; ok {
		scriptsMap, err := parseStrictObject(rawScripts, "scripts", code)
		if err != nil {
			return AgencyConfig{}, err
		}
		if err := rejectUnknownKeys(scriptsMap, "scripts", code, map[string]bool{
			"setup": true, "verify": true, "archive": true,
		}); err != nil {
			return AgencyConfig{}, err
		}
		if err := parseScriptInto(scriptsMap, "setup", "scripts.setup", DefaultSetupTimeout, &cfg.Scripts.Setup); err != nil {
			return AgencyConfig{}, err
		}
		if err := parseScriptInto(scriptsMap, "verify", "scripts.verify", DefaultVerifyTimeout, &cfg.Scripts.Verify); err != nil {
			return AgencyConfig{}, err
		}
		if err := parseScriptInto(scriptsMap, "archive", "scripts.archive", DefaultArchiveTimeout, &cfg.Scripts.Archive); err != nil {
			return AgencyConfig{}, err
		}
	}

	if rawRunnerDefaults, ok := raw["runner_defaults"]; ok {
		defaultsMap, err := parseStrictObject(rawRunnerDefaults, "runner_defaults", code)
		if err != nil {
			return AgencyConfig{}, err
		}
		cfg.RunnerDefaults = make(map[string]RunnerDefaults, len(defaultsMap))
		for runnerName, rawRunnerDefaults := range defaultsMap {
			path := "runner_defaults." + runnerName
			runnerMap, err := parseStrictObject(rawRunnerDefaults, path, code)
			if err != nil {
				return AgencyConfig{}, err
			}
			if _, hasPermissionMode := runnerMap["permission_mode"]; hasPermissionMode {
				return AgencyConfig{}, errors.New(code, path+".permission_mode is not supported in agency.json")
			}
			if err := rejectUnknownKeys(runnerMap, path, code, map[string]bool{
				"model": true, "effort": true,
			}); err != nil {
				return AgencyConfig{}, err
			}
			var rd RunnerDefaults
			if err := parseStrictString(runnerMap, "model", path+".model", code, &rd.Model); err != nil {
				return AgencyConfig{}, err
			}
			if err := parseStrictString(runnerMap, "effort", path+".effort", code, &rd.Effort); err != nil {
				return AgencyConfig{}, err
			}
			cfg.RunnerDefaults[runnerName] = rd
		}
	}

	if rawExecution, ok := raw["execution"]; ok {
		executionMap, err := parseStrictObject(rawExecution, "execution", code)
		if err != nil {
			return AgencyConfig{}, err
		}
		if err := rejectUnknownKeys(executionMap, "execution", code, map[string]bool{
			"profile": true, "checkout_root": true,
		}); err != nil {
			return AgencyConfig{}, err
		}
		if err := parseStrictString(executionMap, "profile", "execution.profile", code, &cfg.Execution.Profile); err != nil {
			return AgencyConfig{}, err
		}
		if err := parseStrictString(executionMap, "checkout_root", "execution.checkout_root", code, &cfg.Execution.CheckoutRoot); err != nil {
			return AgencyConfig{}, err
		}
	}

	return cfg, nil
}

// parseScriptInto reads a script entry from raw[key] and stores the result in dst.
func parseScriptInto(raw map[string]json.RawMessage, key, fieldPath string, defaultTimeout time.Duration, dst *ScriptConfig) error {
	rawVal, ok := raw[key]
	if !ok {
		return nil
	}
	cfg, err := parseScriptConfig(rawVal, fieldPath, defaultTimeout)
	if err != nil {
		return err
	}
	*dst = cfg
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// parseScriptConfig parses a script configuration from raw JSON.
// The script config must be an object with "path" (required) and "timeout" (optional) fields.
func parseScriptConfig(raw json.RawMessage, fieldName string, defaultTimeout time.Duration) (ScriptConfig, error) {
	var cfg ScriptConfig

	// Parse as object
	var scriptMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &scriptMap); err != nil {
		return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+" must be an object with 'path' field")
	}
	if scriptMap == nil {
		return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+" must be an object with 'path' field")
	}

	// Check for unknown keys
	allowedKeys := map[string]bool{"path": true, "timeout": true}
	for key := range scriptMap {
		if !allowedKeys[key] {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+" contains unknown field: "+key)
		}
	}

	// Parse path - required
	rawPath, ok := scriptMap["path"]
	if !ok {
		return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+" missing required field 'path'")
	}
	if isJSONNull(rawPath) {
		return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".path must be a string")
	}
	var path string
	if err := json.Unmarshal(rawPath, &path); err != nil {
		return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".path must be a string")
	}
	cfg.Path = path

	// Parse timeout - optional, defaults to provided default
	cfg.Timeout = defaultTimeout
	if rawTimeout, ok := scriptMap["timeout"]; ok {
		if isJSONNull(rawTimeout) {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout must be a string (Go duration format, e.g., '30m', '1h')")
		}
		var timeoutStr string
		if err := json.Unmarshal(rawTimeout, &timeoutStr); err != nil {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout must be a string (Go duration format, e.g., '30m', '1h')")
		}
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout invalid duration: "+err.Error())
		}
		if err := validateScriptTimeout(fieldName, timeout); err != nil {
			return cfg, err
		}
		cfg.Timeout = timeout
	}

	return cfg, nil
}
