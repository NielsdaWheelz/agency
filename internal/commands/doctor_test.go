package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// mockRunner implements exec.CommandRunner for testing.
type mockRunner struct {
	responses     map[string]agencyexec.CmdResult
	errors        map[string]error
	lookPathPaths map[string]string // file -> path (if found)
	lookPathErrs  map[string]error  // file -> error (if not found)
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		responses:     make(map[string]agencyexec.CmdResult),
		errors:        make(map[string]error),
		lookPathPaths: make(map[string]string),
		lookPathErrs:  make(map[string]error),
	}
}

func (m *mockRunner) SetResponse(name string, args []string, result agencyexec.CmdResult, err error) {
	key := m.key(name, args)
	m.responses[key] = result
	if err != nil {
		m.errors[key] = err
	}
}

// SetLookPath configures the mock response for LookPath calls.
// If err is nil, the path is returned; if err is non-nil, it's returned as the error.
func (m *mockRunner) SetLookPath(file, path string, err error) {
	if err != nil {
		m.lookPathErrs[file] = err
	} else {
		m.lookPathPaths[file] = path
	}
}

// LookPath implements CommandRunner.LookPath for testing.
func (m *mockRunner) LookPath(file string) (string, error) {
	if err, ok := m.lookPathErrs[file]; ok {
		return "", err
	}
	if path, ok := m.lookPathPaths[file]; ok {
		return path, nil
	}
	// Default: command found at /usr/bin/<file>
	return "/usr/bin/" + file, nil
}

func (m *mockRunner) key(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func (m *mockRunner) Run(_ context.Context, name string, args []string, _ agencyexec.RunOpts) (agencyexec.CmdResult, error) {
	key := m.key(name, args)
	if err, ok := m.errors[key]; ok {
		return agencyexec.CmdResult{}, err
	}
	if result, ok := m.responses[key]; ok {
		return result, nil
	}
	// Default: command not found
	return agencyexec.CmdResult{}, fmt.Errorf("mock: command not configured: %s", key)
}

// setupTestRepo creates a temporary git repo with agency.json and executable scripts.
// Returns the repo root path. Cleanup is handled automatically by t.TempDir().
func setupTestRepo(t *testing.T) string {
	t.Helper()

	// Create temp dir (t.TempDir handles cleanup automatically)
	tmpDir := t.TempDir()

	// Create minimal directory structure
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	// Create agency.json
	agencyJSON := `{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "agency.json"), []byte(agencyJSON), 0644); err != nil {
		t.Fatalf("failed to write agency.json: %v", err)
	}

	// Create executable stub scripts
	stubScript := "#!/usr/bin/env bash\nexit 0\n"
	scripts := []string{"agency_setup.sh", "agency_verify.sh", "agency_archive.sh"}
	for _, script := range scripts {
		path := filepath.Join(scriptsDir, script)
		if err := os.WriteFile(path, []byte(stubScript), 0755); err != nil {
			t.Fatalf("failed to write script %s: %v", script, err)
		}
	}

	return tmpDir
}

func writeUserConfig(t *testing.T, configDir string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	cfg := `{
  "version": 1,
  "defaults": {
    "runner": "claude",
    "editor": "code"
  },
  "runners": {
    "claude": "claude",
    "codex": "codex"
  },
  "editors": {
    "code": "code"
  }
}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}
}

// setupMockRunnerAllOK sets up mock runner to respond OK for all tool checks.
func setupMockRunnerAllOK(m *mockRunner, repoRoot string) {
	// git rev-parse --show-toplevel
	m.SetResponse("git", []string{"rev-parse", "--show-toplevel"}, agencyexec.CmdResult{
		Stdout:   repoRoot + "\n",
		ExitCode: 0,
	}, nil)

	// git config --get remote.origin.url (GitHub origin)
	m.SetResponse("git", []string{"config", "--get", "remote.origin.url"}, agencyexec.CmdResult{
		Stdout:   "git@github.com:testowner/testrepo.git\n",
		ExitCode: 0,
	}, nil)

	// git --version
	m.SetResponse("git", []string{"--version"}, agencyexec.CmdResult{
		Stdout:   "git version 2.40.0\n",
		ExitCode: 0,
	}, nil)

	// git branch --show-current
	m.SetResponse("git", []string{"branch", "--show-current"}, agencyexec.CmdResult{
		Stdout:   "main\n",
		ExitCode: 0,
	}, nil)

	// tmux -V
	m.SetResponse("tmux", []string{"-V"}, agencyexec.CmdResult{
		Stdout:   "tmux 3.3a\n",
		ExitCode: 0,
	}, nil)

	// gh --version
	m.SetResponse("gh", []string{"--version"}, agencyexec.CmdResult{
		Stdout:   "gh version 2.40.0 (2024-01-15)\nhttps://github.com/cli/cli/releases/tag/v2.40.0\n",
		ExitCode: 0,
	}, nil)

	// gh auth status
	m.SetResponse("gh", []string{"auth", "status"}, agencyexec.CmdResult{
		ExitCode: 0,
	}, nil)
}

