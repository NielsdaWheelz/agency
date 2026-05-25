package landing

import (
	"context"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/git"
)

func (s *Service) runGit(ctx context.Context, dir string, env map[string]string, label string, args ...string) (exec.CmdResult, error) {
	return git.RunIn(ctx, s.runner, dir, env, label, args...)
}

func (s *Service) getHeadCommit(ctx context.Context, treePath string, env map[string]string) (string, error) {
	result, err := s.runGit(ctx, treePath, env, "git rev-parse", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (s *Service) countCommits(ctx context.Context, repoRoot, baseCommit, sandboxBranch string, env map[string]string) (int, error) {
	result, err := s.runGit(ctx, repoRoot, env, "git rev-list", "rev-list", "--count", fmt.Sprintf("%s..%s", baseCommit, sandboxBranch))
	if err != nil {
		return 0, err
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(result.Stdout), "%d", &count); err != nil {
		return 0, fmt.Errorf("failed to parse commit count: %w", err)
	}
	return count, nil
}

func (s *Service) isSandboxDirty(ctx context.Context, sandboxPath string, env map[string]string) (bool, error) {
	result, err := s.runGit(ctx, sandboxPath, env, "git status", "status", "--porcelain", "--", ":(exclude).agency")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func (s *Service) getConflictFiles(ctx context.Context, treePath string, env map[string]string) ([]string, error) {
	result, err := s.runner.Run(ctx, "git", []string{
		"-C", treePath,
		"diff", "--name-only", "--diff-filter=U",
	}, exec.RunOpts{Env: env})
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
	_, err := s.eventWriter.Append(
		s.eventsPath(repoID, invocationID),
		invocationID,
		eventType,
		data,
		eventlog.AppendOptions{},
	)
	if err != nil {
		return errors.Wrap(errors.ELandFailed, "failed to append invocation event", err)
	}
	return nil
}
