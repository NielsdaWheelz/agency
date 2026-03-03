package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
)

// ReportSchemaVersion is the canonical schema version for report.json.
const ReportSchemaVersion = "1.0"

// Source identifies the source artifact that produced the canonical model.
type Source string

const (
	SourceJSON     Source = "report_json"
	SourceMarkdown Source = "report_markdown"
)

// ViolationCode classifies deterministic report contract violations.
type ViolationCode string

const (
	ViolationMissing            ViolationCode = "report_missing"
	ViolationMalformed          ViolationCode = "report_malformed"
	ViolationOversized          ViolationCode = "report_oversized"
	ViolationSchemaIncompatible ViolationCode = "report_schema_incompatible"
	ViolationIncomplete         ViolationCode = "report_incomplete"
)

// CanonicalModel is the normalized report contract consumed by progression flows.
type CanonicalModel struct {
	SchemaVersion string `json:"schema_version"`
	Summary       string `json:"summary"`
	HowToTest     string `json:"how_to_test"`
}

// Diagnostic is an explicit non-fatal report signal.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

// Resolution is the canonical report result when contract validation succeeds.
type Resolution struct {
	Source      Source         `json:"source"`
	Model       CanonicalModel `json:"model"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
}

// Violation is a deterministic report contract failure.
type Violation struct {
	Code    ViolationCode `json:"code"`
	Message string        `json:"message"`
	Source  Source        `json:"source,omitempty"`
	Path    string        `json:"path,omitempty"`
}

// ResolveOptions configures report contract resolution behavior.
type ResolveOptions struct {
	// MaxBytes bounds report artifact reads.
	// Defaults to MaxPRBodyReportBytes when <= 0.
	MaxBytes int
}

type reportJSON struct {
	SchemaVersion string `json:"schema_version"`
	Summary       string `json:"summary"`
	HowToTest     string `json:"how_to_test"`
}

// ResolveCanonicalReport resolves one canonical model from report.json/report.md
// with deterministic precedence and typed contract violations.
//
// Contract:
//   - report.json is authoritative when present.
//   - markdown is deterministic fallback when report.json is absent.
//   - malformed/oversized/schema-incompatible/incomplete artifacts return
//     deterministic violations (no transport errors).
func ResolveCanonicalReport(fsys agencyfs.FS, worktreePath string, opts ResolveOptions) (*Resolution, *Violation, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxPRBodyReportBytes
	}

	jsonPath := filepath.Join(worktreePath, ".agency", "report.json")
	mdPath := filepath.Join(worktreePath, ".agency", "report.md")

	jsonData, jsonExists, jsonOversized, jsonReadErr := ReadFileBounded(fsys, jsonPath, maxBytes)
	if jsonReadErr != nil {
		return nil, &Violation{
			Code:    ViolationMalformed,
			Message: "report.json could not be read",
			Source:  SourceJSON,
			Path:    jsonPath,
		}, nil
	}

	// report.json is authoritative when present.
	if jsonExists {
		if jsonOversized {
			return nil, &Violation{
				Code:    ViolationOversized,
				Message: fmt.Sprintf("report.json exceeds %d bytes", maxBytes),
				Source:  SourceJSON,
				Path:    jsonPath,
			}, nil
		}

		model, violation := parseJSONCanonical(jsonData, jsonPath)
		if violation != nil {
			return nil, violation, nil
		}

		diags := make([]Diagnostic, 0, 2)
		mdData, mdExists, mdOversized, mdReadErr := ReadFileBounded(fsys, mdPath, maxBytes)
		if mdReadErr != nil {
			diags = append(diags, Diagnostic{
				Code:    "report_markdown_unreadable_ignored",
				Message: "report.md exists but could not be read; report.json remains authoritative",
				Source:  string(SourceMarkdown),
			})
		} else if mdExists && mdOversized {
			diags = append(diags, Diagnostic{
				Code:    "report_markdown_oversized_ignored",
				Message: "report.md exceeds size limit and is ignored because report.json is authoritative",
				Source:  string(SourceMarkdown),
			})
		} else if mdExists {
			mdModel, mdViolation := parseMarkdownCanonical(mdData, mdPath)
			if mdViolation != nil {
				diags = append(diags, Diagnostic{
					Code:    "report_markdown_invalid_ignored",
					Message: "report.md is invalid and ignored because report.json is authoritative",
					Source:  string(SourceMarkdown),
				})
			} else if !modelsEquivalent(model, mdModel) {
				diags = append(diags, Diagnostic{
					Code:    "report_conflict_json_precedence",
					Message: "report.json and report.md differ; report.json takes precedence",
					Source:  "canonical",
				})
			}
		}

		return &Resolution{
			Source:      SourceJSON,
			Model:       model,
			Diagnostics: diags,
		}, nil, nil
	}

	mdData, mdExists, mdOversized, mdReadErr := ReadFileBounded(fsys, mdPath, maxBytes)
	if mdReadErr != nil {
		return nil, &Violation{
			Code:    ViolationMalformed,
			Message: "report.md could not be read",
			Source:  SourceMarkdown,
			Path:    mdPath,
		}, nil
	}
	if !mdExists {
		return nil, &Violation{
			Code:    ViolationMissing,
			Message: "no report artifact found (.agency/report.json or .agency/report.md)",
			Path:    mdPath,
		}, nil
	}
	if mdOversized {
		return nil, &Violation{
			Code:    ViolationOversized,
			Message: fmt.Sprintf("report.md exceeds %d bytes", maxBytes),
			Source:  SourceMarkdown,
			Path:    mdPath,
		}, nil
	}

	mdModel, mdViolation := parseMarkdownCanonical(mdData, mdPath)
	if mdViolation != nil {
		return nil, mdViolation, nil
	}

	return &Resolution{
		Source: SourceMarkdown,
		Model:  mdModel,
	}, nil, nil
}

// WriteCanonicalMarkdownBody materializes a deterministic markdown body from the
// canonical model for report.json-driven progression paths.
func WriteCanonicalMarkdownBody(fsys agencyfs.FS, worktreePath, filename string, model CanonicalModel) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "report_v2_body.md"
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("report body filename must be relative")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("report body filename must not contain path separators")
	}
	if clean := filepath.Clean(name); clean != name || name == "." || name == ".." {
		return "", fmt.Errorf("report body filename is invalid")
	}

	bodyPath := filepath.Join(worktreePath, ".agency", "tmp", name)
	bodyDir := filepath.Dir(bodyPath)
	if err := fsys.MkdirAll(bodyDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create report body directory: %w", err)
	}
	if err := fsys.Chmod(bodyDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to set report body directory permissions: %w", err)
	}

	content := "## summary\n" +
		strings.TrimSpace(model.Summary) +
		"\n\n## how to test\n" +
		strings.TrimSpace(model.HowToTest) +
		"\n"
	if err := fsys.WriteFile(bodyPath, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("failed to write canonical report body: %w", err)
	}
	if err := fsys.Chmod(bodyPath, 0o600); err != nil {
		return "", fmt.Errorf("failed to set canonical report body permissions: %w", err)
	}

	return bodyPath, nil
}

func parseJSONCanonical(data []byte, jsonPath string) (CanonicalModel, *Violation) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var parsed reportJSON
	if err := dec.Decode(&parsed); err != nil {
		return CanonicalModel{}, &Violation{
			Code:    ViolationMalformed,
			Message: "report.json is malformed JSON",
			Source:  SourceJSON,
			Path:    jsonPath,
		}
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return CanonicalModel{}, &Violation{
			Code:    ViolationMalformed,
			Message: "report.json must contain exactly one JSON object",
			Source:  SourceJSON,
			Path:    jsonPath,
		}
	}

	if strings.TrimSpace(parsed.SchemaVersion) != ReportSchemaVersion {
		return CanonicalModel{}, &Violation{
			Code:    ViolationSchemaIncompatible,
			Message: fmt.Sprintf("report.json schema_version must be %s", ReportSchemaVersion),
			Source:  SourceJSON,
			Path:    jsonPath,
		}
	}

	missing := make([]string, 0, 2)
	summary := strings.TrimSpace(parsed.Summary)
	howToTest := strings.TrimSpace(parsed.HowToTest)
	if summary == "" {
		missing = append(missing, "summary")
	}
	if howToTest == "" {
		missing = append(missing, "how_to_test")
	}
	if len(missing) > 0 {
		return CanonicalModel{}, &Violation{
			Code:    ViolationIncomplete,
			Message: "report.json missing required fields: " + strings.Join(missing, ", "),
			Source:  SourceJSON,
			Path:    jsonPath,
		}
	}

	return CanonicalModel{
		SchemaVersion: ReportSchemaVersion,
		Summary:       summary,
		HowToTest:     howToTest,
	}, nil
}

func parseMarkdownCanonical(data []byte, markdownPath string) (CanonicalModel, *Violation) {
	content := string(data)
	sections := parseSections(content)

	summary := strings.TrimSpace(getSectionContent(sections, "summary"))
	howToTest := strings.TrimSpace(getSectionContent(sections, "how to test"))
	missing := make([]string, 0, 2)
	if summary == "" {
		missing = append(missing, "summary")
	}
	if howToTest == "" {
		missing = append(missing, "how to test")
	}
	if len(missing) > 0 {
		return CanonicalModel{}, &Violation{
			Code:    ViolationIncomplete,
			Message: "report.md missing required sections: " + strings.Join(missing, ", "),
			Source:  SourceMarkdown,
			Path:    markdownPath,
		}
	}

	return CanonicalModel{
		SchemaVersion: ReportSchemaVersion,
		Summary:       summary,
		HowToTest:     howToTest,
	}, nil
}

func modelsEquivalent(a, b CanonicalModel) bool {
	return normalizeModelField(a.Summary) == normalizeModelField(b.Summary) &&
		normalizeModelField(a.HowToTest) == normalizeModelField(b.HowToTest)
}

func normalizeModelField(value string) string {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
