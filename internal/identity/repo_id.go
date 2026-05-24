package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const repoIDLen = 16

// RepoIdentity holds the derived identity for a repository.
type RepoIdentity struct {
	// RepoKey is "github:owner/repo" for github.com, "github:host/owner/repo"
	// for any other GitHub-compatible host, or "path:<sha256(abs_repo_root)>"
	// when no GitHub-style origin URL is available.
	RepoKey string

	// RepoID is sha256(RepoKey) truncated for stable display and lookup.
	RepoID string

	// GitHubFlowAvailable is true when origin parses as a GitHub-style URL
	// (github.com or enterprise). gh CLI auto-discovers the host from the
	// origin remote, so enterprise flows work without additional plumbing.
	GitHubFlowAvailable bool
}

// DeriveRepoIdentity computes the repository identity from the absolute repo root
// and origin URL. This is a pure function with no side effects.
//
// repo_key rules:
//   - github.com origin: "github:owner/repo"
//   - other GitHub-compatible host: "github:host/owner/repo"
//   - no parseable GitHub origin: "path:<sha256(absRepoRoot)>"
//
// repo_id is always sha256(repo_key) truncated to repoIDLen hex chars.
func DeriveRepoIdentity(absRepoRoot string, originURL string) RepoIdentity {
	host, owner, repo, ok := ParseGitHubOwnerRepo(originURL)
	if ok {
		repoKey := fmt.Sprintf("github:%s/%s", owner, repo)
		if host != "github.com" {
			repoKey = fmt.Sprintf("github:%s/%s/%s", host, owner, repo)
		}
		return RepoIdentity{
			RepoKey:             repoKey,
			RepoID:              deriveRepoID(repoKey),
			GitHubFlowAvailable: true,
		}
	}

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
