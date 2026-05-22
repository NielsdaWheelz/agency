package landing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Service) landApply(ctx context.Context, opts LandOpts, meta *store.InvocationMeta, integrationPath, headBefore string) (*landResult, error) {
	patchPath, err := s.prepareApplyPatch(ctx, opts, meta)
	if err != nil {
		return nil, err
	}

	if err := s.applyLandingPatch(ctx, opts, integrationPath, patchPath); err != nil {
		return nil, err
	}

	if err := s.commitLandingPatch(ctx, opts, integrationPath); err != nil {
		return nil, err
	}

	headAfter, err := s.getHeadCommit(ctx, integrationPath, opts.Env)
	if err != nil {
		return nil, errors.Wrap(errors.ELandFailed, "failed to capture new integration HEAD", err)
	}

	return &landResult{
		Mode:                  modeApplyPatch,
		IntegrationHeadBefore: headBefore,
		IntegrationHeadAfter:  headAfter,
		CommitsLanded:         1,
	}, nil
}

func (s *Service) prepareApplyPatch(ctx context.Context, opts LandOpts, meta *store.InvocationMeta) (string, error) {
	tmpDir := filepath.Join(s.store.InvocationDir(opts.RepoID, opts.InvocationID), "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to create tmp directory: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to create tmp directory", err),
		)
	}

	patchPath := filepath.Join(tmpDir, "land.patch")
	if err := s.stageLandingPatch(ctx, opts, meta.SandboxPath, meta.BaseCommit, patchPath); err != nil {
		return "", err
	}
	return patchPath, nil
}

func (s *Service) stageLandingPatch(ctx context.Context, opts LandOpts, sandboxPath, baseCommit, patchPath string) error {
	if err := s.stageSandboxChanges(ctx, opts, sandboxPath); err != nil {
		return err
	}

	diffResult, err := s.generateLandingDiff(ctx, opts, sandboxPath, baseCommit)
	if err != nil {
		return err
	}
	if strings.TrimSpace(diffResult.Stdout) == "" {
		return s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"no changes to apply (empty diff)",
			errors.New(errors.ELandNothingToLand, "nothing to land — diff is empty"),
		)
	}

	if err := os.WriteFile(patchPath, []byte(diffResult.Stdout), 0o644); err != nil {
		return s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to write patch file: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to write patch file", err),
		)
	}

	return nil
}

func (s *Service) stageSandboxChanges(ctx context.Context, opts LandOpts, sandboxPath string) error {
	_, err := s.runner.Run(ctx, "git", []string{"-C", sandboxPath, "add", "-A", "--", ":(exclude).agency"}, exec.RunOpts{Env: opts.Env})
	if err != nil {
		return s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to stage changes: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to stage changes in sandbox", err),
		)
	}
	return nil
}

func (s *Service) generateLandingDiff(ctx context.Context, opts LandOpts, sandboxPath, baseCommit string) (exec.CmdResult, error) {
	diffResult, err := s.runner.Run(ctx, "git", []string{
		"-C", sandboxPath,
		"diff", "--cached", baseCommit, "--", ":(exclude).agency",
	}, exec.RunOpts{Env: opts.Env})
	if err != nil {
		return exec.CmdResult{}, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to generate diff: "+err.Error(),
			errors.Wrap(errors.ELandFailed, "failed to generate diff", err),
		)
	}
	if diffResult.ExitCode != 0 {
		return exec.CmdResult{}, s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"diff command failed: "+diffResult.Stderr,
			errors.New(errors.ELandFailed, "failed to generate diff: "+diffResult.Stderr),
		)
	}
	return diffResult, nil
}

func (s *Service) applyLandingPatch(ctx context.Context, opts LandOpts, integrationPath, patchPath string) error {
	applyResult, err := s.runner.Run(ctx, "git", []string{
		"-C", integrationPath,
		"apply", "--index", patchPath,
	}, exec.RunOpts{Env: opts.Env})
	if err != nil {
		return s.resetLandingTreeAndFail(ctx, opts, integrationPath, "failed to apply patch: "+err.Error(), errors.Wrap(errors.ELandFailed, "failed to apply patch", err))
	}
	if applyResult.ExitCode != 0 {
		return s.resetLandingTreeAndFail(ctx, opts, integrationPath, "patch apply failed: "+applyResult.Stderr, errors.New(errors.ELandFailed, "patch apply failed: "+applyResult.Stderr))
	}
	return nil
}

func (s *Service) commitLandingPatch(ctx context.Context, opts LandOpts, integrationPath string) error {
	commitMsg := fmt.Sprintf("agency: land invocation %s", opts.InvocationID)
	commitResult, err := s.runner.Run(ctx, "git", []string{
		"-C", integrationPath,
		"commit", "-m", commitMsg,
	}, exec.RunOpts{Env: opts.Env})
	if err != nil {
		return s.resetLandingTreeAndFail(ctx, opts, integrationPath, "failed to commit: "+err.Error(), errors.Wrap(errors.ELandFailed, "failed to commit applied changes", err))
	}
	if commitResult.ExitCode != 0 {
		return s.resetLandingTreeAndFail(ctx, opts, integrationPath, "commit failed: "+commitResult.Stderr, errors.New(errors.ELandFailed, "commit failed: "+commitResult.Stderr))
	}
	return nil
}

func (s *Service) resetLandingTreeAndFail(ctx context.Context, opts LandOpts, integrationPath, reason string, operationErr error) error {
	result, resetErr := s.runner.Run(ctx, "git", []string{"-C", integrationPath, "reset", "--hard"}, exec.RunOpts{Env: opts.Env})
	if resetErr != nil {
		return s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to reset integration worktree after land failure: "+resetErr.Error(),
			errors.WrapWithDetails(errors.ELandFailed, "failed to reset integration worktree after land failure", resetErr, map[string]string{
				"operation_error": operationErr.Error(),
			}),
		)
	}
	if result.ExitCode != 0 {
		return s.emitLandFailure(
			opts.RepoID,
			opts.InvocationID,
			"failed to reset integration worktree after land failure: "+result.Stderr,
			errors.NewWithDetails(errors.ELandFailed, "failed to reset integration worktree after land failure", map[string]string{
				"exit_code":       fmt.Sprintf("%d", result.ExitCode),
				"operation_error": operationErr.Error(),
				"stderr":          strings.TrimSpace(result.Stderr),
			}),
		)
	}
	return s.emitLandFailure(opts.RepoID, opts.InvocationID, reason, operationErr)
}
