// Package verify provides the verify script execution engine and evidence recording.
package verify

import (
	"encoding/json"
	"errors"
	"os"
)

// VerifyJSON represents the optional structured output from a verify script.
// Written to <worktree>/.agency/out/verify.json by the script (not agency).
type VerifyJSON struct {
	// SchemaVersion must be present and non-empty.
	SchemaVersion string `json:"schema_version"`

	// OK is the verification result from the script's perspective.
	OK bool `json:"ok"`

	// Summary is an optional human-readable summary.
	Summary string `json:"summary,omitempty"`

	// Data is optional arbitrary JSON data.
	Data json.RawMessage `json:"data,omitempty"`
}

// ReadVerifyJSONResult contains the result of reading verify.json.
type ReadVerifyJSONResult struct {
	// VJ is the parsed VerifyJSON, nil if invalid or missing.
	VJ *VerifyJSON

	// Exists is true if the file exists on disk.
	Exists bool

	// Err contains parse/validation info when file exists but is invalid.
	// nil if file does not exist or is valid.
	Err error
}

// ReadVerifyJSON reads and parses the verify.json file at the given path.
//
// Returns:
//   - Exists=true only if file exists
//   - VJ=nil on invalid json or invalid shape (missing schema_version)
//   - Err carries parse/validation info when Exists=true but VJ=nil
func ReadVerifyJSON(path string) ReadVerifyJSONResult {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReadVerifyJSONResult{Exists: false}
		}
		// File exists but can't be read
		return ReadVerifyJSONResult{
			Exists: true,
			Err:    err,
		}
	}

	var vj VerifyJSON
	if err := json.Unmarshal(data, &vj); err != nil {
		return ReadVerifyJSONResult{
			Exists: true,
			Err:    err,
		}
	}

	// "valid enough" rules: require schema_version to exist and be non-empty
	if vj.SchemaVersion == "" {
		return ReadVerifyJSONResult{
			Exists: true,
			Err:    errors.New("verify.json: schema_version is required and must be non-empty"),
		}
	}

	return ReadVerifyJSONResult{
		VJ:     &vj,
		Exists: true,
	}
}
