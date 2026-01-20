package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// completionStubRunner implements exec.CommandRunner for completion testing.
type completionStubRunner struct {
	responses map[string]exec.CmdResult
}

func newCompletionStubRunner() *completionStubRunner {
	return &completionStubRunner{
		responses: make(map[string]exec.CmdResult),
	}
}

func (s *completionStubRunner) On(name string, args []string, result exec.CmdResult) {
	key := s.makeKey(name, args)
	s.responses[key] = result
}

func (s *completionStubRunner) makeKey(name string, args []string) string {
	return name + "|" + strings.Join(args, ",")
}

func (s *completionStubRunner) Run(_ context.Context, name string, args []string, _ exec.RunOpts) (exec.CmdResult, error) {
	key := s.makeKey(name, args)
	if result, ok := s.responses[key]; ok {
		return result, nil
	}
	// Default: command not found
	return exec.CmdResult{ExitCode: 127, Stderr: "command not found"}, nil
}

func (s *completionStubRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

// completionStubFS implements fs.FS for completion testing.
type completionStubFS struct {
	files map[string][]byte
}

func newCompletionStubFS() *completionStubFS {
	return &completionStubFS{files: make(map[string][]byte)}
}

func (s *completionStubFS) MkdirAll(_ string, _ os.FileMode) error {
	return nil
}

func (s *completionStubFS) ReadFile(path string) ([]byte, error) {
	if data, ok := s.files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (s *completionStubFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	s.files[path] = data
	return nil
}

func (s *completionStubFS) Stat(path string) (os.FileInfo, error) {
	if _, ok := s.files[path]; ok {
		return nil, nil
	}
	return nil, os.ErrNotExist
}

func (s *completionStubFS) Rename(_, _ string) error {
	return nil
}

func (s *completionStubFS) Remove(_ string) error {
	return nil
}

func (s *completionStubFS) Chmod(_ string, _ os.FileMode) error {
	return nil
}

func (s *completionStubFS) CreateTemp(_, _ string) (string, io.WriteCloser, error) {
	return "", nil, nil
}

// TestCompletion_BashScript tests that bash completion script is generated correctly.
func TestCompletion_BashScript(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	opts := CompletionOpts{Shell: "bash"}
	err := Completion(ctx, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Completion(bash) failed: %v", err)
	}

	script := stdout.String()

	// Verify key bash completion elements
	if !strings.Contains(script, "_agency()") {
		t.Error("bash script missing _agency function")
	}
	if !strings.Contains(script, "complete -F _agency agency") {
		t.Error("bash script missing complete registration")
	}
	if !strings.Contains(script, "agency __complete runs") {
		t.Error("bash script missing __complete runs call")
	}
	if !strings.Contains(script, "agency __complete commands") {
		t.Error("bash script missing __complete commands call")
	}
	if !strings.Contains(script, "# Installation") {
		t.Error("bash script missing installation instructions")
	}
}

// TestCompletion_ZshScript tests that zsh completion script is generated correctly.
func TestCompletion_ZshScript(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	opts := CompletionOpts{Shell: "zsh"}
	err := Completion(ctx, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Completion(zsh) failed: %v", err)
	}

	script := stdout.String()

	// Verify key zsh completion elements
	if !strings.Contains(script, "#compdef agency") {
		t.Error("zsh script missing #compdef directive")
	}
	if !strings.Contains(script, "_agency()") {
		t.Error("zsh script missing _agency function")
	}
	if !strings.Contains(script, "agency __complete runs") {
		t.Error("zsh script missing __complete runs call")
	}
	if !strings.Contains(script, "# Installation") {
		t.Error("zsh script missing installation instructions")
	}
}

// TestCompletion_InvalidShell tests that invalid shell returns error.
func TestCompletion_InvalidShell(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	opts := CompletionOpts{Shell: "fish"}
	err := Completion(ctx, opts, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("expected 'unsupported shell' error, got: %v", err)
	}
}

// TestCompleteCommands tests that __complete commands returns static command list.
func TestCompleteCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	opts := CompleteOpts{Kind: CompleteKindCommands}
	err := Complete(ctx, cr, fsys, "/tmp", opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(commands) failed: %v", err)
	}

	output := stdout.String()
	commands := strings.Split(strings.TrimSpace(output), "\n")

	// Verify expected commands are present
	expected := []string{"attach", "clean", "completion", "doctor", "init", "kill", "ls", "merge", "open", "path", "push", "resume", "run", "show", "stop", "verify"}
	for _, cmd := range expected {
		found := false
		for _, c := range commands {
			if c == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing command in completion: %s", cmd)
		}
	}

	// Verify __complete is NOT in the list (hidden)
	for _, c := range commands {
		if c == "__complete" {
			t.Error("__complete should not be in command completion")
		}
	}
}

// TestCompleteMergeStrategies tests that __complete merge_strategies returns correct flags.
func TestCompleteMergeStrategies(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	opts := CompleteOpts{Kind: CompleteKindMergeStrategies}
	err := Complete(ctx, cr, fsys, "/tmp", opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(merge_strategies) failed: %v", err)
	}

	output := stdout.String()
	strategies := strings.Split(strings.TrimSpace(output), "\n")

	expected := []string{"--merge", "--rebase", "--squash"}
	if len(strategies) != len(expected) {
		t.Errorf("expected %d strategies, got %d", len(expected), len(strategies))
	}

	for i, s := range expected {
		if strategies[i] != s {
			t.Errorf("expected strategy %d to be %s, got %s", i, s, strategies[i])
		}
	}
}

// TestCompleteRunners tests that __complete runners returns built-in defaults.
func TestCompleteRunners(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	opts := CompleteOpts{Kind: CompleteKindRunners}
	err := Complete(ctx, cr, fsys, "/tmp", opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runners) failed: %v", err)
	}

	output := stdout.String()
	runners := strings.Split(strings.TrimSpace(output), "\n")

	// Should have at least the built-in defaults
	hasClaude := false
	hasCodex := false
	for _, r := range runners {
		if r == "claude" {
			hasClaude = true
		}
		if r == "codex" {
			hasCodex = true
		}
	}

	if !hasClaude {
		t.Error("missing built-in runner: claude")
	}
	if !hasCodex {
		t.Error("missing built-in runner: codex")
	}
}

