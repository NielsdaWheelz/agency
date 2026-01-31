// Package integrationworktree provides integration worktree operations for Slice 8.
// Integration worktrees are stable, human-owned branches that agents execute against.
package integrationworktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// IntegrationMarkerFileName is the name of the marker file that identifies integration worktrees.
const IntegrationMarkerFileName = "INTEGRATION_MARKER"

// Service provides integration worktree operations.
type Service struct {
	Store *store.Store
	CR    exec.CommandRunner
	FS    fs.FS
	Now   func() time.Time
}

// NewService creates a new integration worktree service.
func NewService(st *store.Store, cr exec.CommandRunner, fsys fs.FS, now func() time.Time) *Service {
	return &Service{
		Store: st,
		CR:    cr,
		FS:    fsys,
		Now:   now,
	}
}

// CreateOpts contains options for creating an integration worktree.
type CreateOpts struct {
	// Name is the human-readable name (required, validated).
	Name string

	// RepoRoot is the absolute path to the git repository root.
	RepoRoot string

	// RepoID is the repo identifier.
	RepoID string

	// ParentBranch is the branch to branch from.
	ParentBranch string
}

// CreateResult holds the result of a successful worktree creation.
type CreateResult struct {
	// WorktreeID is the generated worktree identifier.
	WorktreeID string

	// Branch is the created branch name.
	Branch string

	// TreePath is the absolute path to the worktree tree directory.
	TreePath string
}

// Create creates a new integration worktree.
//
// Operations (in order):
//  1. Generate worktree_id
//  2. Compute branch name
//  3. Check name uniqueness among non-archived worktrees
//  4. Create record directory (exclusive)
//  5. Run git worktree add -b <branch> <tree_path> <parent>
//  6. Write INTEGRATION_MARKER to .agency/
//  7. Write meta.json
//
// On failure after git worktree add, cleanup is performed.
func (s *Service) Create(ctx context.Context, opts CreateOpts) (*CreateResult, error) {
	// Validate name
	if err := core.ValidateName(opts.Name); err != nil {
		return nil, err
	}

	// Check name uniqueness
	records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, opts.RepoID)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan integration worktrees", err)
	}

	refs := make([]ids.WorktreeRef, len(records))
	for i, r := range records {
		state := ""
		if r.Meta != nil {
			state = string(r.Meta.State)
		}
		refs[i] = ids.WorktreeRef{
			WorktreeID: r.WorktreeID,
			RepoID:     r.RepoID,
			Name:       r.Name,
			State:      state,
			Broken:     r.Broken,
		}
	}

	if err := ids.CheckWorktreeNameUnique(opts.Name, refs); err != nil {
		return nil, err
	}

	// Generate worktree_id
	worktreeID, err := core.NewRunID(s.Now())
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to generate worktree_id", err)
	}

	// Compute branch name
	branch := core.BranchName(opts.Name, worktreeID)

	// Compute tree path
	treePath := s.Store.IntegrationWorktreeTreePath(opts.RepoID, worktreeID)

	// Create record directory with exclusive semantics
	_, err = s.Store.EnsureIntegrationWorktreeDir(opts.RepoID, worktreeID)
	if err != nil {
		return nil, err
	}

	// Track cleanup needs
	recordDirCreated := true
	gitWorktreeCreated := false
	branchCreated := false

	// Cleanup function
	cleanup := func() {
		if gitWorktreeCreated {
			// Remove worktree (best-effort)
			args := []string{"-C", opts.RepoRoot, "worktree", "remove", "--force", treePath}
			_, _ = s.CR.Run(ctx, "git", args, exec.RunOpts{})
		}
		if branchCreated {
			// Delete branch (best-effort)
			args := []string{"-C", opts.RepoRoot, "branch", "-D", branch}
			_, _ = s.CR.Run(ctx, "git", args, exec.RunOpts{})
		}
		if recordDirCreated {
			// Remove record directory (best-effort)
			_ = s.Store.RemoveIntegrationWorktreeDir(opts.RepoID, worktreeID)
		}
	}

	// Create git worktree + branch
	args := []string{
		"-C", opts.RepoRoot,
		"worktree", "add",
		"-b", branch,
		treePath,
		opts.ParentBranch,
	}

	result, err := s.CR.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to execute git worktree add",
			err,
			map[string]string{"command": "git " + strings.Join(args, " ")},
		)
	}

	if result.ExitCode != 0 {
		cleanup()
		details := map[string]string{
			"command":   "git " + strings.Join(args, " "),
			"exit_code": fmt.Sprintf("%d", result.ExitCode),
		}
		if result.Stderr != "" {
			details["stderr"] = strings.TrimSpace(result.Stderr)
		}
		return nil, errors.NewWithDetails(
			errors.EWorktreeCreateFailed,
			"git worktree add failed: "+strings.TrimSpace(result.Stderr),
			details,
		)
	}

	gitWorktreeCreated = true
	branchCreated = true

	// Create .agency/ directory
	agencyDir := filepath.Join(treePath, ".agency")
	if err := s.FS.MkdirAll(agencyDir, 0o755); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to create .agency directory",
			err,
			map[string]string{"path": agencyDir},
		)
	}

	// Write INTEGRATION_MARKER (before meta.json per spec)
	markerPath := filepath.Join(agencyDir, IntegrationMarkerFileName)
	markerContent := "# This directory is an integration worktree.\n# Runners must not execute here.\n"
	if err := s.FS.WriteFile(markerPath, []byte(markerContent), 0o644); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to write INTEGRATION_MARKER",
			err,
			map[string]string{"path": markerPath},
		)
	}

	// Write meta.json
	meta := store.NewIntegrationWorktreeMeta(
		worktreeID,
		opts.Name,
		opts.RepoID,
		branch,
		opts.ParentBranch,
		treePath,
		s.Now(),
	)

	if err := s.Store.WriteIntegrationWorktreeMeta(opts.RepoID, worktreeID, meta); err != nil {
		cleanup()
		return nil, err
	}

	return &CreateResult{
		WorktreeID: worktreeID,
		Branch:     branch,
		TreePath:   treePath,
	}, nil
}

