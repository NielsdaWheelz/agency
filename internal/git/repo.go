// Package git provides repo discovery and git operations via CommandRunner.
package git

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

// RepoRoot holds the absolute path to a git repository root.
type RepoRoot struct {
	Path string // absolute, clean, no trailing newline
}

// OriginInfo holds information about the remote origin.
type OriginInfo struct {
	Present bool   // true if origin remote exists and has a URL
	URL     string // empty if not present
	Host    string // empty if not present or unparseable
}

// GetRepoRoot discovers the git repository root from the given working directory.
// Uses `git rev-parse --show-toplevel` via CommandRunner.
//
// Returns E_NO_REPO if:
//   - Not inside a git repository (exit code != 0)
//   - Git outputs empty or multi-line stdout
//   - cwd is empty
func GetRepoRoot(ctx context.Context, cr exec.CommandRunner, cwd string) (RepoRoot, error) {
	if cwd == "" {
		return RepoRoot{}, errors.New(errors.ENoRepo, "working directory is empty")
	}

	result, err := cr.Run(ctx, "git", []string{"rev-parse", "--show-toplevel"}, exec.RunOpts{Dir: cwd})
	if err != nil {
		// Binary not found or execution failure
		return RepoRoot{}, errors.Wrap(errors.ENoRepo, "failed to run git rev-parse", err)
	}

	if result.ExitCode != 0 {
		return RepoRoot{}, errors.New(errors.ENoRepo, "not inside a git repository")
	}

	// Trim whitespace (git adds trailing newline)
	out := strings.TrimSpace(result.Stdout)

	// Reject empty output
	if out == "" {
		return RepoRoot{}, errors.New(errors.ENoRepo, "git rev-parse returned empty output")
	}

	// Reject multi-line output (should never happen, but be defensive)
	if strings.Contains(out, "\n") {
		return RepoRoot{}, errors.New(errors.ENoRepo, "git rev-parse returned unexpected multi-line output")
	}

	// Normalize to absolute path
	var absPath string
	if filepath.IsAbs(out) {
		absPath = filepath.Clean(out)
	} else {
		absPath = filepath.Clean(filepath.Join(cwd, out))
	}

	// Make absolute (handles edge cases)
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return RepoRoot{}, errors.Wrap(errors.ENoRepo, "failed to resolve absolute path", err)
	}

	return RepoRoot{Path: absPath}, nil
}

// GetOriginInfo retrieves the remote origin URL from the repository.
// Uses `git config --get remote.origin.url` via CommandRunner.
//
// This function never returns an error. If origin is missing or unparseable,
// it returns OriginInfo{Present: false}.
func GetOriginInfo(ctx context.Context, cr exec.CommandRunner, repoRoot string) OriginInfo {
	result, err := cr.Run(ctx, "git", []string{"config", "--get", "remote.origin.url"}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		// Execution failure (binary not found, etc.)
		return OriginInfo{Present: false, URL: "", Host: ""}
	}

	if result.ExitCode != 0 {
		// Origin not configured (git config returns exit code 1 for missing key)
		return OriginInfo{Present: false, URL: "", Host: ""}
	}

	url := strings.TrimSpace(result.Stdout)
	if url == "" {
		return OriginInfo{Present: false, URL: "", Host: ""}
	}

	return OriginInfo{
		Present: true,
		URL:     url,
		Host:    ParseOriginHost(url),
	}
}

