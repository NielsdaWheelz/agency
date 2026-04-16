package daemon

import (
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportViolationToAgencyError_MapsDeterministicCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		violation    *report.Violation
		expectedCode errors.Code
	}{
		{
			name: "missing",
			violation: &report.Violation{
				Code:    report.ViolationMissing,
				Message: "missing",
				Path:    "/tmp/.agency/report.md",
			},
			expectedCode: errors.EReportMissing,
		},
		{
			name: "malformed",
			violation: &report.Violation{
				Code:    report.ViolationMalformed,
				Message: "malformed",
				Source:  report.SourceJSON,
			},
			expectedCode: errors.EReportMalformed,
		},
		{
			name: "oversized",
			violation: &report.Violation{
				Code:    report.ViolationOversized,
				Message: "oversized",
			},
			expectedCode: errors.EReportOversized,
		},
		{
			name: "schema incompatible",
			violation: &report.Violation{
				Code:    report.ViolationSchemaIncompatible,
				Message: "schema mismatch",
			},
			expectedCode: errors.EReportSchemaIncompatible,
		},
		{
			name: "incomplete",
			violation: &report.Violation{
				Code:    report.ViolationIncomplete,
				Message: "incomplete",
			},
			expectedCode: errors.EReportIncomplete,
		},
		{
			name: "unknown code is reported as invalid",
			violation: &report.Violation{
				Code:    report.ViolationCode("unknown"),
				Message: "unknown",
			},
			expectedCode: errors.EReportInvalid,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := reportViolationToAgencyError(tc.violation)
			require.Error(t, err)
			assert.Equal(t, tc.expectedCode, errors.GetCode(err))

			ae, ok := errors.AsAgencyError(err)
			require.True(t, ok)
			assert.Equal(t, tc.violation.Message, ae.Msg)
			assert.NotEmpty(t, ae.Details["hint"])
			assert.Equal(t, string(tc.violation.Code), ae.Details["report_violation"])
		})
	}
}

func TestReportViolationToAgencyError_NilViolation(t *testing.T) {
	t.Parallel()
	assert.Nil(t, reportViolationToAgencyError(nil))
}

func TestReportViolationToCheckReason_MapsCodes(t *testing.T) {
	t.Parallel()

	reason := reportViolationToCheckReason(&report.Violation{
		Code:    report.ViolationMissing,
		Message: "missing report",
	})
	assert.Equal(t, "report_missing", reason.Code)
	assert.Equal(t, "missing report", reason.Message)
	assert.Contains(t, reason.Hint, ".agency/report")
}

func TestReportDiagnostics_MapsDiagnostics(t *testing.T) {
	t.Parallel()

	mapped := reportDiagnostics([]report.Diagnostic{
		{
			Code:    "report_conflict_json_precedence",
			Message: "json wins",
			Source:  "canonical",
		},
	})
	require.Len(t, mapped, 1)
	assert.Equal(t, "report_conflict_json_precedence", mapped[0].Code)
	assert.Equal(t, "json wins", mapped[0].Message)
	assert.Equal(t, "canonical", mapped[0].Source)
}

func TestReportDiagnostics_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, reportDiagnostics(nil))
	assert.Nil(t, reportDiagnostics([]report.Diagnostic{}))
}
