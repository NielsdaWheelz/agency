package landing

import (
	"context"
	"fmt"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

func (s *Service) cleanupAfterLand(ctx context.Context, repoID, invocationID, repoRoot string, meta *store.InvocationMeta) error {
	var errs []string

	_, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"worktree", "remove", "--force", meta.SandboxPath,
	}, exec.RunOpts{})
	if err != nil {
		errs = append(errs, fmt.Sprintf("worktree remove: %v", err))
	}

	_, err = s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"branch", "-D", meta.SandboxBranch,
	}, exec.RunOpts{})
	if err != nil {
		errs = append(errs, fmt.Sprintf("branch delete: %v", err))
	}

	if err := s.cleanupSnapshotRefs(ctx, repoRoot, invocationID); err != nil {
		errs = append(errs, fmt.Sprintf("snapshot ref cleanup: %v", err))
	}

	if err := fs.SafeRemoveAll(meta.SandboxPath, meta.CheckoutRoot); err != nil {
		errs = append(errs, fmt.Sprintf("sandbox dir removal: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup warnings: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (s *Service) cleanupSnapshotRefs(ctx context.Context, repoRoot, invocationID string) error {
	refPrefix := fmt.Sprintf("refs/agency/snapshots/%s/", invocationID)

	result, err := s.runner.Run(ctx, "git", []string{
		"-C", repoRoot,
		"for-each-ref", "--format=%(refname)", refPrefix,
	}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		return nil
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

func (s *Service) cleanupSandbox(ctx context.Context, repoID, invocationID, repoRoot string, meta *store.InvocationMeta) error {
	var errs []string

	if meta.SandboxPath != "" {
		_, err := s.runner.Run(ctx, "git", []string{
			"-C", repoRoot,
			"worktree", "remove", "--force", meta.SandboxPath,
		}, exec.RunOpts{})
		if err != nil {
			errs = append(errs, fmt.Sprintf("worktree remove: %v", err))
		}
	}

	if meta.SandboxBranch != "" {
		_, err := s.runner.Run(ctx, "git", []string{
			"-C", repoRoot,
			"branch", "-D", meta.SandboxBranch,
		}, exec.RunOpts{})
		if err != nil {
			errs = append(errs, fmt.Sprintf("branch delete: %v", err))
		}
	}

	if err := s.cleanupSnapshotRefs(ctx, repoRoot, invocationID); err != nil {
		errs = append(errs, fmt.Sprintf("snapshot ref cleanup: %v", err))
	}

	if err := fs.SafeRemoveAll(meta.SandboxPath, meta.CheckoutRoot); err != nil {
		errs = append(errs, fmt.Sprintf("sandbox dir removal: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup warnings: %s", strings.Join(errs, "; "))
	}

	return nil
}
