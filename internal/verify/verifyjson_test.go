package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVerifyJSON_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	result := ReadVerifyJSON(path)

	if result.Exists {
		t.Errorf("Exists = true, want false for missing file")
	}
	if result.VJ != nil {
		t.Errorf("VJ = %v, want nil for missing file", result.VJ)
	}
	if result.Err != nil {
		t.Errorf("Err = %v, want nil for missing file", result.Err)
	}
}

func TestReadVerifyJSON_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write invalid JSON
	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if !result.Exists {
		t.Errorf("Exists = false, want true for existing file")
	}
	if result.VJ != nil {
		t.Errorf("VJ = %v, want nil for invalid JSON", result.VJ)
	}
	if result.Err == nil {
		t.Errorf("Err = nil, want non-nil for invalid JSON")
	}
}

func TestReadVerifyJSON_MissingSchemaVersion(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON without schema_version
	if err := os.WriteFile(path, []byte(`{"ok": true}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if !result.Exists {
		t.Errorf("Exists = false, want true for existing file")
	}
	if result.VJ != nil {
		t.Errorf("VJ = %v, want nil for missing schema_version", result.VJ)
	}
	if result.Err == nil {
		t.Errorf("Err = nil, want non-nil for missing schema_version")
	}
	if result.Err != nil && result.Err.Error() != "verify.json: schema_version is required and must be non-empty" {
		t.Errorf("Err = %q, want specific schema_version error", result.Err.Error())
	}
}

func TestReadVerifyJSON_EmptySchemaVersion(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON with empty schema_version
	if err := os.WriteFile(path, []byte(`{"schema_version": "", "ok": true}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if !result.Exists {
		t.Errorf("Exists = false, want true for existing file")
	}
	if result.VJ != nil {
		t.Errorf("VJ = %v, want nil for empty schema_version", result.VJ)
	}
	if result.Err == nil {
		t.Errorf("Err = nil, want non-nil for empty schema_version")
	}
}

func TestReadVerifyJSON_ValidMinimal(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write minimal valid JSON
	if err := os.WriteFile(path, []byte(`{"schema_version": "1.0", "ok": true}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if !result.Exists {
		t.Errorf("Exists = false, want true")
	}
	if result.VJ == nil {
		t.Fatalf("VJ = nil, want non-nil for valid JSON")
	}
	if result.Err != nil {
		t.Errorf("Err = %v, want nil for valid JSON", result.Err)
	}
	if result.VJ.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want \"1.0\"", result.VJ.SchemaVersion)
	}
	if !result.VJ.OK {
		t.Errorf("OK = false, want true")
	}
}

func TestReadVerifyJSON_ValidFull(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write full valid JSON
	content := `{
		"schema_version": "1.0",
		"ok": false,
		"summary": "3 tests failed",
		"data": {"failures": ["test_a", "test_b", "test_c"]}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if !result.Exists {
		t.Errorf("Exists = false, want true")
	}
	if result.VJ == nil {
		t.Fatalf("VJ = nil, want non-nil for valid JSON")
	}
	if result.Err != nil {
		t.Errorf("Err = %v, want nil for valid JSON", result.Err)
	}
	if result.VJ.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want \"1.0\"", result.VJ.SchemaVersion)
	}
	if result.VJ.OK {
		t.Errorf("OK = true, want false")
	}
	if result.VJ.Summary != "3 tests failed" {
		t.Errorf("Summary = %q, want \"3 tests failed\"", result.VJ.Summary)
	}
	if result.VJ.Data == nil {
		t.Errorf("Data = nil, want non-nil")
	}
}

func TestReadVerifyJSON_OKFalse(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON with ok=false (explicit)
	if err := os.WriteFile(path, []byte(`{"schema_version": "1.0", "ok": false}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if !result.Exists {
		t.Errorf("Exists = false, want true")
	}
	if result.VJ == nil {
		t.Fatalf("VJ = nil, want non-nil for valid JSON")
	}
	if result.VJ.OK {
		t.Errorf("OK = true, want false")
	}
}

func TestReadVerifyJSON_TolerateMissingSummary(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON without summary (should be tolerated)
	if err := os.WriteFile(path, []byte(`{"schema_version": "1.0", "ok": true}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if result.VJ == nil {
		t.Fatalf("VJ = nil, want non-nil")
	}
	if result.VJ.Summary != "" {
		t.Errorf("Summary = %q, want empty string", result.VJ.Summary)
	}
}

func TestReadVerifyJSON_TolerateMissingData(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON without data (should be tolerated)
	if err := os.WriteFile(path, []byte(`{"schema_version": "1.0", "ok": true, "summary": "passed"}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if result.VJ == nil {
		t.Fatalf("VJ = nil, want non-nil")
	}
	if result.VJ.Data != nil {
		t.Errorf("Data = %v, want nil", result.VJ.Data)
	}
}

func TestReadVerifyJSON_ExtraFieldsIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON with extra unknown fields (should be ignored)
	content := `{"schema_version": "1.0", "ok": true, "extra_field": "ignored", "nested": {"foo": "bar"}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result := ReadVerifyJSON(path)

	if result.VJ == nil {
		t.Fatalf("VJ = nil, want non-nil")
	}
	if !result.VJ.OK {
		t.Errorf("OK = false, want true")
	}
}
