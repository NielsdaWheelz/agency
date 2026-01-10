// Package worktree provides git worktree creation and workspace scaffolding
// for agency run operations.
package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Warning represents a non-fatal warning emitted during worktree operations.
type Warning struct {
	Code    string
	Message string
}

// CreateResult holds the result of a successful worktree creation.
type CreateResult struct {
	// Branch is the newly created branch name (agency/<slug>-<shortid>).
	Branch string

	// WorktreePath is the absolute path to the worktree directory.
	WorktreePath string

	// ResolvedTitle is the title used for slug/template (may differ from input if defaulted).
	ResolvedTitle string

	// Warnings contains non-fatal warnings (e.g., .agency/ not ignored).
	Warnings []Warning
}

// CreateOpts contains options for creating a worktree.
type CreateOpts struct {
	// RunID is the run identifier (e.g., "20260109013207-a3f2").
	RunID string

	// Title is the run title (may be empty; will default to "untitled-<shortid>").
	Title string

	// RepoRoot is the absolute path to the git repository root.
	RepoRoot string

	// RepoID is the repo identifier (16 hex chars).
	RepoID string

	// ParentBranch is the local branch to branch from (must already exist).
	ParentBranch string

	// DataDir is the resolved AGENCY_DATA_DIR.
	DataDir string
}

// Create creates a git worktree and scaffolds the workspace.
//
// Operations (in order):
//  1. Compute branch name from title + run_id
//  2. Compute worktree path from data_dir + repo_id + run_id
//  3. Create branch + worktree via: git worktree add -b <branch> <path> <parent>
//  4. Create .agency/, .agency/out/, .agency/tmp/ directories
//  5. Create .agency/report.md if missing (with template)
//  6. Check if .agency/ is ignored (best-effort warning)
//
// Error codes:
//   - E_WORKTREE_CREATE_FAILED: any git worktree add failure (including collisions)
func Create(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts CreateOpts) (*CreateResult, error) {
	// 1. Resolve title (default if empty)
	resolvedTitle := opts.Title
	if resolvedTitle == "" {
		shortID := core.ShortID(opts.RunID)
		resolvedTitle = "untitled-" + shortID
	}

	// 2. Compute branch name
	branch := core.BranchName(resolvedTitle, opts.RunID)

	// 3. Compute worktree path
	worktreePath := WorktreePath(opts.DataDir, opts.RepoID, opts.RunID)

	// 4. Create worktree + branch in one command
	// Command: git -C <repo_root> worktree add -b <branch> <worktree_path> <parent_branch>
	args := []string{
		"-C", opts.RepoRoot,
		"worktree", "add",
		"-b", branch,
		worktreePath,
		opts.ParentBranch,
	}

	result, err := cr.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		// Binary not found or execution failure
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to execute git worktree add",
			err,
			map[string]string{
				"command": "git " + strings.Join(args, " "),
			},
		)
	}

	if result.ExitCode != 0 {
		// Git worktree add failed (collision, already checked out, etc.)
		details := map[string]string{
			"command":   "git " + strings.Join(args, " "),
			"exit_code": fmt.Sprintf("%d", result.ExitCode),
		}
		if result.Stderr != "" {
			stderr := result.Stderr
			// Truncate stderr if too long (32KB limit per spec)
			if len(stderr) > 32*1024 {
				stderr = stderr[:32*1024]
				details["stderr_truncated"] = "true"
			}
			details["stderr"] = stderr
		}
		if result.Stdout != "" {
			stdout := result.Stdout
			if len(stdout) > 32*1024 {
				stdout = stdout[:32*1024]
				details["stdout_truncated"] = "true"
			}
			details["stdout"] = stdout
		}

		return nil, errors.NewWithDetails(
			errors.EWorktreeCreateFailed,
			"git worktree add failed: "+strings.TrimSpace(result.Stderr),
			details,
		)
	}

	// 5. Scaffold workspace directories
	if err := scaffoldWorkspace(fsys, worktreePath, resolvedTitle); err != nil {
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to scaffold workspace",
			err,
			map[string]string{
				"worktree_path": worktreePath,
			},
		)
	}

	// 6. Check if .agency/ is ignored (best-effort)
	var warnings []Warning
	if warn := checkIgnored(ctx, cr, worktreePath); warn != nil {
		warnings = append(warnings, *warn)
	}

	return &CreateResult{
		Branch:        branch,
		WorktreePath:  worktreePath,
		ResolvedTitle: resolvedTitle,
		Warnings:      warnings,
	}, nil
}

// WorktreePath returns the worktree path for a run.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/
func WorktreePath(dataDir, repoID, runID string) string {
	return filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
}

// scaffoldWorkspace creates the .agency/ directory structure and report.md.
// This function is idempotent for directories but will not overwrite report.md.
func scaffoldWorkspace(fsys fs.FS, worktreePath, title string) error {
	// Create .agency/ directories
	dirs := []string{
		filepath.Join(worktreePath, ".agency"),
		filepath.Join(worktreePath, ".agency", "out"),
		filepath.Join(worktreePath, ".agency", "tmp"),
	}

	for _, dir := range dirs {
		if err := fsys.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create report.md if it doesn't exist
	reportPath := filepath.Join(worktreePath, ".agency", "report.md")
	if _, err := fsys.Stat(reportPath); os.IsNotExist(err) {
		content := ReportTemplate(title)
		if err := fsys.WriteFile(reportPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create report.md: %w", err)
		}
	}

	return nil
}

// checkIgnored checks if .agency/ is properly ignored in the worktree.
// Returns a warning if not ignored, nil otherwise.
//
// Exit code handling:
//   - 0: ignored (no warning)
//   - 1: not ignored (return warning)
//   - 128: error/unknown (no warning, treat as unknown)
func checkIgnored(ctx context.Context, cr exec.CommandRunner, worktreePath string) *Warning {
	// Check .agency/ directory
	args := []string{"-C", worktreePath, "check-ignore", "-q", ".agency/"}
	result, err := cr.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		// Execution failure - treat as unknown, no warning
		return nil
	}

	switch result.ExitCode {
	case 0:
		// Ignored - no warning
		return nil
	case 1:
		// Not ignored - return warning
		return &Warning{
			Code:    "W_AGENCY_NOT_IGNORED",
			Message: ".agency/ is not ignored; run 'agency init' to add it to .gitignore",
		}
	default:
		// 128 or other - unknown/error, no warning
		return nil
	}
}

// ReportTemplate returns the report.md template with the given title.
// The template follows the standard agency report format.
func ReportTemplate(title string) string {
	return fmt.Sprintf(`# %s

## summary of changes
- ...

## problems encountered
- ...

## solutions implemented
- ...

## decisions made
- ...

## deviations from spec
- ...

## how to test
- ...
`, title)
}

// ScaffoldWorkspaceOnly scaffolds the .agency/ directories and report.md
// without creating a worktree. Useful for testing or recovery scenarios.
// This is an exported wrapper around scaffoldWorkspace for testing.
func ScaffoldWorkspaceOnly(fsys fs.FS, worktreePath, title string) error {
	return scaffoldWorkspace(fsys, worktreePath, title)
}
