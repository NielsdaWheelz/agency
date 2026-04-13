// Package landing implements the landing workflow for applying sandbox changes
// to integration worktrees. All landing mutations flow through this service
// to ensure the daemon is the single authority for git operations.
package landing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// Mode indicates which landing strategy was used.
type Mode string

const (
	// ModeCherryPick indicates commits were cherry-picked.
	ModeCherryPick Mode = "cherry_pick"

	// ModeApplyPatch indicates a patch was applied (no commits).
	ModeApplyPatch Mode = "apply_patch"
)

// LandResult contains the result of a successful land operation.
type LandResult struct {
	Mode                  Mode
	IntegrationHeadBefore string
	IntegrationHeadAfter  string
	CommitsLanded         int
}

// Service handles landing and discard operations.
type Service struct {
	store       *store.Store
	runner      exec.CommandRunner
	fsys        fs.FS
	clock       func() time.Time
	eventsPath  func(repoID, invocationID string) string
	eventWriter invocationevents.Appender
}

// NewService creates a new landing service.
func NewService(
	st *store.Store,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
) *Service {
	return NewServiceWithWriter(st, runner, fsys, clock, invocationevents.NewWriter(clock))
}

// NewServiceWithWriter creates a landing service with a shared invocation
// event writer.
func NewServiceWithWriter(
	st *store.Store,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
	eventWriter invocationevents.Appender,
) *Service {
	if eventWriter == nil {
		eventWriter = invocationevents.NewWriter(clock)
	}
	return &Service{
		store:       st,
		runner:      runner,
		fsys:        fsys,
		clock:       clock,
		eventWriter: eventWriter,
		eventsPath: func(repoID, invocationID string) string {
			return st.InvocationEventsPath(repoID, invocationID)
		},
	}
}

// LandOpts holds options for the land operation.
type LandOpts struct {
	// RepoID is the repository identifier.
	RepoID string

	// InvocationID is the invocation to land.
	InvocationID string

	// RepoRoot is the path to the main repository root.
	RepoRoot string

	// Apply enables apply mode for uncommitted changes.
	Apply bool

	// RequireBase fails if integration has diverged from base_commit.
	RequireBase bool
}

