// Package invocation provides invocation operations.
// Invocations are agent executions inside isolated sandbox worktrees.
package invocation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// SandboxMarkerFileName is the name of the marker file that identifies sandbox worktrees.
const SandboxMarkerFileName = "SANDBOX_MARKER"

// Service provides invocation operations.
type Service struct {
	store  *store.Store
	runner exec.CommandRunner
	fsys   fs.FS
	clock  func() time.Time
}

// NewService creates a new invocation service.
func NewService(st *store.Store, cr exec.CommandRunner, fsys fs.FS, now func() time.Time) *Service {
	return &Service{
		store:  st,
		runner: cr,
		fsys:   fsys,
		clock:  now,
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

	// Runner is the canonical runner id (claude-code, codex, amp, opencode, cursor, droid).
	Runner string

	// Mode is the execution mode (headed, headless).
	Mode store.RunnerMode

	// InvocationName is an optional human-readable label.
	InvocationName string

	// CheckoutRoot is the repo-scoped root for Agency-managed checkouts.
	CheckoutRoot string

	// ExecutionProfile is the profile label selected for this invocation.
	ExecutionProfile string

	// NoIncludeUntracked excludes untracked files from checkpoint snapshots.
	NoIncludeUntracked bool

	// ClientRequestID records control-plane start idempotency, when provided.
	ClientRequestID string

	// RequestFingerprint records the durable fingerprint for ClientRequestID.
	RequestFingerprint string

	// Env overlays daemon-owned git commands for this invocation.
	Env map[string]string
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

	// TmuxSession is the canonical session name for headed invocations.
	TmuxSession string
}

// Create creates a new invocation with its sandbox worktree.
//
// Operations (in order):
//  1. Generate invocation_id
//  2. Validate invocation name uniqueness (if name provided)
//  3. Verify integration marker exists (safety check)
//  4. Compute sandbox paths and validate safety
//  5. Create invocation directory (exclusive)
//  6. Create sandbox directory
//  7. Capture base_commit
//  8. Write invocation meta.json
//  9. Run git worktree add for sandbox
//  10. Write SANDBOX_MARKER
//
// On failure after any step, cleanup is performed.
func (s *Service) Create(ctx context.Context, opts CreateOpts) (*CreateResult, error) {
	// 1. Generate invocation_id
	invocationID, err := core.NewRunID(s.clock())
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to generate invocation_id", err)
	}

	// 2. Validate invocation name uniqueness (if name provided)
	if opts.InvocationName != "" {
		// Validate name format
		if err := core.ValidateName(opts.InvocationName); err != nil {
			return nil, errors.WrapWithDetails(
				errors.EInvalidName,
				"invalid invocation name",
				err,
				map[string]string{
					"name": opts.InvocationName,
					"hint": "names must be 2-40 chars, lowercase alphanumeric + hyphens",
				},
			)
		}

		// Check uniqueness among active invocations
		if err := s.checkNameUniqueness(opts.RepoID, opts.InvocationName); err != nil {
			return nil, err
		}
	}

	// 3. Verify integration marker exists
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

	// 4. Compute sandbox paths
	sandboxTreePath := filepath.Join(opts.CheckoutRoot, "sandboxes", invocationID)
	sandboxBranch := "agency/sandbox-" + invocationID

	// Sandbox path safety check - CRITICAL INVARIANT
	if err := s.validateSandboxPath(sandboxTreePath, integrationTreePath); err != nil {
		return nil, err
	}

	// 5. Create invocation directory (exclusive)
	_, err = s.store.EnsureInvocationDir(opts.RepoID, invocationID)
	if err != nil {
		return nil, err
	}

	// Track cleanup state
	invocationDirCreated := true
	gitWorktreeCreated := false
	branchCreated := false

	// Cleanup function
	cleanup := func() {
		if gitWorktreeCreated {
			// Remove worktree (best-effort)
			args := []string{"-C", opts.RepoRoot, "worktree", "remove", "--force", sandboxTreePath}
			_, _ = s.runner.Run(ctx, "git", args, exec.RunOpts{Env: opts.Env})
		}
		if branchCreated {
			// Delete branch (best-effort)
			args := []string{"-C", opts.RepoRoot, "branch", "-D", sandboxBranch}
			_, _ = s.runner.Run(ctx, "git", args, exec.RunOpts{Env: opts.Env})
		}
		if invocationDirCreated {
			// Remove invocation directory (best-effort)
			_ = s.store.RemoveInvocationDir(opts.RepoID, invocationID)
		}
	}

	// 6. Create sandbox parent directory
	if err := s.fsys.MkdirAll(filepath.Dir(sandboxTreePath), 0o700); err != nil {
		cleanup()
		return nil, errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to create checkout sandboxes directory",
			err,
			map[string]string{"dir": filepath.Dir(sandboxTreePath)},
		)
	}

	// 7. Capture base_commit
	integrationBranch := opts.IntegrationWorktreeMeta.Branch
	baseCommit, err := s.captureBaseCommit(ctx, opts.RepoRoot, integrationBranch, opts.Env)
	if err != nil {
		cleanup()
		return nil, err
	}

	// 8. Write invocation meta.json before git side effects so idempotency survives daemon restart.
	meta := store.NewInvocationMeta(
		invocationID,
		opts.InvocationName,
		opts.IntegrationWorktreeID,
		sandboxTreePath,
		opts.CheckoutRoot,
		opts.ExecutionProfile,
		sandboxBranch,
		baseCommit,
		opts.Runner,
		opts.Mode,
		s.clock(),
	)
	if opts.NoIncludeUntracked {
		meta.CheckpointIncludeUntracked = false
	}
	tmuxSession := ""
	if opts.Mode == store.RunnerModeHeaded {
		tmuxSession = tmux.SessionName(invocationID)
		meta.TmuxSession = tmuxSession
	}
	meta.ClientRequestID = opts.ClientRequestID
	meta.RequestFingerprint = opts.RequestFingerprint
	if err := s.store.WriteInvocationMeta(opts.RepoID, invocationID, meta); err != nil {
		cleanup()
		return nil, err
	}

	// 9. Create sandbox git worktree + branch
	if err := s.createSandboxWorktree(ctx, opts.RepoRoot, sandboxBranch, sandboxTreePath, integrationBranch, opts.Env); err != nil {
		cleanup()
		return nil, err
	}
	gitWorktreeCreated = true
	branchCreated = true

	// 10. Write SANDBOX_MARKER + defensive integration-marker check
	if err := s.writeSandboxMarker(sandboxTreePath); err != nil {
		cleanup()
		return nil, err
	}

	return &CreateResult{
		InvocationID:  invocationID,
		SandboxPath:   sandboxTreePath,
		SandboxBranch: sandboxBranch,
		BaseCommit:    baseCommit,
		TmuxSession:   tmuxSession,
	}, nil
}

