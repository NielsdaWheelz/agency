package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/scaffold"
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
func setupTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create .git directory to simulate a git repo
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	return dir
}

func TestInit_CreatesConfigAndStubs(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Check agency.json exists and matches template
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	content, err := os.ReadFile(agencyJSONPath)
	if err != nil {
		t.Fatalf("failed to read agency.json: %v", err)
	}
	if string(content) != scaffold.AgencyJSONTemplate {
		t.Errorf("agency.json content mismatch:\ngot:\n%s\nwant:\n%s", string(content), scaffold.AgencyJSONTemplate)
	}

	// Check stub scripts exist and are executable
	scripts := []string{
		"scripts/agency_setup.sh",
		"scripts/agency_verify.sh",
		"scripts/agency_archive.sh",
	}
	for _, script := range scripts {
		path := filepath.Join(repoRoot, script)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("script %s not found: %v", script, err)
			continue
		}
		// Check owner executable bit
		if info.Mode()&0100 == 0 {
			t.Errorf("script %s is not executable: mode=%o", script, info.Mode())
		}
	}

	// Check .gitignore exists and contains .agency/
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	gitignoreContent, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignoreContent), ".agency/") {
		t.Errorf(".gitignore does not contain .agency/: %q", string(gitignoreContent))
	}
	// Check ends with newline
	if len(gitignoreContent) > 0 && gitignoreContent[len(gitignoreContent)-1] != '\n' {
		t.Error(".gitignore does not end with newline")
	}

	// Check output
	output := stdout.String()
	if !strings.Contains(output, "repo_root:") {
		t.Error("output missing repo_root")
	}
	if !strings.Contains(output, "agency_json: created") {
		t.Error("output missing agency_json: created")
	}
	if !strings.Contains(output, "scripts_created:") {
		t.Error("output missing scripts_created")
	}
	if !strings.Contains(output, "gitignore: updated") {
		t.Error("output missing gitignore: updated")
	}
}

func TestInit_RefusesOverwrite(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	// Create existing agency.json
	existingContent := `{"version": 999}`
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	if err := os.WriteFile(agencyJSONPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("failed to write existing agency.json: %v", err)
	}

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)

	// Should error
	if err == nil {
		t.Fatal("expected error for existing agency.json")
	}
	if errors.GetCode(err) != errors.EAgencyJSONExists {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.EAgencyJSONExists)
	}

	// Original file should be unchanged
	content, err := os.ReadFile(agencyJSONPath)
	if err != nil {
		t.Fatalf("failed to read agency.json: %v", err)
	}
	if string(content) != existingContent {
		t.Errorf("agency.json was modified: got %q, want %q", string(content), existingContent)
	}
}

func TestInit_ForceOverwritesAgencyJSON(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	// Create existing agency.json with different content
	existingContent := `{"version": 999}`
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	if err := os.WriteFile(agencyJSONPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("failed to write existing agency.json: %v", err)
	}

	// Create existing script with custom content
	scriptsDir := filepath.Join(repoRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}
	customScript := "#!/bin/bash\necho custom\n"
	setupPath := filepath.Join(scriptsDir, "agency_setup.sh")
	if err := os.WriteFile(setupPath, []byte(customScript), 0755); err != nil {
		t.Fatalf("failed to write existing script: %v", err)
	}

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: true}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init with --force failed: %v", err)
	}

	// agency.json should be replaced with template
	content, err := os.ReadFile(agencyJSONPath)
	if err != nil {
		t.Fatalf("failed to read agency.json: %v", err)
	}
	if string(content) != scaffold.AgencyJSONTemplate {
		t.Errorf("agency.json not replaced:\ngot:\n%s\nwant:\n%s", string(content), scaffold.AgencyJSONTemplate)
	}

	// Existing script should NOT be overwritten
	scriptContent, err := os.ReadFile(setupPath)
	if err != nil {
		t.Fatalf("failed to read script: %v", err)
	}
	if string(scriptContent) != customScript {
		t.Errorf("script was overwritten: got %q, want %q", string(scriptContent), customScript)
	}

	// Check output says overwritten
	output := stdout.String()
	if !strings.Contains(output, "agency_json: overwritten") {
		t.Errorf("output should say 'overwritten': %s", output)
	}
}

