package landing

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Service) landCherryPick(ctx context.Context, opts LandOpts, meta *store.InvocationMeta, integrationPath, headBefore string, commitCount int) (*LandResult, error) {
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
		conflictFiles, conflictErr := s.getConflictFiles(ctx, integrationPath)
		if conflictErr == nil && len(conflictFiles) > 0 {
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
					"hint":           "resolve conflicts manually or inspect sandbox with 'agency agent <invocation-ref> open'",
					"conflict_count": fmt.Sprintf("%d", len(conflictFiles)),
					"conflict_files": string(conflictFilesJSON),
				},
			)
		}

		return nil, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"cherry-pick failed: "+result.Stderr,
			errors.New(errors.ELandFailed, "cherry-pick failed: "+result.Stderr),
		)
	}

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
