package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	err := New(EUsage, "test message")

	assert.Equal(t, "E_USAGE: test message", err.Error())
}

func TestWrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying")
	err := Wrap(ENotImplemented, "wrapped message", cause)

	assert.Equal(t, "E_NOT_IMPLEMENTED: wrapped message", err.Error())

	// Test Unwrap
	var ae *AgencyError
	require.True(t, errors.As(err, &ae), "errors.As failed")
	assert.Equal(t, cause, ae.Cause, "Unwrap did not return cause")
}

func TestGetCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"nil error", nil, ""},
		{"agency error", New(EUsage, "x"), EUsage},
		{"wrapped agency error", Wrap(ENotImplemented, "y", errors.New("z")), ENotImplemented},
		{"non-agency error", errors.New("plain"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := GetCode(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"E_USAGE", New(EUsage, "x"), 2},
		{"E_NOT_IMPLEMENTED", New(ENotImplemented, "x"), 1},
		{"non-agency error", errors.New("x"), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ExitCode(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewWithDetails(t *testing.T) {
	t.Parallel()

	details := map[string]string{"key": "value"}
	err := NewWithDetails(EUsage, "test message", details)

	var ae *AgencyError
	require.True(t, errors.As(err, &ae), "errors.As failed")

	assert.Equal(t, EUsage, ae.Code)
	assert.Equal(t, "test message", ae.Msg)
	assert.Equal(t, "value", ae.Details["key"])
}

func TestNewWithDetails_NilDetails(t *testing.T) {
	t.Parallel()

	err := NewWithDetails(EUsage, "test", nil)

	var ae *AgencyError
	require.True(t, errors.As(err, &ae), "errors.As failed")
	assert.Nil(t, ae.Details, "Details should be nil")
}

func TestNewWithDetails_DefensiveCopy(t *testing.T) {
	t.Parallel()

	details := map[string]string{"key": "value"}
	err := NewWithDetails(EUsage, "test", details)

	// Modify the original map
	details["key"] = "modified"

	var ae *AgencyError
	require.True(t, errors.As(err, &ae), "errors.As failed")
	// The error's details should not be affected
	assert.Equal(t, "value", ae.Details["key"], "Details should be defensively copied")
}

func TestWrapWithDetails(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying")
	details := map[string]string{"file": "test.go"}
	err := WrapWithDetails(EUsage, "wrapped", cause, details)

	var ae *AgencyError
	require.True(t, errors.As(err, &ae), "errors.As failed")

	assert.Equal(t, cause, ae.Cause, "Cause not set")
	assert.Equal(t, "test.go", ae.Details["file"])
}

func TestAsAgencyError(t *testing.T) {
	t.Parallel()

	t.Run("direct AgencyError", func(t *testing.T) {
		t.Parallel()

		err := New(EUsage, "test")
		ae, ok := AsAgencyError(err)
		assert.True(t, ok, "should return true for AgencyError")
		assert.Equal(t, EUsage, ae.Code)
	})

	t.Run("non AgencyError", func(t *testing.T) {
		t.Parallel()

		err := errors.New("regular error")
		ae, ok := AsAgencyError(err)
		assert.False(t, ok, "should return false for non-AgencyError")
		assert.Nil(t, ae, "should return nil for non-AgencyError")
	})

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()

		ae, ok := AsAgencyError(nil)
		assert.False(t, ok, "should return false for nil")
		assert.Nil(t, ae, "should return nil for nil")
	})
}

func TestCodeOr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, EUsage, CodeOr(New(EUsage, "x"), EInternal))
	assert.Equal(t, EInternal, CodeOr(errors.New("plain"), EInternal))
}