// ParseOriginHost extracts the hostname from a git remote URL.
// Supports:
//   - scp-like: git@github.com:owner/repo.git -> github.com
//   - https: https://github.com/owner/repo.git -> github.com
//
// Returns "" for:
//   - ssh:// URLs (unsupported in v1)
//   - Other URL schemes (git://, file://, etc.)
//   - Unparseable URLs
//   - Empty input
func ParseOriginHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Check for scp-like format: git@host:path
	// Must match pattern: <user>@<host>:<path>
	if strings.Contains(raw, "@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		// Find @ and : positions
		atIdx := strings.Index(raw, "@")
		colonIdx := strings.Index(raw, ":")

		// Colon must come after @
		if colonIdx > atIdx {
			host := raw[atIdx+1 : colonIdx]
			if host != "" && isValidHost(host) {
				return host
			}
		}
		return ""
	}

	// Check for https:// URL
	if strings.HasPrefix(raw, "https://") {
		// Extract host from https://host/path
		rest := strings.TrimPrefix(raw, "https://")
		// Find first slash
		slashIdx := strings.Index(rest, "/")
		if slashIdx > 0 {
			host := rest[:slashIdx]
			// Remove port if present
			if colonIdx := strings.Index(host, ":"); colonIdx > 0 {
				host = host[:colonIdx]
			}
			if isValidHost(host) {
				return host
			}
		}
		return ""
	}

	// ssh:// URLs are explicitly unsupported in v1
	if strings.HasPrefix(raw, "ssh://") {
		return ""
	}

	// Other schemes (git://, file://, etc.) unsupported
	return ""
}

// isValidHost performs basic validation on a hostname.
func isValidHost(host string) bool {
	if host == "" {
		return false
	}
	// Basic check: contains at least one dot and no invalid characters
	if !strings.Contains(host, ".") {
		return false
	}
	// Reject obvious invalid patterns
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	return true
}

// HasCommits checks if the repository has at least one commit.
// Uses `git rev-parse --verify HEAD` via CommandRunner.
//
// Returns (true, nil) if HEAD exists (repo has commits).
// Returns (false, nil) if HEAD does not exist (empty repo, fresh git init).
// Returns (false, error) only for execution failures (binary not found, etc.).
func HasCommits(ctx context.Context, cr exec.CommandRunner, repoRoot string) (bool, error) {
	result, err := cr.Run(ctx, "git", []string{"rev-parse", "--verify", "HEAD"}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		// Execution failure (binary not found, etc.)
		return false, errors.Wrap(errors.EInternal, "failed to run git rev-parse --verify HEAD", err)
	}

	// Exit code 0 = HEAD exists, non-zero = no commits
	return result.ExitCode == 0, nil
}

// IsClean checks if the working tree is clean (no uncommitted changes).
// Uses `git status --porcelain` via CommandRunner.
//
// Returns (true, nil) if the working tree is clean (stdout empty).
// Returns (false, nil) if there are uncommitted changes.
// Returns (false, error) only for execution failures.
func IsClean(ctx context.Context, cr exec.CommandRunner, repoRoot string) (bool, error) {
	result, err := cr.Run(ctx, "git", []string{"status", "--porcelain"}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		// Execution failure (binary not found, etc.)
		return false, errors.Wrap(errors.EInternal, "failed to run git status --porcelain", err)
	}

	// Non-zero exit code from git status is unusual but treat as dirty
	if result.ExitCode != 0 {
		return false, nil
	}

	// Clean = empty stdout
	return strings.TrimSpace(result.Stdout) == "", nil
}

// BranchExists checks if a local branch exists.
// Uses `git show-ref --verify refs/heads/<branch>` via CommandRunner.
//
// Returns (true, nil) if the branch exists locally.
// Returns (false, nil) if the branch does not exist.
// Returns (false, error) only for execution failures.
func BranchExists(ctx context.Context, cr exec.CommandRunner, repoRoot, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	result, err := cr.Run(ctx, "git", []string{"show-ref", "--verify", ref}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		// Execution failure (binary not found, etc.)
		return false, errors.Wrap(errors.EInternal, "failed to run git show-ref --verify", err)
	}

	// Exit code 0 = branch exists, non-zero = does not exist
	return result.ExitCode == 0, nil
}

// GetOriginURL retrieves the origin remote URL using `git remote get-url origin`.
// Returns the URL if origin exists, or empty string if missing.
// Never returns an error; failures result in empty string.
func GetOriginURL(ctx context.Context, cr exec.CommandRunner, repoRoot string) string {
	result, err := cr.Run(ctx, "git", []string{"remote", "get-url", "origin"}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		return ""
	}
	if result.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}