// TestCompleteRuns_EmptyStore tests that __complete runs returns empty for no runs.
func TestCompleteRuns_EmptyStore(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	// Create a temp directory structure for data dir
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "agency-data")

	// Set AGENCY_DATA_DIR to control where completion looks
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Create fake git repo
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	cr := newCompletionStubRunner()
	// Stub git rev-parse to return repo root
	cr.On("git", []string{"rev-parse", "--show-toplevel"}, exec.CmdResult{
		ExitCode: 0,
		Stdout:   repoDir + "\n",
	})
	// Stub git config to return origin URL
	cr.On("git", []string{"config", "--get", "remote.origin.url"}, exec.CmdResult{
		ExitCode: 0,
		Stdout:   "git@github.com:test/repo.git\n",
	})

	fsys := newCompletionStubFS()

	opts := CompleteOpts{Kind: CompleteKindRuns}
	err := Complete(ctx, cr, fsys, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runs) failed: %v", err)
	}

	// Should be empty (no runs)
	output := strings.TrimSpace(stdout.String())
	if output != "" {
		t.Errorf("expected empty output for no runs, got: %q", output)
	}
}

// TestCompleteRuns_WithRuns tests that __complete runs returns correct candidates.
func TestCompleteRuns_WithRuns(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	// Create a temp directory structure for data dir
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "agency-data")

	// Set AGENCY_DATA_DIR to control where completion looks
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Create repo structure
	repoID := "abcd1234ef567890"
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	// Create two runs with different names
	now := time.Now().UTC()
	run1ID := "20260120120000-a1b2"
	run2ID := "20260120130000-c3d4"

	// Run 1 - older
	run1Dir := filepath.Join(runsDir, run1ID)
	if err := os.MkdirAll(run1Dir, 0755); err != nil {
		t.Fatalf("failed to create run1 dir: %v", err)
	}
	meta1 := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         run1ID,
		RepoID:        repoID,
		Name:          "feature-one",
		Runner:        "claude",
		CreatedAt:     now.Add(-1 * time.Hour).Format(time.RFC3339),
	}
	meta1JSON, _ := json.Marshal(meta1)
	if err := os.WriteFile(filepath.Join(run1Dir, "meta.json"), meta1JSON, 0644); err != nil {
		t.Fatalf("failed to write meta1.json: %v", err)
	}

	// Run 2 - newer
	run2Dir := filepath.Join(runsDir, run2ID)
	if err := os.MkdirAll(run2Dir, 0755); err != nil {
		t.Fatalf("failed to create run2 dir: %v", err)
	}
	meta2 := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         run2ID,
		RepoID:        repoID,
		Name:          "feature-two",
		Runner:        "codex",
		CreatedAt:     now.Format(time.RFC3339),
	}
	meta2JSON, _ := json.Marshal(meta2)
	if err := os.WriteFile(filepath.Join(run2Dir, "meta.json"), meta2JSON, 0644); err != nil {
		t.Fatalf("failed to write meta2.json: %v", err)
	}

	// Create fake git repo
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	cr := newCompletionStubRunner()
	// Stub git commands (not used in all-repos mode)
	cr.On("git", []string{"rev-parse", "--show-toplevel"}, exec.CmdResult{
		ExitCode: 0,
		Stdout:   repoDir + "\n",
	})
	cr.On("git", []string{"config", "--get", "remote.origin.url"}, exec.CmdResult{
		ExitCode: 1, // No origin
	})

	fsys := newCompletionStubFS()

	// Use --all-repos to bypass repo matching
	opts := CompleteOpts{Kind: CompleteKindRuns, AllRepos: true}
	err := Complete(ctx, cr, fsys, repoDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runs) failed: %v", err)
	}

	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have 4 candidates: 2 run_ids + 2 unique names
	// Newer run should come first (sorted by created_at DESC)
	if len(lines) != 4 {
		t.Errorf("expected 4 candidates, got %d: %v", len(lines), lines)
	}

	// First should be newer run_id
	if lines[0] != run2ID {
		t.Errorf("expected first candidate to be %s (newer), got %s", run2ID, lines[0])
	}
}

