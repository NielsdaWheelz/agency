package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	invocationpkg "github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTempGitRepo creates a real temp git repo and returns its root plus a temp config directory.
func setupTempGitRepo(t *testing.T) (string, string) {
	t.Helper()
	return testutil.SetupGitRepo(t), t.TempDir()
}

func registerInitTestRepo(t *testing.T, dataDir, repoRoot string) {
	t.Helper()
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, "")
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoIdentity.RepoKey: {
				RepoID:     repoIdentity.RepoID,
				Paths:      []string{repoRoot},
				LastSeenAt: "2026-01-01T00:00:00Z",
			},
		},
	}))
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoIdentity.RepoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    false,
		Capabilities:     store.Capabilities{},
		CreatedAt:        "2026-01-01T00:00:00Z",
		UpdatedAt:        "2026-01-01T00:00:00Z",
	}))
}

func requireDefaultAgencyConfig(t *testing.T, fsys fs.FS, repoRoot, configDir, repoID, source string) config.ResolvedAgencyConfig {
	t.Helper()

	resolved, err := config.ResolveAgencyConfig(fsys, repoRoot, configDir, repoID, "")
	require.NoError(t, err)
	assert.Equal(t, source, resolved.Source)
	assert.Equal(t, config.AgencyConfigVersion, resolved.Config.Version)
	assert.Equal(t, config.CheckoutRootSibling, resolved.Config.Execution.CheckoutRoot)

	configRoot := filepath.Dir(resolved.Path)
	assert.Equal(t, filepath.Join(configRoot, "scripts/agency_setup.sh"), resolved.Config.Scripts.Setup.Path)
	assert.Equal(t, filepath.Join(configRoot, "scripts/agency_verify.sh"), resolved.Config.Scripts.Verify.Path)
	assert.Equal(t, filepath.Join(configRoot, "scripts/agency_archive.sh"), resolved.Config.Scripts.Archive.Path)
	assert.Equal(t, config.DefaultSetupTimeout, resolved.Config.Scripts.Setup.Timeout)
	assert.Equal(t, config.DefaultVerifyTimeout, resolved.Config.Scripts.Verify.Timeout)
	assert.Equal(t, config.DefaultArchiveTimeout, resolved.Config.Scripts.Archive.Timeout)

	return resolved
}

func TestInit_CreatesConfigAndStubs(t *testing.T) {
	repoRoot, configDir := setupTempGitRepo(t)

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, ConfigDirOverride: configDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	repoID := identity.DeriveRepoIdentity(repoRoot, "").RepoID
	agencyJSONPath := config.LocalAgencyConfigPath(configDir, repoID)

	resolved := requireDefaultAgencyConfig(t, fsys, repoRoot, configDir, repoID, "local")
	assert.Equal(t, agencyJSONPath, resolved.Path)

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
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

	// Create existing agency.json
	existingContent := `{"version": 999}`
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	require.NoError(t, os.WriteFile(agencyJSONPath, []byte(existingContent), 0644), "failed to write existing agency.json")

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
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
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

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

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: true, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init with --force failed")

	content, err := os.ReadFile(agencyJSONPath)
	require.NoError(t, err, "failed to read agency.json")
	assert.NotEqual(t, existingContent, string(content), "agency.json was not replaced")

	repoID := identity.DeriveRepoIdentity(repoRoot, "").RepoID
	resolved := requireDefaultAgencyConfig(t, fsys, repoRoot, configDir, repoID, "repo")
	assert.Equal(t, agencyJSONPath, resolved.Path)

	// Existing script should NOT be overwritten
	scriptContent, err := os.ReadFile(setupPath)
	require.NoError(t, err, "failed to read script")
	assert.Equal(t, customScript, string(scriptContent), "script was overwritten")

	// Check output says overwritten
	output := stdout.String()
	assert.Contains(t, output, "agency_json: overwritten")
}

func TestInit_GitignoreIdempotent(t *testing.T) {
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

	// Create .gitignore with .agency/ already present
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/\n.agency/\n"
	require.NoError(t, os.WriteFile(gitignorePath, []byte(existing), 0644), "failed to write .gitignore")

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
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
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

	// Create .gitignore with .agency (no trailing slash) - should be treated as present
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/\n.agency\n"
	require.NoError(t, os.WriteFile(gitignorePath, []byte(existing), 0644), "failed to write .gitignore")

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	require.NoError(t, err, "Init failed")

	// .gitignore should NOT have .agency/ added (since .agency is treated as equivalent)
	content, err := os.ReadFile(gitignorePath)
	require.NoError(t, err, "failed to read .gitignore")
	// Should have exactly one .agency entry (the original without slash)
	assert.Equal(t, 1, strings.Count(string(content), ".agency"), ".agency count mismatch in: %q", string(content))
}

func TestInit_NoGitignore(t *testing.T) {
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: true, Force: false, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
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
	// Use a temp dir that is NOT a git repo
	dir := t.TempDir()

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, dir, opts, &stdout, &stderr)

	require.Error(t, err, "expected error when not in git repo")
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestInit_RepoConfigRejectsAgencyManagedTrees(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()

	tests := []struct {
		name string
		kind string
		id   string
	}{
		{name: "integration worktree", kind: "integration_worktrees", id: "wt-1"},
		{name: "sandbox", kind: "sandboxes", id: "inv-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := filepath.Join(dataDir, "repos", "repo-1", tt.kind, tt.id, "tree")
			testutil.HermeticGitEnv(t)
			require.NoError(t, os.MkdirAll(repoRoot, 0o755))
			result, runErr := exec.NewRealRunner().Run(context.Background(), "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoRoot})
			require.NoError(t, runErr)
			require.Zero(t, result.ExitCode, strings.TrimSpace(result.Stdout+"\n"+result.Stderr))

			require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".agency"), 0o755), "failed to create managed marker dir")
			markerName := integrationworktree.IntegrationMarkerFileName
			if tt.kind == "sandboxes" {
				markerName = invocationpkg.SandboxMarkerFileName
			}
			require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".agency", markerName), []byte("managed\n"), 0o644))

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()
			var stdout, stderr bytes.Buffer

			err := Init(ctx, cr, fsys, repoRoot, InitOpts{RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}, &stdout, &stderr)
			require.Error(t, err)
			assert.Equal(t, errors.EUnsafeRepoRoot, errors.GetCode(err))
			assert.Empty(t, stdout.String())
			assert.NoFileExists(t, filepath.Join(repoRoot, "agency.json"))
		})
	}
}

func TestInit_GitignoreNoTrailingNewline(t *testing.T) {
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

	// Create .gitignore WITHOUT trailing newline
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/" // no newline at end
	require.NoError(t, os.WriteFile(gitignorePath, []byte(existing), 0644), "failed to write .gitignore")

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
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

func TestInit_ScriptsNotCreatedIfExist(t *testing.T) {
	repoRoot, configDir := setupTempGitRepo(t)
	dataDir := t.TempDir()
	registerInitTestRepo(t, dataDir, repoRoot)

	// Pre-create scripts with custom content
	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0755), "failed to create scripts dir")

	customSetup := "#!/bin/bash\n# custom setup\n"
	customVerify := "#!/bin/bash\n# custom verify\n"
	customArchive := "#!/bin/bash\n# custom archive\n"

	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(customSetup), 0755), "failed to write setup script")
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_verify.sh"), []byte(customVerify), 0755), "failed to write verify script")
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "agency_archive.sh"), []byte(customArchive), 0755), "failed to write archive script")

	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false, RepoConfig: true, ConfigDirOverride: configDir, DataDirOverride: dataDir}
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