func TestDoctor_Success(t *testing.T) {
	repoRoot := setupTestRepo(t)

	// Create temp data dir (t.TempDir handles cleanup automatically)
	dataDir := t.TempDir()

	// Set env var for data dir (t.Setenv auto-restores after test)
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	// Setup mock
	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("doctor failed: %v", err)
	}

	output := stdout.String()

	// Check key output lines
	expectedLines := []string{
		"repo_root: " + repoRoot,
		"agency_data_dir: " + dataDir,
		"agency_config_dir: " + configDir,
		"user_config_path: " + filepath.Join(configDir, "config.json"),
		"repo_key: github:testowner/testrepo",
		"origin_present: true",
		"origin_url: git@github.com:testowner/testrepo.git",
		"origin_host: github.com",
		"github_flow_available: true",
		"git_version: git version 2.40.0",
		"tmux_version: tmux 3.3a",
		"gh_version: gh version 2.40.0 (2024-01-15)",
		"gh_authenticated: true",
		"defaults_parent_branch: main",
		"defaults_runner: claude",
		"defaults_editor: code",
		"runner_cmd: /usr/bin/claude",
		"status: ok",
	}

	for _, line := range expectedLines {
		if !strings.Contains(output, line) {
			t.Errorf("output missing expected line: %s\nfull output:\n%s", line, output)
		}
	}

	// Check persistence files were created
	repoIndexPath := filepath.Join(dataDir, "repo_index.json")
	if _, err := os.Stat(repoIndexPath); os.IsNotExist(err) {
		t.Error("repo_index.json was not created")
	}
}

func TestDoctor_GhNotAuthenticated(t *testing.T) {
	repoRoot := setupTestRepo(t)

	// Create temp data dir (t.TempDir handles cleanup automatically)
	dataDir := t.TempDir()

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)
	// Override gh auth status to fail
	m.SetResponse("gh", []string{"auth", "status"}, agencyexec.CmdResult{
		Stderr:   "You are not logged in",
		ExitCode: 1,
	}, nil)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unauthenticated gh")
	}

	if !strings.Contains(err.Error(), "E_GH_NOT_AUTHENTICATED") {
		t.Errorf("expected E_GH_NOT_AUTHENTICATED error, got: %v", err)
	}

	// stdout should be empty on failure
	if stdout.Len() > 0 {
		t.Errorf("stdout should be empty on failure, got: %s", stdout.String())
	}

	// Persistence files should NOT be created on failure
	repoIndexPath := filepath.Join(dataDir, "repo_index.json")
	if _, err := os.Stat(repoIndexPath); !os.IsNotExist(err) {
		t.Error("repo_index.json should not be created on failure")
	}
}

func TestDoctor_ScriptNotExecutable(t *testing.T) {
	repoRoot := setupTestRepo(t)

	// Make setup script non-executable
	setupScript := filepath.Join(repoRoot, "scripts", "agency_setup.sh")
	if err := os.Chmod(setupScript, 0644); err != nil {
		t.Fatalf("failed to chmod script: %v", err)
	}

	dataDir := t.TempDir()

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for non-executable script")
	}

	if !strings.Contains(err.Error(), "E_SCRIPT_NOT_EXECUTABLE") {
		t.Errorf("expected E_SCRIPT_NOT_EXECUTABLE error, got: %v", err)
	}

	// Check chmod hint
	if !strings.Contains(err.Error(), "chmod +x") {
		t.Errorf("expected chmod hint in error, got: %v", err)
	}
}

func TestDoctor_ScriptMissing(t *testing.T) {
	repoRoot := setupTestRepo(t)

	// Remove setup script
	setupScript := filepath.Join(repoRoot, "scripts", "agency_setup.sh")
	if err := os.Remove(setupScript); err != nil {
		t.Fatalf("failed to remove script: %v", err)
	}

	dataDir := t.TempDir()

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing script")
	}

	if !strings.Contains(err.Error(), "E_SCRIPT_NOT_FOUND") {
		t.Errorf("expected E_SCRIPT_NOT_FOUND error, got: %v", err)
	}
}

