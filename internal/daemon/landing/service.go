// Package landing implements the landing workflow for applying sandbox changes
// to integration worktrees. All landing mutations flow through this service
// to ensure the daemon is the single authority for git operations.
package landing

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runnerstatus"
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
	RepoID       string
	InvocationID string
	RepoRoot     string
	Apply        bool
	RequireBase  bool
}

// Land applies sandbox changes to the integration worktree.
// All git mutations are performed under the repo lock.
func (s *Service) Land(ctx context.Context, opts LandOpts) (*LandResult, error) {
	meta, err := s.store.ReadInvocationMeta(opts.RepoID, opts.InvocationID)
	if err != nil {
		return nil, err
	}

	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(opts.RepoID, meta.IntegrationWorktreeID)
	if err != nil {
		return nil, errors.Wrap(errors.ELandFailed, "failed to read integration worktree meta", err)
	}

	if err := s.validateLandPreconditions(meta); err != nil {
		return nil, err
	}
	if err := s.ensureLandPathsExist(meta.SandboxPath, wtMeta.TreePath); err != nil {
		return nil, err
	}
	if err := s.emitLandStarted(opts.RepoID, opts.InvocationID, meta.IntegrationWorktreeID); err != nil {
		return nil, err
	}

	headBefore, err := s.getHeadCommit(ctx, wtMeta.TreePath)
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to capture integration HEAD: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to capture integration HEAD", err),
		)
	}
	if err := s.ensureLandingBase(opts, meta, headBefore); err != nil {
		return nil, s.emitLandFailure(opts.RepoID, opts.InvocationID, err.Error(), err)
	}

	commitCount, err := s.countCommits(ctx, opts.RepoRoot, meta.BaseCommit, meta.SandboxBranch)
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to count commits: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to determine commit count", err),
		)
	}

	result, err := s.landForState(ctx, opts, meta, wtMeta.TreePath, headBefore, commitCount)
	if err != nil {
		return nil, err
	}

	if err := s.syncWorktreeRunnerStatus(opts.RepoID, opts.InvocationID, meta.IntegrationWorktreeID); err != nil {
		if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.land_runner_status_warning", map[string]any{
			"invocation_id": opts.InvocationID,
			"warning":       err.Error(),
		}); emitErr != nil {
			return nil, emitErr
		}
	}

	if err := s.cleanupAfterLand(ctx, opts.RepoID, opts.InvocationID, opts.RepoRoot, meta); err != nil {
		if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.land_cleanup_warning", map[string]any{
			"invocation_id": opts.InvocationID,
			"warning":       err.Error(),
		}); emitErr != nil {
			return nil, emitErr
		}
	}

	if err := s.finalizeLand(opts.RepoID, opts.InvocationID, meta.IntegrationWorktreeID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) landForState(ctx context.Context, opts LandOpts, meta *store.InvocationMeta, integrationPath, headBefore string, commitCount int) (*LandResult, error) {
	if commitCount > 0 {
		return s.landCherryPick(ctx, opts, meta, integrationPath, headBefore, commitCount)
	}

	dirty, err := s.isSandboxDirty(ctx, meta.SandboxPath)
	if err != nil {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to check sandbox status: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to check sandbox status", err),
		)
	}
	if !dirty {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"nothing to land",
			errors.New(errors.ELandNothingToLand, "nothing to land — sandbox has no commits and no uncommitted changes"),
		)
	}
	if !opts.Apply {
		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"no commits to cherry-pick; --apply required",
			errors.NewWithDetails(errors.ELandApplyRequired,
				"no commits to cherry-pick; use --apply to land working tree changes",
				map[string]string{"hint": "run 'agency agent <invocation-ref> land --apply' to apply uncommitted changes"},
			),
		)
	}

	return s.landApply(ctx, opts, meta, integrationPath, headBefore)
}

// DiscardOpts holds options for the discard operation.
type DiscardOpts struct {
	RepoID       string
	InvocationID string
	RepoRoot     string
	StopCallback func(ctx context.Context, repoID, invocationID string) error
}

