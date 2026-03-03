package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintRunSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		result   *RunResult
		detached bool
		expected string
	}{
		{
			name: "full result detached",
			result: &RunResult{
				RunID:           "20260110120000-a3f2",
				Name:            "test-run",
				Runner:          "claude",
				Parent:          "main",
				Branch:          "agency/test-run-a3f2",
				WorktreePath:    "/path/to/worktree",
				TmuxSessionName: "agency_20260110120000-a3f2",
			},
			detached: true,
			expected: `run_id: 20260110120000-a3f2
name: test-run
runner: claude
parent: main
branch: agency/test-run-a3f2
worktree: /path/to/worktree
tmux: agency_20260110120000-a3f2
next: agency attach test-run
`,
		},
		{
			name: "full result attached (no next hint)",
			result: &RunResult{
				RunID:           "20260110120000-a3f2",
				Name:            "test-run",
				Runner:          "claude",
				Parent:          "main",
				Branch:          "agency/test-run-a3f2",
				WorktreePath:    "/path/to/worktree",
				TmuxSessionName: "agency_20260110120000-a3f2",
			},
			detached: false,
			expected: `run_id: 20260110120000-a3f2
name: test-run
runner: claude
parent: main
branch: agency/test-run-a3f2
worktree: /path/to/worktree
tmux: agency_20260110120000-a3f2
`,
		},
		{
			name: "another run detached",
			result: &RunResult{
				RunID:           "20260110130000-b4c5",
				Name:            "fix-bug",
				Runner:          "codex",
				Parent:          "develop",
				Branch:          "agency/fix-bug-b4c5",
				WorktreePath:    "/tmp/worktree",
				TmuxSessionName: "agency_20260110130000-b4c5",
			},
			detached: true,
			expected: `run_id: 20260110130000-b4c5
name: fix-bug
runner: codex
parent: develop
branch: agency/fix-bug-b4c5
worktree: /tmp/worktree
tmux: agency_20260110130000-b4c5
next: agency attach fix-bug
`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			printRunSuccess(&buf, tt.result, tt.detached)
			assert.Equal(t, tt.expected, buf.String())
		})
	}
}

func TestPrintRunSuccessOrderAndKeys(t *testing.T) {
	t.Parallel()
	// Verify the exact order and keys per spec:
	// 1. run_id
	// 2. name
	// 3. runner
	// 4. parent
	// 5. branch
	// 6. worktree
	// 7. tmux
	// 8. next (only when detached)

	result := &RunResult{
		RunID:           "id",
		Name:            "my-name",
		Runner:          "runner",
		Parent:          "parent",
		Branch:          "branch",
		WorktreePath:    "worktree",
		TmuxSessionName: "tmux",
	}

	// Test detached mode (includes next: hint)
	var buf bytes.Buffer
	printRunSuccess(&buf, result, true)

	expectedKeysDetached := []string{
		"run_id:",
		"name:",
		"runner:",
		"parent:",
		"branch:",
		"worktree:",
		"tmux:",
		"next:",
	}

	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	for i, key := range expectedKeysDetached {
		if i >= len(lines) {
			assert.Fail(t, "detached: missing line", "line %d: expected key %s", i, key)
			continue
		}
		assert.True(t, bytes.HasPrefix(lines[i], []byte(key)), "detached: line %d: expected prefix %q, got %q", i, key, string(lines[i]))
	}

	// Test attached mode (no next: hint)
	buf.Reset()
	printRunSuccess(&buf, result, false)

	expectedKeysAttached := []string{
		"run_id:",
		"name:",
		"runner:",
		"parent:",
		"branch:",
		"worktree:",
		"tmux:",
	}

	lines = bytes.Split(buf.Bytes(), []byte("\n"))
	for i, key := range expectedKeysAttached {
		if i >= len(lines) {
			assert.Fail(t, "attached: missing line", "line %d: expected key %s", i, key)
			continue
		}
		assert.True(t, bytes.HasPrefix(lines[i], []byte(key)), "attached: line %d: expected prefix %q, got %q", i, key, string(lines[i]))
	}
	// Verify no extra lines (just the empty line from trailing newline)
	assert.LessOrEqual(t, len(lines), len(expectedKeysAttached)+1, "attached: too many lines")
}

func TestRunResultWarnings(t *testing.T) {
	t.Parallel()
	// Test that warnings are stored correctly in result
	result := &RunResult{
		RunID:           "id",
		Name:            "title",
		Runner:          "runner",
		Parent:          "parent",
		Branch:          "branch",
		WorktreePath:    "worktree",
		TmuxSessionName: "tmux",
		Warnings: []pipeline.Warning{
			{Code: "W_TEST", Message: "test warning"},
		},
	}

	require.Len(t, result.Warnings, 1)
	assert.Equal(t, "W_TEST", result.Warnings[0].Code)
}

func TestRunOptsDefaults(t *testing.T) {
	t.Parallel()
	// Test that empty opts are valid (defaults resolved later)
	opts := RunOpts{}

	assert.Equal(t, "", opts.Name, "expected empty title by default")
	assert.Equal(t, "", opts.Runner, "expected empty runner by default")
	assert.Equal(t, "", opts.Parent, "expected empty parent by default")
	assert.False(t, opts.Attach, "expected attach=false by default")
}

func TestRunOptsWithValues(t *testing.T) {
	t.Parallel()
	opts := RunOpts{
		Name:   "my title",
		Runner: "claude",
		Parent: "main",
		Attach: true,
	}

	assert.Equal(t, "my title", opts.Name)
	assert.Equal(t, "claude", opts.Runner)
	assert.Equal(t, "main", opts.Parent)
	assert.True(t, opts.Attach, "expected attach=true")
}

