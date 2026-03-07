package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRunnerArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		runner    string
		args      []string
		wantError bool
	}{
		{
			name:      "claude: no args",
			runner:    "claude",
			args:      nil,
			wantError: false,
		},
		{
			name:      "claude: valid args",
			runner:    "claude",
			args:      []string{"--model", "opus"},
			wantError: false,
		},
		{
			name:      "claude: reserved --output-format",
			runner:    "claude",
			args:      []string{"--output-format", "json"},
			wantError: true,
		},
		{
			name:      "claude: reserved --output-format=",
			runner:    "claude",
			args:      []string{"--output-format=json"},
			wantError: true,
		},
		{
			name:      "claude: reserved -p",
			runner:    "claude",
			args:      []string{"-p"},
			wantError: true,
		},
		{
			name:      "claude: reserved --print",
			runner:    "claude",
			args:      []string{"--print"},
			wantError: true,
		},
		{
			name:      "claude: reserved --verbose",
			runner:    "claude",
			args:      []string{"--verbose"},
			wantError: true,
		},
		{
			name:      "claude-code: no args",
			runner:    "claude-code",
			args:      nil,
			wantError: false,
		},
		{
			name:      "claude-code: reserved --output-format",
			runner:    "claude-code",
			args:      []string{"--output-format", "json"},
			wantError: true,
		},
		{
			name:      "codex: no args",
			runner:    "codex",
			args:      nil,
			wantError: false,
		},
		{
			name:      "codex: valid args",
			runner:    "codex",
			args:      []string{"--model", "gpt-4"},
			wantError: false,
		},
		{
			name:      "codex: reserved exec",
			runner:    "codex",
			args:      []string{"exec"},
			wantError: true,
		},
		{
			name:      "codex: reserved --json",
			runner:    "codex",
			args:      []string{"--json"},
			wantError: true,
		},
		{
			name:      "codex: reserved -C",
			runner:    "codex",
			args:      []string{"-C", "/some/path"},
			wantError: true,
		},
		{
			name:      "codex: reserved --cd",
			runner:    "codex",
			args:      []string{"--cd", "/some/path"},
			wantError: true,
		},
		{
			name:      "codex: reserved --cd=",
			runner:    "codex",
			args:      []string{"--cd=/some/path"},
			wantError: true,
		},
		{
			name:      "amp: no args",
			runner:    "amp",
			args:      nil,
			wantError: false,
		},
		{
			name:      "amp: reserved -x",
			runner:    "amp",
			args:      []string{"-x"},
			wantError: true,
		},
		{
			name:      "opencode: no args",
			runner:    "opencode",
			args:      nil,
			wantError: false,
		},
		{
			name:      "opencode: reserved run",
			runner:    "opencode",
			args:      []string{"run"},
			wantError: true,
		},
		{
			name:      "cursor: no args",
			runner:    "cursor",
			args:      nil,
			wantError: false,
		},
		{
			name:      "cursor-cli alias: no args",
			runner:    "cursor-cli",
			args:      nil,
			wantError: false,
		},
		{
			name:      "cursor: reserved -p",
			runner:    "cursor",
			args:      []string{"-p"},
			wantError: true,
		},
		{
			name:      "cursor: reserved --resume",
			runner:    "cursor",
			args:      []string{"--resume", "sess-123"},
			wantError: true,
		},
		{
			name:      "cursor: reserved --workspace",
			runner:    "cursor",
			args:      []string{"--workspace", "/tmp/unsafe"},
			wantError: true,
		},
		{
			name:      "droid: no args",
			runner:    "droid",
			args:      nil,
			wantError: false,
		},
		{
			name:      "droid: reserved exec",
			runner:    "droid",
			args:      []string{"exec"},
			wantError: true,
		},
		{
			name:      "unknown runner: rejected",
			runner:    "unknown",
			args:      []string{"--anything"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateRunnerArgs(tt.runner, tt.args)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsInsideAgencyManagedWorktree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     string
		dataDir  string
		expected bool
	}{
		{
			name:     "normal repo path",
			path:     "/home/user/myrepo",
			dataDir:  "/home/user/.agency",
			expected: false,
		},
		{
			name:     "inside integration worktree tree",
			path:     "/home/user/.agency/repos/abc123/integration_worktrees/123-abcd/tree",
			dataDir:  "/home/user/.agency",
			expected: true,
		},
		{
			name:     "inside integration worktree tree subdir",
			path:     "/home/user/.agency/repos/abc123/integration_worktrees/123-abcd/tree/src/main",
			dataDir:  "/home/user/.agency",
			expected: true,
		},
		{
			name:     "inside sandbox tree",
			path:     "/home/user/.agency/repos/abc123/sandboxes/456-efgh/tree",
			dataDir:  "/home/user/.agency",
			expected: true,
		},
		{
			name:     "inside sandbox tree subdir",
			path:     "/home/user/.agency/repos/abc123/sandboxes/456-efgh/tree/src",
			dataDir:  "/home/user/.agency",
			expected: true,
		},
		{
			name:     "inside repos but not tree",
			path:     "/home/user/.agency/repos/abc123",
			dataDir:  "/home/user/.agency",
			expected: false,
		},
		{
			name:     "inside repos dir but not correct structure",
			path:     "/home/user/.agency/repos/abc123/runs/123/worktree",
			dataDir:  "/home/user/.agency",
			expected: false,
		},
		{
			name:     "different data dir",
			path:     "/home/user/.other/repos/abc123/sandboxes/456-efgh/tree",
			dataDir:  "/home/user/.agency",
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isInsideAgencyManagedWorktree(tt.path, tt.dataDir)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildRunnerArgsWithSandbox(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		runner      string
		prompt      string
		sandboxPath string
		extraArgs   []string
		wantArgs    []string
	}{
		{
			name:        "claude basic",
			runner:      "claude",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   nil,
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose", "--dangerously-skip-permissions"},
		},
		{
			name:        "claude with extra args",
			runner:      "claude",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "opus"},
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose", "--dangerously-skip-permissions", "--model", "opus"},
		},
		{
			name:        "claude-code canonical",
			runner:      "claude-code",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "opus"},
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose", "--dangerously-skip-permissions", "--model", "opus"},
		},
		{
			name:        "codex basic - includes --cd flag",
			runner:      "codex",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   nil,
			wantArgs:    []string{"exec", "--cd", "/sandbox/path", "--json", "--full-auto", "fix the bug"},
		},
		{
			name:        "codex with extra args",
			runner:      "codex",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "gpt-4"},
			wantArgs:    []string{"exec", "--cd", "/sandbox/path", "--json", "--full-auto", "--model", "gpt-4", "fix the bug"},
		},
		{
			name:        "amp basic",
			runner:      "amp",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "amp-fast"},
			wantArgs:    []string{"-x", "--stream-json", "--stream-json-input", "--model", "amp-fast"},
		},
		{
			name:        "opencode basic",
			runner:      "opencode",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "open"},
			wantArgs:    []string{"run", "--mode", "auto", "--model", "open", "fix the bug"},
		},
		{
			name:        "cursor basic",
			runner:      "cursor",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--profile", "default"},
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--force", "--workspace", "/sandbox/path", "--profile", "default", "fix the bug"},
		},
		{
			name:        "cursor-cli alias basic",
			runner:      "cursor-cli",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--profile", "default"},
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--force", "--workspace", "/sandbox/path", "--profile", "default", "fix the bug"},
		},
		{
			name:        "droid basic",
			runner:      "droid",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--agent", "android"},
			wantArgs:    []string{"exec", "--output-format", "stream-json", "--input-format", "stream-json", "--agent", "android"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildRunnerArgsWithSandbox(tt.runner, tt.prompt, tt.sandboxPath, tt.extraArgs)
			require.NoError(t, err)
			require.Equal(t, len(tt.wantArgs), len(got), "got: %v, want: %v", got, tt.wantArgs)
			for i := range got {
				assert.Equal(t, tt.wantArgs[i], got[i], "arg[%d]", i)
			}
		})
	}
}

