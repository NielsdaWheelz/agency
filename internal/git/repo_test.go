package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	result, err := exec.NewRealRunner().Run(context.Background(), "git", args, exec.RunOpts{Dir: dir})
	require.NoError(t, err, "git %s", strings.Join(args, " "))
	require.Equal(t, 0, result.ExitCode, "git %s: %s", strings.Join(args, " "), result.Stderr)
}

func TestGetRepoRoot(t *testing.T) {
	t.Run("from subdirectory", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)
		subdir := filepath.Join(repoRoot, "nested", "dir")
		require.NoError(t, os.MkdirAll(subdir, 0o755))

		root, err := GetRepoRoot(context.Background(), exec.NewRealRunner(), subdir, nil)
		require.NoError(t, err)
		assert.Equal(t, repoRoot, root.Path)
	})

	t.Run("outside repository", func(t *testing.T) {
		rootDir := t.TempDir()

		_, err := GetRepoRoot(context.Background(), exec.NewRealRunner(), rootDir, nil)

		require.Error(t, err)
		assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
	})
}

func TestGetRepoRoot_EmptyCwd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	_, err := GetRepoRoot(ctx, exec.NewRealRunner(), "", nil)

	require.Error(t, err)
	assert.Equal(t, errors.ENoRepo, errors.GetCode(err))
}

func TestGetCurrentBranch(t *testing.T) {
	t.Run("on branch", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)

		branch, ok, err := GetCurrentBranch(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "main", branch)
	})

	t.Run("detached head", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)
		runGit(t, repoRoot, "checkout", "--detach", "HEAD")

		branch, ok, err := GetCurrentBranch(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		require.NoError(t, err)
		assert.False(t, ok)
		assert.Empty(t, branch)
	})
}

func TestGetOriginInfo(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)
		runGit(t, repoRoot, "remote", "add", "origin", "git@github.com:owner/repo.git")

		info := GetOriginInfo(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		assert.True(t, info.Present)
		assert.Equal(t, "git@github.com:owner/repo.git", info.URL)
		assert.Equal(t, "github.com", info.Host)
	})

	t.Run("missing", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)

		info := GetOriginInfo(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		assert.False(t, info.Present)
		assert.Empty(t, info.URL)
		assert.Empty(t, info.Host)
	})

	t.Run("empty url", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)
		runGit(t, repoRoot, "config", "remote.origin.url", "")

		info := GetOriginInfo(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		assert.False(t, info.Present)
		assert.Empty(t, info.URL)
		assert.Empty(t, info.Host)
	})
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
			name: "ssh URL",
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

func TestHasCommits(t *testing.T) {
	tests := []struct {
		name    string
		repoDir func(t *testing.T) string
		want    bool
	}{
		{
			name: "has commits",
			repoDir: func(t *testing.T) string {
				t.Helper()
				return testutil.SetupGitRepo(t)
			},
			want: true,
		},
		{
			name: "empty repo",
			repoDir: func(t *testing.T) string {
				t.Helper()
				testutil.HermeticGitEnv(t)
				repoRoot := t.TempDir()
				result, err := exec.NewRealRunner().Run(context.Background(), "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoRoot})
				require.NoError(t, err)
				require.Equal(t, 0, result.ExitCode, result.Stderr)
				return repoRoot
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			has, err := HasCommits(context.Background(), exec.NewRealRunner(), tt.repoDir(t), nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, has)
		})
	}
}

func TestIsClean(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, repoRoot string)
		want   bool
	}{
		{
			name: "clean",
			want: true,
		},
		{
			name: "modified tracked file",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# Changed\n"), 0o644))
			},
			want: false,
		},
		{
			name: "untracked file",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "newfile.txt"), []byte("new\n"), 0o644))
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := testutil.SetupGitRepo(t)
			if tt.mutate != nil {
				tt.mutate(t, repoRoot)
			}

			clean, err := IsClean(context.Background(), exec.NewRealRunner(), repoRoot, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, clean)
		})
	}
}

func TestIsCleanExcludingAgency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, repoRoot string)
		want   bool
	}{
		{
			name: "clean",
			want: true,
		},
		{
			name: "agency metadata only",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				stateDir := filepath.Join(repoRoot, ".agency", "state")
				require.NoError(t, os.MkdirAll(stateDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), []byte("{}\n"), 0o644))
			},
			want: true,
		},
		{
			name: "non-.agency untracked file",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "notes.txt"), []byte("dirty\n"), 0o644))
			},
			want: false,
		},
		{
			name: "tracked file modified",
			mutate: func(t *testing.T, repoRoot string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# Changed\n"), 0o644))
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := testutil.SetupGitRepo(t)
			if tt.mutate != nil {
				tt.mutate(t, repoRoot)
			}

			clean, err := IsCleanExcludingAgency(context.Background(), exec.NewRealRunner(), repoRoot, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, clean)
		})
	}
}

func TestBranchExists(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{name: "existing branch", branch: "main", want: true},
		{name: "missing branch", branch: "nonexistent", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			exists, err := BranchExists(context.Background(), exec.NewRealRunner(), testutil.SetupGitRepo(t), tt.branch, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, exists)
		})
	}
}

func TestGetOriginURL(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)
		runGit(t, repoRoot, "remote", "add", "origin", "git@github.com:owner/repo.git")

		url := GetOriginURL(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		assert.Equal(t, "git@github.com:owner/repo.git", url)
	})

	t.Run("missing", func(t *testing.T) {
		repoRoot := testutil.SetupGitRepo(t)

		url := GetOriginURL(context.Background(), exec.NewRealRunner(), repoRoot, nil)

		assert.Empty(t, url)
	})
}
