// Package config handles loading and validation of agency.json configuration files.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Default timeouts for scripts.
const (
	DefaultSetupTimeout   = 10 * time.Minute
	DefaultVerifyTimeout  = 30 * time.Minute
	DefaultArchiveTimeout = 5 * time.Minute
	MinTimeout            = 1 * time.Minute
	MaxTimeout            = 24 * time.Hour
)

// AgencyConfig represents the parsed and validated agency.json configuration.
type AgencyConfig struct {
	Version int     `json:"version"`
	Scripts Scripts `json:"scripts"`
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

// LoadAgencyConfig reads and parses agency.json from the given repo root.
// Returns E_NO_AGENCY_JSON if the file does not exist.
// Returns E_INVALID_AGENCY_JSON if the file is not valid JSON.
// Does NOT perform semantic validation; call ValidateAgencyConfig for that.
func LoadAgencyConfig(filesystem fs.FS, repoRoot string) (AgencyConfig, error) {
	path := filepath.Join(repoRoot, "agency.json")

	data, err := filesystem.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AgencyConfig{}, errors.New(errors.ENoAgencyJSON, "agency.json not found; run 'agency init' to create it")
		}
		return AgencyConfig{}, errors.Wrap(errors.ENoAgencyJSON, "failed to read agency.json", err)
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
	allowedKeys := map[string]bool{
		"version": true,
		"scripts": true,
	}
	for key := range raw {
		if !allowedKeys[key] {
			return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "unknown field: "+key)
		}
	}

	// Parse version - required, must be integer
	if rawVersion, ok := raw["version"]; ok {
		var version int
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			// Check if it's a different type
			var floatVal float64
			if json.Unmarshal(rawVersion, &floatVal) == nil {
				// It's a float - check if it's a whole number
				if floatVal != float64(int(floatVal)) {
					return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "version must be an integer")
				}
				version = int(floatVal)
			} else {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "version must be an integer")
			}
		}
		cfg.Version = version
	}

	// Parse scripts - required, must be object
	if rawScripts, ok := raw["scripts"]; ok {
		// First check if it's an object
		var scriptsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawScripts, &scriptsMap); err != nil {
			return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "scripts must be an object")
		}

		// Parse scripts.setup
		if rawSetup, ok := scriptsMap["setup"]; ok {
			scriptCfg, err := parseScriptConfig(rawSetup, "scripts.setup", DefaultSetupTimeout)
			if err != nil {
				return AgencyConfig{}, err
			}
			cfg.Scripts.Setup = scriptCfg
		}

		// Parse scripts.verify
		if rawVerify, ok := scriptsMap["verify"]; ok {
			scriptCfg, err := parseScriptConfig(rawVerify, "scripts.verify", DefaultVerifyTimeout)
			if err != nil {
				return AgencyConfig{}, err
			}
			cfg.Scripts.Verify = scriptCfg
		}

		// Parse scripts.archive
		if rawArchive, ok := scriptsMap["archive"]; ok {
			scriptCfg, err := parseScriptConfig(rawArchive, "scripts.archive", DefaultArchiveTimeout)
			if err != nil {
				return AgencyConfig{}, err
			}
			cfg.Scripts.Archive = scriptCfg
		}
	}

	return cfg, nil
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
	var path string
	if err := json.Unmarshal(rawPath, &path); err != nil {
		return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".path must be a string")
	}
	cfg.Path = path

	// Parse timeout - optional, defaults to provided default
	cfg.Timeout = defaultTimeout
	if rawTimeout, ok := scriptMap["timeout"]; ok {
		var timeoutStr string
		if err := json.Unmarshal(rawTimeout, &timeoutStr); err != nil {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout must be a string (Go duration format, e.g., '30m', '1h')")
		}
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout invalid duration: "+err.Error())
		}
		if timeout < MinTimeout {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout must be at least 1m")
		}
		if timeout > MaxTimeout {
			return cfg, errors.New(errors.EInvalidAgencyJSON, fieldName+".timeout must be at most 24h")
		}
		cfg.Timeout = timeout
	}

	return cfg, nil
}
