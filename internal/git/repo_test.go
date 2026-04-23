package git

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

// stubRunner implements exec.CommandRunner for testing.
type stubRunner struct {
	// responses maps (name, args, dir) -> CmdResult
	// key format: "name|arg1,arg2|dir"
	responses map[string]exec.CmdResult
	// calls records all calls made
	calls []stubCall
}

type stubCall struct {
	Name string
	Args []string
	Dir  string
}

func newStubRunner() *stubRunner {
	return &stubRunner{
		responses: make(map[string]exec.CmdResult),
	}
}

func (s *stubRunner) On(name string, args []string, dir string, result exec.CmdResult) {
	key := s.makeKey(name, args, dir)
	s.responses[key] = result
}

func (s *stubRunner) makeKey(name string, args []string, dir string) string {
	return name + "|" + joinArgs(args) + "|" + dir
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	result := args[0]
	for i := 1; i < len(args); i++ {
		result += "," + args[i]
	}
	return result
}

func (s *stubRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	s.calls = append(s.calls, stubCall{Name: name, Args: args, Dir: opts.Dir})

	key := s.makeKey(name, args, opts.Dir)
	if result, ok := s.responses[key]; ok {
		return result, nil
	}

	// Default: command not found
	return exec.CmdResult{ExitCode: 127, Stderr: "command not found"}, nil
}

func (s *stubRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

func TestGetRepoRoot_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	cwd := "/some/project/subdir"
	expectedRoot := "/some/project"

	cr.On("git", []string{"rev-parse", "--show-toplevel"}, cwd, exec.CmdResult{
		Stdout:   expectedRoot + "\n",
		ExitCode: 0,
	})

	root, err := GetRepoRoot(ctx, cr, cwd)

	require.NoError(t, err)
	assert.Equal(t, expectedRoot, root.Path)

	// Verify correct command was called
	require.Len(t, cr.calls, 1)
	call := cr.calls[0]
	assert.Equal(t, "git", call.Name)
	assert.Equal(t, cwd, call.Dir)
}

func TestGetRepoRoot_NotInRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	cwd := "/not/a/repo"

	cr.On("git", []string{"rev-parse", "--show-toplevel"}, cwd, exec.CmdResult{
		Stderr:   "fatal: not a git repository",
		ExitCode: 128,
	})

	_, err := GetRepoRoot(ctx, cr, cwd)

	require.Error(t, err)
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestGetRepoRoot_EmptyOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	cwd := "/some/dir"

	cr.On("git", []string{"rev-parse", "--show-toplevel"}, cwd, exec.CmdResult{
		Stdout:   "",
		ExitCode: 0,
	})

	_, err := GetRepoRoot(ctx, cr, cwd)

	require.Error(t, err)
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestGetRepoRoot_MultiLineOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	cwd := "/some/dir"

	cr.On("git", []string{"rev-parse", "--show-toplevel"}, cwd, exec.CmdResult{
		Stdout:   "/path/one\n/path/two\n",
		ExitCode: 0,
	})

	_, err := GetRepoRoot(ctx, cr, cwd)

	require.Error(t, err)
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestGetRepoRoot_EmptyCwd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	_, err := GetRepoRoot(ctx, cr, "")

	require.Error(t, err)
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestGetRepoRoot_RelativePathNormalized(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	cwd := "/some/project/subdir"

	// Git returns relative path (unusual but possible)
	cr.On("git", []string{"rev-parse", "--show-toplevel"}, cwd, exec.CmdResult{
		Stdout:   "../..\n",
		ExitCode: 0,
	})

	root, err := GetRepoRoot(ctx, cr, cwd)

	require.NoError(t, err)

	// Should be normalized to absolute path
	expected := filepath.Clean("/some")
	assert.Equal(t, expected, root.Path)
}