// Land applies sandbox changes to the integration worktree.
// All git mutations are performed under the repo lock.
func (s *Service) Land(ctx context.Context, opts LandOpts) (*LandResult, error) {
	// 1. Load invocation meta
	meta, err := s.store.ReadInvocationMeta(opts.RepoID, opts.InvocationID)
	if err != nil {
		return nil, err
	}

	// 2. Load integration worktree meta
	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(opts.RepoID, meta.IntegrationWorktreeID)
	if err != nil {
		return nil, errors.Wrap(errors.ELandFailed, "failed to read integration worktree meta", err)
	}

	// 3. Validate preconditions
	if err := s.validateLandPreconditions(meta); err != nil {
		return nil, err
	}

	// 4. Verify paths exist
	sandboxPath := meta.SandboxPath
	integrationPath := wtMeta.TreePath

	if _, err := os.Stat(sandboxPath); os.IsNotExist(err) {
		return nil, errors.New(errors.ESandboxMissing, "sandbox tree no longer exists")
	}
	if _, err := os.Stat(integrationPath); os.IsNotExist(err) {
		return nil, errors.New(errors.EIntegrationTreeMissing, "integration worktree tree no longer exists")
	}

	// 5. Emit land_started event
	if err := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.land_started", map[string]any{
		"invocation_id": opts.InvocationID,
		"worktree_id":   meta.IntegrationWorktreeID,
	}); err != nil {
		return nil, err
	}

	// 6. Capture integration HEAD before landing
	headBefore, err := s.getHeadCommit(ctx, integrationPath)
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to capture integration HEAD: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to capture integration HEAD", err),
		)
	}

	// 7. Check if base_commit check is required
	if opts.RequireBase && headBefore != meta.BaseCommit {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"integration branch has diverged from base_commit",
			errors.NewWithDetails(errors.ELandFailed,
				"integration branch has diverged from base_commit",
				map[string]string{
					"base_commit":      meta.BaseCommit,
					"integration_head": headBefore,
					"hint":             "remove --require-base to allow landing onto moved HEAD, or rebase sandbox manually",
				},
			),
		)
	}

	// 8. Determine landing mode
	commitCount, err := s.countCommits(ctx, opts.RepoRoot, meta.BaseCommit, meta.SandboxBranch)
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to count commits: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to determine commit count", err),
		)
	}

	var result *LandResult
	if commitCount > 0 {
		// Case A: Cherry-pick mode
		result, err = s.landCherryPick(ctx, opts, meta, integrationPath, headBefore, commitCount)
	} else {
		// Case B: Check for dirty tree
		var dirty bool
		dirty, err = s.isSandboxDirty(ctx, sandboxPath)
		if err != nil {
			return nil, s.emitLandFailure(
				opts.RepoID,
				opts.InvocationID,
				"failed to check sandbox status: "+err.Error(),
				errors.Wrap(errors.ELandFailed, "failed to check sandbox status", err),
			)
		}

		if !dirty {
			// Case C: Nothing to land
			return nil, s.emitLandFailure(
				opts.RepoID,
				opts.InvocationID,
				"nothing to land",
				errors.New(errors.ELandNothingToLand, "nothing to land — sandbox has no commits and no uncommitted changes"),
			)
		}

		if !opts.Apply {
			// No commits but dirty tree - require --apply
			return nil, s.emitLandFailure(
				opts.RepoID,
				opts.InvocationID,
				"no commits to cherry-pick; --apply required",
				errors.NewWithDetails(errors.ELandApplyRequired,
					"no commits to cherry-pick; use --apply to land working tree changes",
					map[string]string{"hint": "run 'agency agent land --apply' to apply uncommitted changes"},
				),
			)
		}

		// Apply mode
		result, err = s.landApply(ctx, opts, meta, integrationPath, headBefore)
	}

	if err != nil {
		return nil, err
	}

	// 9. Cleanup on success
	if err := s.cleanupAfterLand(ctx, opts.RepoID, opts.InvocationID, opts.RepoRoot, meta); err != nil {
		if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.land_cleanup_warning", map[string]any{
			"invocation_id": opts.InvocationID,
			"warning":       err.Error(),
		}); emitErr != nil {
			return nil, emitErr
		}
	}

	// 10. Update invocation meta
	now := s.clock().UTC().Format(time.RFC3339)
	_ = s.store.UpdateInvocationMeta(opts.RepoID, opts.InvocationID, func(m *store.InvocationMeta) {
		m.LandingStatus = store.LandingStatusLanded
		m.FinishedAt = now
	})

	// 11. Update worktree last_used_at
	_ = s.store.UpdateIntegrationWorktreeMeta(opts.RepoID, meta.IntegrationWorktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now
	})

	// 12. Emit success event
	if err := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.land_succeeded", map[string]any{
		"invocation_id": opts.InvocationID,
		"head_before":   result.IntegrationHeadBefore,
		"head_after":    result.IntegrationHeadAfter,
		"mode":          string(result.Mode),
		"commits":       result.CommitsLanded,
	}); err != nil {
		return nil, err
	}

	return result, nil
}

// validateLandPreconditions checks that the invocation is in a valid state for landing.
func (s *Service) validateLandPreconditions(meta *store.InvocationMeta) error {
	// Must be terminal
	if meta.Status != store.InvocationStatusFinished && meta.Status != store.InvocationStatusFailed {
		return errors.NewWithDetails(errors.EInvocationStillRunning,
			"invocation is still running",
			map[string]string{"hint": "stop the invocation first with 'agency agent stop' or 'agency agent kill'"},
		)
	}

	// Must not already be landed
	if meta.LandingStatus == store.LandingStatusLanded {
		return errors.New(errors.ELandAlreadyLanded, "invocation has already been landed")
	}

	// Must not already be discarded
	if meta.LandingStatus == store.LandingStatusDiscarded {
		return errors.New(errors.ELandAlreadyDiscarded, "invocation has already been discarded")
	}

	return nil
}