func TestInit_GitignoreIdempotent(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	// Create .gitignore with .agency/ already present
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/\n.agency/\n"
	if err := os.WriteFile(gitignorePath, []byte(existing), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// .gitignore should have .agency/ exactly once
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	count := strings.Count(string(content), ".agency")
	if count != 1 {
		t.Errorf(".agency appears %d times, want 1: %q", count, string(content))
	}

	// Check output says unchanged
	output := stdout.String()
	if !strings.Contains(output, "gitignore: unchanged") {
		t.Errorf("output should say 'unchanged': %s", output)
	}
}

func TestInit_GitignoreWithAgencyNoSlash(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	// Create .gitignore with .agency (no trailing slash) - should be treated as present
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/\n.agency\n"
	if err := os.WriteFile(gitignorePath, []byte(existing), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// .gitignore should NOT have .agency/ added (since .agency is treated as equivalent)
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	// Should have exactly one .agency entry (the original without slash)
	count := strings.Count(string(content), ".agency")
	if count != 1 {
		t.Errorf(".agency appears %d times, want 1: %q", count, string(content))
	}
}

func TestInit_NoGitignore(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: true, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// .gitignore should NOT exist
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	_, err = os.Stat(gitignorePath)
	if !os.IsNotExist(err) {
		t.Error(".gitignore should not be created with --no-gitignore")
	}

	// Check output says skipped and warning
	output := stdout.String()
	if !strings.Contains(output, "gitignore: skipped") {
		t.Errorf("output should say 'skipped': %s", output)
	}
	if !strings.Contains(output, "warning: gitignore_skipped") {
		t.Errorf("output should contain warning: %s", output)
	}
}

func TestInit_NotInRepo(t *testing.T) {
	// Use a temp dir that is NOT a git repo
	dir := t.TempDir()

	cr := &stubRunner{repoRoot: "", exitCode: 128} // git returns 128 when not in repo
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, dir, opts, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

func TestInit_GitignoreNoTrailingNewline(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	// Create .gitignore WITHOUT trailing newline
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	existing := "node_modules/" // no newline at end
	if err := os.WriteFile(gitignorePath, []byte(existing), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// .gitignore should end with newline after init
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if len(content) == 0 || content[len(content)-1] != '\n' {
		t.Errorf(".gitignore should end with newline: %q", string(content))
	}
	// Should contain .agency/
	if !strings.Contains(string(content), ".agency/") {
		t.Errorf(".gitignore should contain .agency/: %q", string(content))
	}
}

func TestInit_VerifyStubContent(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify setup script content
	setupPath := filepath.Join(repoRoot, "scripts/agency_setup.sh")
	setupContent, err := os.ReadFile(setupPath)
	if err != nil {
		t.Fatalf("failed to read setup script: %v", err)
	}
	if string(setupContent) != scaffold.SetupStub {
		t.Errorf("setup script content mismatch:\ngot:\n%s\nwant:\n%s", string(setupContent), scaffold.SetupStub)
	}

	// Verify verify script content (should exit 1 and print message)
	verifyPath := filepath.Join(repoRoot, "scripts/agency_verify.sh")
	verifyContent, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatalf("failed to read verify script: %v", err)
	}
	if string(verifyContent) != scaffold.VerifyStub {
		t.Errorf("verify script content mismatch:\ngot:\n%s\nwant:\n%s", string(verifyContent), scaffold.VerifyStub)
	}
	if !strings.Contains(string(verifyContent), "exit 1") {
		t.Error("verify script should exit 1")
	}
	if !strings.Contains(string(verifyContent), `echo "replace scripts/agency_verify.sh"`) {
		t.Error("verify script should print replacement message")
	}

	// Verify archive script content
	archivePath := filepath.Join(repoRoot, "scripts/agency_archive.sh")
	archiveContent, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("failed to read archive script: %v", err)
	}
	if string(archiveContent) != scaffold.ArchiveStub {
		t.Errorf("archive script content mismatch:\ngot:\n%s\nwant:\n%s", string(archiveContent), scaffold.ArchiveStub)
	}
}

func TestInit_ScriptsNotCreatedIfExist(t *testing.T) {
	repoRoot := setupTempGitRepo(t)

	// Pre-create scripts with custom content
	scriptsDir := filepath.Join(repoRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}

	customSetup := "#!/bin/bash\n# custom setup\n"
	customVerify := "#!/bin/bash\n# custom verify\n"
	customArchive := "#!/bin/bash\n# custom archive\n"

	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_setup.sh"), []byte(customSetup), 0755); err != nil {
		t.Fatalf("failed to write setup script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_verify.sh"), []byte(customVerify), 0755); err != nil {
		t.Fatalf("failed to write verify script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "agency_archive.sh"), []byte(customArchive), 0755); err != nil {
		t.Fatalf("failed to write archive script: %v", err)
	}

	cr := &stubRunner{repoRoot: repoRoot, exitCode: 0}
	fsys := fs.NewRealFS()
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	opts := InitOpts{NoGitignore: false, Force: false}
	err := Init(ctx, cr, fsys, repoRoot, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// All scripts should be unchanged
	gotSetup, _ := os.ReadFile(filepath.Join(scriptsDir, "agency_setup.sh"))
	if string(gotSetup) != customSetup {
		t.Errorf("setup script was modified: got %q, want %q", string(gotSetup), customSetup)
	}

	gotVerify, _ := os.ReadFile(filepath.Join(scriptsDir, "agency_verify.sh"))
	if string(gotVerify) != customVerify {
		t.Errorf("verify script was modified: got %q, want %q", string(gotVerify), customVerify)
	}

	gotArchive, _ := os.ReadFile(filepath.Join(scriptsDir, "agency_archive.sh"))
	if string(gotArchive) != customArchive {
		t.Errorf("archive script was modified: got %q, want %q", string(gotArchive), customArchive)
	}

	// Output should say scripts_created: none
	output := stdout.String()
	if !strings.Contains(output, "scripts_created: none") {
		t.Errorf("output should say 'scripts_created: none': %s", output)
	}
}