func TestBuildRunnerArgsForHeaded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		runner    string
		extraArgs []string
		wantArgs  []string
	}{
		{
			name:      "claude headed",
			runner:    "claude",
			extraArgs: []string{"--model", "opus"},
			wantArgs:  []string{"--model", "opus"},
		},
		{
			name:      "codex headed",
			runner:    "codex",
			extraArgs: []string{"--model", "gpt-5"},
			wantArgs:  []string{"--model", "gpt-5"},
		},
		{
			name:      "amp headed",
			runner:    "amp",
			extraArgs: []string{"--model", "amp-fast"},
			wantArgs:  []string{"--model", "amp-fast"},
		},
		{
			name:      "opencode headed",
			runner:    "opencode",
			extraArgs: []string{"--model", "open"},
			wantArgs:  []string{"--model", "open"},
		},
		{
			name:      "cursor headed",
			runner:    "cursor",
			extraArgs: []string{"--model", "cursor-fast"},
			wantArgs:  []string{"--model", "cursor-fast"},
		},
		{
			name:      "droid headed",
			runner:    "droid",
			extraArgs: []string{"--model", "droid-1"},
			wantArgs:  []string{"--model", "droid-1"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildRunnerArgsForHeaded(tt.runner, tt.extraArgs)
			require.NoError(t, err)
			assert.Equal(t, tt.wantArgs, got)
		})
	}
}

func TestIdempotencyKey(t *testing.T) {
	t.Parallel()
	key := idempotencyKey("repo123", "client-uuid-456")
	expected := "repo123:client-uuid-456"
	assert.Equal(t, expected, key)
}
