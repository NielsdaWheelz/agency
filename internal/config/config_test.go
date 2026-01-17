package config

import (
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	stub := newStubFS()
	_, err := LoadAgencyConfig(stub, "/repo")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if errors.GetCode(err) != errors.ENoAgencyJSON {
		t.Errorf("expected E_NO_AGENCY_JSON, got %s", errors.GetCode(err))
	}
}

func TestLoadAgencyConfig_InvalidJSON(t *testing.T) {
	stub := newStubFS()
	stub.files["/repo/agency.json"] = []byte(`{"version": 1, "scripts": {`)
	_, err := LoadAgencyConfig(stub, "/repo")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if errors.GetCode(err) != errors.EInvalidAgencyJSON {
		t.Errorf("expected E_INVALID_AGENCY_JSON, got %s", errors.GetCode(err))
	}
	if !strings.Contains(err.Error(), "invalid json") {
		t.Errorf("error should contain 'invalid json': %s", err.Error())
	}
}

func TestLoadAgencyConfig_ValidMinimal(t *testing.T) {
	stub := newStubFS()
	data, err := os.ReadFile("testdata/valid_min.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Scripts.Setup != "scripts/agency_setup.sh" {
		t.Errorf("Scripts.Setup = %q, want %q", cfg.Scripts.Setup, "scripts/agency_setup.sh")
	}
	if cfg.Scripts.Verify != "scripts/agency_verify.sh" {
		t.Errorf("Scripts.Verify = %q, want %q", cfg.Scripts.Verify, "scripts/agency_verify.sh")
	}
	if cfg.Scripts.Archive != "scripts/agency_archive.sh" {
		t.Errorf("Scripts.Archive = %q, want %q", cfg.Scripts.Archive, "scripts/agency_archive.sh")
	}
}

func TestLoadAgencyConfig_WrongTypes(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wantMsg string
	}{
		{"scripts as array", "wrong_types_scripts.json", "scripts must be an object"},
		{"script verify as object", "wrong_types_script_verify.json", "scripts.verify must be a string"},
		{"version as string", "wrong_version_string.json", "version must be an integer"},
		{"version as float", "wrong_version_float.json", "version must be an integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("failed to read fixture: %v", err)
			}
			stub := newStubFS()
			stub.files["/repo/agency.json"] = data

			_, err = LoadAgencyConfig(stub, "/repo")
			if err == nil {
				t.Fatal("expected error")
			}
			if errors.GetCode(err) != errors.EInvalidAgencyJSON {
				t.Errorf("expected E_INVALID_AGENCY_JSON, got %s", errors.GetCode(err))
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error should contain %q: %s", tt.wantMsg, err.Error())
			}
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
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("failed to read fixture: %v", err)
			}
			stub := newStubFS()
			stub.files["/repo/agency.json"] = data

			cfg, err := LoadAgencyConfig(stub, "/repo")
			if err != nil {
				t.Fatalf("load error: %v", err)
			}

			_, err = ValidateAgencyConfig(cfg)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if errors.GetCode(err) != errors.EInvalidAgencyJSON {
				t.Errorf("expected E_INVALID_AGENCY_JSON, got %s", errors.GetCode(err))
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error should contain %q: %s", tt.wantMsg, err.Error())
			}
		})
	}
}

func TestValidateAgencyConfig_WrongVersion(t *testing.T) {
	data, err := os.ReadFile("testdata/wrong_version.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	_, err = ValidateAgencyConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "version must be 1") {
		t.Errorf("error should contain 'version must be 1': %s", err.Error())
	}
}