// TestCompleteRuns_DuplicateNames tests that duplicate names are excluded.
func TestCompleteRuns_DuplicateNames(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	// Create a temp directory structure for data dir
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "agency-data")

	// Set AGENCY_DATA_DIR to control where completion looks
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Create repo structure
	repoID := "abcd1234ef567890"
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	// Create two runs with the SAME name
	now := time.Now().UTC()
	run1ID := "20260120120000-a1b2"
	run2ID := "20260120130000-c3d4"

	// Both runs have the same name
	for i, runID := range []string{run1ID, run2ID} {
		runDir := filepath.Join(runsDir, runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatalf("failed to create run dir: %v", err)
		}
		meta := &store.RunMeta{
			SchemaVersion: "1.0",
			RunID:         runID,
			RepoID:        repoID,
			Name:          "duplicate-name", // Same name!
			Runner:        "claude",
			CreatedAt:     now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
		}
		metaJSON, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaJSON, 0644); err != nil {
			t.Fatalf("failed to write meta.json: %v", err)
		}
	}

	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	// Use --all-repos to bypass repo matching
	opts := CompleteOpts{Kind: CompleteKindRuns, AllRepos: true}
	err := Complete(ctx, cr, fsys, tmpDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runs) failed: %v", err)
	}

	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have only 2 candidates: just the run_ids (no names, since they're duplicated)
	if len(lines) != 2 {
		t.Errorf("expected 2 candidates (run_ids only, no duplicate names), got %d: %v", len(lines), lines)
	}

	// Verify name is NOT in output
	for _, line := range lines {
		if line == "duplicate-name" {
			t.Error("duplicate name should NOT be in completion candidates")
		}
	}
}

