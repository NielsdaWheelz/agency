package config

import (
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/core"
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

func shellQuoteForTest(s string) string {
	return core.ShellEscapePosix(s)
}

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
	stub.files["/repo/agency.json"] = []byte(`{"version": 4, "scripts": {`)
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
	assert.Equal(t, 4, cfg.Version)
	assert.Equal(t, "scripts/agency_setup.sh", cfg.Scripts.Setup.Path)
	assert.Equal(t, 10*time.Minute, cfg.Scripts.Setup.Timeout)
	assert.Equal(t, "scripts/agency_verify.sh", cfg.Scripts.Verify.Path)
	assert.Equal(t, 30*time.Minute, cfg.Scripts.Verify.Timeout)
	assert.Equal(t, "scripts/agency_archive.sh", cfg.Scripts.Archive.Path)
	assert.Equal(t, 5*time.Minute, cfg.Scripts.Archive.Timeout)
	assert.Equal(t, "personal", cfg.Execution.Profile)
	assert.Equal(t, "repo-sibling", cfg.Execution.CheckoutRoot)
}

func TestLoadAgencyConfig_ValidWithRunnerDefaults(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/agency_valid_with_runner_defaults.json")
	require.NoError(t, err, "failed to read fixture")
	stub := newStubFS()
	stub.files["/repo/agency.json"] = data

	cfg, err := LoadAgencyConfig(stub, "/repo")
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.Version)
	assert.Equal(t, "scripts/agency_setup.sh", cfg.Scripts.Setup.Path)
	assert.Equal(t, "work", cfg.Execution.Profile)
	assert.Equal(t, "/tmp/agency-checkouts", cfg.Execution.CheckoutRoot)
}

