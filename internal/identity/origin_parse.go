// Package identity provides repo identity parsing and derivation.
package identity

import (
	"regexp"
	"strings"
)

// ParseGitHubOwnerRepo extracts owner and repo from a GitHub remote URL.
// Supports:
//   - scp-like: git@github.com:owner/repo.git
//   - https: https://github.com/owner/repo.git
//
// Returns ok=false for:
//   - Non-github.com hosts
//   - Invalid owner/repo characters (must match [A-Za-z0-9_.-]+)
//   - ssh:// URLs (unsupported in v1)
//   - Empty or malformed URLs
func ParseGitHubOwnerRepo(raw string) (owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	// Try scp-like: git@github.com:owner/repo.git
	if owner, repo, ok = parseScpLike(raw); ok {
		return owner, repo, true
	}

	// Try https://github.com/owner/repo.git
	if owner, repo, ok = parseHTTPS(raw); ok {
		return owner, repo, true
	}

	return "", "", false
}

// validNamePattern matches valid GitHub owner/repo names: [A-Za-z0-9_.-]+
var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// parseScpLike parses git@github.com:owner/repo.git format.
func parseScpLike(raw string) (owner, repo string, ok bool) {
	// Must have @ and : but not ://
	if !strings.Contains(raw, "@") || !strings.Contains(raw, ":") || strings.Contains(raw, "://") {
		return "", "", false
	}

	// Extract host and path
	atIdx := strings.Index(raw, "@")
	colonIdx := strings.Index(raw, ":")
	if colonIdx <= atIdx {
		return "", "", false
	}

	host := raw[atIdx+1 : colonIdx]
	if host != "github.com" {
		return "", "", false
	}

	path := raw[colonIdx+1:]
	return parseOwnerRepo(path)
}

// parseHTTPS parses https://github.com/owner/repo.git format.
func parseHTTPS(raw string) (owner, repo string, ok bool) {
	if !strings.HasPrefix(raw, "https://github.com/") {
		return "", "", false
	}

	path := strings.TrimPrefix(raw, "https://github.com/")
	return parseOwnerRepo(path)
}

// parseOwnerRepo extracts owner/repo from a path like "owner/repo.git" or "owner/repo".
func parseOwnerRepo(path string) (owner, repo string, ok bool) {
	// Strip trailing .git if present
	path = strings.TrimSuffix(path, ".git")

	// Must have exactly one slash separating owner/repo
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return "", "", false
	}

	owner = parts[0]
	repo = parts[1]

	// Validate owner and repo names
	if owner == "" || repo == "" {
		return "", "", false
	}
	if !validNamePattern.MatchString(owner) || !validNamePattern.MatchString(repo) {
		return "", "", false
	}

	return owner, repo, true
}
