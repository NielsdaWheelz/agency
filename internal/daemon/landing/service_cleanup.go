package landing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Service) cleanupAfterLand(ctx context.Context, repoID, invocationID, repoRoot string, meta *store.InvocationMeta, env map[string]string) error {
	var errs []string

	if err := s.removeGitWorktreeIfPresent(ctx, repoRoot, meta.SandboxPath, env); err != nil {
		errs = append(errs, err.Error())
	}

	if err := s.deleteGitBranchIfPresent(ctx, repoRoot, meta.SandboxBranch, env); err != nil {
		errs = append(errs, err.Error())
	}

	if err := s.cleanupSnapshotRefs(ctx, repoRoot, invocationID, env); err != nil {
		errs = append(errs, fmt.Sprintf("snapshot ref cleanup: %v", err))
	}

	if err := fs.SafeRemoveAll(meta.SandboxPath, meta.CheckoutRoot); err != nil {
		errs = append(errs, fmt.Sprintf("sandbox dir removal: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (s *Service) cleanupSnapshotRefs(ctx context.Context, repoRoot, invocationID string, env map[string]string) error {
	refPrefix := fmt.Sprintf("refs/agency/snapshots/%s/", invocationID)

	result, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"for-each-ref", "--format=%(refname)", refPrefix,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return fmt.Errorf("list snapshot refs: %w", err)
	}
	if result.ExitCode != 0 {
		msg := fmt.Sprintf("list snapshot refs exited %d", result.ExitCode)
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			msg += ": " + stderr
		}
		return errors.New(msg)
	}

	refs := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		result, err := s.runner.Run(ctx, "git", []string{
			"-C", repoRoot,
			"update-ref", "-d", ref,
		}, exec.RunOpts{Env: env})
		if err != nil {
			return fmt.Errorf("delete snapshot ref %s: %w", ref, err)
		}
		if result.ExitCode != 0 {
			msg := fmt.Sprintf("delete snapshot ref %s exited %d", ref, result.ExitCode)
			if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
				msg += ": " + stderr
			}
			return errors.New(msg)
		}
	}

	return nil
}

func (s *Service) cleanupSandbox(ctx context.Context, repoID, invocationID, repoRoot string, meta *store.InvocationMeta, env map[string]string) error {
	var errs []string

	if err := s.removeGitWorktreeIfPresent(ctx, repoRoot, meta.SandboxPath, env); err != nil {
		errs = append(errs, err.Error())
	}

	if err := s.deleteGitBranchIfPresent(ctx, repoRoot, meta.SandboxBranch, env); err != nil {
		errs = append(errs, err.Error())
	}

	if err := s.cleanupSnapshotRefs(ctx, repoRoot, invocationID, env); err != nil {
		errs = append(errs, fmt.Sprintf("snapshot ref cleanup: %v", err))
	}

	if err := fs.SafeRemoveAll(meta.SandboxPath, meta.CheckoutRoot); err != nil {
		errs = append(errs, fmt.Sprintf("sandbox dir removal: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (s *Service) removeGitWorktreeIfPresent(ctx context.Context, repoRoot, treePath string, env map[string]string) error {
	if treePath == "" {
		return nil
	}
	if _, err := os.Stat(treePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("worktree stat: %w", err)
	}

	result, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"worktree", "remove", "--force", treePath,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return fmt.Errorf("worktree remove: %w", err)
	}
	if result.ExitCode != 0 {
		msg := fmt.Sprintf("worktree remove exited %d", result.ExitCode)
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			msg += ": " + stderr
		}
		return errors.New(msg)
	}
	return nil
}

func (s *Service) deleteGitBranchIfPresent(ctx context.Context, repoRoot, branch string, env map[string]string) error {
	if branch == "" {
		return nil
	}
	ref := "refs/heads/" + branch
	result, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"show-ref", "--verify", ref,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return fmt.Errorf("branch lookup: %w", err)
	}
	if result.ExitCode != 0 {
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			return fmt.Errorf("branch lookup exited %d: %s", result.ExitCode, stderr)
		}
		return nil
	}

	result, err = s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"branch", "-D", branch,
	}, exec.RunOpts{Env: env})
	if err != nil {
		return fmt.Errorf("branch delete: %w", err)
	}
	if result.ExitCode != 0 {
		msg := fmt.Sprintf("branch delete exited %d", result.ExitCode)
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			msg += ": " + stderr
		}
		return errors.New(msg)
	}
	return nil
}
