package identity

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGitHubOwnerRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		// Valid scp-like SSH formats
		{
			name:      "scp-like with .git",
			raw:       "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "scp-like without .git",
			raw:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "scp-like preserves case",
			raw:       "git@github.com:NielsdaWheelz/Agency.git",
			wantOwner: "NielsdaWheelz",
			wantRepo:  "Agency",
			wantOK:    true,
		},
		{
			name:      "scp-like with dots in repo name",
			raw:       "git@github.com:owner/repo.name.git",
			wantOwner: "owner",
			wantRepo:  "repo.name",
			wantOK:    true,
		},
		{
			name:      "scp-like with underscores",
			raw:       "git@github.com:owner_name/repo_name.git",
			wantOwner: "owner_name",
			wantRepo:  "repo_name",
			wantOK:    true,
		},
		{
			name:      "scp-like with hyphens",
			raw:       "git@github.com:owner-name/repo-name.git",
			wantOwner: "owner-name",
			wantRepo:  "repo-name",
			wantOK:    true,
		},

		// Valid HTTPS formats
		{
			name:      "https with .git",
			raw:       "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "https without .git",
			raw:       "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "https preserves case",
			raw:       "https://github.com/NielsdaWheelz/Agency",
			wantOwner: "NielsdaWheelz",
			wantRepo:  "Agency",
			wantOK:    true,
		},

		// Non-github.com hosts (should fail)
		{
			name:   "enterprise scp-like",
			raw:    "git@github.enterprise.com:owner/repo.git",
			wantOK: false,
		},
		{
			name:   "enterprise https",
			raw:    "https://github.enterprise.com/owner/repo.git",
			wantOK: false,
		},
		{
			name:   "gitlab",
			raw:    "git@gitlab.com:owner/repo.git",
			wantOK: false,
		},
		{
			name:   "bitbucket",
			raw:    "git@bitbucket.org:owner/repo.git",
			wantOK: false,
		},

		// Unsupported URL forms
		{
			name:   "ssh:// URL",
			raw:    "ssh://git@github.com/owner/repo.git",
			wantOK: false,
		},

		// Invalid formats
		{
			name:   "empty string",
			raw:    "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			raw:    "   \n\t   ",
			wantOK: false,
		},
		{
			name:   "missing owner",
			raw:    "git@github.com:/repo.git",
			wantOK: false,
		},
		{
			name:   "missing repo",
			raw:    "git@github.com:owner/.git",
			wantOK: false,
		},
		{
			name:   "too many path components",
			raw:    "git@github.com:owner/repo/extra.git",
			wantOK: false,
		},
		{
			name:   "invalid char in owner (space)",
			raw:    "git@github.com:owner name/repo.git",
			wantOK: false,
		},
		{
			name:   "invalid char in repo (space)",
			raw:    "git@github.com:owner/repo name.git",
			wantOK: false,
		},
		{
			name:   "invalid char in owner (slash)",
			raw:    "git@github.com:owner/extra/repo.git",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			owner, repo, ok := ParseGitHubOwnerRepo(tt.raw)

			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantOwner, owner)
				assert.Equal(t, tt.wantRepo, repo)
			}
		})
	}
}

func TestDeriveRepoIdentity_GitHub(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		absRepoRoot string
		originURL   string
		wantKey     string
		wantGHFlow  bool
	}{
		{
			name:        "github ssh",
			absRepoRoot: "/some/path",
			originURL:   "git@github.com:owner/repo.git",
			wantKey:     "github:owner/repo",
			wantGHFlow:  true,
		},
		{
			name:        "github https",
			absRepoRoot: "/some/path",
			originURL:   "https://github.com/owner/repo.git",
			wantKey:     "github:owner/repo",
			wantGHFlow:  true,
		},
		{
			name:        "preserves case",
			absRepoRoot: "/path",
			originURL:   "git@github.com:NielsdaWheelz/Agency.git",
			wantKey:     "github:NielsdaWheelz/Agency",
			wantGHFlow:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id := DeriveRepoIdentity(tt.absRepoRoot, tt.originURL)

			assert.Equal(t, tt.wantKey, id.RepoKey)
			assert.Equal(t, tt.wantGHFlow, id.GitHubFlowAvailable)
			assert.Len(t, id.RepoID, repoIDLen)
		})
	}
}

func TestDeriveRepoIdentity_PathKeyForNonGitHubOrigins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		absRepoRoot string
		originURL   string
	}{
		{
			name:        "non-github host",
			absRepoRoot: "/some/path",
			originURL:   "git@gitlab.com:owner/repo.git",
		},
		{
			name:        "no origin",
			absRepoRoot: "/some/path",
			originURL:   "",
		},
		{
			name:        "enterprise github",
			absRepoRoot: "/some/path",
			originURL:   "git@github.enterprise.com:owner/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id := DeriveRepoIdentity(tt.absRepoRoot, tt.originURL)

			// Should use path-based key
			assert.True(t, strings.HasPrefix(id.RepoKey, "path:"), "RepoKey = %q, expected path: prefix", id.RepoKey)

			// Path hash should be full sha256 hex
			pathHash := id.RepoKey[5:] // strip "path:"
			assert.Len(t, pathHash, sha256.Size*2)

			assert.False(t, id.GitHubFlowAvailable, "GitHubFlowAvailable = true, want false")
			assert.Len(t, id.RepoID, repoIDLen)
		})
	}
}

func TestDeriveRepoIdentity_Determinism(t *testing.T) {
	t.Parallel()

	// Same inputs should always produce same outputs
	absRepoRoot := "/home/user/project"
	originURL := "git@github.com:owner/repo.git"

	id1 := DeriveRepoIdentity(absRepoRoot, originURL)
	id2 := DeriveRepoIdentity(absRepoRoot, originURL)

	assert.Equal(t, id1.RepoKey, id2.RepoKey, "RepoKey not deterministic")
	assert.Equal(t, id1.RepoID, id2.RepoID, "RepoID not deterministic")
}

func TestDeriveRepoIdentity_PathHashDeterminism(t *testing.T) {
	t.Parallel()

	// Same path should always produce same hash
	absRepoRoot := "/home/user/project"

	id1 := DeriveRepoIdentity(absRepoRoot, "")
	id2 := DeriveRepoIdentity(absRepoRoot, "")

	assert.Equal(t, id1.RepoKey, id2.RepoKey, "path-based RepoKey not deterministic")
}

func TestDeriveRepoIdentity_DifferentPaths(t *testing.T) {
	t.Parallel()

	// Different paths should produce different hashes
	id1 := DeriveRepoIdentity("/path/one", "")
	id2 := DeriveRepoIdentity("/path/two", "")

	assert.NotEqual(t, id1.RepoKey, id2.RepoKey, "different paths produced same RepoKey")
	assert.NotEqual(t, id1.RepoID, id2.RepoID, "different paths produced same RepoID")
}