// RemoveOpts contains options for removing an integration worktree.
type RemoveOpts struct {
	// RepoRoot is the absolute path to the git repository root.
	RepoRoot string

	// Force forces removal even if the worktree is dirty.
	Force bool
}

// Remove removes an integration worktree.
//
// Operations:
//  1. Run git worktree remove [--force] <tree_path>
//  2. Set state = archived in meta.json
//
// Does not delete the record directory or meta.json.
func (s *Service) Remove(ctx context.Context, repoID, worktreeID string, opts RemoveOpts) error {
	// Read meta to get tree path
	meta, err := s.Store.ReadIntegrationWorktreeMeta(repoID, worktreeID)
	if err != nil {
		return err
	}

	// Check if already archived
	if meta.State == store.WorktreeStateArchived {
		return errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"worktree is already archived",
			map[string]string{"worktree_id": worktreeID},
		)
	}

	// Build git worktree remove command
	args := []string{"-C", opts.RepoRoot, "worktree", "remove"}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, meta.TreePath)

	result, runErr := s.CR.Run(ctx, "git", args, exec.RunOpts{})
	if runErr != nil {
		return errors.WrapWithDetails(
			errors.EWorktreeRemoveFailed,
			"failed to execute git worktree remove",
			runErr,
			map[string]string{"command": "git " + strings.Join(args, " ")},
		)
	}

	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(result.Stderr)
		// Check for dirty worktree error
		if !opts.Force && (strings.Contains(stderr, "untracked") || strings.Contains(stderr, "modified")) {
			return errors.NewWithDetails(
				errors.EDirtyWorktree,
				"worktree has uncommitted changes; commit/stash your changes or use --force",
				map[string]string{
					"worktree_id": worktreeID,
					"tree_path":   meta.TreePath,
					"hint":        "commit or stash your changes, or rerun with --force",
				},
			)
		}
		return errors.NewWithDetails(
			errors.EWorktreeRemoveFailed,
			"git worktree remove failed: "+stderr,
			map[string]string{
				"command":   "git " + strings.Join(args, " "),
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
				"stderr":    stderr,
			},
		)
	}

	// Update meta to archived state
	err = s.Store.UpdateIntegrationWorktreeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.State = store.WorktreeStateArchived
	})
	if err != nil {
		// Log but don't fail - the worktree is already removed
		// The meta file will show present but tree is gone
		return err
	}

	return nil
}

// Resolve resolves a worktree identifier (name, id, or prefix) to a record.
// Returns the resolved record or an error.
func (s *Service) Resolve(repoID, input string, includeArchived bool) (*store.IntegrationWorktreeRecord, error) {
	records, err := store.ScanIntegrationWorktreesForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan integration worktrees", err)
	}

	refs := make([]ids.WorktreeRef, len(records))
	recordMap := make(map[string]*store.IntegrationWorktreeRecord)

	for i, r := range records {
		state := ""
		if r.Meta != nil {
			state = string(r.Meta.State)
		}
		refs[i] = ids.WorktreeRef{
			WorktreeID: r.WorktreeID,
			RepoID:     r.RepoID,
			Name:       r.Name,
			State:      state,
			Broken:     r.Broken,
		}
		recordMap[r.WorktreeID] = &records[i]
	}

	ref, err := ids.ResolveWorktreeRef(input, refs, ids.ResolveWorktreeRefOpts{
		IncludeArchived: includeArchived,
	})
	if err != nil {
		// Convert to agency errors
		if _, ok := err.(*ids.ErrWorktreeNotFound); ok {
			return nil, errors.NewWithDetails(
				errors.EWorktreeNotFound,
				"worktree not found: "+input,
				map[string]string{"input": input},
			)
		}
		if ambErr, ok := err.(*ids.ErrWorktreeAmbiguous); ok {
			candidates := make([]string, len(ambErr.Candidates))
			for i, c := range ambErr.Candidates {
				candidates[i] = c.WorktreeID
			}
			return nil, errors.NewWithDetails(
				errors.EWorktreeIDAmbiguous,
				"ambiguous worktree identifier '"+input+"' matches multiple worktrees: "+strings.Join(candidates, ", "),
				map[string]string{"input": input},
			)
		}
		return nil, err
	}

	return recordMap[ref.WorktreeID], nil
}

// HasIntegrationMarker checks if a directory contains the INTEGRATION_MARKER file.
func HasIntegrationMarker(path string) bool {
	markerPath := filepath.Join(path, ".agency", IntegrationMarkerFileName)
	_, err := os.Stat(markerPath)
	return err == nil
}

// ValidateRepoContext validates the repo context for worktree operations.
// Checks: CWD is inside a git repo, parent tree is clean.
func ValidateRepoContext(ctx context.Context, cr exec.CommandRunner, cwd string) (repoRoot string, err error) {
	// Check we're inside a git repo
	root, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return "", err
	}

	// Check parent tree is clean
	clean, err := git.IsClean(ctx, cr, root.Path)
	if err != nil {
		return "", err
	}
	if !clean {
		return "", errors.New(errors.EParentDirty, "working tree has uncommitted changes; commit or stash before creating a worktree")
	}

	return root.Path, nil
}