// landCherryPick performs cherry-pick landing.
func (s *Service) landCherryPick(ctx context.Context, opts LandOpts, meta *store.InvocationMeta, integrationPath, headBefore string, commitCount int) (*LandResult, error) {
	// Execute cherry-pick
	cherryPickArgs := []string{
		"-C", integrationPath,
		"cherry-pick", "--no-edit",
		fmt.Sprintf("%s..%s", meta.BaseCommit, meta.SandboxBranch),
	}

	result, err := s.runner.Run(ctx, "git", cherryPickArgs, exec.RunOpts{})
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"cherry-pick execution error: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to execute cherry-pick", err),
		)
	}

	if result.ExitCode != 0 {
		// Check for conflicts
		conflictFiles, conflictErr := s.getConflictFiles(ctx, integrationPath)
		if conflictErr == nil && len(conflictFiles) > 0 {
			// Abort the cherry-pick
			_, _ = s.runner.Run(ctx, "git", []string{"-C", integrationPath, "cherry-pick", "--abort"}, exec.RunOpts{})

			if err := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.conflict_detected", map[string]any{
				"invocation_id": opts.InvocationID,
				"files":         conflictFiles,
			}); err != nil {
				return nil, err
			}

			conflictFilesJSON, _ := json.Marshal(conflictFiles)
			return nil, errors.NewWithDetails(errors.ELandConflict,
				"cherry-pick resulted in merge conflicts",
				map[string]string{
					"hint":           "resolve conflicts manually or inspect sandbox with 'agency agent open'",
					"conflict_count": fmt.Sprintf("%d", len(conflictFiles)),
					"conflict_files": string(conflictFilesJSON),
				},
			)
		}

		// Non-conflict failure
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"cherry-pick failed: "+result.Stderr,
			errors.New(errors.ELandFailed, "cherry-pick failed: "+result.Stderr),
		)
	}

	// Get new HEAD
	headAfter, err := s.getHeadCommit(ctx, integrationPath)
	if err != nil {
		return nil, errors.Wrap(errors.ELandFailed, "failed to capture new integration HEAD", err)
	}

	return &LandResult{
		Mode:                  ModeCherryPick,
		IntegrationHeadBefore: headBefore,
		IntegrationHeadAfter:  headAfter,
		CommitsLanded:         commitCount,
	}, nil
}

