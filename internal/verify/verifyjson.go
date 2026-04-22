// Package verify provides the verify script execution engine and evidence recording.
package verify

import (
	"encoding/json"
	"errors"
	"os"
)

type verifyJSON struct {
	// SchemaVersion must be present and non-empty.
	SchemaVersion string `json:"schema_version"`

	// OK is the verification result from the script's perspective.
	OK bool `json:"ok"`

	// Summary is an optional human-readable summary.
	Summary string `json:"summary,omitempty"`

	// Data is optional arbitrary JSON data.
	Data json.RawMessage `json:"data,omitempty"`
}

type readVerifyJSONResult struct {
	value  *verifyJSON
	exists bool
	err    error
}

func readVerifyJSON(path string) readVerifyJSONResult {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return readVerifyJSONResult{}
		}
		// File exists but can't be read
		return readVerifyJSONResult{
			exists: true,
			err:    err,
		}
	}

	var vj verifyJSON
	if err := json.Unmarshal(data, &vj); err != nil {
		return readVerifyJSONResult{
			exists: true,
			err:    err,
		}
	}

	// "valid enough" rules: require schema_version to exist and be non-empty
	if vj.SchemaVersion == "" {
		return readVerifyJSONResult{
			exists: true,
			err:    errors.New("verify.json: schema_version is required and must be non-empty"),
		}
	}

	return readVerifyJSONResult{
		value:  &vj,
		exists: true,
	}
}