func TestLoadAgencyConfig_RunnerDefaultsPermissionModeRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/repo/agency.json"] = []byte(`{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "runner_defaults": {
    "claude-code": {
      "permission_mode": "default"
    }
  }
}`)

	_, err := LoadAgencyConfig(stub, "/repo")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
	assert.Contains(t, err.Error(), "runner_defaults.claude-code.permission_mode is not supported in agency.json")
}

func TestResolveAgencyConfig_PrefersRepoThenLocal(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read fixture")

	stub := newStubFS()
	stub.files["/repo/agency.json"] = data
	stub.files["/config/repos/repo-1/agency.json"] = []byte(strings.ReplaceAll(string(data), "scripts/agency_setup.sh", "local/setup.sh"))

	resolved, err := ResolveAgencyConfig(stub, "/repo", "/config", "repo-1", "")
	require.NoError(t, err)
	assert.Equal(t, "/repo/agency.json", resolved.Path)
	assert.Equal(t, "repo", resolved.Source)
	assert.Equal(t, "/repo/scripts/agency_setup.sh", resolved.Config.Scripts.Setup.Path)
}

func TestResolveAgencyConfig_FallsBackToLocal(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read fixture")

	stub := newStubFS()
	stub.files["/config/repos/repo-1/agency.json"] = data

	resolved, err := ResolveAgencyConfig(stub, "/repo", "/config", "repo-1", "")
	require.NoError(t, err)
	assert.Equal(t, "/config/repos/repo-1/agency.json", resolved.Path)
	assert.Equal(t, "local", resolved.Source)
	assert.Equal(t, "/config/repos/repo-1/scripts/agency_verify.sh", resolved.Config.Scripts.Verify.Path)
}

func TestResolveAgencyConfig_ExplicitOverridesRepo(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read fixture")

	stub := newStubFS()
	stub.files["/repo/agency.json"] = data
	stub.files["/custom/agency.json"] = []byte(strings.ReplaceAll(string(data), "scripts/agency_archive.sh", "custom/archive.sh"))

	resolved, err := ResolveAgencyConfig(stub, "/repo", "/config", "repo-1", "/custom/agency.json")
	require.NoError(t, err)
	assert.Equal(t, "/custom/agency.json", resolved.Path)
	assert.Equal(t, "explicit", resolved.Source)
	assert.Equal(t, "/custom/custom/archive.sh", resolved.Config.Scripts.Archive.Path)
}

func TestResolveAgencyConfig_InvalidRepoDoesNotFallBackToLocal(t *testing.T) {
	t.Parallel()
	repoData, err := os.ReadFile("testdata/wrong_version.json")
	require.NoError(t, err, "failed to read invalid repo fixture")
	localData, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read valid local fixture")

	stub := newStubFS()
	stub.files["/repo/agency.json"] = repoData
	stub.files["/config/repos/repo-1/agency.json"] = localData

	_, err = ResolveAgencyConfig(stub, "/repo", "/config", "repo-1", "")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "/repo/agency.json", ae.Details["path"])
	assert.Equal(t, "repo", ae.Details["source"])
	assert.Contains(t, ae.Details["hint"], "agency init --path "+shellQuoteForTest("/repo")+" --repo-config --force")
}

func TestResolveAgencyConfig_InvalidLocalIncludesPathSourceAndHint(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/wrong_version.json")
	require.NoError(t, err, "failed to read invalid local fixture")

	stub := newStubFS()
	stub.files["/config/repos/repo-1/agency.json"] = data

	_, err = ResolveAgencyConfig(stub, "/repo", "/config", "repo-1", "")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "/config/repos/repo-1/agency.json", ae.Details["path"])
	assert.Equal(t, "local", ae.Details["source"])
	assert.Contains(t, ae.Details["hint"], "agency init --path "+shellQuoteForTest("/repo")+" --force")
}

func TestResolveAgencyConfig_InvalidRepoHintShellQuotesRepoRoot(t *testing.T) {
	t.Parallel()

	repoRoot := "/tmp/repo with spaces/it's quoted"
	repoData, err := os.ReadFile("testdata/wrong_version.json")
	require.NoError(t, err, "failed to read invalid repo fixture")
	localData, err := os.ReadFile("testdata/valid_min.json")
	require.NoError(t, err, "failed to read valid local fixture")

	stub := newStubFS()
	stub.files[filepath.Join(repoRoot, "agency.json")] = repoData
	stub.files["/config/repos/repo-1/agency.json"] = localData

	_, err = ResolveAgencyConfig(stub, repoRoot, "/config", "repo-1", "")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "agency init --path "+shellQuoteForTest(repoRoot)+" --repo-config --force")
}

func TestResolveAgencyConfig_InvalidExplicitIncludesPathSourceAndHint(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/wrong_version.json")
	require.NoError(t, err, "failed to read invalid explicit fixture")

	stub := newStubFS()
	stub.files["/custom/agency.json"] = data

	_, err = ResolveAgencyConfig(stub, "/repo", "/config", "repo-1", "/custom/agency.json")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "/custom/agency.json", ae.Details["path"])
	assert.Equal(t, "explicit", ae.Details["source"])
	assert.Contains(t, ae.Details["hint"], "--agency-config")
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
		{"runner_defaults as array", "agency_wrong_types_runner_defaults.json", "runner_defaults must be an object"},
		{"runner_defaults entry as string", "agency_wrong_types_runner_defaults_entry.json", "runner_defaults.codex must be an object"},
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

func TestLoadAgencyConfig_VersionFloatWholeRejected(t *testing.T) {
	t.Parallel()
	stub := newStubFS()
	stub.files["/repo/agency.json"] = []byte(`{
  "version": 4.0,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  }
}`)

	_, err := LoadAgencyConfig(stub, "/repo")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version must be an integer")
}

func TestLoadAgencyConfig_ExecutionWrongTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  string
		wantMsg string
	}{
		{
			name: "execution as array",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "execution": []
}`,
			wantMsg: "execution must be an object",
		},
		{
			name: "execution null",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "execution": null
}`,
			wantMsg: "execution must be an object",
		},
		{
			name: "profile as number",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "execution": {"profile": 42}
			}`,
			wantMsg: "execution.profile must be a string",
		},
		{
			name: "profile null",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "execution": {"profile": null}
}`,
			wantMsg: "execution.profile must be a string",
		},
		{
			name: "checkout root as number",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "execution": {"checkout_root": 42}
			}`,
			wantMsg: "execution.checkout_root must be a string",
		},
		{
			name: "checkout root null",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  },
  "execution": {"checkout_root": null}
}`,
			wantMsg: "execution.checkout_root must be a string",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := newStubFS()
			stub.files["/repo/agency.json"] = []byte(tt.config)

			_, err := LoadAgencyConfig(stub, "/repo")
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
		{"missing scripts", "missing_scripts.json", "missing required field scripts.setup.path"},
		{"missing script setup", "missing_script_setup.json", "missing required field scripts.setup.path"},
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
	assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
	assert.Contains(t, err.Error(), "version must be 4")
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

func TestLoadAgencyConfig_UnknownScriptKeysRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  string
		wantMsg string
	}{
		{
			name: "unknown scripts child",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"},
    "lint": {"path": "scripts/lint.sh"}
  }
}`,
			wantMsg: "scripts contains unknown field: lint",
		},
		{
			name: "unknown script config child",
			config: `{
  "version": 4,
  "scripts": {
    "setup": {"path": "scripts/setup.sh", "command": "make setup"},
    "verify": {"path": "scripts/verify.sh"},
    "archive": {"path": "scripts/archive.sh"}
  }
}`,
			wantMsg: "scripts.setup contains unknown field: command",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := newStubFS()
			stub.files["/repo/agency.json"] = []byte(tt.config)

			_, err := LoadAgencyConfig(stub, "/repo")
			require.Error(t, err)
			assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
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

// Integration test using real filesystem
func TestLoadAgencyConfig_RealFS(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	configContent := `{
  "version": 4,
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

	assert.Equal(t, 4, cfg.Version)
	assert.Equal(t, "scripts/setup.sh", cfg.Scripts.Setup.Path)
	assert.Equal(t, 10*time.Minute, cfg.Scripts.Setup.Timeout)
}

func TestValidateAgencyConfig_ExecutionDefaultsAndValidation(t *testing.T) {
	t.Parallel()

	valid := AgencyConfig{
		Version: AgencyConfigVersion,
		Scripts: Scripts{
			Setup:   ScriptConfig{Path: "scripts/setup.sh"},
			Verify:  ScriptConfig{Path: "scripts/verify.sh"},
			Archive: ScriptConfig{Path: "scripts/archive.sh"},
		},
	}
	cfg, err := ValidateAgencyConfig(valid)
	require.NoError(t, err)
	assert.Equal(t, CheckoutRootSibling, cfg.Execution.CheckoutRoot)

	valid.Execution = AgencyExecutionConfig{
		Profile:      "work-profile",
		CheckoutRoot: "/tmp/agency-checkouts",
	}
	cfg, err = ValidateAgencyConfig(valid)
	require.NoError(t, err)
	assert.Equal(t, "work-profile", cfg.Execution.Profile)
	assert.Equal(t, "/tmp/agency-checkouts", cfg.Execution.CheckoutRoot)

	valid.Execution.Profile = "Work"
	_, err = ValidateAgencyConfig(valid)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidExecutionProfile, errors.GetCode(err))
	assert.Contains(t, err.Error(), "execution.profile must contain only lowercase letters, digits, and hyphens")

	valid.Execution.Profile = "work"
	valid.Execution.CheckoutRoot = "relative/path"
	_, err = ValidateAgencyConfig(valid)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidCheckoutRoot, errors.GetCode(err))
	assert.Contains(t, err.Error(), "execution.checkout_root must be repo-sibling or an absolute path")
}

func TestResolveCheckoutRoot(t *testing.T) {
	t.Parallel()

	got, err := ResolveCheckoutRoot("/repo/project", "repo-1", CheckoutRootSibling)
	require.NoError(t, err)
	assert.Equal(t, "/repo/.agency/checkouts/repo-1", got)

	got, err = ResolveCheckoutRoot("/repo/project", "repo-1", "/tmp/agency-checkouts")
	require.NoError(t, err)
	expected, err := fs.ResolveSymlinks("/tmp/agency-checkouts/repo-1")
	require.NoError(t, err)
	assert.Equal(t, expected, got)

	_, err = ResolveCheckoutRoot("/repo/project", "repo-1", "relative")
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidCheckoutRoot, errors.GetCode(err))

	_, err = ResolveCheckoutRoot("/repo/project", "repo-1", "/repo/project/checkouts")
	require.Error(t, err)
	assert.Equal(t, errors.ECheckoutRootUnsafe, errors.GetCode(err))
}

func TestResolveCheckoutRoot_UsesCanonicalRepoRoot(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "real", "project")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))
	linkRoot := filepath.Join(tmp, "project-link")
	require.NoError(t, os.Symlink(repoRoot, linkRoot))

	got, err := ResolveCheckoutRoot(linkRoot, "repo-1", CheckoutRootSibling)
	require.NoError(t, err)
	canonicalRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(filepath.Dir(canonicalRepoRoot), ".agency", "checkouts", "repo-1"), got)
}

func TestResolveCheckoutRoot_RejectsSymlinkIntoRepo(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "repo")
	repoCheckoutTarget := filepath.Join(repoRoot, "checkouts")
	require.NoError(t, os.MkdirAll(repoCheckoutTarget, 0755))
	checkoutRoot := filepath.Join(tmp, "checkout-link")
	require.NoError(t, os.Symlink(repoCheckoutTarget, checkoutRoot))

	_, err := ResolveCheckoutRoot(repoRoot, "repo-1", checkoutRoot)
	require.Error(t, err)
	assert.Equal(t, errors.ECheckoutRootUnsafe, errors.GetCode(err))
}

func TestValidateAgencyConfig_RunnerDefaultsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		fixture string
		wantMsg string
	}{
		{"unknown runner", "agency_runner_defaults_unknown_runner.json", "runner_defaults.amp is not supported"},
		{"cursor effort unsupported", "agency_runner_defaults_cursor_effort.json", "runner_defaults.cursor.effort is not supported"},
		{"missing model and effort", "agency_runner_defaults_empty_entry.json", "runner_defaults.codex requires at least one of model or effort"},
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
			require.NoError(t, err)

			_, err = ValidateAgencyConfig(cfg)
			require.Error(t, err)
			assert.Equal(t, errors.EInvalidAgencyJSON, errors.GetCode(err))
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}
