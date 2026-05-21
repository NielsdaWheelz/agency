package daemon

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"strings"
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

type requestShapeError struct {
	message string
}

func (e *requestShapeError) Error() string {
	return e.message
}

func newRequestShapeError(format string, args ...any) error {
	return &requestShapeError{message: fmt.Sprintf(format, args...)}
}

func strictJSONDecodeErrorMessage(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "expected a JSON object" || msg == "expected a single JSON object" {
		return "invalid request body: " + msg
	}

	const unknownFieldPrefix = "json: unknown field "
	if strings.HasPrefix(msg, unknownFieldPrefix) {
		field := strings.TrimSpace(strings.Trim(strings.TrimPrefix(msg, unknownFieldPrefix), `"`))
		if field != "" {
			return fmt.Sprintf("invalid request body: unknown field %q", field)
		}
	}

	if _, ok := err.(*json.SyntaxError); ok || err == io.ErrUnexpectedEOF {
		return "invalid request body: malformed JSON"
	}

	if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
		if field := strings.TrimSpace(typeErr.Field); field != "" {
			return fmt.Sprintf("invalid request body: field %q must be %s", field, typeErr.Type.String())
		}
		return "invalid request body: invalid value type"
	}

	var shapeErr *requestShapeError
	if stderrors.As(err, &shapeErr) {
		return "invalid request body: " + shapeErr.Error()
	}

	return "invalid request body: malformed JSON"
}
