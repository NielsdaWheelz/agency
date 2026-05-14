package landing

import (
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Service) ensureLandPathsExist(sandboxPath, integrationPath string) error {
	if _, err := os.Stat(sandboxPath); os.IsNotExist(err) {
		return errors.New(errors.ESandboxMissing, "sandbox tree no longer exists")
	}
	if _, err := os.Stat(integrationPath); os.IsNotExist(err) {
		return errors.New(errors.EIntegrationTreeMissing, "integration worktree tree no longer exists")
	}
	return nil
}

func (s *Service) emitLandStarted(repoID, invocationID, worktreeID string) error {
	return s.emitEvent(repoID, invocationID, "agency.land_started", map[string]any{
		"invocation_id": invocationID,
		"worktree_id":   worktreeID,
	})
}

func (s *Service) ensureLandingBase(opts LandOpts, meta *store.InvocationMeta, headBefore string) error {
	if !opts.RequireBase || headBefore == meta.BaseCommit {
		return nil
	}

	return errors.NewWithDetails(errors.ELandFailed,
		"integration branch has diverged from base_commit",
		map[string]string{
			"base_commit":      meta.BaseCommit,
			"integration_head": headBefore,
			"hint":             "remove --require-base to allow landing onto moved HEAD, or rebase sandbox manually",
		},
	)
}

func (s *Service) finalizeLand(repoID, invocationID, worktreeID string, result *LandResult) error {
	now := s.clock().UTC().Format(time.RFC3339)
	if err := s.store.UpdateInvocationMeta(repoID, invocationID, func(m *store.InvocationMeta) {
		m.LandingStatus = store.LandingStatusLanded
		m.FinishedAt = now
	}); err != nil {
		return errors.Wrap(errors.ELandFailed, "failed to update invocation meta after land", err)
	}

	if err := s.store.UpdateIntegrationWorktreeMeta(repoID, worktreeID, func(m *store.IntegrationWorktreeMeta) {
		m.LastUsedAt = now
	}); err != nil {
		return errors.Wrap(errors.ELandFailed, "failed to update integration worktree meta after land", err)
	}

	return s.emitEvent(repoID, invocationID, "agency.land_succeeded", map[string]any{
		"invocation_id": invocationID,
		"head_before":   result.IntegrationHeadBefore,
		"head_after":    result.IntegrationHeadAfter,
		"mode":          string(result.Mode),
		"commits":       result.CommitsLanded,
	})
}

func (s *Service) validateLandPreconditions(meta *store.InvocationMeta) error {
	if meta.Status != store.InvocationStatusFinished && meta.Status != store.InvocationStatusFailed {
		return errors.NewWithDetails(errors.EInvocationStillRunning,
			"invocation is still active",
			map[string]string{"hint": "stop the invocation first with 'agency agent <invocation-ref> stop' or 'agency agent <invocation-ref> kill'"},
		)
	}

	if meta.LandingStatus == store.LandingStatusLanded {
		return errors.New(errors.ELandAlreadyLanded, "invocation has already been landed")
	}

	if meta.LandingStatus == store.LandingStatusDiscarded {
		return errors.New(errors.ELandAlreadyDiscarded, "invocation has already been discarded")
	}

	return nil
}