// landApply performs apply mode landing (for uncommitted changes).
func (s *Service) landApply(ctx context.Context, opts LandOpts, meta *store.InvocationMeta, integrationPath, headBefore string) (*LandResult, error) {
	sandboxPath := meta.SandboxPath

	// Create tmp directory for patch file
	tmpDir := filepath.Join(s.store.SandboxDir(opts.RepoID, opts.InvocationID), "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to create tmp directory: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to create tmp directory", err),
		)
	}

	patchPath := filepath.Join(tmpDir, "land.patch")

	// Stage all changes in sandbox (to include untracked files in diff).
	// Exclude .agency/ — the daemon writes SANDBOX_MARKER there, not the user.
	_, err := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "add", "-A", "--", ":(exclude).agency"}, exec.RunOpts{})
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to stage changes: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to stage changes in sandbox", err),
		)
	}

	// Generate patch: diff between base_commit and current staged state.
	// Exclude .agency/ so daemon artifacts don't leak into the patch.
	diffResult, err := s.runner.Run(ctx, "git", []string{
		"-C", sandboxPath,
		"diff", "--cached", meta.BaseCommit, "--", ":(exclude).agency",
	}, exec.RunOpts{})
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to generate diff: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to generate diff", err),
		)
	}

	if diffResult.ExitCode != 0 {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"diff command failed: "+diffResult.Stderr,
			errors.New(errors.ELandFailed, "failed to generate diff: "+diffResult.Stderr),
		)
	}

	if strings.TrimSpace(diffResult.Stdout) == "" {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"no changes to apply (empty diff)",
			errors.New(errors.ELandNothingToLand, "nothing to land — diff is empty"),
		)
	}

	// Write patch file
	if err := os.WriteFile(patchPath, []byte(diffResult.Stdout), 0o644); err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to write patch file: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to write patch file", err),
		)
	}

	// Apply patch to integration tree
	applyResult, err := s.runner.Run(ctx, "git", []string{
		"-C", integrationPath,
		"apply", "--index", patchPath,
	}, exec.RunOpts{})
	if err != nil {
		// Reset integration tree on failure
		_, _ = s.runner.Run(ctx, "git", []string{"-C", integrationPath, "reset", "--hard"}, exec.RunOpts{})
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to apply patch: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to apply patch", err),
		)
	}

	if applyResult.ExitCode != 0 {
		// Reset integration tree on failure
		_, _ = s.runner.Run(ctx, "git", []string{"-C", integrationPath, "reset", "--hard"}, exec.RunOpts{})
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"patch apply failed: "+applyResult.Stderr,
			errors.New(errors.ELandFailed, "patch apply failed: "+applyResult.Stderr),
		)
	}

	// Commit the changes
	commitMsg := fmt.Sprintf("agency: land invocation %s", opts.InvocationID)
	commitResult, err := s.runner.Run(ctx, "git", []string{
		"-C", integrationPath,
		"commit", "-m", commitMsg,
	}, exec.RunOpts{})
	if err != nil {
		// Reset integration tree on failure
		_, _ = s.runner.Run(ctx, "git", []string{"-C", integrationPath, "reset", "--hard"}, exec.RunOpts{})
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to commit: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to commit applied changes", err),
		)
	}

	if commitResult.ExitCode != 0 {
		// Reset integration tree on failure
		_, _ = s.runner.Run(ctx, "git", []string{"-C", integrationPath, "reset", "--hard"}, exec.RunOpts{})
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"commit failed: "+commitResult.Stderr,
			errors.New(errors.ELandFailed, "commit failed: "+commitResult.Stderr),
		)
	}

	// Get new HEAD
	headAfter, err := s.getHeadCommit(ctx, integrationPath)
	if err != nil {
		return nil, errors.Wrap(errors.ELandFailed, "failed to capture new integration HEAD", err)
	}

	return &LandResult{
		Mode:                  ModeApplyPatch,
		IntegrationHeadBefore: headBefore,
		IntegrationHeadAfter:  headAfter,
		CommitsLanded:         1, // One commit created
	}, nil
}

// cleanupAfterLand performs cleanup after successful landing.
func (s *Service) cleanupAfterLand(ctx context.Context, repoID, invocationID, repoRoot string, meta *store.InvocationMeta) error {
	var errs []string

	// 1. Remove sandbox git worktree
	_, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"worktree", "remove", "--force", meta.SandboxPath,
	}, exec.RunOpts{})
	if err != nil {
		errs = append(errs, fmt.Sprintf("worktree remove: %v", err))
	}

	// 2. Delete sandbox branch
	_, err = s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"branch", "-D", meta.SandboxBranch,
	}, exec.RunOpts{})
	if err != nil {
		errs = append(errs, fmt.Sprintf("branch delete: %v", err))
	}

	// 3. Delete snapshot refs
	if err := s.cleanupSnapshotRefs(ctx, repoRoot, invocationID); err != nil {
		errs = append(errs, fmt.Sprintf("snapshot ref cleanup: %v", err))
	}

	// 4. Remove sandbox directory.
	sandboxDir := s.store.SandboxDir(repoID, invocationID)
	if err := fs.SafeRemoveAll(sandboxDir, s.store.SandboxesDir(repoID)); err != nil {
		errs = append(errs, fmt.Sprintf("sandbox dir removal: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup warnings: %s", strings.Join(errs, "; "))
	}

	return nil
}

