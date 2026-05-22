package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const repoIDLen = 16

// RepoIdentity holds the derived identity for a repository.
type RepoIdentity struct {
	// RepoKey is either "github:owner/repo" or "path:<sha256(abs_repo_root)>"
	RepoKey string

	// RepoID is sha256(RepoKey) truncated for stable display and lookup.
	RepoID string

	// GitHubFlowAvailable is true when origin is github.com and owner/repo parsed successfully
	GitHubFlowAvailable bool
}

// DeriveRepoIdentity computes the repository identity from the absolute repo root
// and origin URL. This is a pure function with no side effects.
//
// repo_key rules:
//   - If originURL matches github.com ssh/https: repo_key = "github:owner/repo"
//   - Otherwise: repo_key = "path:<sha256(absRepoRoot)>"
//
// repo_id is always sha256(repo_key) truncated to repoIDLen hex chars.
func DeriveRepoIdentity(absRepoRoot string, originURL string) RepoIdentity {
	// Try to parse as GitHub repo
	owner, repo, ok := ParseGitHubOwnerRepo(originURL)
	if ok {
		repoKey := fmt.Sprintf("github:%s/%s", owner, repo)
		return RepoIdentity{
			RepoKey:             repoKey,
			RepoID:              deriveRepoID(repoKey),
			GitHubFlowAvailable: true,
		}
	}

	// Use a path-based key when the origin is not a supported GitHub remote.
	pathHash := sha256Hex(absRepoRoot)
	repoKey := fmt.Sprintf("path:%s", pathHash)
	return RepoIdentity{
		RepoKey:             repoKey,
		RepoID:              deriveRepoID(repoKey),
		GitHubFlowAvailable: false,
	}
}

// deriveRepoID computes sha256(repoKey) and truncates to repoIDLen hex chars.
func deriveRepoID(repoKey string) string {
	hash := sha256Hex(repoKey)
	return hash[:repoIDLen]
}

// sha256Hex computes the lowercase hex-encoded SHA256 of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
