// Package invocation provides invocation operations for Slice 8.
// Invocations are agent executions inside isolated sandbox worktrees.
package invocation

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
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// SandboxMarkerFileName is the name of the marker file that identifies sandbox worktrees.
const SandboxMarkerFileName = "SANDBOX_MARKER"

// Service provides invocation operations.
type Service struct {
	Store *store.Store
	CR    exec.CommandRunner
	FS    fs.FS
	Now   func() time.Time
}

// NewService creates a new invocation service.
func NewService(st *store.Store, cr exec.CommandRunner, fsys fs.FS, now func() time.Time) *Service {
	return &Service{
		Store: st,
		CR:    cr,
		FS:    fsys,
		Now:   now,
	}
}

// CreateOpts contains options for creating an invocation.
type CreateOpts struct {
	// IntegrationWorktreeID is the target integration worktree.
	IntegrationWorktreeID string

	// IntegrationWorktreeMeta is the resolved integration worktree metadata.
	IntegrationWorktreeMeta *store.IntegrationWorktreeMeta

	// RepoRoot is the absolute path to the git repository root.
	RepoRoot string

	// RepoID is the repo identifier.
	RepoID string

	// Runner is the runner type (claude, codex).
	Runner string

	// Mode is the execution mode (headed, headless).
	Mode store.RunnerMode

	// InvocationName is an optional human-readable label.
	InvocationName string
}

// CreateResult holds the result of a successful invocation creation.
type CreateResult struct {
	// InvocationID is the generated invocation identifier.
	InvocationID string

	// SandboxPath is the absolute path to the sandbox tree directory.
	SandboxPath string

	// SandboxBranch is the created sandbox branch name.
	SandboxBranch string

	// BaseCommit is the integration branch commit at invocation start.
	BaseCommit string
}

