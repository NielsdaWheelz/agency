package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
	"github.com/NielsdaWheelz/agency/internal/scaffold"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/worktree"
)

// TestRunnerStatusE2E tests the full runner status lifecycle.
// This test does not require GitHub or tmux - it simulates the state.
func TestRunnerStatusE2E(t *testing.T) {
	if os.Getenv("AGENCY_E2E") == "" {
		t.Skip("set AGENCY_E2E=1 to enable e2e tests")
	}

	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()

	// Create temp directory structure
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "config")
	cacheDir := filepath.Join(tmpDir, "cache")

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	t.Setenv("AGENCY_CACHE_DIR", cacheDir)
	testutil.HermeticGitEnv(t)

	// Create config dir and user config
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	userConfig := `{
  "version": 1,
  "defaults": {
    "runner": "echo",
    "editor": "code"
  },
  "runners": {
    "echo": "echo"
  },
  "editors": {
    "code": "echo"
  }
}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a git repo
	repoRoot := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, ctx, cr, repoRoot, "git", "init")

	// Create initial commit
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, ctx, cr, repoRoot, "git", "add", ".")
	runCmd(t, ctx, cr, repoRoot, "git", "commit", "-m", "initial")

	// Test 1: agency init creates CLAUDE.md
	t.Run("init creates CLAUDE.md", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Init(ctx, cr, fsys, repoRoot, InitOpts{}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("init failed: %v\nstderr: %s", err, stderr.String())
		}

		// Verify CLAUDE.md was created
		claudeMDPath := filepath.Join(repoRoot, scaffold.ClaudeMDFileName)
		data, err := os.ReadFile(claudeMDPath)
		if err != nil {
			t.Fatalf("CLAUDE.md not created: %v", err)
		}

		// Verify content mentions runner_status.json
		if !strings.Contains(string(data), "runner_status.json") {
			t.Error("CLAUDE.md does not mention runner_status.json")
		}
		if !strings.Contains(string(data), "working") {
			t.Error("CLAUDE.md does not mention 'working' status")
		}

		// Verify output mentions claude_md
		if !strings.Contains(stdout.String(), "claude_md: created") {
			t.Errorf("init output missing claude_md: created\noutput: %s", stdout.String())
		}
	})

	// Test 2: init does not overwrite existing CLAUDE.md
	t.Run("init does not overwrite CLAUDE.md", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Init(ctx, cr, fsys, repoRoot, InitOpts{Force: true}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("init --force failed: %v", err)
		}

		if !strings.Contains(stdout.String(), "claude_md: exists") {
			t.Errorf("init output should say claude_md: exists\noutput: %s", stdout.String())
		}
	})

	// Simulate a run with runner_status.json
	repoID := "abcd1234ef567890" // Simulated repo ID
	runID := time.Now().Format("20060102150405") + "-test"

	// Create run directory and meta
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(filepath.Join(runDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create worktree directory (simulated)
	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatal(err)
	}

	// Test 3: worktree scaffold creates runner_status.json
	t.Run("worktree scaffold creates runner_status.json", func(t *testing.T) {
		err := worktree.ScaffoldWorkspaceOnly(fsys, worktreePath, "test-run")
		if err != nil {
			t.Fatalf("scaffold failed: %v", err)
		}

		// Verify runner_status.json was created
		statusPath := runnerstatus.StatusPath(worktreePath)
		data, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatalf("runner_status.json not created: %v", err)
		}

		var status runnerstatus.RunnerStatus
		if err := json.Unmarshal(data, &status); err != nil {
			t.Fatalf("failed to parse runner_status.json: %v", err)
		}

		if status.Status != runnerstatus.StatusWorking {
			t.Errorf("initial status = %q, want %q", status.Status, runnerstatus.StatusWorking)
		}
		if status.Summary != "Starting work" {
			t.Errorf("initial summary = %q, want %q", status.Summary, "Starting work")
		}
	})

	// Create meta.json for the run
	meta := store.NewRunMeta(runID, repoID, "test-run", "echo", "echo", "main", "agency/test-run-test", worktreePath, time.Now())
	meta.TmuxSessionName = "agency_" + runID
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644); err != nil {
		t.Fatal(err)
	}

	// Test 4: ls shows runner-reported status from file
	t.Run("ls shows runner status from file", func(t *testing.T) {
		// Update runner_status.json to needs_input
		statusPath := runnerstatus.StatusPath(worktreePath)
		newStatus := &runnerstatus.RunnerStatus{
			SchemaVersion: runnerstatus.SchemaVersion,
			Status:        runnerstatus.StatusNeedsInput,
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
			Summary:       "Which auth library should I use?",
			Questions:     []string{"OAuth2 or JWT?", "What session store?"},
			Blockers:      []string{},
			HowToTest:     "",
			Risks:         []string{},
		}
		data, _ := json.MarshalIndent(newStatus, "", "  ")
		if err := os.WriteFile(statusPath, data, 0644); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("ls failed: %v\nstderr: %s", err, stderr.String())
		}

		output := stdout.String()
		// Should show "needs input" status
		if !strings.Contains(output, "needs input") {
			t.Errorf("ls output missing 'needs input' status\noutput: %s", output)
		}
		// Should show summary
		if !strings.Contains(output, "Which auth library") {
			t.Errorf("ls output missing summary\noutput: %s", output)
		}
	})

	// Test 5: show displays questions/blockers/how_to_test
	t.Run("show displays runner status details", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Show(ctx, cr, fsys, worktreePath, ShowOpts{RunID: runID}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("show failed: %v\nstderr: %s", err, stderr.String())
		}

		output := stdout.String()
		// Should show runner_status section
		if !strings.Contains(output, "runner_status:") {
			t.Errorf("show output missing runner_status section\noutput: %s", output)
		}
		// Should show status
		if !strings.Contains(output, "status: needs_input") {
			t.Errorf("show output missing status: needs_input\noutput: %s", output)
		}
		// Should show questions
		if !strings.Contains(output, "questions:") {
			t.Errorf("show output missing questions\noutput: %s", output)
		}
		if !strings.Contains(output, "OAuth2 or JWT?") {
			t.Errorf("show output missing question content\noutput: %s", output)
		}
	})

	// Test 6: show JSON includes runner_status
	t.Run("show JSON includes runner_status", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Show(ctx, cr, fsys, worktreePath, ShowOpts{RunID: runID, JSON: true}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("show --json failed: %v\nstderr: %s", err, stderr.String())
		}

		var result struct {
			Data struct {
				Derived struct {
					RunnerStatus *struct {
						Status    string   `json:"status"`
						Questions []string `json:"questions"`
					} `json:"runner_status"`
				} `json:"derived"`
			} `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, stdout.String())
		}

		if result.Data.Derived.RunnerStatus == nil {
			t.Fatalf("runner_status is nil in JSON output\noutput: %s", stdout.String())
		}
		if result.Data.Derived.RunnerStatus.Status != "needs_input" {
			t.Errorf("runner_status.status = %q, want %q", result.Data.Derived.RunnerStatus.Status, "needs_input")
		}
		if len(result.Data.Derived.RunnerStatus.Questions) != 2 {
			t.Errorf("runner_status.questions length = %d, want 2", len(result.Data.Derived.RunnerStatus.Questions))
		}
	})

	// Test 7: ls falls back when no status file
	t.Run("ls fallback when no status file", func(t *testing.T) {
		// Remove the status file
		statusPath := runnerstatus.StatusPath(worktreePath)
		if err := os.Remove(statusPath); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("ls failed: %v\nstderr: %s", err, stderr.String())
		}

		output := stdout.String()
		// Should fall back to "idle" (no tmux session in test)
		if !strings.Contains(output, "idle") {
			t.Errorf("ls output should fall back to 'idle' when no status file\noutput: %s", output)
		}
	})

	// Test 8: ls handles invalid status file gracefully
	t.Run("ls handles invalid status file", func(t *testing.T) {
		// Create an invalid status file
		statusPath := runnerstatus.StatusPath(worktreePath)
		if err := os.WriteFile(statusPath, []byte("not valid json"), 0644); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("ls failed with invalid status file: %v\nstderr: %s", err, stderr.String())
		}

		output := stdout.String()
		// Should fall back to tmux detection (idle in this case)
		if !strings.Contains(output, "idle") && !strings.Contains(output, "active") {
			t.Errorf("ls should fall back gracefully with invalid status file\noutput: %s", output)
		}
	})

	// Test 9: ls handles status file with invalid status value gracefully
	t.Run("ls handles invalid status value", func(t *testing.T) {
		statusPath := runnerstatus.StatusPath(worktreePath)
		invalidStatus := `{
			"schema_version": "1.0",
			"status": "unknown_status",
			"updated_at": "2026-01-19T12:00:00Z",
			"summary": "Test"
		}`
		if err := os.WriteFile(statusPath, []byte(invalidStatus), 0644); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("ls failed with invalid status value: %v", err)
		}

		// Should not crash, should fall back
		output := stdout.String()
		if !strings.Contains(output, runID) {
			t.Errorf("ls output missing run_id\noutput: %s", output)
		}
	})

	// Test 10: different runner statuses display correctly
	t.Run("all runner statuses display correctly", func(t *testing.T) {
		statusPath := runnerstatus.StatusPath(worktreePath)
		testCases := []struct {
			status      runnerstatus.Status
			wantDisplay string
			extra       map[string]interface{}
		}{
			{
				status:      runnerstatus.StatusWorking,
				wantDisplay: "working",
			},
			{
				status:      runnerstatus.StatusNeedsInput,
				wantDisplay: "needs input",
				extra:       map[string]interface{}{"questions": []string{"Q1"}},
			},
			{
				status:      runnerstatus.StatusBlocked,
				wantDisplay: "blocked",
				extra:       map[string]interface{}{"blockers": []string{"B1"}},
			},
			{
				status:      runnerstatus.StatusReadyForReview,
				wantDisplay: "ready for review",
				extra:       map[string]interface{}{"how_to_test": "Run tests"},
			},
		}

		for _, tc := range testCases {
			t.Run(string(tc.status), func(t *testing.T) {
				newStatus := &runnerstatus.RunnerStatus{
					SchemaVersion: runnerstatus.SchemaVersion,
					Status:        tc.status,
					UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
					Summary:       "Test summary",
					Questions:     []string{},
					Blockers:      []string{},
					HowToTest:     "",
					Risks:         []string{},
				}
				if q, ok := tc.extra["questions"].([]string); ok {
					newStatus.Questions = q
				}
				if b, ok := tc.extra["blockers"].([]string); ok {
					newStatus.Blockers = b
				}
				if h, ok := tc.extra["how_to_test"].(string); ok {
					newStatus.HowToTest = h
				}

				data, _ := json.MarshalIndent(newStatus, "", "  ")
				if err := os.WriteFile(statusPath, data, 0644); err != nil {
					t.Fatal(err)
				}

				var stdout, stderr bytes.Buffer
				err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
				if err != nil {
					t.Fatalf("ls failed: %v", err)
				}

				if !strings.Contains(stdout.String(), tc.wantDisplay) {
					t.Errorf("ls output missing status %q\noutput: %s", tc.wantDisplay, stdout.String())
				}
			})
		}
	})
}

