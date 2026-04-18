package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/scaffold"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRunner is a CommandRunner that returns a fixed repo root.
type stubRunner struct {
	repoRoot string
	exitCode int
}

func (s *stubRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	// Handle git rev-parse --show-toplevel
	if name == "git" && len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
		return exec.CmdResult{
			Stdout:   s.repoRoot + "\n",
			Stderr:   "",
			ExitCode: s.exitCode,
		}, nil
	}
	return exec.CmdResult{ExitCode: 1}, nil
}

func (s *stubRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

// setupTempGitRepo creates a temp directory and initializes a minimal git repo.
// Returns the repo root and a temp config directory.
func setupTempGitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configDir := t.TempDir()

	// Create .git directory to simulate a git repo
	gitDir := filepath.Join(dir, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0755), "failed to create .git dir")

	return dir, configDir
}

func TestInit_CreatesConfigAndStubs(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	repoID := identity.DeriveRepoIdentity(repoRoot, "").RepoID
	agencyJSONPath := config.LocalAgencyConfigPath(configDir, repoID)

	// Check local agency.json exists and matches template
	content, err := os.ReadFile(agencyJSONPath)
	require.NoError(t, err, "failed to read agency.json")
	assert.Equal(t, scaffold.AgencyJSONTemplate, string(content), "agency.json content mismatch")

	// Check stub scripts exist and are executable
	scripts := []string{
		"scripts/agency_setup.sh",
		"scripts/agency_verify.sh",
		"scripts/agency_archive.sh",
	}
	for _, script := range scripts {
		path := filepath.Join(filepath.Dir(agencyJSONPath), script)
		info, err := os.Stat(path)
		if assert.NoError(t, err, "script %s not found", script) {
			// Check owner executable bit
			assert.NotZero(t, info.Mode()&0100, "script %s is not executable: mode=%o", script, info.Mode())
		}
	}

	assert.NoFileExists(t, filepath.Join(repoRoot, "agency.json"), "default init should not write agency.json into repo")
	assert.NoFileExists(t, filepath.Join(repoRoot, ".gitignore"), "default init should not write .gitignore")
	assert.NoFileExists(t, filepath.Join(repoRoot, "CLAUDE.md"), "default init should not write CLAUDE.md")

	// Check output
	output := stdout.String()
	assert.Contains(t, output, "repo_root:", "output missing repo_root")
	assert.Contains(t, output, "agency_json_path: "+agencyJSONPath, "output missing agency_json_path")
	assert.Contains(t, output, "agency_json_source: local", "output missing agency_json_source")
	assert.Contains(t, output, "agency_json: created", "output missing agency_json: created")
	assert.Contains(t, output, "scripts_created:", "output missing scripts_created")
	assert.Contains(t, output, "gitignore: skipped", "output missing gitignore: skipped")
	assert.Contains(t, output, "claude_md: skipped", "output missing claude_md: skipped")

	userConfigPath := filepath.Join(configDir, "config.json")
	assert.NoFileExists(t, userConfigPath, "agency init should not create user config")
}

func TestInit_RefusesOverwrite(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	// Create existing agency.json
	existingContent := `{"version": 999}`
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	require.NoError(t, os.WriteFile(agencyJSONPath, []byte(existingContent), 0644), "failed to write existing agency.json")

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)

	// Should error
	require.Error(t, err, "expected error for existing agency.json")
	assert.Equal(t, errors.EAgencyJSONExists, errors.GetCode(err))

	// Original file should be unchanged
	content, err := os.ReadFile(agencyJSONPath)
	require.NoError(t, err, "failed to read agency.json")
	assert.Equal(t, existingContent, string(content), "agency.json was modified")
}

func TestInit_ForceOverwritesAgencyJSON(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	// Create existing agency.json with different content
	existingContent := `{"version": 999}`
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	require.NoError(t, os.WriteFile(agencyJSONPath, []byte(existingContent), 0644), "failed to write existing agency.json")

	// Create existing script with custom content
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")
	customScript := "#!/bin/bash\necho custom\n"
	setupPath := filepath.Join(scriptsDir, "agency_setup.sh")
	require.NoError(t, os.WriteFile(setupPath, []byte(customScript), 0755), "failed to write existing script")

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: true, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init with --force failed")

	// agency.json should be replaced with template
	content, err := os.ReadFile(agencyJSONPath)
	require.NoError(t, err, "failed to read agency.json")
	assert.Equal(t, scaffold.AgencyJSONTemplate, string(content), "agency.json not replaced")

	// Existing script should NOT be overwritten
	scriptContent, err := os.ReadFile(setupPath)
	require.NoError(t, err, "failed to read script")
	assert.Equal(t, customScript, string(scriptContent), "script was overwritten")

	// Check output says overwritten
	output := stdout.String()
	assert.Contains(t, output, "agency_json: overwritten")
}

