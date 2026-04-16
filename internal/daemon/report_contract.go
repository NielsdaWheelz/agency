package daemon

import (
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/report"
)

func reportViolationToAgencyError(violation *report.Violation) error {
	if violation == nil {
		return nil
	}

	code := errors.EReportInvalid
	hint := "update .agency/report.json or .agency/report.md with valid summary/how_to_test content"
	switch violation.Code {
	case report.ViolationMissing:
		code = errors.EReportMissing
		hint = "create .agency/report.json or .agency/report.md before progression"
	case report.ViolationMalformed:
		code = errors.EReportMalformed
		hint = "fix malformed report syntax (JSON/markdown) or remove invalid report.json"
	case report.ViolationOversized:
		code = errors.EReportOversized
		hint = fmt.Sprintf("reduce report artifact size below %d bytes", report.MaxPRBodyReportBytes)
	case report.ViolationSchemaIncompatible:
		code = errors.EReportSchemaIncompatible
		hint = fmt.Sprintf("set report.json schema_version to %s", report.ReportSchemaVersion)
	case report.ViolationIncomplete:
		code = errors.EReportIncomplete
		hint = "fill summary and how to test fields in report artifact"
	}

	details := map[string]string{
		"hint":             hint,
		"report_violation": string(violation.Code),
	}
	if violation.Path != "" {
		details["path"] = violation.Path
	}
	if violation.Source != "" {
		details["source"] = string(violation.Source)
	}

	return errors.NewWithDetails(code, violation.Message, details)
}

func reportViolationToCheckReason(violation *report.Violation) InvocationCheckReason {
	if violation == nil {
		return InvocationCheckReason{}
	}

	hint := "update report artifacts before running pr sync/merge"
	switch violation.Code {
	case report.ViolationMissing:
		hint = "create .agency/report.json or .agency/report.md"
	case report.ViolationMalformed:
		hint = "fix malformed report syntax (JSON/markdown)"
	case report.ViolationOversized:
		hint = fmt.Sprintf("reduce report size below %d bytes", report.MaxPRBodyReportBytes)
	case report.ViolationSchemaIncompatible:
		hint = fmt.Sprintf("set report.json schema_version to %s", report.ReportSchemaVersion)
	case report.ViolationIncomplete:
		hint = "fill summary and how to test sections"
	}
	return InvocationCheckReason{
		Code:    string(violation.Code),
		Message: violation.Message,
		Hint:    hint,
	}
}

func reportDiagnostics(diags []report.Diagnostic) []ReportDiagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]ReportDiagnostic, 0, len(diags))
	for _, d := range diags {
		out = append(out, ReportDiagnostic{
			Code:    d.Code,
			Message: d.Message,
			Source:  d.Source,
		})
	}
	return out
}
