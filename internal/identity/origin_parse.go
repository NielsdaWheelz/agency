// Package identity provides repo identity parsing and derivation.
package identity

import (
	"regexp"
	"strings"
)

// ParseGitHubOwnerRepo extracts the host, owner, and repo from a GitHub-style
// remote URL. It accepts any host (github.com, GitHub Enterprise, or
// self-hosted) so long as the URL conforms to one of the two GitHub URL forms:
//
//   - scp-like: git@<host>:owner/repo[.git]
//   - https:    https://<host>/owner/repo[.git]
//
// Returns ok=false for ssh:// URLs, malformed owner/repo names, or other URL
// schemes. Callers needing host-specific behavior should inspect the returned
// host.
func ParseGitHubOwnerRepo(raw string) (host, owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}
	if host, owner, repo, ok = parseScpLike(raw); ok {
		return host, owner, repo, true
	}
	if host, owner, repo, ok = parseHTTPS(raw); ok {
		return host, owner, repo, true
	}
	return "", "", "", false
}

// validNamePattern matches valid GitHub owner/repo names: [A-Za-z0-9_.-]+
var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// parseScpLike parses git@<host>:owner/repo.git format.
func parseScpLike(raw string) (host, owner, repo string, ok bool) {
	if !strings.Contains(raw, "@") || !strings.Contains(raw, ":") || strings.Contains(raw, "://") {
		return "", "", "", false
	}
	atIdx := strings.Index(raw, "@")
	colonIdx := strings.Index(raw, ":")
	if colonIdx <= atIdx {
		return "", "", "", false
	}
	host = raw[atIdx+1 : colonIdx]
	if !isValidGitHost(host) {
		return "", "", "", false
	}
	owner, repo, ok = parseOwnerRepo(raw[colonIdx+1:])
	if !ok {
		return "", "", "", false
	}
	return host, owner, repo, true
}

// parseHTTPS parses https://<host>/owner/repo.git format.
func parseHTTPS(raw string) (host, owner, repo string, ok bool) {
	rest, ok := strings.CutPrefix(raw, "https://")
	if !ok {
		return "", "", "", false
	}
	slashIdx := strings.Index(rest, "/")
	if slashIdx <= 0 {
		return "", "", "", false
	}
	host = rest[:slashIdx]
	if colon := strings.Index(host, ":"); colon > 0 {
		host = host[:colon]
	}
	if !isValidGitHost(host) {
		return "", "", "", false
	}
	owner, repo, ok = parseOwnerRepo(rest[slashIdx+1:])
	if !ok {
		return "", "", "", false
	}
	return host, owner, repo, true
}

// isValidGitHost performs basic host validation: non-empty, no invalid
// characters, contains at least one dot.
func isValidGitHost(host string) bool {
	if host == "" || strings.ContainsAny(host, " \t\n\r/") {
		return false
	}
	if !strings.Contains(host, ".") {
		return false
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	return true
}

// parseOwnerRepo extracts owner/repo from a path like "owner/repo.git" or "owner/repo".
func parseOwnerRepo(path string) (owner, repo string, ok bool) {
	path = strings.TrimSuffix(path, ".git")
	owner, repo, ok = strings.Cut(path, "/")
	if !ok || strings.Contains(repo, "/") {
		return "", "", false
	}
	if owner == "" || repo == "" {
		return "", "", false
	}
	if !validNamePattern.MatchString(owner) || !validNamePattern.MatchString(repo) {
		return "", "", false
	}
	return owner, repo, true
}
