package store

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"io"
)

func decodeStrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return stderrors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func strictJSONObjectFields(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := decodeStrictJSON(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, stderrors.New("expected JSON object")
	}
	return fields, nil
}
