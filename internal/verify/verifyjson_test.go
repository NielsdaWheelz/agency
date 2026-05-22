package verify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadVerifyJSON_MissingFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	result := readVerifyJSON(path)

	assert.False(t, result.exists, "exists = true, want false for missing file")
	assert.Nil(t, result.value, "value should be nil for missing file")
	assert.NoError(t, result.err, "err should be nil for missing file")
}

func TestReadVerifyJSON_InvalidJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write invalid JSON
	require.NoError(t, os.WriteFile(path, []byte("not valid json"), 0o644))

	result := readVerifyJSON(path)

	assert.True(t, result.exists, "exists = false, want true for existing file")
	assert.Nil(t, result.value, "value should be nil for invalid JSON")
	require.Error(t, result.err, "err should be non-nil for invalid JSON")
}

func TestReadVerifyJSON_MissingSchemaVersion(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON without schema_version
	require.NoError(t, os.WriteFile(path, []byte(`{"ok": true}`), 0o644))

	result := readVerifyJSON(path)

	assert.True(t, result.exists, "exists = false, want true for existing file")
	assert.Nil(t, result.value, "value should be nil for missing schema_version")
	require.Error(t, result.err, "err should be non-nil for missing schema_version")
	assert.Equal(t, "verify.json: schema_version is required and must be non-empty", result.err.Error())
}

func TestReadVerifyJSON_ValidMinimal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write minimal valid JSON
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version": "1.0", "ok": true}`), 0o644))

	result := readVerifyJSON(path)

	assert.True(t, result.exists, "exists = false, want true")
	require.NotNil(t, result.value, "value should be non-nil for valid JSON")
	assert.NoError(t, result.err, "err should be nil for valid JSON")
	assert.Equal(t, "1.0", result.value.SchemaVersion)
	assert.True(t, result.value.OK, "ok = false, want true")
}

func TestReadVerifyJSON_ValidFull(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write full valid JSON
	content := `{
		"schema_version": "1.0",
		"ok": false,
		"summary": "3 tests failed",
		"data": {"failures": ["test_a", "test_b", "test_c"]}
	}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	result := readVerifyJSON(path)

	assert.True(t, result.exists, "exists = false, want true")
	require.NotNil(t, result.value, "value should be non-nil for valid JSON")
	assert.NoError(t, result.err, "err should be nil for valid JSON")
	assert.Equal(t, "1.0", result.value.SchemaVersion)
	assert.False(t, result.value.OK, "ok = true, want false")
	assert.Equal(t, "3 tests failed", result.value.Summary)
}

func TestReadVerifyJSON_OKFalse(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON with ok=false (explicit)
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version": "1.0", "ok": false}`), 0o644))

	result := readVerifyJSON(path)

	assert.True(t, result.exists, "exists = false, want true")
	require.NotNil(t, result.value, "value should be non-nil for valid JSON")
	assert.False(t, result.value.OK, "ok = true, want false")
}

func TestReadVerifyJSON_ExtraFieldsIgnored(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "verify.json")

	// Write JSON with extra unknown fields (should be ignored)
	content := `{"schema_version": "1.0", "ok": true, "extra_field": "ignored", "nested": {"foo": "bar"}}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	result := readVerifyJSON(path)

	require.NotNil(t, result.value, "value should be non-nil")
	assert.True(t, result.value.OK, "ok = false, want true")
}
