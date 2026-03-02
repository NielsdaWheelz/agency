//go:build e2e

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.MkdirAll(configDir, 0755))
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
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userConfig), 0644))

	// Create a git repo
	repoRoot := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))
	runCmd(t, ctx, cr, repoRoot, "git", "init")

	// Create initial commit
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# Test\n"), 0644))
	runCmd(t, ctx, cr, repoRoot, "git", "add", ".")
	runCmd(t, ctx, cr, repoRoot, "git", "commit", "-m", "initial")

	// Test 1: agency init creates CLAUDE.md
	t.Run("init creates CLAUDE.md", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Init(ctx, cr, fsys, repoRoot, InitOpts{}, &stdout, &stderr)
		require.NoError(t, err, "init failed\nstderr: %s", stderr.String())

		// Verify CLAUDE.md was created
		claudeMDPath := filepath.Join(repoRoot, scaffold.ClaudeMDFileName)
		data, err := os.ReadFile(claudeMDPath)
		require.NoError(t, err, "CLAUDE.md not created")

		// Verify content mentions runner_status.json
		assert.Contains(t, string(data), "runner_status.json")
		assert.Contains(t, string(data), "working")

		// Verify output mentions claude_md
		assert.Contains(t, stdout.String(), "claude_md: created")
	})

	// Test 2: init does not overwrite existing CLAUDE.md
	t.Run("init does not overwrite CLAUDE.md", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Init(ctx, cr, fsys, repoRoot, InitOpts{Force: true}, &stdout, &stderr)
		require.NoError(t, err, "init --force failed")

		assert.Contains(t, stdout.String(), "claude_md: exists")
	})

	// Simulate a run with runner_status.json
	repoID := "abcd1234ef567890" // Simulated repo ID
	runID := time.Now().Format("20060102150405") + "-test"

	// Create run directory and meta
	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "logs"), 0755))

	// Create worktree directory (simulated)
	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	require.NoError(t, os.MkdirAll(worktreePath, 0755))

	// Test 3: worktree scaffold creates runner_status.json
	t.Run("worktree scaffold creates runner_status.json", func(t *testing.T) {
		err := worktree.ScaffoldWorkspaceOnly(fsys, worktreePath, "test-run")
		require.NoError(t, err, "scaffold failed")

		// Verify runner_status.json was created
		statusPath := runnerstatus.StatusPath(worktreePath)
		data, err := os.ReadFile(statusPath)
		require.NoError(t, err, "runner_status.json not created")

		var status runnerstatus.RunnerStatus
		require.NoError(t, json.Unmarshal(data, &status), "failed to parse runner_status.json")

		assert.Equal(t, runnerstatus.StatusWorking, status.Status)
		assert.Equal(t, "Starting work", status.Summary)
	})

	// Create meta.json for the run
	meta := store.NewRunMeta(runID, repoID, "test-run", "echo", "echo", "main", "agency/test-run-test", worktreePath, time.Now())
	meta.TmuxSessionName = "agency_" + runID
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644))

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
		require.NoError(t, os.WriteFile(statusPath, data, 0644))

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		require.NoError(t, err, "ls failed\nstderr: %s", stderr.String())

		output := stdout.String()
		// Should show "needs input" status
		assert.Contains(t, output, "needs input")
		// Should show summary
		assert.Contains(t, output, "Which auth library")
	})

	// Test 5: show displays questions/blockers/how_to_test
	t.Run("show displays runner status details", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Show(ctx, cr, fsys, worktreePath, ShowOpts{RunID: runID}, &stdout, &stderr)
		require.NoError(t, err, "show failed\nstderr: %s", stderr.String())

		output := stdout.String()
		// Should show runner_status section
		assert.Contains(t, output, "runner_status:")
		// Should show status
		assert.Contains(t, output, "status: needs_input")
		// Should show questions
		assert.Contains(t, output, "questions:")
		assert.Contains(t, output, "OAuth2 or JWT?")
	})

	// Test 6: show JSON includes runner_status
	t.Run("show JSON includes runner_status", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := Show(ctx, cr, fsys, worktreePath, ShowOpts{RunID: runID, JSON: true}, &stdout, &stderr)
		require.NoError(t, err, "show --json failed\nstderr: %s", stderr.String())

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
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), "failed to parse JSON output\noutput: %s", stdout.String())

		require.NotNil(t, result.Data.Derived.RunnerStatus, "runner_status is nil in JSON output\noutput: %s", stdout.String())
		assert.Equal(t, "needs_input", result.Data.Derived.RunnerStatus.Status)
		require.Len(t, result.Data.Derived.RunnerStatus.Questions, 2)
	})

	// Test 7: ls falls back when no status file
	t.Run("ls fallback when no status file", func(t *testing.T) {
		// Remove the status file
		statusPath := runnerstatus.StatusPath(worktreePath)
		require.NoError(t, os.Remove(statusPath))

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		require.NoError(t, err, "ls failed\nstderr: %s", stderr.String())

		output := stdout.String()
		// Should fall back to "idle" (no tmux session in test)
		assert.Contains(t, output, "idle")
	})

	// Test 8: ls handles invalid status file gracefully
	t.Run("ls handles invalid status file", func(t *testing.T) {
		// Create an invalid status file
		statusPath := runnerstatus.StatusPath(worktreePath)
		require.NoError(t, os.WriteFile(statusPath, []byte("not valid json"), 0644))

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		require.NoError(t, err, "ls failed with invalid status file\nstderr: %s", stderr.String())

		output := stdout.String()
		// Should fall back to tmux detection (idle in this case)
		assert.True(t, strings.Contains(output, "idle") || strings.Contains(output, "active"),
			"ls should fall back gracefully with invalid status file\noutput: %s", output)
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
		require.NoError(t, os.WriteFile(statusPath, []byte(invalidStatus), 0644))

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		require.NoError(t, err, "ls failed with invalid status value")

		// Should not crash, should fall back
		output := stdout.String()
		assert.Contains(t, output, runID)
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
				require.NoError(t, os.WriteFile(statusPath, data, 0644))

				var stdout, stderr bytes.Buffer
				err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
				require.NoError(t, err, "ls failed")

				assert.Contains(t, stdout.String(), tc.wantDisplay)
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
	require.NoError(t, os.MkdirAll(configDir, 0755))
	userConfig := `{"version": 1, "defaults": {"runner": "echo", "editor": "echo"}}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(userConfig), 0644))

	// Create a git repo
	repoRoot := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))
	runCmd(t, ctx, cr, repoRoot, "git", "init")
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# Test\n"), 0644))
	runCmd(t, ctx, cr, repoRoot, "git", "add", ".")
	runCmd(t, ctx, cr, repoRoot, "git", "commit", "-m", "initial")

	// Set up simulated run
	repoID := "stall1234ef567890"
	runID := "20260119120000-stal"

	runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "logs"), 0755))

	worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
	require.NoError(t, os.MkdirAll(worktreePath, 0755))

	// Scaffold workspace
	require.NoError(t, worktree.ScaffoldWorkspaceOnly(fsys, worktreePath, "stall-test"))

	// Create meta.json
	meta := store.NewRunMeta(runID, repoID, "stall-test", "echo", "echo", "main", "agency/stall-test", worktreePath, time.Now())
	meta.TmuxSessionName = "agency_" + runID
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "meta.json"), metaData, 0644))

	// Note: We can't easily test stalled detection without a real tmux session
	// because the stall check requires tmux to be active.
	// The unit tests in watchdog_test.go cover the stall logic.
	// Here we just verify the code path doesn't crash.

	t.Run("ls with old status file does not crash", func(t *testing.T) {
		// Set the status file modification time to 30 minutes ago
		statusPath := runnerstatus.StatusPath(worktreePath)
		oldTime := time.Now().Add(-30 * time.Minute)
		require.NoError(t, os.Chtimes(statusPath, oldTime, oldTime))

		var stdout, stderr bytes.Buffer
		err := LS(ctx, cr, fsys, worktreePath, LSOpts{}, &stdout, &stderr)
		require.NoError(t, err, "ls failed")

		// Without tmux session, it should show "idle" not "stalled"
		// (stalled requires tmux to be active)
		assert.Contains(t, stdout.String(), runID)
	})
}