// cleanupSnapshotRefs removes all snapshot refs for an invocation.
func (s *Service) cleanupSnapshotRefs(ctx context.Context, repoRoot, invocationID string) error {
	// List all refs under refs/agency/snapshots/<invocation_id>/
	refPrefix := fmt.Sprintf("refs/agency/snapshots/%s/", invocationID)

	result, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"for-each-ref", "--format=%(refname)", refPrefix,
	}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		return nil // No refs to clean up or error - not critical
	}

	refs := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		_, _ = s.runner.Run(ctx, "git", []string{
			"-C", repoRoot,
			"update-ref", "-d", ref,
		}, exec.RunOpts{})
	}

	return nil
}

// DiscardOpts holds options for the discard operation.
type DiscardOpts struct {
	// RepoID is the repository identifier.
	RepoID string

	// InvocationID is the invocation to discard.
	InvocationID string

	// RepoRoot is the path to the main repository root.
	RepoRoot string

	// StopCallback is called to stop a running invocation before discarding.
	// May be nil if no special stop handling is needed.
	StopCallback func(ctx context.Context, repoID, invocationID string) error
}

// Discard removes a sandbox without landing its changes.
func (s *Service) Discard(ctx context.Context, opts DiscardOpts) error {
	// 1. Load invocation meta
	meta, err := s.store.ReadInvocationMeta(opts.RepoID, opts.InvocationID)
	if err != nil {
		return err
	}

	// 2. Check if already discarded/landed
	if meta.LandingStatus == store.LandingStatusLanded {
		return errors.New(errors.ELandAlreadyLanded, "invocation has already been landed")
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		return errors.New(errors.ELandAlreadyDiscarded, "invocation has already been discarded")
	}

	// 3. Emit discard_started event
	if err := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_started", map[string]any{
		"invocation_id": opts.InvocationID,
	}); err != nil {
		return err
	}

	// 4. If running, stop first
	if meta.Status == store.InvocationStatusRunning || meta.Status == store.InvocationStatusStarting {
		if opts.StopCallback != nil {
			if err := opts.StopCallback(ctx, opts.RepoID, opts.InvocationID); err != nil {
				if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_stop_warning", map[string]any{
					"invocation_id": opts.InvocationID,
					"warning":       err.Error(),
				}); emitErr != nil {
					return emitErr
				}
				// Continue with discard anyway
			}
		}
	}

	// 5. Cleanup sandbox
	if err := s.cleanupSandbox(ctx, opts.RepoID, opts.InvocationID, opts.RepoRoot, meta); err != nil {
		if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_cleanup_warning", map[string]any{
			"invocation_id": opts.InvocationID,
			"warning":       err.Error(),
		}); emitErr != nil {
			return emitErr
		}
	}

	// 6. Update invocation meta
	now := s.clock().UTC().Format(time.RFC3339)
	_ = s.store.UpdateInvocationMeta(opts.RepoID, opts.InvocationID, func(m *store.InvocationMeta) {
		m.LandingStatus = store.LandingStatusDiscarded
		if m.FinishedAt == "" {
			m.FinishedAt = now
		}
		if m.Status == store.InvocationStatusRunning || m.Status == store.InvocationStatusStarting {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "discarded"
		}
	})

	// 7. Emit success event
	if err := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_succeeded", map[string]any{
		"invocation_id": opts.InvocationID,
	}); err != nil {
		return err
	}

	return nil
}

