package core

import "testing"

func TestShellEscapePosix_Table(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got := ShellEscapePosix(tt.input)
			if got != tt.expect {
				t.Errorf("ShellEscapePosix(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestShellEscapePosix_EmptyString(t *testing.T) {
	got := ShellEscapePosix("")
	if got != "''" {
		t.Errorf("ShellEscapePosix(\"\") = %q, want \"''\"", got)
	}
}

func TestShellEscapePosix_Newline(t *testing.T) {
	got := ShellEscapePosix("a\nb")
	expect := "'a\nb'"
	if got != expect {
		t.Errorf("ShellEscapePosix(%q) = %q, want %q", "a\nb", got, expect)
	}
}

func TestBuildRunnerShellScript(t *testing.T) {
	tests := []struct {
		name        string
		worktree    string
		runnerCmd   string
		expect      string
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
		t.Run(tt.name, func(t *testing.T) {
			got := BuildRunnerShellScript(tt.worktree, tt.runnerCmd)
			if got != tt.expect {
				t.Errorf("BuildRunnerShellScript(%q, %q) = %q, want %q",
					tt.worktree, tt.runnerCmd, got, tt.expect)
			}
		})
	}
}