func TestOpenCreatedWorkspace_Success(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	editorPath := filepath.Join(homeDir, "editor-ok.sh")
	require.NoError(t, os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	writeUserConfigForEditorTest(t, homeDir, "test-editor", editorPath)

	worktreePath := t.TempDir()
	err := openCreatedWorkspace(context.Background(), agencyexec.NewRealRunner(), agencyfs.NewRealFS(), worktreePath)
	require.NoError(t, err)
}

func TestOpenCreatedWorkspace_EditorExitNonZero(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	editorPath := filepath.Join(homeDir, "editor-fail.sh")
	require.NoError(t, os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 17\n"), 0o755))
	writeUserConfigForEditorTest(t, homeDir, "test-editor", editorPath)

	worktreePath := t.TempDir()
	err := openCreatedWorkspace(context.Background(), agencyexec.NewRealRunner(), agencyfs.NewRealFS(), worktreePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "editor exited with code 17")
}

func TestRunWithDeps_OpenFailureSignalsFailedAndSkipsAttach(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	attachCalled := false
	openCalled := false

	opts := RunOpts{
		Name:   "demo",
		Attach: true,
		Open:   true,
	}

	err := runWithDeps(context.Background(), nil, nil, "/repo", opts, &stdout, &stderr, runExecutionDeps{
		executePipeline: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string, _ RunOpts) (string, error) {
			return "run-1", nil
		},
		loadResult: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string, _ string) (*RunResult, error) {
			return &RunResult{
				RunID:           "run-1",
				Name:            "demo",
				Runner:          "claude",
				Parent:          "main",
				Branch:          "agency/demo",
				WorktreePath:    "/tmp/worktree",
				TmuxSessionName: "agency_run-1",
			}, nil
		},
		openWorkspace: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string) error {
			openCalled = true
			return errors.New("editor exited with code 17")
		},
		attachSession: func(_ context.Context, _ string) error {
			attachCalled = true
			return nil
		},
	})

	require.NoError(t, err)
	assert.True(t, openCalled, "open dispatch should be attempted when --open is requested")
	assert.False(t, attachCalled, "open-on-create path must skip auto-attach")
	assert.Contains(t, stdout.String(), "open_status: failed\n")
	assert.Contains(t, stderr.String(), "warning: workspace created but open dispatch failed: editor exited with code 17")
}

func TestRunWithDeps_OpenSuccessSignalsOpenedAndSkipsAttach(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	attachCalled := false
	openCalled := false

	opts := RunOpts{
		Name:   "demo",
		Attach: true,
		Open:   true,
	}

	err := runWithDeps(context.Background(), nil, nil, "/repo", opts, &stdout, &stderr, runExecutionDeps{
		executePipeline: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string, _ RunOpts) (string, error) {
			return "run-1", nil
		},
		loadResult: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string, _ string) (*RunResult, error) {
			return &RunResult{
				RunID:           "run-1",
				Name:            "demo",
				Runner:          "claude",
				Parent:          "main",
				Branch:          "agency/demo",
				WorktreePath:    "/tmp/worktree",
				TmuxSessionName: "agency_run-1",
			}, nil
		},
		openWorkspace: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string) error {
			openCalled = true
			return nil
		},
		attachSession: func(_ context.Context, _ string) error {
			attachCalled = true
			return nil
		},
	})

	require.NoError(t, err)
	assert.True(t, openCalled, "open dispatch should be attempted when --open is requested")
	assert.False(t, attachCalled, "open-on-create path must skip auto-attach")
	assert.Contains(t, stdout.String(), "open_status: opened\n")
	assert.Empty(t, stderr.String())
}

func TestRunWithDeps_NoOpenAttachesWhenRequested(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	attachCalled := false
	openCalled := false

	opts := RunOpts{
		Name:   "demo",
		Attach: true,
		Open:   false,
	}

	err := runWithDeps(context.Background(), nil, nil, "/repo", opts, &stdout, &stderr, runExecutionDeps{
		executePipeline: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string, _ RunOpts) (string, error) {
			return "run-1", nil
		},
		loadResult: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string, _ string) (*RunResult, error) {
			return &RunResult{
				RunID:           "run-1",
				Name:            "demo",
				Runner:          "claude",
				Parent:          "main",
				Branch:          "agency/demo",
				WorktreePath:    "/tmp/worktree",
				TmuxSessionName: "agency_run-1",
			}, nil
		},
		openWorkspace: func(_ context.Context, _ agencyexec.CommandRunner, _ agencyfs.FS, _ string) error {
			openCalled = true
			return nil
		},
		attachSession: func(_ context.Context, _ string) error {
			attachCalled = true
			return nil
		},
	})

	require.NoError(t, err)
	assert.False(t, openCalled, "open dispatch should not run when --open is disabled")
	assert.True(t, attachCalled, "default attach path should remain unchanged")
	assert.NotContains(t, stdout.String(), "open_status:")
	assert.Empty(t, stderr.String())
}

func writeUserConfigForEditorTest(t *testing.T, homeDir, editorName, editorCmd string) {
	t.Helper()

	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	configPath := filepath.Join(dirs.ConfigDir, "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))

	raw := map[string]any{
		"version": 1,
		"defaults": map[string]string{
			"runner": "claude",
			"editor": editorName,
		},
		"editors": map[string]string{
			editorName: editorCmd,
		},
	}
	data, err := json.Marshal(raw)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o644))
}
