package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCanonicalReport_JSONAuthoritativeWhenConflict(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{
  "schema_version": "1.0",
  "summary": "json summary",
  "how_to_test": "go test ./..."
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
markdown summary

## how to test
go test ./internal/...
`), 0o644))

	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, violation)
	require.NotNil(t, result)
	assert.Equal(t, SourceJSON, result.Source)
	assert.Equal(t, "json summary", result.Model.Summary)
	assert.Equal(t, "go test ./...", result.Model.HowToTest)
	assert.Contains(t, diagnosticCodes(result.Diagnostics), "report_conflict_json_precedence")
}

func TestResolveCanonicalReport_ParityAcrossJSONAndMarkdown(t *testing.T) {
	t.Parallel()

	makeJSON := func(t *testing.T) string {
		t.Helper()
		worktree := t.TempDir()
		agencyDir := filepath.Join(worktree, ".agency")
		require.NoError(t, os.MkdirAll(agencyDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{
  "schema_version": "1.0",
  "summary": "stable summary",
  "how_to_test": "go test ./..."
}`), 0o644))
		return worktree
	}
	makeMarkdown := func(t *testing.T) string {
		t.Helper()
		worktree := t.TempDir()
		agencyDir := filepath.Join(worktree, ".agency")
		require.NoError(t, os.MkdirAll(agencyDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
stable summary

## how to test
go test ./...
`), 0o644))
		return worktree
	}

	jsonResult, jsonViolation, err := ResolveCanonicalReport(fs.NewRealFS(), makeJSON(t), ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, jsonViolation)
	require.NotNil(t, jsonResult)

	mdResult, mdViolation, err := ResolveCanonicalReport(fs.NewRealFS(), makeMarkdown(t), ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, mdViolation)
	require.NotNil(t, mdResult)

	assert.Equal(t, jsonResult.Model, mdResult.Model)
}

func TestResolveCanonicalReport_MissingBothReturnsMissingViolation(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, violation)
	assert.Equal(t, ViolationMissing, violation.Code)
}

func TestResolveCanonicalReport_JSONMalformedIsStrictViolation(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{"schema_version":"1.0","summary":`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
valid markdown

## how to test
go test ./...
`), 0o644))

	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, violation)
	assert.Equal(t, ViolationMalformed, violation.Code)
}

func TestResolveCanonicalReport_JSONSchemaMismatchReturnsSchemaViolation(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{
  "schema_version": "9.9",
  "summary": "summary",
  "how_to_test": "go test ./..."
}`), 0o644))

	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, violation)
	assert.Equal(t, ViolationSchemaIncompatible, violation.Code)
}

func TestResolveCanonicalReport_OversizedJSONReturnsOversizedViolation(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), bytes.Repeat([]byte("x"), 2048), 0o644))

	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{MaxBytes: 1024})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, violation)
	assert.Equal(t, ViolationOversized, violation.Code)
}

func TestResolveCanonicalReport_MarkdownOnlyComplete(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
markdown summary

## how to test
go test ./...
`), 0o644))

	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, violation)
	require.NotNil(t, result)
	assert.Equal(t, SourceMarkdown, result.Source)
	assert.Equal(t, "markdown summary", result.Model.Summary)
	assert.Equal(t, "go test ./...", result.Model.HowToTest)
}

func TestResolveCanonicalReport_MarkdownIncompleteReturnsIncompleteViolation(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
only summary is present
`), 0o644))

	result, violation, err := ResolveCanonicalReport(fs.NewRealFS(), worktree, ResolveOptions{})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, violation)
	assert.Equal(t, ViolationIncomplete, violation.Code)
}

func TestWriteCanonicalMarkdownBody(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	realFS := fs.NewRealFS()

	path, err := WriteCanonicalMarkdownBody(realFS, worktree, "canonical_body.md", CanonicalModel{
		SchemaVersion: "1.0",
		Summary:       "json summary",
		HowToTest:     "go test ./...",
	})
	require.NoError(t, err)

	content, err := realFS.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "## summary")
	assert.Contains(t, string(content), "json summary")
	assert.Contains(t, string(content), "## how to test")
	assert.Contains(t, string(content), "go test ./...")
}

func TestWriteCanonicalMarkdownBody_RejectsInvalidFilename(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	realFS := fs.NewRealFS()
	model := CanonicalModel{
		SchemaVersion: "1.0",
		Summary:       "summary",
		HowToTest:     "go test ./...",
	}

	invalidNames := []string{
		"../escape.md",
		"nested/escape.md",
		"nested\\escape.md",
		".",
		"..",
		filepath.Join(string(filepath.Separator), "tmp", "escape.md"),
	}

	for _, name := range invalidNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path, err := WriteCanonicalMarkdownBody(realFS, worktree, name, model)
			require.Error(t, err)
			assert.Empty(t, path)
		})
	}
}

func diagnosticCodes(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.Code != "" {
			out = append(out, d.Code)
		}
	}
	return out
}
