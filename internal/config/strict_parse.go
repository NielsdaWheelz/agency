package config

import (
	"encoding/json"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// parseStrictString unmarshals raw[key] into dst if present. Returns a strict
// error if the value is JSON null or the wrong type.
func parseStrictString(raw map[string]json.RawMessage, key, fieldPath string, errCode errors.Code, dst *string) error {
	rawVal, ok := raw[key]
	if !ok {
		return nil
	}
	if isJSONNull(rawVal) {
		return errors.New(errCode, fieldPath+" must be a string")
	}
	if err := json.Unmarshal(rawVal, dst); err != nil {
		return errors.New(errCode, fieldPath+" must be a string")
	}
	return nil
}

// parseStrictInt unmarshals raw[key] into dst if present. Returns a strict
// error if the value is JSON null or the wrong type.
func parseStrictInt(raw map[string]json.RawMessage, key, fieldPath string, errCode errors.Code, dst *int) error {
	rawVal, ok := raw[key]
	if !ok {
		return nil
	}
	if isJSONNull(rawVal) {
		return errors.New(errCode, fieldPath+" must be an integer")
	}
	if err := json.Unmarshal(rawVal, dst); err != nil {
		return errors.New(errCode, fieldPath+" must be an integer")
	}
	return nil
}

// parseStrictObject unmarshals rawVal as a JSON object. Returns a strict error
// if the value is JSON null or not an object.
func parseStrictObject(rawVal json.RawMessage, fieldPath string, errCode errors.Code) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rawVal, &m); err != nil {
		return nil, errors.New(errCode, fieldPath+" must be an object")
	}
	if m == nil {
		return nil, errors.New(errCode, fieldPath+" must be an object")
	}
	return m, nil
}

// rejectUnknownKeys returns a strict error if raw contains any key not in
// allowed. fieldPath is the JSON path of the parent object (empty for root).
func rejectUnknownKeys(raw map[string]json.RawMessage, fieldPath string, errCode errors.Code, allowed map[string]bool) error {
	for key := range raw {
		if allowed[key] {
			continue
		}
		if fieldPath == "" {
			return errors.New(errCode, "unknown field: "+key)
		}
		return errors.New(errCode, fieldPath+" contains unknown field: "+key)
	}
	return nil
}

// parseStrictStringMap unmarshals raw[key] as an object of string values into
// dst. Null/non-object/non-string-leaf values return a strict error.
func parseStrictStringMap(raw map[string]json.RawMessage, key, fieldPath string, errCode errors.Code, dst *map[string]string) error {
	rawVal, ok := raw[key]
	if !ok {
		return nil
	}
	fields, err := parseStrictObject(rawVal, fieldPath, errCode)
	if err != nil {
		return err
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		var val string
		if err := parseStrictString(map[string]json.RawMessage{k: v}, k, fieldPath+"."+k, errCode, &val); err != nil {
			return err
		}
		out[k] = val
	}
	*dst = out
	return nil
}
