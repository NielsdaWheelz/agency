// Package worktree provides git worktree creation and workspace scaffolding
// for agency run operations.
package worktree

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
)

// Warning represents a non-fatal warning emitted during worktree operations.
type Warning struct {
	Code    string
	Message string
}

// CreateResult holds the result of a successful worktree creation.
type CreateResult struct {
	// Branch is the newly created branch name (agency/<name>-<shortid>).
	Branch string

	// WorktreePath is the absolute path to the worktree directory.
	WorktreePath string

	// Warnings contains non-fatal warnings (e.g., .agency/ not ignored).
	Warnings []Warning
}

// CreateOpts contains options for creating a worktree.
type CreateOpts struct {
	// RunID is the run identifier (e.g., "20260109013207-a3f2").
	RunID string

	// Name is the run name (required, validated).
	Name string

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
//  1. Compute branch name from name + run_id
//  2. Compute worktree path from data_dir + repo_id + run_id
//  3. Create branch + worktree via: git worktree add -b <branch> <path> <parent>
//  4. Create .agency/, .agency/out/, .agency/tmp/ directories
//  5. Create .agency/report.md if missing (with template)
//  6. Check if .agency/ is ignored (best-effort warning)
//
// Error codes:
//   - E_WORKTREE_CREATE_FAILED: any git worktree add failure (including collisions)
func Create(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts CreateOpts) (*CreateResult, error) {
	// 1. Compute branch name (Name is pre-validated, no need to default)
	branch := core.BranchName(opts.Name, opts.RunID)

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
	if err := scaffoldWorkspace(fsys, worktreePath, opts.Name); err != nil {
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
		Branch:       branch,
		WorktreePath: worktreePath,
		Warnings:     warnings,
	}, nil
}

// WorktreePath returns the worktree path for a run.
// Format: ${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/
func WorktreePath(dataDir, repoID, runID string) string {
	return filepath.Join(dataDir, "repos", repoID, "worktrees", runID)
}

// scaffoldWorkspace creates the .agency/ directory structure, report.md, INSTRUCTIONS.md, and runner_status.json.
// This function is idempotent for directories but will not overwrite existing files (except INSTRUCTIONS.md).
// INSTRUCTIONS.md is unconditionally overwritten on every run per spec.
func scaffoldWorkspace(fsys fs.FS, worktreePath, name string) error {
	// Create .agency/ directories including state/
	dirs := []string{
		filepath.Join(worktreePath, ".agency"),
		filepath.Join(worktreePath, ".agency", "out"),
		filepath.Join(worktreePath, ".agency", "tmp"),
		filepath.Join(worktreePath, ".agency", "state"),
	}

	for _, dir := range dirs {
		if err := fsys.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create INSTRUCTIONS.md - unconditionally overwritten on every run per spec
	instructionsPath := filepath.Join(worktreePath, ".agency", "INSTRUCTIONS.md")
	instructionsContent := InstructionsTemplate()
	if err := fsys.WriteFile(instructionsPath, []byte(instructionsContent), 0644); err != nil {
		return fmt.Errorf("failed to create INSTRUCTIONS.md: %w", err)
	}

	// Create report.md if it doesn't exist
	reportPath := filepath.Join(worktreePath, ".agency", "report.md")
	if _, err := fsys.Stat(reportPath); os.IsNotExist(err) {
		content := ReportTemplate(name)
		if err := fsys.WriteFile(reportPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create report.md: %w", err)
		}
	}

	// Create runner_status.json with initial "working" status
	statusPath := runnerstatus.StatusPath(worktreePath)
	if _, err := fsys.Stat(statusPath); os.IsNotExist(err) {
		initialStatus := runnerstatus.NewInitial()
		data, err := json.MarshalIndent(initialStatus, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal initial runner status: %w", err)
		}
		data = append(data, '\n')
		if err := fsys.WriteFile(statusPath, data, 0644); err != nil {
			return fmt.Errorf("failed to create runner_status.json: %w", err)
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

// ReportTemplate returns the report.md template with the given name.
// The template follows the standard agency report format.
// Per spec, it includes a reference to INSTRUCTIONS.md right after the title.
func ReportTemplate(name string) string {
	return fmt.Sprintf(`# %s

runner: read `+"`"+`.agency/INSTRUCTIONS.md`+"`"+` before starting.

## summary
- what changed (high level)
- why (intent)

## scope
- completed
- explicitly not done / deferred

## decisions
- important choices + rationale
- tradeoffs

## deviations
- where it diverged from spec + why

## problems encountered
- failing tests, tricky bugs, constraints

## how to test
- exact commands
- expected output

## review notes
- files deserving scrutiny
- potential risks

## follow-ups
- blockers or questions
`, name)
}

// InstructionsTemplate returns the INSTRUCTIONS.md content.
// This file is tool-owned, never committed, and overwritten on each run.
// It contains short, imperative, checklist-style guidance for runners.
func InstructionsTemplate() string {
	return `# Agency Runner Instructions

**Read this before starting work.**

## Workflow

- [ ] Make incremental, focused commits
- [ ] Keep commits buildable (tests should pass after each commit)
- [ ] Update ` + "`" + `.agency/report.md` + "`" + ` before finishing

## Report Requirements

Fill in at least these sections in ` + "`" + `.agency/report.md` + "`" + `:
- [ ] ` + "`" + `## summary` + "`" + ` — describe what changed and why
- [ ] ` + "`" + `## how to test` + "`" + ` — provide exact commands and expected output

## Status Tracking

If supported, record your status in ` + "`" + `.agency/state/runner_status.json` + "`" + `:
- ` + "`" + `working` + "`" + ` — actively making progress (include summary)
- ` + "`" + `needs_input` + "`" + ` — waiting for user answer (include questions[])
- ` + "`" + `blocked` + "`" + ` — cannot proceed (include blockers[])
- ` + "`" + `ready_for_review` + "`" + ` — work complete (include how_to_test)

## Notes

- This file is advisory only; no correctness depends on it
- Do not commit this file (it is in .gitignore via .agency/)
- This file is regenerated on every ` + "`" + `agency run` + "`" + `
`
}

// ScaffoldWorkspaceOnly scaffolds the .agency/ directories and report.md
// without creating a worktree. Useful for testing or recovery scenarios.
// This is an exported wrapper around scaffoldWorkspace for testing.
func ScaffoldWorkspaceOnly(fsys fs.FS, worktreePath, name string) error {
	return scaffoldWorkspace(fsys, worktreePath, name)
}