// cleanupSandbox removes the sandbox worktree, branch, and refs.
func (s *Service) cleanupSandbox(ctx context.Context, repoID, invocationID, repoRoot string, meta *store.InvocationMeta) error {
	var errs []string

	// 1. Remove sandbox git worktree
	if meta.SandboxPath != "" {
		_, err := s.runner.Run(ctx, "git", []string{
			"-C", repoRoot,
			"worktree", "remove", "--force", meta.SandboxPath,
		}, exec.RunOpts{})
		if err != nil {
			errs = append(errs, fmt.Sprintf("worktree remove: %v", err))
		}
	}

	// 2. Delete sandbox branch
	if meta.SandboxBranch != "" {
		_, err := s.runner.Run(ctx, "git", []string{
			"-C", repoRoot,
			"branch", "-D", meta.SandboxBranch,
		}, exec.RunOpts{})
		if err != nil {
			errs = append(errs, fmt.Sprintf("branch delete: %v", err))
		}
	}

	// 3. Delete snapshot refs
	if err := s.cleanupSnapshotRefs(ctx, repoRoot, invocationID); err != nil {
		errs = append(errs, fmt.Sprintf("snapshot ref cleanup: %v", err))
	}

	// 4. Remove sandbox directory.
	sandboxDir := s.store.SandboxDir(repoID, invocationID)
	if err := fs.SafeRemoveAll(sandboxDir, s.store.SandboxesDir(repoID)); err != nil {
		errs = append(errs, fmt.Sprintf("sandbox dir removal: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup warnings: %s", strings.Join(errs, "; "))
	}

	return nil
}

// Helper methods

func (s *Service) getHeadCommit(ctx context.Context, treePath string) (string, error) {
	result, err := s.runner.Run(ctx, "git", []string{
		"-C", treePath,
		"rev-parse", "HEAD",
	}, exec.RunOpts{})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse failed: %s", result.Stderr)
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (s *Service) countCommits(ctx context.Context, repoRoot, baseCommit, sandboxBranch string) (int, error) {
	result, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"rev-list", "--count", fmt.Sprintf("%s..%s", baseCommit, sandboxBranch),
	}, exec.RunOpts{})
	if err != nil {
		return 0, err
	}
	if result.ExitCode != 0 {
		return 0, fmt.Errorf("git rev-list failed: %s", result.Stderr)
	}

	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(result.Stdout), "%d", &count); err != nil {
		return 0, fmt.Errorf("failed to parse commit count: %w", err)
	}
	return count, nil
}

func (s *Service) isSandboxDirty(ctx context.Context, sandboxPath string) (bool, error) {
	// Exclude .agency/ — the daemon writes SANDBOX_MARKER there at invocation
	// startup, and it should not count as a user change.
	result, err := s.runner.Run(ctx, "git", []string{
		"-C", sandboxPath,
		"status", "--porcelain", "--", ":(exclude).agency",
	}, exec.RunOpts{})
	if err != nil {
		return false, err
	}
	if result.ExitCode != 0 {
		return false, fmt.Errorf("git status failed: %s", result.Stderr)
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func (s *Service) getConflictFiles(ctx context.Context, treePath string) ([]string, error) {
	result, err := s.runner.Run(ctx, "git", []string{
		"-C", treePath,
		"diff", "--name-only", "--diff-filter=U",
	}, exec.RunOpts{})
	if err != nil {
		return nil, err
	}

	if result.Stdout == "" {
		return nil, nil
	}

	files := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	var conflicts []string
	for _, f := range files {
		if f != "" {
			conflicts = append(conflicts, f)
		}
	}
	return conflicts, nil
}

func (s *Service) emitLandFailure(repoID, invocationID, reason string, operationErr error) error {
	if err := s.emitEvent(repoID, invocationID, "agency.land_failed", map[string]any{
		"invocation_id": invocationID,
		"reason":        reason,
	}); err != nil {
		return errors.WrapWithDetails(errors.ELandFailed, "failed to append invocation event", err, map[string]string{
			"operation_error": operationErr.Error(),
		})
	}
	return operationErr
}

func (s *Service) emitEvent(repoID, invocationID, eventType string, data map[string]any) error {
	writer := s.eventWriter
	if writer == nil {
		writer = invocationevents.NewWriter(s.clock)
		s.eventWriter = writer
	}

	_, err := writer.Append(
		s.eventsPath(repoID, invocationID),
		invocationID,
		eventType,
		data,
		invocationevents.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.ELandFailed, "failed to append invocation event", err)
	}
	return nil
}
