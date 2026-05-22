// Package integrationworktree provides integration worktree operations.
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
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// IntegrationMarkerFileName is the name of the marker file that identifies integration worktrees.
const IntegrationMarkerFileName = "INTEGRATION_MARKER"

// Service provides integration worktree operations.
type Service struct {
	store  *store.Store
	runner exec.CommandRunner
	fsys   fs.FS
	clock  func() time.Time
}

// NewService creates a new integration worktree service.
func NewService(st *store.Store, cr exec.CommandRunner, fsys fs.FS, now func() time.Time) *Service {
	return &Service{
		store:  st,
		runner: cr,
		fsys:   fsys,
		clock:  now,
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

	// BaseBranch is the branch to branch from.
	BaseBranch string

	// CheckoutRoot is the repo-scoped root for Agency-managed checkouts.
	CheckoutRoot string

	// ExecutionProfile is the profile label selected for this worktree.
	ExecutionProfile string

	// Env is the noninteractive Git environment for this worktree's profile.
	Env map[string]string

	// IdempotencyKey records create-request idempotency, when provided.
	IdempotencyKey string

	// RequestFingerprint records the durable fingerprint for IdempotencyKey.
	RequestFingerprint string
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
//  5. Write meta.json
//  6. Run git worktree add -b <branch> <tree_path> <base_branch>
//  7. Write INTEGRATION_MARKER to .agency/
//
// On failure after git worktree add, cleanup is performed.
func (s *Service) Create(ctx context.Context, opts CreateOpts) (*CreateResult, error) {
	// Validate name
	if err := core.ValidateName(opts.Name); err != nil {
		return nil, err
	}

	// Check name uniqueness
	records, err := s.store.ScanIntegrationWorktreesForRepo(opts.RepoID)
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
	worktreeID, err := core.NewRunID(s.clock())
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to generate worktree_id", err)
	}

	// Compute branch name
	branch := core.BranchName(opts.Name, worktreeID)

	// Compute tree path
	treePath := filepath.Join(opts.CheckoutRoot, "worktrees", opts.Name+"-"+core.ShortID(worktreeID))

	// Create record directory with exclusive semantics
	_, err = s.store.EnsureIntegrationWorktreeDir(opts.RepoID, worktreeID)
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
			_, _ = s.runner.Run(ctx, "git", args, exec.RunOpts{Env: opts.Env})
		}
		if branchCreated {
			// Delete branch (best-effort)
			args := []string{"-C", opts.RepoRoot, "branch", "-D", branch}
			_, _ = s.runner.Run(ctx, "git", args, exec.RunOpts{Env: opts.Env})
		}
		if recordDirCreated {
			// Remove record directory (best-effort)
			_ = s.store.RemoveIntegrationWorktreeDir(opts.RepoID, worktreeID)
		}
	}

	if err := s.fsys.MkdirAll(filepath.Dir(treePath), 0o700); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to create checkout worktrees directory",
			err,
			map[string]string{"dir": filepath.Dir(treePath)},
		)
	}

	// Write meta.json before git side effects so idempotency survives daemon restart.
	meta := store.NewIntegrationWorktreeMeta(
		worktreeID,
		opts.Name,
		opts.RepoID,
		branch,
		opts.BaseBranch,
		treePath,
		opts.CheckoutRoot,
		opts.ExecutionProfile,
		s.clock(),
	)
	meta.IdempotencyKey = opts.IdempotencyKey
	meta.RequestFingerprint = opts.RequestFingerprint
	if err := s.store.WriteIntegrationWorktreeMeta(opts.RepoID, worktreeID, meta); err != nil {
		cleanup()
		return nil, err
	}

	// Create git worktree + branch
	args := []string{
		"-C", opts.RepoRoot,
		"worktree", "add",
		"-b", branch,
		treePath,
		opts.BaseBranch,
	}

	result, err := s.runner.Run(ctx, "git", args, exec.RunOpts{Env: opts.Env})
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
	if err := s.fsys.MkdirAll(agencyDir, 0o755); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to create .agency directory",
			err,
			map[string]string{"path": agencyDir},
		)
	}

	// Write INTEGRATION_MARKER.
	markerPath := filepath.Join(agencyDir, IntegrationMarkerFileName)
	markerContent := "# This directory is an integration worktree.\n# Runners must not execute here.\n"
	if err := s.fsys.WriteFile(markerPath, []byte(markerContent), 0o644); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.EWorktreeCreateFailed,
			"failed to write INTEGRATION_MARKER",
			err,
			map[string]string{"path": markerPath},
		)
	}

	return &CreateResult{
		WorktreeID: worktreeID,
		Branch:     branch,
		TreePath:   treePath,
	}, nil
}

// Resolve resolves a worktree identifier (name, id, or prefix) to a record.
// Returns the resolved record or an error.
func (s *Service) Resolve(repoID, input string, includeArchived bool) (*store.IntegrationWorktreeRecord, error) {
	records, err := s.store.ScanIntegrationWorktreesForRepo(repoID)
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
