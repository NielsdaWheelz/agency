package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/git"
)

// Hash length constants per constitution.
const (
	// RepoIDLen is the number of hex characters for repo_id (truncated sha256).
	RepoIDLen = 16

	// PathHashLen is the full sha256 hex length used in path-based repo keys.
	PathHashLen = 64
)

// RepoIdentity holds the derived identity for a repository.
type RepoIdentity struct {
	// RepoKey is either "github:owner/repo" or "path:<sha256(abs_repo_root)>"
	RepoKey string

	// RepoID is sha256(RepoKey) truncated to RepoIDLen hex characters
	RepoID string

	// GitHubFlowAvailable is true when origin is github.com and owner/repo parsed successfully
	GitHubFlowAvailable bool

	// Origin holds the parsed origin information
	Origin git.OriginInfo
}

// DeriveRepoIdentity computes the repository identity from the absolute repo root
// and origin URL. This is a pure function with no side effects.
//
// repo_key rules:
//   - If originURL matches github.com ssh/https: repo_key = "github:owner/repo"
//   - Otherwise: repo_key = "path:<sha256(absRepoRoot)>"
//
// repo_id is always sha256(repo_key) truncated to RepoIDLen hex chars.
func DeriveRepoIdentity(absRepoRoot string, originURL string) RepoIdentity {
	origin := git.OriginInfo{
		Present: originURL != "",
		URL:     originURL,
		Host:    git.ParseOriginHost(originURL),
	}

	// Try to parse as GitHub repo
	owner, repo, ok := ParseGitHubOwnerRepo(originURL)
	if ok {
		repoKey := fmt.Sprintf("github:%s/%s", owner, repo)
		return RepoIdentity{
			RepoKey:             repoKey,
			RepoID:              deriveRepoID(repoKey),
			GitHubFlowAvailable: true,
			Origin:              origin,
		}
	}

	// Fallback to path-based key
	pathHash := Sha256Hex(absRepoRoot)
	repoKey := fmt.Sprintf("path:%s", pathHash)
	return RepoIdentity{
		RepoKey:             repoKey,
		RepoID:              deriveRepoID(repoKey),
		GitHubFlowAvailable: false,
		Origin:              origin,
	}
}

// deriveRepoID computes sha256(repoKey) and truncates to RepoIDLen hex chars.
func deriveRepoID(repoKey string) string {
	hash := Sha256Hex(repoKey)
	return hash[:RepoIDLen]
}

// Sha256Hex computes the lowercase hex-encoded SHA256 of a string.
func Sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
