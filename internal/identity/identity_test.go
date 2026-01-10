package identity

import (
	"testing"
)

func TestParseGitHubOwnerRepo(t *testing.T) {
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

		// ssh:// URLs (unsupported in v1)
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
			owner, repo, ok := ParseGitHubOwnerRepo(tt.raw)

			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if ok && repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestDeriveRepoIdentity_GitHub(t *testing.T) {
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
			id := DeriveRepoIdentity(tt.absRepoRoot, tt.originURL)

			if id.RepoKey != tt.wantKey {
				t.Errorf("RepoKey = %q, want %q", id.RepoKey, tt.wantKey)
			}
			if id.GitHubFlowAvailable != tt.wantGHFlow {
				t.Errorf("GitHubFlowAvailable = %v, want %v", id.GitHubFlowAvailable, tt.wantGHFlow)
			}
			if len(id.RepoID) != RepoIDLen {
				t.Errorf("RepoID length = %d, want %d", len(id.RepoID), RepoIDLen)
			}
			if !id.Origin.Present {
				t.Error("Origin.Present = false, want true")
			}
		})
	}
}

func TestDeriveRepoIdentity_PathFallback(t *testing.T) {
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
			id := DeriveRepoIdentity(tt.absRepoRoot, tt.originURL)

			// Should use path-based key
			if len(id.RepoKey) < 5 || id.RepoKey[:5] != "path:" {
				t.Errorf("RepoKey = %q, expected path: prefix", id.RepoKey)
			}

			// Path hash should be full sha256 hex
			pathHash := id.RepoKey[5:] // strip "path:"
			if len(pathHash) != PathHashLen {
				t.Errorf("path hash length = %d, want %d", len(pathHash), PathHashLen)
			}

			if id.GitHubFlowAvailable {
				t.Error("GitHubFlowAvailable = true, want false")
			}
			if len(id.RepoID) != RepoIDLen {
				t.Errorf("RepoID length = %d, want %d", len(id.RepoID), RepoIDLen)
			}
		})
	}
}

func TestDeriveRepoIdentity_Determinism(t *testing.T) {
	// Same inputs should always produce same outputs
	absRepoRoot := "/home/user/project"
	originURL := "git@github.com:owner/repo.git"

	id1 := DeriveRepoIdentity(absRepoRoot, originURL)
	id2 := DeriveRepoIdentity(absRepoRoot, originURL)

	if id1.RepoKey != id2.RepoKey {
		t.Errorf("RepoKey not deterministic: %q != %q", id1.RepoKey, id2.RepoKey)
	}
	if id1.RepoID != id2.RepoID {
		t.Errorf("RepoID not deterministic: %q != %q", id1.RepoID, id2.RepoID)
	}
}

func TestDeriveRepoIdentity_PathHashDeterminism(t *testing.T) {
	// Same path should always produce same hash
	absRepoRoot := "/home/user/project"

	id1 := DeriveRepoIdentity(absRepoRoot, "")
	id2 := DeriveRepoIdentity(absRepoRoot, "")

	if id1.RepoKey != id2.RepoKey {
		t.Errorf("path-based RepoKey not deterministic: %q != %q", id1.RepoKey, id2.RepoKey)
	}
}

func TestDeriveRepoIdentity_DifferentPaths(t *testing.T) {
	// Different paths should produce different hashes
	id1 := DeriveRepoIdentity("/path/one", "")
	id2 := DeriveRepoIdentity("/path/two", "")

	if id1.RepoKey == id2.RepoKey {
		t.Error("different paths produced same RepoKey")
	}
	if id1.RepoID == id2.RepoID {
		t.Error("different paths produced same RepoID")
	}
}

func TestSha256Hex(t *testing.T) {
	// Verify hash is lowercase hex and correct length
	hash := Sha256Hex("test")

	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}

	// Verify it's all lowercase hex
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash contains non-lowercase-hex character: %c", c)
		}
	}

	// Verify determinism
	hash2 := Sha256Hex("test")
	if hash != hash2 {
		t.Errorf("Sha256Hex not deterministic")
	}

	// Known value test
	// echo -n "test" | sha256sum -> 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
	expected := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if hash != expected {
		t.Errorf("Sha256Hex(\"test\") = %q, want %q", hash, expected)
	}
}

func TestRepoIDLen(t *testing.T) {
	// Verify constant value per spec
	if RepoIDLen != 16 {
		t.Errorf("RepoIDLen = %d, want 16", RepoIDLen)
	}
}

func TestPathHashLen(t *testing.T) {
	// Verify constant value per spec
	if PathHashLen != 64 {
		t.Errorf("PathHashLen = %d, want 64", PathHashLen)
	}
}

func TestDeriveRepoIdentity_OriginInfo(t *testing.T) {
	// Test that Origin info is properly populated
	t.Run("with origin", func(t *testing.T) {
		id := DeriveRepoIdentity("/path", "git@github.com:owner/repo.git")

		if !id.Origin.Present {
			t.Error("Origin.Present = false, want true")
		}
		if id.Origin.URL != "git@github.com:owner/repo.git" {
			t.Errorf("Origin.URL = %q, want %q", id.Origin.URL, "git@github.com:owner/repo.git")
		}
		if id.Origin.Host != "github.com" {
			t.Errorf("Origin.Host = %q, want %q", id.Origin.Host, "github.com")
		}
	})

	t.Run("without origin", func(t *testing.T) {
		id := DeriveRepoIdentity("/path", "")

		if id.Origin.Present {
			t.Error("Origin.Present = true, want false")
		}
		if id.Origin.URL != "" {
			t.Errorf("Origin.URL = %q, want empty", id.Origin.URL)
		}
	})
}