func TestInit_GitignoreIdempotent(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	// Create .gitignore with .agency/ already present
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/\n.agency/\n"
	require.NoError(t, os.WriteFile(gitignorePath, []byte(existing), 0644), "failed to write .gitignore")

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// .gitignore should have .agency/ exactly once
	content, err := os.ReadFile(gitignorePath)
	require.NoError(t, err, "failed to read .gitignore")
	assert.Equal(t, 1, strings.Count(string(content), ".agency"), ".agency count mismatch in: %q", string(content))

	// Check output says unchanged
	assert.Contains(t, stdout.String(), "gitignore: unchanged")
}

func TestInit_GitignoreWithAgencyNoSlash(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	// Create .gitignore with .agency (no trailing slash) - should be treated as present
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/\n.agency\n"
	require.NoError(t, os.WriteFile(gitignorePath, []byte(existing), 0644), "failed to write .gitignore")

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// .gitignore should NOT have .agency/ added (since .agency is treated as equivalent)
	content, err := os.ReadFile(gitignorePath)
	require.NoError(t, err, "failed to read .gitignore")
	// Should have exactly one .agency entry (the original without slash)
	assert.Equal(t, 1, strings.Count(string(content), ".agency"), ".agency count mismatch in: %q", string(content))
}

func TestInit_NoGitignore(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: true, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// .gitignore should NOT exist
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	assert.NoFileExists(t, gitignorePath, ".gitignore should not be created with --no-gitignore")

	// Check output says skipped and warning
	output := stdout.String()
	assert.Contains(t, output, "gitignore: skipped")
	assert.Contains(t, output, "warning: gitignore_skipped")
}

func TestInit_NotInRepo(t *testing.T) {
	t.Parallel()
	// Use a temp dir that is NOT a git repo
	dir := t.TempDir()

	cr := &stubRunner{repoRoot: "", exitCode: 128} // git returns 128 when not in repo
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, dir, opts, &stdout, &stderr)

	require.Error(t, err, "expected error when not in git repo")
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestInit_GitignoreNoTrailingNewline(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	// Create .gitignore WITHOUT trailing newline
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/" // no newline at end
	require.NoError(t, os.WriteFile(gitignorePath, []byte(existing), 0644), "failed to write .gitignore")

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// .gitignore should end with newline after init
	content, err := os.ReadFile(gitignorePath)
	require.NoError(t, err, "failed to read .gitignore")
	require.NotEmpty(t, content, ".gitignore should not be empty")
	assert.Equal(t, byte('\n'), content[len(content)-1], ".gitignore should end with newline")
	// Should contain .agency/
	assert.Contains(t, string(content), ".agency/")
}

func TestInit_VerifyStubContent(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// Verify setup script content
	setupPath := filepath.Join(repoRoot, "scripts/agency_setup.sh")
	setupContent, err := os.ReadFile(setupPath)
	require.NoError(t, err, "failed to read setup script")
	assert.Equal(t, scaffold.SetupStub, string(setupContent), "setup script content mismatch")

	// Verify verify script content (should exit 1 and print message)
	verifyPath := filepath.Join(repoRoot, "scripts/agency_verify.sh")
	verifyContent, err := os.ReadFile(verifyPath)
	require.NoError(t, err, "failed to read verify script")
	assert.Equal(t, scaffold.VerifyStub, string(verifyContent), "verify script content mismatch")
	assert.Contains(t, string(verifyContent), "exit 1", "verify script should exit 1")
	assert.Contains(t, string(verifyContent), `echo "replace scripts/agency_verify.sh"`, "verify script should print replacement message")

	// Verify archive script content
	archivePath := filepath.Join(repoRoot, "scripts/agency_archive.sh")
	archiveContent, err := os.ReadFile(archivePath)
	require.NoError(t, err, "failed to read archive script")
	assert.Equal(t, scaffold.ArchiveStub, string(archiveContent), "archive script content mismatch")
}

func TestInit_ScriptsNotCreatedIfExist(t *testing.T) {
	t.Parallel()
	repoRoot, configDir := setupTempGitRepo(t)

	// Pre-create scripts with custom content
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	customSetup := "#!/bin/bash\n# custom setup\n"
	customVerify := "#!/bin/bash\n# custom verify\n"
	customArchive := "#!/bin/bash\n# custom archive\n"

	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(customSetup), 0755), "failed to write setup script")
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_verify.sh"), []byte(customVerify), 0755), "failed to write verify script")
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_archive.sh"), []byte(customArchive), 0755), "failed to write archive script")

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// All scripts should be unchanged
	gotSetup, _ := os.ReadFile(filepath.Join(scriptsDir, "agency_setup.sh"))
	assert.Equal(t, customSetup, string(gotSetup), "setup script was modified")

	gotVerify, _ := os.ReadFile(filepath.Join(scriptsDir, "agency_verify.sh"))
	assert.Equal(t, customVerify, string(gotVerify), "verify script was modified")

	gotArchive, _ := os.ReadFile(filepath.Join(scriptsDir, "agency_archive.sh"))
	assert.Equal(t, customArchive, string(gotArchive), "archive script was modified")

	// Output should say scripts_created: none
	assert.Contains(t, stdout.String(), "scripts_created: none")
}
