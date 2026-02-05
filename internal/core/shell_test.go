package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellEscapePosix_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"simple", "abc", "'abc'"},
		{"single quote", "a'b", "'a'\"'\"'b'"},
		{"empty string", "", "''"},
		{"spaces", "a b c", "'a b c'"},
		{"path with spaces", "/tmp/a b", "'/tmp/a b'"},
		{"double quotes", `a"b`, `'a"b'`},
		{"backslash", `a\b`, `'a\b'`},
		{"dollar sign", "a$b", "'a$b'"},
		{"backticks", "a`b", "'a`b'"},
		{"multiple single quotes", "a''b", "'a'\"'\"''\"'\"'b'"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ShellEscapePosix(tt.input)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestShellEscapePosix_EmptyString(t *testing.T) {
	t.Parallel()

	got := ShellEscapePosix("")
	assert.Equal(t, "''", got)
}

func TestShellEscapePosix_Newline(t *testing.T) {
	t.Parallel()

	got := ShellEscapePosix("a\nb")
	expect := "'a\nb'"
	assert.Equal(t, expect, got)
}

func TestBuildRunnerShellScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		worktree  string
		runnerCmd string
		expect    string
	}{
		{
			name:      "simple path",
			worktree:  "/tmp/worktree",
			runnerCmd: "claude",
			expect:    "cd '/tmp/worktree' && exec claude",
		},
		{
			name:      "path with spaces",
			worktree:  "/tmp/a b",
			runnerCmd: "claude --foo",
			expect:    "cd '/tmp/a b' && exec claude --foo",
		},
		{
			name:      "path with single quote",
			worktree:  "/tmp/it's",
			runnerCmd: "codex",
			expect:    "cd '/tmp/it'\"'\"'s' && exec codex",
		},
		{
			name:      "empty runner cmd",
			worktree:  "/tmp/test",
			runnerCmd: "",
			expect:    "cd '/tmp/test' && exec ",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BuildRunnerShellScript(tt.worktree, tt.runnerCmd)
			assert.Equal(t, tt.expect, got)
		})
	}
}
