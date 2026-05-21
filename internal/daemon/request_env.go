package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func decodeRequestEnv(data []byte, allowedKeys map[string]bool) (map[string]string, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, err
	}
	for key := range raw {
		if !allowedKeys[key] {
			return nil, false, fmt.Errorf("json: unknown field %q", key)
		}
	}
	rawEnv, ok := raw["env"]
	if !ok {
		return nil, false, nil
	}
	var rawValues map[string]json.RawMessage
	if err := json.Unmarshal(rawEnv, &rawValues); err != nil {
		return nil, true, newRequestShapeError("env must be an object")
	}
	if rawValues == nil {
		return nil, true, newRequestShapeError("env must be an object")
	}
	env := make(map[string]string, len(rawValues))
	for key, rawValue := range rawValues {
		if key == "" || strings.Contains(key, "=") || strings.ContainsRune(key, '\x00') {
			return nil, true, newRequestShapeError("env keys must be non-empty and must not contain '=' or NUL")
		}
		if bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) {
			return nil, true, newRequestShapeError("env.%s must be a string", key)
		}
		var value string
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return nil, true, newRequestShapeError("env.%s must be a string", key)
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, true, newRequestShapeError("env.%s must not contain NUL", key)
		}
		env[key] = value
	}
	return env, true, nil
}