func TestGetCurrentBranch_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	repoRoot := "/some/project"
	cr.On("git", []string{"branch", "--show-current"}, repoRoot, exec.CmdResult{
		Stdout:   "main\n",
		ExitCode: 0,
	})

	branch, ok, err := GetCurrentBranch(ctx, cr, repoRoot)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "main", branch)
}

func TestGetCurrentBranch_DetachedHead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	repoRoot := "/some/project"
	cr.On("git", []string{"branch", "--show-current"}, repoRoot, exec.CmdResult{
		Stdout:   "\n",
		ExitCode: 0,
	})

	branch, ok, err := GetCurrentBranch(ctx, cr, repoRoot)

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, branch)
}

func TestGetCurrentBranch_MultiLineOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	repoRoot := "/some/project"
	cr.On("git", []string{"branch", "--show-current"}, repoRoot, exec.CmdResult{
		Stdout:   "main\nfeature\n",
		ExitCode: 0,
	})

	_, _, err := GetCurrentBranch(ctx, cr, repoRoot)

	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
}

func TestGetOriginInfo_Present(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	repoRoot := "/some/project"

	cr.On("git", []string{"config", "--get", "remote.origin.url"}, repoRoot, exec.CmdResult{
		Stdout:   "git@github.com:owner/repo.git\n",
		ExitCode: 0,
	})

	info := GetOriginInfo(ctx, cr, repoRoot)

	assert.True(t, info.Present)
	assert.Equal(t, "git@github.com:owner/repo.git", info.URL)
	assert.Equal(t, "github.com", info.Host)
}

func TestGetOriginInfo_Missing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	repoRoot := "/some/project"

	cr.On("git", []string{"config", "--get", "remote.origin.url"}, repoRoot, exec.CmdResult{
		Stderr:   "",
		ExitCode: 1, // git config returns 1 for missing key
	})

	info := GetOriginInfo(ctx, cr, repoRoot)

	assert.False(t, info.Present)
	assert.Empty(t, info.URL)
	assert.Empty(t, info.Host)
}

func TestGetOriginInfo_EmptyURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()

	repoRoot := "/some/project"

	cr.On("git", []string{"config", "--get", "remote.origin.url"}, repoRoot, exec.CmdResult{
		Stdout:   "\n",
		ExitCode: 0,
	})

	info := GetOriginInfo(ctx, cr, repoRoot)

	assert.False(t, info.Present)
}

func TestParseOriginHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// scp-like SSH (supported)
		{
			name: "scp-like github.com with .git",
			raw:  "git@github.com:foo/bar.git",
			want: "github.com",
		},
		{
			name: "scp-like github.com without .git",
			raw:  "git@github.com:foo/bar",
			want: "github.com",
		},
		{
			name: "scp-like enterprise host",
			raw:  "git@enterprise.example.com:foo/bar.git",
			want: "enterprise.example.com",
		},
		{
			name: "scp-like with subdomain",
			raw:  "git@git.company.io:team/project.git",
			want: "git.company.io",
		},

		// HTTPS (supported)
		{
			name: "https github.com with .git",
			raw:  "https://github.com/foo/bar.git",
			want: "github.com",
		},
		{
			name: "https github.com without .git",
			raw:  "https://github.com/foo/bar",
			want: "github.com",
		},
		{
			name: "https enterprise host",
			raw:  "https://github.enterprise.com/org/repo.git",
			want: "github.enterprise.com",
		},
		{
			name: "https with port",
			raw:  "https://github.com:443/foo/bar.git",
			want: "github.com",
		},

		// Unsupported formats
		{
			name: "ssh:// URL (unsupported in v1)",
			raw:  "ssh://git@github.com/foo/bar.git",
			want: "",
		},
		{
			name: "git:// URL (unsupported)",
			raw:  "git://github.com/foo/bar.git",
			want: "",
		},
		{
			name: "file:// URL (unsupported)",
			raw:  "file:///path/to/repo",
			want: "",
		},

		// Edge cases
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace only",
			raw:  "   \n\t  ",
			want: "",
		},
		{
			name: "malformed scp-like (no colon)",
			raw:  "git@github.com/foo/bar.git",
			want: "",
		},
		{
			name: "malformed scp-like (no at)",
			raw:  "github.com:foo/bar.git",
			want: "",
		},
		{
			name: "localhost (single component host)",
			raw:  "git@localhost:foo/bar.git",
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseOriginHost(tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Tests for HasCommits

func TestHasCommits_HasCommits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"rev-parse", "--verify", "HEAD"}, repoRoot, exec.CmdResult{
		Stdout:   "abc123def456\n",
		ExitCode: 0,
	})

	has, err := HasCommits(ctx, cr, repoRoot)
	require.NoError(t, err)
	assert.True(t, has)
}

