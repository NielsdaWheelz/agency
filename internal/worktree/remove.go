// Package worktree provides git worktree operations for agency.
// This file implements worktree removal functionality.
package worktree

import (
	"context"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

// RemoveResult holds the result of a worktree removal attempt.
type RemoveResult struct {
	// Success is true if the worktree was removed successfully.
	Success bool

	// GitRemoveOK is true if git worktree remove succeeded.
	GitRemoveOK bool

	// FallbackUsed is true if fallback rm -rf was used.
	FallbackUsed bool

	// Error contains any error message if the operation failed.
	Error string
}

// Remove attempts to remove a git worktree.
// First tries: git -C <repoRoot> worktree remove --force <worktreePath>
// Returns the result indicating success/failure and method used.
//
// Parameters:
//   - repoRoot: the path to the main git repository (may be empty/missing)
//   - worktreePath: the absolute path to the worktree to remove
//
// If repoRoot is empty or doesn't exist, the git command is skipped and
// the result will have GitRemoveOK=false with an appropriate error.
func Remove(ctx context.Context, cr exec.CommandRunner, repoRoot, worktreePath string) *RemoveResult {
	result := &RemoveResult{}

	// Skip git removal if repoRoot is empty
	if repoRoot == "" {
		result.Error = "repo_root is empty; skipping git worktree remove"
		return result
	}

	// Try git worktree remove --force
	args := []string{
		"-C", repoRoot,
		"worktree", "remove", "--force", worktreePath,
	}

	cmdResult, err := cr.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		// Execution failure (binary not found, ctx canceled, etc.)
		result.Error = "git execution failed: " + err.Error()
		return result
	}

	if cmdResult.ExitCode != 0 {
		// Git worktree remove failed
		stderr := strings.TrimSpace(cmdResult.Stderr)
		if stderr != "" {
			result.Error = "git worktree remove failed: " + stderr
		} else {
			result.Error = "git worktree remove failed (exit code " + string(rune(cmdResult.ExitCode+'0')) + ")"
		}
		return result
	}

	// Success
	result.Success = true
	result.GitRemoveOK = true
	return result
}