// Discard removes a sandbox without landing its changes.
func (s *Service) Discard(ctx context.Context, opts DiscardOpts) error {
	meta, err := s.store.ReadInvocationMeta(opts.RepoID, opts.InvocationID)
	if err != nil {
		return err
	}
	if meta.LandingStatus == store.LandingStatusLanded {
		return errors.New(errors.ELandAlreadyLanded, "invocation has already been landed")
	}
	if meta.LandingStatus == store.LandingStatusDiscarded {
		return errors.New(errors.ELandAlreadyDiscarded, "invocation has already been discarded")
	}

	if err := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_started", map[string]any{
		"invocation_id": opts.InvocationID,
	}); err != nil {
		return err
	}

	if meta.Status == store.InvocationStatusRunning ||
		meta.Status == store.InvocationStatusStarting ||
		meta.Status == store.InvocationStatusStopping {
		if opts.StopCallback != nil {
			if err := opts.StopCallback(ctx, opts.RepoID, opts.InvocationID); err != nil {
				if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_stop_warning", map[string]any{
					"invocation_id": opts.InvocationID,
					"warning":       err.Error(),
				}); emitErr != nil {
					return emitErr
				}
			}
		}
	}

	if err := s.cleanupSandbox(ctx, opts.RepoID, opts.InvocationID, opts.RepoRoot, meta); err != nil {
		if emitErr := s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_cleanup_warning", map[string]any{
			"invocation_id": opts.InvocationID,
			"warning":       err.Error(),
		}); emitErr != nil {
			return emitErr
		}
	}

	now := s.clock().UTC().Format(time.RFC3339)
	_ = s.store.UpdateInvocationMeta(opts.RepoID, opts.InvocationID, func(m *store.InvocationMeta) {
		m.LandingStatus = store.LandingStatusDiscarded
		if m.FinishedAt == "" {
			m.FinishedAt = now
		}
		if m.Status == store.InvocationStatusRunning ||
			m.Status == store.InvocationStatusStarting ||
			m.Status == store.InvocationStatusStopping {
			m.Status = store.InvocationStatusFailed
			m.ExitReason = "discarded"
		}
	})

	return s.emitEvent(opts.RepoID, opts.InvocationID, "agency.discard_succeeded", map[string]any{
		"invocation_id": opts.InvocationID,
	})
}

func (s *Service) syncWorktreeRunnerStatus(repoID, invocationID, worktreeID string) error {
	wtMeta, err := s.store.ReadIntegrationWorktreeMeta(repoID, worktreeID)
	if err != nil {
		return err
	}

	worktreeStatus := runnerstatus.RunnerStatus{
		SchemaVersion: runnerstatus.SchemaVersion,
		Status:        runnerstatus.StatusReady,
		UpdatedAt:     s.clock().UTC().Format(time.RFC3339),
		Summary:       "Landed invocation " + invocationID,
		Questions:     []string{},
		Blockers:      []string{},
		HowToTest:     "How to test not provided.",
		Risks:         []string{},
	}

	invocationStatus, err := runnerstatus.Load(s.store.InvocationDir(repoID, invocationID))
	if err == nil && invocationStatus != nil {
		if strings.TrimSpace(invocationStatus.Summary) != "" {
			worktreeStatus.Summary = strings.TrimSpace(invocationStatus.Summary)
		}
		if strings.TrimSpace(invocationStatus.HowToTest) != "" {
			worktreeStatus.HowToTest = strings.TrimSpace(invocationStatus.HowToTest)
		}
		if strings.TrimSpace(invocationStatus.SchemaVersion) == runnerstatus.SchemaVersion && invocationStatus.Validate() == nil {
			worktreeStatus = *invocationStatus
		}
	}

	statusPath := runnerstatus.StatusPath(wtMeta.TreePath)
	statusDir := filepath.Dir(statusPath)
	if err := s.fsys.MkdirAll(statusDir, 0o700); err != nil {
		return err
	}
	if err := s.fsys.Chmod(statusDir, 0o700); err != nil {
		return err
	}
	return fs.WriteJSONAtomic(statusPath, worktreeStatus, 0o600)
}