// TestCompleteRuns_ExcludesArchived tests that archived runs are excluded by default.
func TestCompleteRuns_ExcludesArchived(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	// Create a temp directory structure for data dir
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "agency-data")

	// Set AGENCY_DATA_DIR to control where completion looks
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Create repo structure
	repoID := "abcd1234ef567890"
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	now := time.Now().UTC()

	// Run 1 - active
	run1ID := "20260120120000-a1b2"
	run1Dir := filepath.Join(runsDir, run1ID)
	if err := os.MkdirAll(run1Dir, 0755); err != nil {
		t.Fatalf("failed to create run1 dir: %v", err)
	}
	meta1 := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         run1ID,
		RepoID:        repoID,
		Name:          "active-run",
		Runner:        "claude",
		CreatedAt:     now.Format(time.RFC3339),
	}
	meta1JSON, _ := json.Marshal(meta1)
	if err := os.WriteFile(filepath.Join(run1Dir, "meta.json"), meta1JSON, 0644); err != nil {
		t.Fatalf("failed to write meta1.json: %v", err)
	}

	// Run 2 - archived
	run2ID := "20260120130000-c3d4"
	run2Dir := filepath.Join(runsDir, run2ID)
	if err := os.MkdirAll(run2Dir, 0755); err != nil {
		t.Fatalf("failed to create run2 dir: %v", err)
	}
	meta2 := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         run2ID,
		RepoID:        repoID,
		Name:          "archived-run",
		Runner:        "codex",
		CreatedAt:     now.Format(time.RFC3339),
		Archive: &store.RunMetaArchive{
			ArchivedAt: now.Format(time.RFC3339),
		},
	}
	meta2JSON, _ := json.Marshal(meta2)
	if err := os.WriteFile(filepath.Join(run2Dir, "meta.json"), meta2JSON, 0644); err != nil {
		t.Fatalf("failed to write meta2.json: %v", err)
	}

	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	// Default: exclude archived
	opts := CompleteOpts{Kind: CompleteKindRuns, AllRepos: true}
	err := Complete(ctx, cr, fsys, tmpDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runs) failed: %v", err)
	}

	output := stdout.String()

	// Should NOT contain the archived run
	if strings.Contains(output, run2ID) {
		t.Error("archived run should be excluded by default")
	}
	if strings.Contains(output, "archived-run") {
		t.Error("archived run name should be excluded by default")
	}

	// Should contain the active run
	if !strings.Contains(output, run1ID) {
		t.Error("active run should be included")
	}
}

// TestCompleteRuns_IncludesArchivedWithFlag tests that --include-archived includes archived runs.
func TestCompleteRuns_IncludesArchivedWithFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	// Create a temp directory structure for data dir
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "agency-data")

	// Set AGENCY_DATA_DIR to control where completion looks
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	// Create repo structure
	repoID := "abcd1234ef567890"
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	now := time.Now().UTC()

	// Run - archived
	runID := "20260120130000-c3d4"
	runDir := filepath.Join(runsDir, runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	meta := &store.RunMeta{
		SchemaVersion: "1.0",
		RunID:         runID,
		RepoID:        repoID,
		Name:          "archived-run",
		Runner:        "codex",
		CreatedAt:     now.Format(time.RFC3339),
		Archive: &store.RunMetaArchive{
			ArchivedAt: now.Format(time.RFC3339),
		},
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaJSON, 0644); err != nil {
		t.Fatalf("failed to write meta.json: %v", err)
	}

	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	// With --include-archived
	opts := CompleteOpts{Kind: CompleteKindRuns, AllRepos: true, IncludeArchived: true}
	err := Complete(ctx, cr, fsys, tmpDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runs) failed: %v", err)
	}

	output := stdout.String()

	// Should contain the archived run
	if !strings.Contains(output, runID) {
		t.Error("archived run should be included with --include-archived")
	}
}

// TestCompleteRuns_NotInRepo tests that __complete runs returns empty when not in a repo.
func TestCompleteRuns_NotInRepo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()

	tmpDir := t.TempDir()

	cr := newCompletionStubRunner()
	// Stub git to fail (not in a repo)
	cr.On("git", []string{"rev-parse", "--show-toplevel"}, exec.CmdResult{
		ExitCode: 128,
		Stderr:   "fatal: not a git repository",
	})

	fsys := newCompletionStubFS()

	// Default: current repo only
	opts := CompleteOpts{Kind: CompleteKindRuns}
	err := Complete(ctx, cr, fsys, tmpDir, opts, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Complete(runs) should not fail: %v", err)
	}

	// Should be empty (per spec: not in repo -> print nothing, exit 0)
	output := strings.TrimSpace(stdout.String())
	if output != "" {
		t.Errorf("expected empty output when not in repo, got: %q", output)
	}
}

// TestComplete_SilentOnError tests that completion is silent on internal errors.
func TestComplete_SilentOnError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	cr := newCompletionStubRunner()
	fsys := newCompletionStubFS()

	// Use invalid kind
	opts := CompleteOpts{Kind: CompleteKind("invalid")}
	err := Complete(ctx, cr, fsys, "/tmp", opts, &stdout, &stderr)

	// Should NOT return error (silent failure)
	if err != nil {
		t.Errorf("expected nil error for silent failure, got: %v", err)
	}

	// Should produce no output
	if stdout.String() != "" {
		t.Errorf("expected empty stdout, got: %q", stdout.String())
	}
}
