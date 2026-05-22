package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/mergeflow"
	"github.com/NielsdaWheelz/agency/internal/store"
)

const (
	mergeEventStarted   = "agency.merge_started"
	mergeEventSucceeded = "agency.merge_succeeded"
	mergeEventFailed    = "agency.merge_failed"

	mergeConfirmationYes   = "yes"
	mergeConfirmationTyped = "typed"
)

type mergeStrategy string

const (
	mergeStrategySquash mergeStrategy = "squash"
	mergeStrategyMerge  mergeStrategy = "merge"
	mergeStrategyRebase mergeStrategy = "rebase"
)

type normalizedMergeRequest struct {
	Strategy         mergeStrategy
	ConfirmationMode string
	DeleteBranch     bool
	AgencyConfigPath string
}

type mergeResult struct {
	Branch         string
	PRNumber       int
	PRURL          string
	Strategy       mergeStrategy
	DeleteBranch   bool
	MergeLogPath   string
	ArchiveLogPath string
	VerifyLogPath  string
}

type mergePRView struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
	Mergeable   string `json:"mergeable"`
	HeadRefName string `json:"headRefName"`
}

func normalizeMergeRequest(req WorktreePRMergeRequest) (normalizedMergeRequest, error) {
	mode := strings.TrimSpace(req.ConfirmationMode)
	if mode != mergeConfirmationYes && mode != mergeConfirmationTyped {
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EInvalidArgument,
			"confirmation_mode must be one of: yes, typed",
			map[string]string{
				"hint": "for non-interactive automation, pass --yes",
			},
		)
	}
	if !req.Confirmed {
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EConfirmationRequired,
			"merge requires explicit confirmation",
			map[string]string{
				"hint": "re-run with --yes (non-interactive) or provide interactive typed confirmation",
			},
		)
	}

	strategy := mergeStrategy(strings.TrimSpace(req.Strategy))
	if strategy == "" {
		strategy = mergeStrategySquash
	}
	switch strategy {
	case mergeStrategySquash, mergeStrategyMerge, mergeStrategyRebase:
	default:
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EInvalidArgument,
			"strategy must be one of: squash, merge, rebase",
			map[string]string{
				"strategy": string(strategy),
			},
		)
	}

	agencyConfigPath := strings.TrimSpace(req.AgencyConfigPath)
	if agencyConfigPath != "" && !filepath.IsAbs(agencyConfigPath) {
		return normalizedMergeRequest{}, errors.NewWithDetails(
			errors.EInvalidArgument,
			"agency_config_path must be absolute",
			map[string]string{"agency_config_path": agencyConfigPath},
		)
	}

	return normalizedMergeRequest{
		Strategy:         strategy,
		ConfirmationMode: mode,
		DeleteBranch:     !req.NoDeleteBranch,
		AgencyConfigPath: agencyConfigPath,
	}, nil
}

func (s *Server) resolveMergePR(ctx context.Context, wtMeta *store.IntegrationWorktreeMeta, ghRepo, owner, workDir string, env map[string]string) (*mergePRView, error) {
	prs, err := mergeListPRsForBranchWithRetry(ctx, s.runner, workDir, owner, wtMeta.Branch, env)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, errors.NewWithDetails(
			errors.ENoPR,
			fmt.Sprintf("no pull request found for branch %q", wtMeta.Branch),
			map[string]string{
				"branch": wtMeta.Branch,
				"hint":   "run 'agency worktree <worktree_ref> pr sync' first",
			},
		)
	}
	if len(prs) > 1 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("multiple PRs found for branch %q", wtMeta.Branch),
			map[string]string{
				"branch": wtMeta.Branch,
				"count":  fmt.Sprintf("%d", len(prs)),
			},
		)
	}

	pr, err := mergeViewPR(ctx, s.runner, workDir, ghRepo, prs[0].Number, env)
	if err != nil {
		return nil, err
	}
	if pr.IsDraft {
		return nil, errors.NewWithDetails(
			errors.EPRDraft,
			fmt.Sprintf("PR #%d is in draft state", pr.Number),
			map[string]string{"pr_number": fmt.Sprintf("%d", pr.Number)},
		)
	}
	if strings.TrimSpace(pr.HeadRefName) != strings.TrimSpace(wtMeta.Branch) {
		return nil, errors.NewWithDetails(
			errors.EPRMismatch,
			fmt.Sprintf("PR head branch mismatch: expected %q, got %q", wtMeta.Branch, pr.HeadRefName),
			map[string]string{
				"expected_branch": wtMeta.Branch,
				"actual_branch":   pr.HeadRefName,
			},
		)
	}
	switch strings.ToUpper(strings.TrimSpace(pr.State)) {
	case "OPEN":
		if err := mergeEnsureMergeable(ctx, s.runner, workDir, ghRepo, pr.Number, env); err != nil {
			return nil, err
		}
	case "MERGED":
		return pr, nil
	default:
		return nil, errors.NewWithDetails(
			errors.EPRNotOpen,
			fmt.Sprintf("PR #%d exists but state is %s (expected OPEN or MERGED)", pr.Number, pr.State),
			map[string]string{
				"pr_number": fmt.Sprintf("%d", pr.Number),
				"state":     pr.State,
			},
		)
	}

	return pr, nil
}