// TestRunnerStatusStalledDetection tests stall detection via ls.
// This requires simulating an old status file.
func TestRunnerStatusStalledDetection(t *testing.T) {
	if os.Getenv("AGENCY_E2E") == "" {
		t.Skip("set AGENCY_E2E=1 to enable e2e tests")
	}

	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("AGENCY_CACHE_DIR", filepath.Join(tmpDir, "cache"))
	testutil.HermeticGitEnv(t)

	// Create config
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	userConfig := `{"version": 1, "defaults": {"runner": "echo", "editor": "echo"}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a git repo
	repoRoot := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, ctx, cr, repoRoot, "git", "init")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, ctx, cr, repoRoot, "git", "add", ".")
	runCmd(t, ctx, cr, repoRoot, "git", "commit", "-m", "initial")

	// Set up simulated run
	repoID := "stall1234ef567890"
	runID := "20260119120000-stal"

	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(filepath.Join(runDir, "logs"), 0755); err != nil {
		t.Fatal(err)
	}

	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatal(err)
	}

	// Scaffold workspace
	if err := worktree.ScaffoldWorkspaceOnly(fsys, worktreePath, "stall-test"); err != nil {
		t.Fatal(err)
	}

	// Create meta.json
	meta := store.NewRunMeta(runID, repoID, "stall-test", "echo", "echo", "main", "agency/stall-test", worktreePath, time.Now())
	meta.TmuxSessionName = "agency_" + runID
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644); err != nil {
		t.Fatal(err)
	}

	// Note: We can't easily test stalled detection without a real tmux session
	// because the stall check requires tmux to be active.
	// The unit tests in watchdog_test.go cover the stall logic.
	// Here we just verify the code path doesn't crash.

	t.Run("ls with old status file does not crash", func(t *testing.T) {
		// Set the status file modification time to 30 minutes ago
		statusPath := runnerstatus.StatusPath(worktreePath)
		oldTime := time.Now().Add(-30 * time.Minute)
		if err := os.Chtimes(statusPath, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("ls failed: %v", err)
		}

		// Without tmux session, it should show "idle" not "stalled"
		// (stalled requires tmux to be active)
		if !strings.Contains(stdout.String(), runID) {
			t.Errorf("ls output missing run_id\noutput: %s", stdout.String())
		}
	})
}