func TestValidateAgencyConfig_UnknownKeys(t *testing.T) {
	data, err := os.ReadFile("testdata/unknown_keys.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	_, err = LoadAgencyConfig(stub, "/repo")
	if err == nil {
		t.Fatal("expected error for unknown keys")
	}
	if errors.GetCode(err) != errors.EInvalidAgencyJSON {
		t.Errorf("expected E_INVALID_AGENCY_JSON, got %s", errors.GetCode(err))
	}
}

func TestFirstValidationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"agency error", errors.New(errors.EInvalidAgencyJSON, "test message"), "test message"},
		{"plain error", os.ErrNotExist, "file does not exist"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstValidationError(tt.err)
			if got != tt.want {
				t.Errorf("FirstValidationError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFirstValidationError_Stability(t *testing.T) {
	testCases := []struct {
		fixture string
		wantMsg string
	}{
		{"missing_scripts.json", "missing required field scripts.setup"},
		{"wrong_version.json", "version must be 1"},
	}

	for _, tc := range testCases {
		t.Run(tc.fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatalf("failed to read fixture: %v", err)
			}
			stub := newStubFS()
			stub.files["/repo/agency.json"] = data

			cfg, err := LoadAgencyConfig(stub, "/repo")
			if err != nil {
				t.Fatalf("load error: %v", err)
			}

			_, err = ValidateAgencyConfig(cfg)
			if err == nil {
				t.Fatal("expected validation error")
			}

			msg := FirstValidationError(err)
			if msg != tc.wantMsg {
				t.Errorf("FirstValidationError() = %q, want %q", msg, tc.wantMsg)
			}
		})
	}
}

func TestContainsWhitespace(t *testing.T) {
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
		t.Run(tt.input, func(t *testing.T) {
			got := containsWhitespace(tt.input)
			if got != tt.want {
				t.Errorf("containsWhitespace(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// S1-specific validation tests

func TestValidateForS1_SetupOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/s1_valid_setup_only.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	_, err = ValidateForS1(cfg)
	if err != nil {
		t.Fatalf("S1 validation should pass with setup only: %v", err)
	}
}

func TestValidateForS1_FullConfig(t *testing.T) {
	data, err := os.ReadFile("testdata/valid_min.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	validated, err := ValidateForS1(cfg)
	if err != nil {
		t.Fatalf("S1 validation should pass with full config: %v", err)
	}
	if validated.Scripts.Setup != "scripts/agency_setup.sh" {
		t.Errorf("Scripts.Setup = %q, want %q", validated.Scripts.Setup, "scripts/agency_setup.sh")
	}
}

func TestValidateForS1_MissingSetup(t *testing.T) {
	data, err := os.ReadFile("testdata/missing_script_setup.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	_, err = ValidateForS1(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing setup")
	}
	if errors.GetCode(err) != errors.EInvalidAgencyJSON {
		t.Errorf("expected E_INVALID_AGENCY_JSON, got %s", errors.GetCode(err))
	}
	if !strings.Contains(err.Error(), "scripts.setup") {
		t.Errorf("error should mention scripts.setup: %s", err.Error())
	}
}

func TestLoadAndValidateForS1(t *testing.T) {
	data, err := os.ReadFile("testdata/s1_valid_setup_only.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	_, err = LoadAndValidateForS1(stub, "/repo")
	if err != nil {
		t.Fatalf("LoadAndValidateForS1 error: %v", err)
	}
}

func TestLoadAndValidateForS1_MissingFile(t *testing.T) {
	stub := newStubFS()
	_, err := LoadAndValidateForS1(stub, "/repo")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if errors.GetCode(err) != errors.ENoAgencyJSON {
		t.Errorf("expected E_NO_AGENCY_JSON, got %s", errors.GetCode(err))
	}
}

// Integration test using real filesystem
func TestLoadAgencyConfig_RealFS(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `{
  "version": 1,
  "scripts": {
    "setup": "scripts/setup.sh",
    "verify": "scripts/verify.sh",
    "archive": "scripts/archive.sh"
  }
}`

	err := os.WriteFile(filepath.Join(tmpDir, "agency.json"), []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	realFS := fs.NewRealFS()
	cfg, err := LoadAgencyConfig(realFS, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Scripts.Setup != "scripts/setup.sh" {
		t.Errorf("Scripts.Setup = %q, want %q", cfg.Scripts.Setup, "scripts/setup.sh")
	}
}
