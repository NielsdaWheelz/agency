package errors

import (
	"bytes"
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
		tt := tt
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ExitCode(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPrint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		// Format now includes blank line after message (for context block)
		{"E_USAGE", New(EUsage, "bad args"), "error_code: E_USAGE\nbad args\n\n"},
		{"E_NOT_IMPLEMENTED", New(ENotImplemented, "not ready"), "error_code: E_NOT_IMPLEMENTED\nnot ready\n\n"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			Print(&buf, tt.err)
			got := buf.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestErrorFormatStability(t *testing.T) {
	t.Parallel()

	// This test ensures the error format is stable and matches the spec exactly.
	// The format MUST be: "CODE: message"
	err := New(EUsage, "x")
	expected := "E_USAGE: x"
	assert.Equal(t, expected, err.Error())
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

func TestLandingErrorFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code Code
		msg  string
		want string
	}{
		{ELandConflict, "cherry-pick conflicts", "E_LAND_CONFLICT: cherry-pick conflicts"},
		{ELandNothingToLand, "no changes", "E_LAND_NOTHING_TO_LAND: no changes"},
		{ELandApplyRequired, "uncommitted changes", "E_LAND_APPLY_REQUIRED: uncommitted changes"},
		{ELandFailed, "generic failure", "E_LAND_FAILED: generic failure"},
		{ELandAlreadyLanded, "already landed", "E_LAND_ALREADY_LANDED: already landed"},
		{ELandAlreadyDiscarded, "already discarded", "E_LAND_ALREADY_DISCARDED: already discarded"},
		{ESandboxMissing, "sandbox gone", "E_SANDBOX_MISSING: sandbox gone"},
		{EIntegrationTreeMissing, "tree gone", "E_INTEGRATION_TREE_MISSING: tree gone"},
		{ELandDenylistViolation, "blocked files", "E_LAND_DENYLIST_VIOLATION: blocked files"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.code), func(t *testing.T) {
			t.Parallel()

			err := New(tt.code, tt.msg)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}

func TestPushPRErrorFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code Code
		msg  string
		want string
	}{
		{EUnsupportedOriginHost, "origin is not github.com", "E_UNSUPPORTED_ORIGIN_HOST: origin is not github.com"},
		{ENoOrigin, "no origin remote", "E_NO_ORIGIN: no origin remote"},
		{EBaseNotFound, "branch main not found", "E_BASE_NOT_FOUND: branch main not found"},
		{EGitPushFailed, "push rejected", "E_GIT_PUSH_FAILED: push rejected"},
		{EGHPRCreateFailed, "gh pr create failed", "E_GH_PR_CREATE_FAILED: gh pr create failed"},
		{EGHPREditFailed, "gh pr edit failed", "E_GH_PR_EDIT_FAILED: gh pr edit failed"},
		{EGHPRViewFailed, "gh pr view failed after retries", "E_GH_PR_VIEW_FAILED: gh pr view failed after retries"},
		{EPRNotOpen, "PR is closed", "E_PR_NOT_OPEN: PR is closed"},
		{EEmptyDiff, "no commits ahead", "E_EMPTY_DIFF: no commits ahead"},
		{EDirtyWorktree, "worktree has uncommitted changes", "E_DIRTY_WORKTREE: worktree has uncommitted changes"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.code), func(t *testing.T) {
			t.Parallel()

			err := New(tt.code, tt.msg)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}