func mergeListPRsForBranchWithRetry(ctx context.Context, runner exec.CommandRunner, workDir, owner, branch string, env map[string]string) ([]prSyncPR, error) {
	headWithOwner := prSyncHeadRef(owner, branch)
	prs, err := mergeListPRsByHeadWithRetry(ctx, runner, workDir, headWithOwner, env)
	if err != nil {
		return nil, err
	}
	if len(prs) > 0 {
		return prs, nil
	}

	// GitHub can surface head refs without owner prefix for same-repo branches.
	if strings.TrimSpace(owner) != "" {
		prs, err = mergeListPRsByHeadWithRetry(ctx, runner, workDir, branch, env)
		if err != nil {
			return nil, err
		}
	}
	return prs, nil
}

func mergeListPRsByHeadWithRetry(ctx context.Context, runner exec.CommandRunner, workDir, head string, env map[string]string) ([]prSyncPR, error) {
	delays := []time.Duration{
		0,
		250 * time.Millisecond,
		750 * time.Millisecond,
		1500 * time.Millisecond,
		3 * time.Second,
	}

	var prs []prSyncPR
	for idx, delay := range delays {
		if idx > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, errors.Wrap(errors.ENoPR, "context canceled while waiting for PR visibility", ctx.Err())
			case <-timer.C:
			}
		}

		var err error
		prs, err = prSyncListPRsByHead(ctx, runner, workDir, head, env)
		if err != nil {
			return nil, err
		}
		if len(prs) > 0 {
			return prs, nil
		}
	}

	return prs, nil
}

func mergeViewPR(ctx context.Context, runner exec.CommandRunner, workDir, ghRepo string, prNumber int, env map[string]string) (*mergePRView, error) {
	result, err := runner.Run(ctx, "gh", []string{
		"pr", "view", fmt.Sprintf("%d", prNumber),
		"-R", ghRepo,
		"--json", "number,url,state,isDraft,mergeable,headRefName",
	}, exec.RunOpts{
		Dir: workDir,
		Env: env,
	})
	if err != nil {
		return nil, errors.Wrap(errors.EGHPRViewFailed, "gh pr view failed to start", err)
	}
	if result.ExitCode != 0 {
		return nil, errors.NewWithDetails(
			errors.EGHPRViewFailed,
			fmt.Sprintf("gh pr view failed: %s", strings.TrimSpace(result.Stderr)),
			map[string]string{
				"pr_number": fmt.Sprintf("%d", prNumber),
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
			},
		)
	}

	var pr mergePRView
	if err := json.Unmarshal([]byte(result.Stdout), &pr); err != nil {
		return nil, errors.Wrap(errors.EGHPRViewFailed, "failed to parse gh pr view output", err)
	}
	if pr.Number == 0 || strings.TrimSpace(pr.URL) == "" || strings.TrimSpace(pr.State) == "" {
		return nil, errors.New(errors.EGHPRViewFailed, "gh pr view output missing required fields")
	}
	return &pr, nil
}

