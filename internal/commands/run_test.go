package commands

import (
	"bytes"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/pipeline"
)

func TestPrintRunSuccess(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printRunSuccess(&buf, tt.result, tt.detached)
			if buf.String() != tt.expected {
				t.Errorf("printRunSuccess() output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), tt.expected)
			}
		})
	}
}

func TestPrintRunSuccessOrderAndKeys(t *testing.T) {
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
			t.Errorf("detached: missing line %d: expected key %s", i, key)
			continue
		}
		if !bytes.HasPrefix(lines[i], []byte(key)) {
			t.Errorf("detached: line %d: expected prefix %q, got %q", i, key, string(lines[i]))
		}
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
			t.Errorf("attached: missing line %d: expected key %s", i, key)
			continue
		}
		if !bytes.HasPrefix(lines[i], []byte(key)) {
			t.Errorf("attached: line %d: expected prefix %q, got %q", i, key, string(lines[i]))
		}
	}
	// Verify no extra lines (just the empty line from trailing newline)
	if len(lines) > len(expectedKeysAttached)+1 {
		t.Errorf("attached: expected %d lines, got %d", len(expectedKeysAttached)+1, len(lines))
	}
}

func TestRunResultWarnings(t *testing.T) {
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

	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}
	if result.Warnings[0].Code != "W_TEST" {
		t.Errorf("expected warning code W_TEST, got %s", result.Warnings[0].Code)
	}
}

func TestRunOptsDefaults(t *testing.T) {
	// Test that empty opts are valid (defaults resolved later)
	opts := RunOpts{}

	if opts.Name != "" {
		t.Error("expected empty title by default")
	}
	if opts.Runner != "" {
		t.Error("expected empty runner by default")
	}
	if opts.Parent != "" {
		t.Error("expected empty parent by default")
	}
	if opts.Attach {
		t.Error("expected attach=false by default")
	}
}

func TestRunOptsWithValues(t *testing.T) {
	opts := RunOpts{
		Name:   "my title",
		Runner: "claude",
		Parent: "main",
		Attach: true,
	}

	if opts.Name != "my title" {
		t.Errorf("expected title 'my title', got %q", opts.Name)
	}
	if opts.Runner != "claude" {
		t.Errorf("expected runner 'claude', got %q", opts.Runner)
	}
	if opts.Parent != "main" {
		t.Errorf("expected parent 'main', got %q", opts.Parent)
	}
	if !opts.Attach {
		t.Error("expected attach=true")
	}
}
