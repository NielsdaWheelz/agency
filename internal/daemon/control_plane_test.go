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
			name:      "unknown runner: any args allowed",
			runner:    "unknown",
			args:      []string{"--anything"},
			wantError: false,
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
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--verbose", "fix the bug"},
		},
		{
			name:        "claude with extra args",
			runner:      "claude",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "opus"},
			wantArgs:    []string{"-p", "--output-format", "stream-json", "--verbose", "--model", "opus", "fix the bug"},
		},
		{
			name:        "codex basic - includes -C flag",
			runner:      "codex",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   nil,
			wantArgs:    []string{"-C", "/sandbox/path", "exec", "--json", "fix the bug"},
		},
		{
			name:        "codex with extra args",
			runner:      "codex",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--model", "gpt-4"},
			wantArgs:    []string{"-C", "/sandbox/path", "exec", "--json", "--model", "gpt-4", "fix the bug"},
		},
		{
			name:        "unknown runner",
			runner:      "unknown",
			prompt:      "fix the bug",
			sandboxPath: "/sandbox/path",
			extraArgs:   []string{"--flag"},
			wantArgs:    []string{"--flag", "fix the bug"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildRunnerArgsWithSandbox(tt.runner, tt.prompt, tt.sandboxPath, tt.extraArgs)
			require.Equal(t, len(tt.wantArgs), len(got), "got: %v, want: %v", got, tt.wantArgs)
			for i := range got {
				assert.Equal(t, tt.wantArgs[i], got[i], "arg[%d]", i)
			}
		})
	}
}

func TestIdempotencyKey(t *testing.T) {
	t.Parallel()
	key := idempotencyKey("repo123", "client-uuid-456")
	expected := "repo123:client-uuid-456"
	assert.Equal(t, expected, key)
}