func TestDoctor_NoGitHubOrigin(t *testing.T) {
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)
	// Override origin to be missing
	m.SetResponse("git", []string{"config", "--get", "remote.origin.url"}, agencyexec.CmdResult{
		ExitCode: 1, // git config returns 1 for missing key
	}, nil)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	// Doctor should still succeed with missing origin
	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("doctor should succeed without GitHub origin: %v", err)
	}

	output := stdout.String()

	// Check that github_flow_available is false
	if !strings.Contains(output, "github_flow_available: false") {
		t.Errorf("expected github_flow_available: false, got:\n%s", output)
	}
	if !strings.Contains(output, "origin_present: false") {
		t.Errorf("expected origin_present: false, got:\n%s", output)
	}
	// repo_key should be path-based
	if !strings.Contains(output, "repo_key: path:") {
		t.Errorf("expected path-based repo_key, got:\n%s", output)
	}
	if !strings.Contains(output, "status: ok") {
		t.Errorf("expected status: ok, got:\n%s", output)
	}
}

func TestDoctor_PersistenceCreatedAtPreserved(t *testing.T) {
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)

	fsys := fs.NewRealFS()

	// Run doctor twice
	var stdout1, stderr1 bytes.Buffer
	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout1, &stderr1)
	if err != nil {
		t.Fatalf("first doctor run failed: %v", err)
	}

	// Small delay to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	var stdout2, stderr2 bytes.Buffer
	err = Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout2, &stderr2)
	if err != nil {
		t.Fatalf("second doctor run failed: %v", err)
	}

	// Load repo.json and verify created_at is preserved
	st := store.NewStore(fsys, dataDir, time.Now)
	idx, err := st.LoadRepoIndex()
	if err != nil {
		t.Fatalf("failed to load repo_index: %v", err)
	}

	// Should have exactly one entry
	if len(idx.Repos) != 1 {
		t.Errorf("expected 1 repo entry, got %d", len(idx.Repos))
	}

	// Get the repo_id
	var repoID string
	for _, entry := range idx.Repos {
		repoID = entry.RepoID
		break
	}

	rec, exists, err := st.LoadRepoRecord(repoID)
	if err != nil || !exists {
		t.Fatalf("failed to load repo.json: exists=%v err=%v", exists, err)
	}

	// Verify timestamps
	if rec.CreatedAt == "" {
		t.Error("created_at should not be empty")
	}
	if rec.UpdatedAt == "" {
		t.Error("updated_at should not be empty")
	}
	// updated_at should be >= created_at (we can't easily test they're different due to timing)
	if rec.UpdatedAt < rec.CreatedAt {
		t.Errorf("updated_at (%s) should be >= created_at (%s)", rec.UpdatedAt, rec.CreatedAt)
	}
}

func TestDoctor_OutputOrder(t *testing.T) {
	repoRoot := setupTestRepo(t)

	dataDir := t.TempDir()

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	writeUserConfig(t, configDir)

	m := newMockRunner()
	setupMockRunnerAllOK(m, repoRoot)

	fsys := fs.NewRealFS()
	var stdout, stderr bytes.Buffer

	err := Doctor(context.Background(), m, fsys, repoRoot, DoctorOpts{}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("doctor failed: %v", err)
	}

	output := stdout.String()
	lines := strings.Split(output, "\n")

	// Verify key order per spec
	expectedKeyOrder := []string{
		"repo_root:",
		"agency_data_dir:",
		"agency_config_dir:",
		"user_config_path:",
		"agency_cache_dir:",
		"repo_key:",
		"repo_id:",
		"origin_present:",
		"origin_url:",
		"origin_host:",
		"github_flow_available:",
		"git_version:",
		"tmux_version:",
		"gh_version:",
		"gh_authenticated:",
		"defaults_parent_branch:",
		"defaults_runner:",
		"defaults_editor:",
		"runner_cmd:",
		"script_setup:",
		"script_verify:",
		"script_archive:",
		"status:",
	}

	keyIndex := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		if keyIndex >= len(expectedKeyOrder) {
			t.Errorf("unexpected extra line: %s", line)
			continue
		}
		if !strings.HasPrefix(line, expectedKeyOrder[keyIndex]) {
			t.Errorf("line %d: expected prefix %q, got %q", keyIndex, expectedKeyOrder[keyIndex], line)
		}
		keyIndex++
	}

	if keyIndex != len(expectedKeyOrder) {
		t.Errorf("expected %d lines, got %d", len(expectedKeyOrder), keyIndex)
	}
}
