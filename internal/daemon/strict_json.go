package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func decodeStrictJSON(body io.Reader, dst any) error {
	raw, err := decodeOneJSONValue(body, false)
	if err != nil {
		return err
	}
	return decodeRawStrictJSON(raw, dst)
}

func decodeOptionalStrictJSON(body io.Reader, dst any) error {
	raw, err := decodeOneJSONValue(body, true)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	return decodeRawStrictJSON(raw, dst)
}

func decodeOneJSONValue(body io.Reader, optional bool) (json.RawMessage, error) {
	dec := json.NewDecoder(body)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		if optional && err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("expected a single JSON object")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return raw, nil
}

func decodeRawStrictJSON(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
