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
	stub.files["/repo/agency.json"] = []byte(`{"version": 1, "defaults": {`)
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
	if cfg.Defaults.ParentBranch != "main" {
		t.Errorf("ParentBranch = %q, want %q", cfg.Defaults.ParentBranch, "main")
	}
	if cfg.Defaults.Runner != "claude" {
		t.Errorf("Runner = %q, want %q", cfg.Defaults.Runner, "claude")
	}
}

func TestLoadAgencyConfig_WrongTypes(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		wantMsg  string
	}{
		{"defaults as string", "wrong_types.json", "defaults must be an object"},
		{"scripts as array", "wrong_types_scripts.json", "scripts must be an object"},
		{"script verify as object", "wrong_types_script_verify.json", "scripts.verify must be a string"},
		{"runners as array", "wrong_types_runners.json", "runners must be an object"},
		{"runner value as number", "wrong_types_runner_value.json", "runners.claude must be a string"},
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
		{"missing parent_branch", "missing_parent_branch.json", "missing required field defaults.parent_branch"},
		{"missing runner", "missing_runner.json", "missing required field defaults.runner"},
		{"missing scripts", "missing_scripts.json", "missing required field scripts.setup"},
		{"missing script setup", "missing_script_setup.json", "missing required field scripts.setup"},
		{"empty parent_branch", "empty_strings.json", "missing required field defaults.parent_branch"},
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

func TestValidateAgencyConfig_EmptyRunnerValue(t *testing.T) {
	data, err := os.ReadFile("testdata/empty_runner_value.json")
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
	if !strings.Contains(err.Error(), "runners.claude must be a non-empty string") {
		t.Errorf("error should mention empty runner value: %s", err.Error())
	}
}

func TestValidateAgencyConfig_RunnerWithArgs(t *testing.T) {
	data, err := os.ReadFile("testdata/runner_with_args.json")
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
		t.Fatal("expected validation error for runner with args")
	}
	if errors.GetCode(err) != errors.EInvalidAgencyJSON {
		t.Errorf("expected E_INVALID_AGENCY_JSON, got %s", errors.GetCode(err))
	}
	if !strings.Contains(err.Error(), "single executable") {
		t.Errorf("error should mention 'single executable': %s", err.Error())
	}
}

func TestValidateAgencyConfig_UnknownKeys(t *testing.T) {
	data, err := os.ReadFile("testdata/unknown_keys.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	validated, err := ValidateAgencyConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if validated.Defaults.ParentBranch != "main" {
		t.Errorf("ParentBranch = %q, want %q", validated.Defaults.ParentBranch, "main")
	}
}

func TestRunnerResolution_Claude(t *testing.T) {
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

	validated, err := ValidateAgencyConfig(cfg)
	if err != nil {
		t.Fatalf("validation error: %v", err)
	}
	if validated.ResolvedRunnerCmd != "claude" {
		t.Errorf("ResolvedRunnerCmd = %q, want %q", validated.ResolvedRunnerCmd, "claude")
	}
}

func TestRunnerResolution_Codex(t *testing.T) {
	data, err := os.ReadFile("testdata/codex_runner.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	validated, err := ValidateAgencyConfig(cfg)
	if err != nil {
		t.Fatalf("validation error: %v", err)
	}
	if validated.ResolvedRunnerCmd != "codex" {
		t.Errorf("ResolvedRunnerCmd = %q, want %q", validated.ResolvedRunnerCmd, "codex")
	}
}

func TestRunnerResolution_CustomOk(t *testing.T) {
	data, err := os.ReadFile("testdata/runner_custom_ok.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	validated, err := ValidateAgencyConfig(cfg)
	if err != nil {
		t.Fatalf("validation error: %v", err)
	}
	if validated.ResolvedRunnerCmd != "path/to/custom-runner" {
		t.Errorf("ResolvedRunnerCmd = %q, want %q", validated.ResolvedRunnerCmd, "path/to/custom-runner")
	}
}

func TestRunnerResolution_CustomMissing(t *testing.T) {
	data, err := os.ReadFile("testdata/runner_custom_missing.json")
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
		t.Fatal("expected error for unconfigured custom runner")
	}
	if errors.GetCode(err) != errors.ERunnerNotConfigured {
		t.Errorf("expected E_RUNNER_NOT_CONFIGURED, got %s", errors.GetCode(err))
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("error should mention runner name: %s", err.Error())
	}
}

func TestRunnerResolution_MapOverridesDefault(t *testing.T) {
	// When runners.claude is set, it should override the default PATH lookup
	data, err := os.ReadFile("testdata/valid_full.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	validated, err := ValidateAgencyConfig(cfg)
	if err != nil {
		t.Fatalf("validation error: %v", err)
	}
	// runners.claude = "claude" should be used
	if validated.ResolvedRunnerCmd != "claude" {
		t.Errorf("ResolvedRunnerCmd = %q, want %q", validated.ResolvedRunnerCmd, "claude")
	}
}

func TestFirstValidationError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    string
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
	// This test ensures error message format stability for doctor output
	testCases := []struct {
		fixture string
		wantMsg string
	}{
		{"missing_parent_branch.json", "missing required field defaults.parent_branch"},
		{"missing_runner.json", "missing required field defaults.runner"},
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

// Integration test using real filesystem
func TestLoadAgencyConfig_RealFS(t *testing.T) {
	// Create temp directory with agency.json
	tmpDir := t.TempDir()
	
	configContent := `{
  "version": 1,
  "defaults": {
    "parent_branch": "main",
    "runner": "claude"
  },
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
	if cfg.Defaults.ParentBranch != "main" {
		t.Errorf("ParentBranch = %q, want %q", cfg.Defaults.ParentBranch, "main")
	}
}