func TestHasCommits_NoCommits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"rev-parse", "--verify", "HEAD"}, repoRoot, exec.CmdResult{
		Stderr:   "fatal: Needed a single revision",
		ExitCode: 128,
	})

	has, err := HasCommits(ctx, cr, repoRoot)
	require.NoError(t, err)
	assert.False(t, has, "expected HasCommits = false for empty repo")
}

// Tests for IsClean

func TestIsClean_Clean(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"status", "--porcelain"}, repoRoot, exec.CmdResult{
		Stdout:   "",
		ExitCode: 0,
	})

	clean, err := IsClean(ctx, cr, repoRoot)
	require.NoError(t, err)
	assert.True(t, clean)
}

func TestIsClean_Dirty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"status", "--porcelain"}, repoRoot, exec.CmdResult{
		Stdout:   " M file.txt\n",
		ExitCode: 0,
	})

	clean, err := IsClean(ctx, cr, repoRoot)
	require.NoError(t, err)
	assert.False(t, clean, "expected IsClean = false for dirty tree")
}

func TestIsClean_DirtyWithUntracked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"status", "--porcelain"}, repoRoot, exec.CmdResult{
		Stdout:   "?? newfile.txt\n",
		ExitCode: 0,
	})

	clean, err := IsClean(ctx, cr, repoRoot)
	require.NoError(t, err)
	assert.False(t, clean, "expected IsClean = false for untracked files")
}

// Tests for BranchExists

func TestBranchExists_Exists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"
	branch := "main"

	cr.On("git", []string{"show-ref", "--verify", "refs/heads/main"}, repoRoot, exec.CmdResult{
		Stdout:   "abc123 refs/heads/main\n",
		ExitCode: 0,
	})

	exists, err := BranchExists(ctx, cr, repoRoot, branch)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestBranchExists_NotExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"
	branch := "nonexistent"

	cr.On("git", []string{"show-ref", "--verify", "refs/heads/nonexistent"}, repoRoot, exec.CmdResult{
		Stderr:   "fatal: 'refs/heads/nonexistent' - not a valid ref",
		ExitCode: 1,
	})

	exists, err := BranchExists(ctx, cr, repoRoot, branch)
	require.NoError(t, err)
	assert.False(t, exists, "expected BranchExists = false for nonexistent branch")
}

// Tests for GetOriginURL

func TestGetOriginURL_Present(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"remote", "get-url", "origin"}, repoRoot, exec.CmdResult{
		Stdout:   "git@github.com:owner/repo.git\n",
		ExitCode: 0,
	})

	url := GetOriginURL(ctx, cr, repoRoot)
	assert.Equal(t, "git@github.com:owner/repo.git", url)
}

func TestGetOriginURL_Missing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cr := newStubRunner()
	repoRoot := "/some/project"

	cr.On("git", []string{"remote", "get-url", "origin"}, repoRoot, exec.CmdResult{
		Stderr:   "fatal: No such remote 'origin'",
		ExitCode: 2,
	})

	url := GetOriginURL(ctx, cr, repoRoot)
	assert.Empty(t, url)
}
