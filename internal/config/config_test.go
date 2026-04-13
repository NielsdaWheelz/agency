package config

import (
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// stubFS is a test stub for the fs.FS interface.
type stubFS struct {
	files map[string][]byte
}

func newStubFS() *stubFS {
	return &stubFS{files: make(map[string][]byte)}
}

func (s *stubFS) ReadFile(path string) ([]byte, error) {
	data, ok := s.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (s *stubFS) MkdirAll(path string, perm os.FileMode) error         { return nil }
func (s *stubFS) WriteFile(path string, d []byte, p os.FileMode) error { return nil }
func (s *stubFS) Stat(path string) (iofs.FileInfo, error)              { return nil, nil }
func (s *stubFS) Rename(o, n string) error                             { return nil }
func (s *stubFS) Remove(path string) error                             { return nil }
func (s *stubFS) Chmod(path string, perm os.FileMode) error            { return nil }
func (s *stubFS) CreateTemp(dir, pattern string) (string, io.WriteCloser, error) {
	return "", nil, nil
}

// Verify stubFS implements fs.FS interface (compile-time check)
var _ fs.FS = (*stubFS)(nil)

func TestLoadAgencyConfig_MissingFile(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	_, err := LoadAgencyConfig(stub, "/repo")
	require.Error(t, err, "expected error for missing file")
	assert.Equal(t, errors.ENoAgencyJSON, errors.GetCode(err))
}

func TestLoadAgencyConfig_InvalidJSON(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/repo/agency.json"] = []byte(`{"version": 1, "scripts": {`)
	_, err := LoadAgencyConfig(stub, "/repo")
	require.Error(t, err, "expected error for invalid JSON")
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
	assert.Contains(t, err.Error(), "invalid json")
}

func TestLoadAgencyConfig_ValidMinimal(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	data, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read fixture")
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, "scripts/agency_setup.sh", cfg.Scripts.Setup.Path)
	assert.Equal(t, 10*time.Minute, cfg.Scripts.Setup.Timeout)
	assert.Equal(t, "scripts/agency_verify.sh", cfg.Scripts.Verify.Path)
	assert.Equal(t, 30*time.Minute, cfg.Scripts.Verify.Timeout)
	assert.Equal(t, "scripts/agency_archive.sh", cfg.Scripts.Archive.Path)
	assert.Equal(t, 5*time.Minute, cfg.Scripts.Archive.Timeout)
}

func TestLoadAgencyConfig_WrongTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fixture string
		wantMsg string
	}{
		{"scripts as array", "wrong_types_scripts.json", "scripts must be an object"},
		{"script verify missing path", "wrong_types_script_verify.json", "scripts.verify missing required field 'path'"},
		{"version as string", "wrong_version_string.json", "version must be an integer"},
		{"version as float", "wrong_version_float.json", "version must be an integer"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			require.NoError(t, err, "failed to read fixture")
			stub := newStubFS()
			stub.files["/repo/agency.json"] = data

			_, err = LoadAgencyConfig(stub, "/repo")
			require.Error(t, err)
			assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateAgencyConfig_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wantMsg string
	}{
		{"missing scripts", "missing_scripts.json", "missing required field scripts.setup"},
		{"missing script setup", "missing_script_setup.json", "missing required field scripts.setup"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			require.NoError(t, err, "failed to read fixture")
			stub := newStubFS()
			stub.files["/repo/agency.json"] = data

			cfg, err := LoadAgencyConfig(stub, "/repo")
			require.NoError(t, err, "load error")

			_, err = ValidateAgencyConfig(cfg)
			require.Error(t, err, "expected validation error")
			assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateAgencyConfig_WrongVersion(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/wrong_version.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	require.NoError(t, err, "load error")

	_, err = ValidateAgencyConfig(cfg)
	require.Error(t, err, "expected validation error")
	assert.Contains(t, err.Error(), "version must be 1")
}

func TestValidateAgencyConfig_UnknownKeys(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/unknown_keys.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	_, err = LoadAgencyConfig(stub, "/repo")
	require.Error(t, err, "expected error for unknown keys")
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
}

func TestContainsWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"claude", false},
		{"path/to/runner", false},
		{"asdf exec claude", true},
		{"cmd\targ", true},
		{"cmd\narg", true},
		{"", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := containsWhitespace(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// S1-specific validation tests

func TestValidateForS1_SetupOnly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/s1_valid_setup_only.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	require.NoError(t, err, "load error")

	_, err = ValidateForS1(cfg)
	require.NoError(t, err, "S1 validation should pass with setup only")
}

func TestValidateForS1_FullConfig(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	require.NoError(t, err, "load error")

	validated, err := ValidateForS1(cfg)
	require.NoError(t, err, "S1 validation should pass with full config")
	assert.Equal(t, "scripts/agency_setup.sh", validated.Scripts.Setup.Path)
}

func TestValidateForS1_MissingSetup(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/missing_script_setup.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	require.NoError(t, err, "load error")

	_, err = ValidateForS1(cfg)
	require.Error(t, err, "expected validation error for missing setup")
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
	assert.Contains(t, err.Error(), "scripts.setup")
}

func TestLoadAndValidateForS1(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/s1_valid_setup_only.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	_, err = LoadAndValidateForS1(stub, "/repo")
	require.NoError(t, err, "LoadAndValidateForS1 error")
}

func TestLoadAndValidateForS1_MissingFile(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	_, err := LoadAndValidateForS1(stub, "/repo")
	require.Error(t, err, "expected error for missing file")
	assert.Equal(t, errors.ENoAgencyJSON, errors.GetCode(err))
}

// Integration test using real filesystem
func TestLoadAgencyConfig_RealFS(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	configContent := `{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/archive.sh",
      "timeout": "5m"
    }
  }
}`

	err := os.WriteFile(filepath.Join(tmpDir, "agency.json"), []byte(configContent), 0644)
	require.NoError(t, err, "failed to write test file")

	realFS := fs.NewRealFS()
	cfg, err := LoadAgencyConfig(realFS, tmpDir)
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, "scripts/setup.sh", cfg.Scripts.Setup.Path)
	assert.Equal(t, 10*time.Minute, cfg.Scripts.Setup.Timeout)
}