// writeSandboxMarker creates .agency/SANDBOX_MARKER in sandboxTreePath and
// defensively rejects any tree that turns out to carry an INTEGRATION_MARKER.
func (s *Service) writeSandboxMarker(sandboxTreePath string) error {
	agencyDir := filepath.Join(sandboxTreePath, ".agency")
	if err := s.fsys.MkdirAll(agencyDir, 0o755); err != nil {
		return errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to create .agency directory in sandbox",
			err,
			map[string]string{"path": agencyDir},
		)
	}
	markerPath := filepath.Join(agencyDir, SandboxMarkerFileName)
	markerContent := "# This directory is a sandbox worktree.\n# Runners may execute here.\n"
	if err := s.fsys.WriteFile(markerPath, []byte(markerContent), 0o644); err != nil {
		return errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to write SANDBOX_MARKER",
			err,
			map[string]string{"path": markerPath},
		)
	}
	if integrationworktree.HasIntegrationMarker(sandboxTreePath) {
		return errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"CRITICAL: sandbox tree contains INTEGRATION_MARKER - aborting",
			map[string]string{
				"sandbox_path": sandboxTreePath,
				"hint":         "this is a bug - sandbox path resolved to integration tree",
			},
		)
	}
	return nil
}

// captureBaseCommit runs git rev-parse on integrationBranch and returns the
// resulting commit. All failures are wrapped as ESandboxCreateFailed.
func (s *Service) captureBaseCommit(ctx context.Context, repoRoot, integrationBranch string, env map[string]string) (string, error) {
	result, err := s.runner.Run(ctx, "git", []string{"-C", repoRoot, "rev-parse", integrationBranch}, exec.RunOpts{Env: env})
	if err != nil {
		return "", errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to get base commit",
			err,
			map[string]string{"branch": integrationBranch},
		)
	}
	if result.ExitCode != 0 {
		return "", errors.NewWithDetails(
			errors.ESandboxCreateFailed,
			"failed to get base commit: "+strings.TrimSpace(result.Stderr),
			map[string]string{"branch": integrationBranch},
		)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// createSandboxWorktree adds a git worktree at sandboxTreePath on a new
// sandboxBranch starting from integrationBranch. All failures are wrapped as
// ESandboxCreateFailed.
func (s *Service) createSandboxWorktree(ctx context.Context, repoRoot, sandboxBranch, sandboxTreePath, integrationBranch string, env map[string]string) error {
	args := []string{"-C", repoRoot, "worktree", "add", "-b", sandboxBranch, sandboxTreePath, integrationBranch}
	result, err := s.runner.Run(ctx, "git", args, exec.RunOpts{Env: env})
	command := "git " + strings.Join(args, " ")
	if err != nil {
		return errors.WrapWithDetails(
			errors.ESandboxCreateFailed,
			"failed to execute git worktree add",
			err,
			map[string]string{"command": command},
		)
	}
	if result.ExitCode != 0 {
		details := map[string]string{
			"command":   command,
			"exit_code": fmt.Sprintf("%d", result.ExitCode),
		}
		if result.Stderr != "" {
			details["stderr"] = strings.TrimSpace(result.Stderr)
		}
		return errors.NewWithDetails(
			errors.ESandboxCreateFailed,
			"git worktree add failed: "+strings.TrimSpace(result.Stderr),
			details,
		)
	}
	return nil
}

// validateSandboxPath ensures the sandbox path does not resolve to the integration tree.
// This is a CRITICAL safety check per the spec.
func (s *Service) validateSandboxPath(sandboxPath, integrationPath string) error {
	sandboxClean := filepath.Clean(sandboxPath)
	integrationClean := filepath.Clean(integrationPath)

	if !filepath.IsAbs(sandboxClean) {
		return errors.NewWithDetails(
			errors.ESandboxPathUnsafe,
			"sandbox path must be absolute",
			map[string]string{"sandbox_path": sandboxPath},
		)
	}

	sandboxCanonical, err := fs.ResolveSymlinks(sandboxClean)
	if err != nil {
		return errors.WrapWithDetails(
			errors.ESandboxPathUnsafe,
			"sandbox path could not be resolved safely",
			err,
			map[string]string{"sandbox_path": sandboxPath},
		)
	}
	integrationCanonical, err := fs.ResolveSymlinks(integrationClean)
	if err != nil {
		return errors.WrapWithDetails(
			errors.ESandboxPathUnsafe,
			"integration path could not be resolved safely",
			err,
			map[string]string{"integration_path": integrationPath},
		)
	}

	// Check 1: Paths must not be equal
	if sandboxCanonical == integrationCanonical {
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
	if fs.PathContains(sandboxCanonical, integrationCanonical) {
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
	if fs.PathContains(integrationCanonical, sandboxCanonical) {
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
	if integrationworktree.HasIntegrationMarker(sandboxCanonical) {
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

// checkNameUniqueness checks if an invocation name is already used by an active invocation.
// Returns E_INVOCATION_NAME_EXISTS if the name is taken.
func (s *Service) checkNameUniqueness(repoID, name string) error {
	records, err := s.store.ScanInvocationsForRepo(repoID)
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to scan invocations for name check", err)
	}

	for _, r := range records {
		// Skip broken invocations
		if r.Broken || r.Meta == nil {
			continue
		}

		// Skip terminal invocations (names are released when invocation reaches terminal state)
		if r.Meta.Status == store.InvocationStatusFinished || r.Meta.Status == store.InvocationStatusFailed {
			continue
		}
		if r.Meta.LandingStatus == store.LandingStatusLanded || r.Meta.LandingStatus == store.LandingStatusDiscarded {
			continue
		}

		// Check if name matches
		if r.Meta.InvocationName == name {
			return errors.NewWithDetails(
				errors.EInvocationNameExists,
				"invocation name '"+name+"' is already used by an active invocation",
				map[string]string{
					"name":                name,
					"existing_invocation": r.InvocationID,
					"existing_status":     string(r.Meta.Status),
					"hint":                "use a different name or wait for the existing invocation to complete",
				},
			)
		}
	}

	return nil
}
