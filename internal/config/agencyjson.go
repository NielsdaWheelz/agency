// Package config handles loading and validation of agency.json configuration files.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// AgencyConfig represents the parsed and validated agency.json configuration.
type AgencyConfig struct {
	Version  int               `json:"version"`
	Defaults Defaults          `json:"defaults"`
	Scripts  Scripts           `json:"scripts"`
	Runners  map[string]string `json:"runners,omitempty"`

	// Derived (not from JSON):
	ResolvedRunnerCmd string `json:"-"`
}

// Defaults contains default values for agency operations.
type Defaults struct {
	ParentBranch string `json:"parent_branch"`
	Runner       string `json:"runner"`
}

// Scripts contains paths to the required agency scripts.
type Scripts struct {
	Setup   string `json:"setup"`
	Verify  string `json:"verify"`
	Archive string `json:"archive"`
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

	// Parse defaults - required, must be object
	if rawDefaults, ok := raw["defaults"]; ok {
		// First check if it's an object
		var defaultsMap map[string]json.RawMessage
		if err := json.Unmarshal(rawDefaults, &defaultsMap); err != nil {
			return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "defaults must be an object")
		}

		// Parse defaults.parent_branch
		if rawPB, ok := defaultsMap["parent_branch"]; ok {
			var pb string
			if err := json.Unmarshal(rawPB, &pb); err != nil {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "defaults.parent_branch must be a string")
			}
			cfg.Defaults.ParentBranch = pb
		}

		// Parse defaults.runner
		if rawRunner, ok := defaultsMap["runner"]; ok {
			var runner string
			if err := json.Unmarshal(rawRunner, &runner); err != nil {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "defaults.runner must be a string")
			}
			cfg.Defaults.Runner = runner
		}
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
			var setup string
			if err := json.Unmarshal(rawSetup, &setup); err != nil {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "scripts.setup must be a string")
			}
			cfg.Scripts.Setup = setup
		}

		// Parse scripts.verify
		if rawVerify, ok := scriptsMap["verify"]; ok {
			var verify string
			if err := json.Unmarshal(rawVerify, &verify); err != nil {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "scripts.verify must be a string")
			}
			cfg.Scripts.Verify = verify
		}

		// Parse scripts.archive
		if rawArchive, ok := scriptsMap["archive"]; ok {
			var archive string
			if err := json.Unmarshal(rawArchive, &archive); err != nil {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "scripts.archive must be a string")
			}
			cfg.Scripts.Archive = archive
		}
	}

	// Parse runners - optional, must be object if present
	if rawRunners, ok := raw["runners"]; ok {
		// First check if it's an object (not array, not primitive)
		var runnersMap map[string]json.RawMessage
		if err := json.Unmarshal(rawRunners, &runnersMap); err != nil {
			return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "runners must be an object")
		}

		cfg.Runners = make(map[string]string)
		for key, rawVal := range runnersMap {
			var val string
			if err := json.Unmarshal(rawVal, &val); err != nil {
				return AgencyConfig{}, errors.New(errors.EInvalidAgencyJSON, "runners."+key+" must be a string")
			}
			cfg.Runners[key] = val
		}
	}

	return cfg, nil
}