func mergeEnsureMergeable(ctx context.Context, runner exec.CommandRunner, workDir, ghRepo string, prNumber int, env map[string]string) error {
	delays := []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond, 1500 * time.Millisecond}

	for idx, delay := range delays {
		if idx > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return errors.Wrap(errors.EPRMergeabilityUnknown, "context canceled while waiting for mergeability", ctx.Err())
			case <-timer.C:
			}
		}

		pr, err := mergeViewPR(ctx, runner, workDir, ghRepo, prNumber, env)
		if err != nil {
			continue
		}
		mergeable := strings.ToUpper(strings.TrimSpace(pr.Mergeable))
		switch mergeable {
		case "MERGEABLE":
			return nil
		case "CONFLICTING":
			return errors.NewWithDetails(
				errors.EPRNotMergeable,
				fmt.Sprintf("PR #%d is not mergeable (conflicting)", prNumber),
				map[string]string{
					"pr_number": fmt.Sprintf("%d", prNumber),
				},
			)
		case "UNKNOWN", "":
			continue
		default:
			return errors.NewWithDetails(
				errors.EPRMergeabilityUnknown,
				fmt.Sprintf("unexpected mergeable value %q", mergeable),
				map[string]string{
					"pr_number": fmt.Sprintf("%d", prNumber),
					"mergeable": mergeable,
				},
			)
		}
	}

	return errors.NewWithDetails(
		errors.EPRMergeabilityUnknown,
		fmt.Sprintf("mergeability for PR #%d remained UNKNOWN after retries", prNumber),
		map[string]string{"pr_number": fmt.Sprintf("%d", prNumber)},
	)
}

func (s *Server) resolveMergeRepoRoot(repoID, workspaceRoot string) (string, error) {
	return mergeflow.ResolveRepoRoot(s.store, repoID, workspaceRoot)
}

func (s *Server) resolveMergeGitHubRepo(ctx context.Context, repoID, workDir string, env map[string]string) (string, string, error) {
	originURL := ""
	if repoRecord, exists, err := s.store.LoadRepoRecord(repoID); err == nil && exists {
		originURL = strings.TrimSpace(repoRecord.OriginURL)
	}
	if originURL == "" {
		result, err := s.runner.Run(ctx, "git", []string{"config", "--get", "remote.origin.url"}, exec.RunOpts{
			Dir: workDir,
			Env: env,
		})
		if err != nil || result.ExitCode != 0 {
			return "", "", errors.New(errors.EGHRepoParseFailed, "failed to determine GitHub repository from origin remote")
		}
		originURL = strings.TrimSpace(result.Stdout)
	}

	owner, repo, ok := identity.ParseGitHubOwnerRepo(originURL)
	if !ok {
		return "", "", errors.NewWithDetails(
			errors.EGHRepoParseFailed,
			"failed to parse owner/repo from origin URL",
			map[string]string{"origin_url": originURL},
		)
	}
	return owner + "/" + repo, owner, nil
}

func mergeConfirmPRMerged(ctx context.Context, runner exec.CommandRunner, workDir, ghRepo string, prNumber int, env map[string]string) (bool, error) {
	delays := []time.Duration{0, 250 * time.Millisecond, 750 * time.Millisecond, 1500 * time.Millisecond}

	for idx, delay := range delays {
		if idx > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, errors.Wrap(errors.EGHPRMergeFailed, "context canceled while confirming merged state", ctx.Err())
			case <-timer.C:
			}
		}

		result, err := runner.Run(ctx, "gh", []string{
			"pr", "view", fmt.Sprintf("%d", prNumber),
			"-R", ghRepo,
			"--json", "state",
		}, exec.RunOpts{
			Dir: workDir,
			Env: env,
		})
		if err != nil || result.ExitCode != 0 {
			continue
		}

		var state struct {
			State string `json:"state"`
		}
		if jsonErr := json.Unmarshal([]byte(result.Stdout), &state); jsonErr != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(state.State), "MERGED") {
			return true, nil
		}
	}

	return false, nil
}