// Create creates a new invocation with its sandbox worktree.
//
// Operations (in order):
//  1. Generate invocation_id
//  2. Verify integration marker exists (safety check)
//  3. Compute sandbox paths and validate safety
//  4. Create invocation directory (exclusive)
//  5. Create sandbox directory
//  6. Capture base_commit
//  7. Run git worktree add for sandbox
//  8. Write SANDBOX_MARKER
//  9. Write invocation meta.json
//
// On failure after any step, cleanup is performed.
func (s *Service) Create(ctx context.Context, opts CreateOpts) (*CreateResult, error) {
	// 1. Generate invocation_id
	invocationID, err := core.NewRunID(s.Now())
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to generate invocation_id", err)
	}

	// 2. Verify integration marker exists
	integrationTreePath := opts.IntegrationWorktreeMeta.TreePath
	if !integrationworktree.HasIntegrationMarker(integrationTreePath) {
		return nil, errors.NewWithDetails(
			errors.EIntegrationMarkerMissing,
			"target is not an integration worktree (INTEGRATION_MARKER missing)",
			map[string]string{
				"worktree_id": opts.IntegrationWorktreeID,
				"tree_path":   integrationTreePath,
				"hint":        "use 'agency worktree create' to create an integration worktree first",
			},
		)
	}

	// 3. Compute sandbox paths
	sandboxTreePath := s.Store.SandboxTreePath(opts.RepoID, invocationID)
	sandboxBranch := "agency/sandbox-" + invocationID

	// Sandbox path safety check - CRITICAL INVARIANT
	if err := s.validateSandboxPath(sandboxTreePath, integrationTreePath); err != nil {
		return nil, err
	}

	// 4. Create invocation directory (exclusive)
	_, err = s.Store.EnsureInvocationDir(opts.RepoID, invocationID)
	if err != nil {
		return nil, err
	}

	// Track cleanup state
	invocationDirCreated := true
	sandboxDirCreated := false
	gitWorktreeCreated := false
	branchCreated := false

	// Cleanup function
	cleanup := func() {
		if gitWorktreeCreated {
			// Remove worktree (best-effort)
			args := []string{"-C", opts.RepoRoot, "worktree", "remove", "--force", sandboxTreePath}
			_, _ = s.CR.Run(ctx, "git", args, exec.RunOpts{})
		}
		if branchCreated {
			// Delete branch (best-effort)
			args := []string{"-C", opts.RepoRoot, "branch", "-D", sandboxBranch}
			_, _ = s.CR.Run(ctx, "git", args, exec.RunOpts{})
		}
		if sandboxDirCreated {
			// Remove sandbox directory (best-effort)
			_ = s.Store.RemoveSandboxDir(opts.RepoID, invocationID)
		}
		if invocationDirCreated {
			// Remove invocation directory (best-effort)
			_ = s.Store.RemoveInvocationDir(opts.RepoID, invocationID)
		}
	}

	// 5. Create sandbox directory
	_, err = s.Store.EnsureSandboxDir(opts.RepoID, invocationID)
	if err != nil {
		cleanup()
		return nil, err
	}
	sandboxDirCreated = true

	// 6. Capture base_commit
	integrationBranch := opts.IntegrationWorktreeMeta.Branch
	baseCommitResult, err := s.CR.Run(ctx, "git", []string{
		"-C", opts.RepoRoot,
		"rev-parse", integrationBranch,
	}, exec.RunOpts{})
	if err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to get base commit",
			err,
			map[string]string{"branch": integrationBranch},
		)
	}
	if baseCommitResult.ExitCode != 0 {
		cleanup()
		return nil, errors.NewWithDetails(
			errors.ESandboxCreateFailed,
			"failed to get base commit: "+strings.TrimSpace(baseCommitResult.Stderr),
			map[string]string{"branch": integrationBranch},
		)
	}
	baseCommit := strings.TrimSpace(baseCommitResult.Stdout)

	// 7. Create sandbox git worktree + branch
	args := []string{
		"-C", opts.RepoRoot,
		"worktree", "add",
		"-b", sandboxBranch,
		sandboxTreePath,
		integrationBranch,
	}

	result, err := s.CR.Run(ctx, "git", args, exec.RunOpts{})
	if err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
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
			errors.ESandboxCreateFailed,
			"git worktree add failed: "+strings.TrimSpace(result.Stderr),
			details,
		)
	}

	gitWorktreeCreated = true
	branchCreated = true

	// 8. Write SANDBOX_MARKER
	agencyDir := filepath.Join(sandboxTreePath, ".agency")
	if err := s.FS.MkdirAll(agencyDir, 0o755); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to create .agency directory in sandbox",
			err,
			map[string]string{"path": agencyDir},
		)
	}

	markerPath := filepath.Join(agencyDir, SandboxMarkerFileName)
	markerContent := "# This directory is a sandbox worktree.\n# Runners may execute here.\n"
	if err := s.FS.WriteFile(markerPath, []byte(markerContent), 0o644); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to write SANDBOX_MARKER",
			err,
			map[string]string{"path": markerPath},
		)
	}

	// Verify sandbox does NOT have integration marker (defensive check)
	if integrationworktree.HasIntegrationMarker(sandboxTreePath) {
		cleanup()
		return nil, errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"CRITICAL: sandbox tree contains INTEGRATION_MARKER - aborting",
			map[string]string{
				"sandbox_path": sandboxTreePath,
				"hint":         "this is a bug - sandbox path resolved to integration tree",
			},
		)
	}

	// 9. Write invocation meta.json
	meta := store.NewInvocationMeta(
		invocationID,
		opts.InvocationName,
		opts.IntegrationWorktreeID,
		sandboxTreePath,
		sandboxBranch,
		baseCommit,
		opts.Runner,
		opts.Mode,
		s.Now(),
	)

	if err := s.Store.WriteInvocationMeta(opts.RepoID, invocationID, meta); err != nil {
		cleanup()
		return nil, err
	}

	return &CreateResult{
		InvocationID:  invocationID,
		SandboxPath:   sandboxTreePath,
		SandboxBranch: sandboxBranch,
		BaseCommit:    baseCommit,
	}, nil
}

// validateSandboxPath ensures the sandbox path does not resolve to the integration tree.
// This is a CRITICAL safety check per the spec.
func (s *Service) validateSandboxPath(sandboxPath, integrationPath string) error {
	// Clean paths for comparison
	sandboxClean := filepath.Clean(sandboxPath)
	integrationClean := filepath.Clean(integrationPath)

	// Check 1: Paths must not be equal
	if sandboxClean == integrationClean {
		return errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"sandbox path equals integration tree path",
			map[string]string{
				"sandbox_path":     sandboxPath,
				"integration_path": integrationPath,
				"hint":             "this is a bug - sandbox would overwrite integration tree",
			},
		)
	}

	// Check 2: Sandbox must not be a parent of integration
	if strings.HasPrefix(integrationClean, sandboxClean+string(filepath.Separator)) {
		return errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"sandbox path is a parent of integration tree path",
			map[string]string{
				"sandbox_path":     sandboxPath,
				"integration_path": integrationPath,
			},
		)
	}

	// Check 3: Sandbox must not be a child of integration
	if strings.HasPrefix(sandboxClean, integrationClean+string(filepath.Separator)) {
		return errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"sandbox path is a child of integration tree path",
			map[string]string{
				"sandbox_path":     sandboxPath,
				"integration_path": integrationPath,
			},
		)
	}

	// Check 4: If sandbox path already exists, it must not contain INTEGRATION_MARKER
	if integrationworktree.HasIntegrationMarker(sandboxPath) {
		return errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"sandbox path already contains INTEGRATION_MARKER",
			map[string]string{
				"sandbox_path": sandboxPath,
				"hint":         "this is a bug - sandbox path resolved to existing integration tree",
			},
		)
	}

	return nil
}

// Resolve resolves an invocation identifier (id or prefix) to a record.
// Returns the resolved record or an error.
func (s *Service) Resolve(repoID, input string, opts ResolveOpts) (*store.InvocationRecord, error) {
	records, err := store.ScanInvocationsForRepo(s.Store.DataDir, repoID)
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to scan invocations", err)
	}

	refs := make([]ids.InvocationRef, len(records))
	recordMap := make(map[string]*store.InvocationRecord)

	for i, r := range records {
		status := ""
		landingStatus := ""
		if r.Meta != nil {
			status = string(r.Meta.Status)
			landingStatus = string(r.Meta.LandingStatus)
		}
		refs[i] = ids.InvocationRef{
			InvocationID:          r.InvocationID,
			RepoID:                r.RepoID,
			IntegrationWorktreeID: r.IntegrationWorktreeID,
			InvocationName:        r.InvocationName,
			Status:                status,
			LandingStatus:         landingStatus,
			SandboxExists:         r.SandboxExists,
			Broken:                r.Broken,
		}
		recordMap[r.InvocationID] = &records[i]
	}

	ref, err := ids.ResolveInvocationRef(input, refs, ids.ResolveInvocationRefOpts{
		IncludeFinished: opts.IncludeFinished,
		WorktreeFilter:  opts.WorktreeFilter,
	})
	if err != nil {
		// Convert to agency errors
		if _, ok := err.(*ids.ErrInvocationNotFound); ok {
			return nil, errors.NewWithDetails(
				errors.EInvocationNotFound,
				"invocation not found: "+input,
				map[string]string{"input": input},
			)
		}
		if ambErr, ok := err.(*ids.ErrInvocationAmbiguous); ok {
			candidates := make([]string, len(ambErr.Candidates))
			for i, c := range ambErr.Candidates {
				candidates[i] = c.InvocationID
			}
			return nil, errors.NewWithDetails(
				errors.EInvocationIDAmbiguous,
				"ambiguous invocation identifier '"+input+"' matches multiple invocations: "+strings.Join(candidates, ", "),
				map[string]string{"input": input},
			)
		}
		return nil, err
	}

	return recordMap[ref.InvocationID], nil
}

// ResolveOpts contains options for invocation resolution.
type ResolveOpts struct {
	// IncludeFinished allows resolving finished (landed/discarded) invocations.
	IncludeFinished bool

	// WorktreeFilter limits resolution to a specific worktree ID.
	WorktreeFilter string
}

// HasSandboxMarker checks if a directory contains the SANDBOX_MARKER file.
func HasSandboxMarker(path string) bool {
	markerPath := filepath.Join(path, ".agency", SandboxMarkerFileName)
	_, err := os.Stat(markerPath)
	return err == nil
}
