// Package runservice provides the concrete implementation of pipeline.RunService.
// It wires together all the real step implementations (repo gates, config loading,
// worktree creation, etc.) for the run pipeline.
package runservice

import (
	"context"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/pipeline"
	"github.com/NielsdaWheelz/agency/internal/repo"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/worktree"
)

// Service is the production implementation of pipeline.RunService.
type Service struct {
	cr      exec.CommandRunner
	fsys    fs.FS
	nowFunc func() time.Time
}

// New creates a new Service with production dependencies.
func New() *Service {
	return &Service{
		cr:      exec.NewRealRunner(),
		fsys:    fs.NewRealFS(),
		nowFunc: time.Now,
	}
}

// NewWithDeps creates a new Service with injected dependencies for testing.
func NewWithDeps(cr exec.CommandRunner, fsys fs.FS) *Service {
	return &Service{
		cr:      cr,
		fsys:    fsys,
		nowFunc: time.Now,
	}
}

// SetNowFunc overrides the time source for testing.
func (s *Service) SetNowFunc(fn func() time.Time) {
	s.nowFunc = fn
}

// CheckRepoSafe verifies repo safety (clean working tree, parent branch exists, etc.).
func (s *Service) CheckRepoSafe(ctx context.Context, st *pipeline.PipelineState) error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get current directory", err)
	}

	// Determine parent branch: use from opts if provided, otherwise will be resolved from config later
	parentBranch := st.Parent
	if parentBranch == "" {
		// Temporarily use a placeholder; will be validated after config is loaded
		// For now, we need to pass something to CheckRepoSafe
		// The actual parent branch will be validated in a real run when we have config
		//
		// Actually, looking at the pipeline order, CheckRepoSafe runs BEFORE LoadAgencyConfig.
		// This means we need to either:
		// 1. Load config first (but that changes order)
		// 2. Do parent branch check in LoadAgencyConfig (after config loads)
		// 3. Skip parent branch check if not provided, do it later
		//
		// Looking at the existing repo.CheckRepoSafe, it validates parent branch.
		// The pipeline spec says CheckRepoSafe first, then LoadAgencyConfig.
		// This means if --parent is not provided on CLI, we can't check it here.
		//
		// For now, let's handle this by:
		// - If Parent is provided, validate it
		// - If not provided, skip parent branch validation here (do it after config loads)
		parentBranch = "__deferred__" // sentinel value
	}

	// Only run full CheckRepoSafe with parent validation if parent is provided
	if parentBranch != "__deferred__" {
		result, err := repo.CheckRepoSafe(ctx, s.cr, s.fsys, cwd, repo.CheckRepoSafeOpts{
			ParentBranch: parentBranch,
		})
		if err != nil {
			return err
		}

		// Populate pipeline state
		st.RepoRoot = result.RepoRoot
		st.RepoID = result.RepoID
		st.RepoKey = result.RepoKey
		st.OriginURL = result.OriginURL
		st.DataDir = result.DataDir
		return nil
	}

	// Parent not provided - do basic repo checks without parent validation
	// The parent branch check will be done after config loads
	result, err := checkRepoSafeWithoutParent(ctx, s.cr, s.fsys, cwd)
	if err != nil {
		return err
	}

	st.RepoRoot = result.RepoRoot
	st.RepoID = result.RepoID
	st.RepoKey = result.RepoKey
	st.OriginURL = result.OriginURL
	st.DataDir = result.DataDir
	return nil
}

// checkRepoSafeWithoutParent performs repo safety checks without parent branch validation.
// Parent branch will be validated later after config is loaded.
func checkRepoSafeWithoutParent(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string) (*repo.RepoContext, error) {
	// This is a simplified version that doesn't check parent branch.
	// We'll use a dummy branch name that we know exists (HEAD) just to satisfy the API,
	// but actually the gates.go will check parent branch existence.
	//
	// Actually, looking more closely, the gates.go does a separate branch existence check.
	// Let me reconsider the design...
	//
	// The cleanest approach: have the pipeline do the parent branch check after
	// LoadAgencyConfig if it wasn't already checked. Let's just pass a sentinel
	// and document that we need to check parent branch later.
	//
	// For now, let's call the existing CheckRepoSafe but first check what the
	// current branch is, and use that if parent is not specified.
	// This defers the actual parent branch check.

	// We need to load repo info even without parent branch validation.
	// Let's inline the repo checks without the parent branch check.
	return checkRepoContextOnly(ctx, cr, fsys, cwd)
}

// checkRepoContextOnly resolves repo context without running all gates.
// This is used when parent branch will be validated later.
func checkRepoContextOnly(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string) (*repo.RepoContext, error) {
	// Import the packages we need and run the checks inline
	// Since this is getting complex, let me just call the actual CheckRepoSafe
	// with a branch we know exists - the current HEAD.

	// Get current branch
	result, err := cr.Run(ctx, "git", []string{"branch", "--show-current"}, exec.RunOpts{Dir: cwd})
	if err != nil {
		return nil, errors.Wrap(errors.ENoRepo, "failed to get current branch", err)
	}

	currentBranch := result.Stdout
	if len(currentBranch) > 0 && currentBranch[len(currentBranch)-1] == '\n' {
		currentBranch = currentBranch[:len(currentBranch)-1]
	}

	// Fallback to a common default if no branch (detached HEAD, etc.)
	if currentBranch == "" {
		currentBranch = "main"
	}

	// Now call the full CheckRepoSafe with the current branch
	// This validates everything except the *actual* parent branch the user wants
	return repo.CheckRepoSafe(ctx, cr, fsys, cwd, repo.CheckRepoSafeOpts{
		ParentBranch: currentBranch,
	})
}

// LoadAgencyConfig loads and validates agency.json, populates runner/setup info.
func (s *Service) LoadAgencyConfig(ctx context.Context, st *pipeline.PipelineState) error {
	// Load and validate config for S1 requirements
	cfg, err := config.LoadAndValidateForS1(s.fsys, st.RepoRoot)
	if err != nil {
		return err
	}

	// Determine runner name to use
	runnerName := st.Runner
	if runnerName == "" {
		runnerName = cfg.Defaults.Runner
	}

	// If a non-default runner is requested, we need to resolve it
	// ValidateForS1 already resolved the default runner, but if user specified
	// a different one, we need to check if it's configured
	resolvedRunnerCmd := cfg.ResolvedRunnerCmd
	if runnerName != cfg.Defaults.Runner {
		// Check if the requested runner is configured
		if cfg.Runners != nil {
			if cmd, ok := cfg.Runners[runnerName]; ok {
				resolvedRunnerCmd = cmd
			} else if runnerName == "claude" || runnerName == "codex" {
				// Standard runners fallback to PATH
				resolvedRunnerCmd = runnerName
			} else {
				return errors.New(errors.ERunnerNotConfigured,
					"runner \""+runnerName+"\" not configured; set runners."+runnerName+" or choose claude/codex")
			}
		} else if runnerName == "claude" || runnerName == "codex" {
			resolvedRunnerCmd = runnerName
		} else {
			return errors.New(errors.ERunnerNotConfigured,
				"runner \""+runnerName+"\" not configured; set runners."+runnerName+" or choose claude/codex")
		}
	}

	// Resolve parent branch
	parentBranch := st.Parent
	if parentBranch == "" {
		parentBranch = cfg.Defaults.ParentBranch
	}

	// If parent branch wasn't checked in CheckRepoSafe (was deferred), validate it now
	if st.Parent == "" {
		// Need to validate the resolved parent branch exists
		exists, err := branchExists(ctx, s.cr, st.RepoRoot, parentBranch)
		if err != nil {
			return err
		}
		if !exists {
			return errors.NewWithDetails(
				errors.EParentBranchNotFound,
				"local branch '"+parentBranch+"' not found; checkout or fetch parent locally",
				map[string]string{"branch": parentBranch},
			)
		}
	}

	// Populate state
	st.Runner = runnerName // Store the resolved runner name (may differ from CLI input)
	st.ResolvedRunnerCmd = resolvedRunnerCmd
	st.SetupScript = cfg.Scripts.Setup
	st.ParentBranch = parentBranch

	return nil
}

// branchExists checks if a local branch exists.
func branchExists(ctx context.Context, cr exec.CommandRunner, repoRoot, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	result, err := cr.Run(ctx, "git", []string{"show-ref", "--verify", ref}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		return false, errors.Wrap(errors.EInternal, "failed to check branch existence", err)
	}
	return result.ExitCode == 0, nil
}

// CreateWorktree creates the git worktree and .agency/ directories.
func (s *Service) CreateWorktree(ctx context.Context, st *pipeline.PipelineState) error {
	result, err := worktree.Create(ctx, s.cr, s.fsys, worktree.CreateOpts{
		RunID:        st.RunID,
		Title:        st.Title,
		RepoRoot:     st.RepoRoot,
		RepoID:       st.RepoID,
		ParentBranch: st.ParentBranch,
		DataDir:      st.DataDir,
	})
	if err != nil {
		return err
	}

	// Populate state
	st.Branch = result.Branch
	st.WorktreePath = result.WorktreePath

	// If title was empty, use the resolved title for later use
	if st.Title == "" {
		st.Title = result.ResolvedTitle
	}

	// Convert worktree warnings to pipeline warnings
	for _, w := range result.Warnings {
		st.Warnings = append(st.Warnings, pipeline.Warning{
			Code:    w.Code,
			Message: w.Message,
		})
	}

	return nil
}

// WriteMeta writes the initial meta.json for the run.
// Creates the run directory with exclusive semantics, creates the logs subdirectory,
// and writes meta.json atomically with required fields.
func (s *Service) WriteMeta(ctx context.Context, st *pipeline.PipelineState) error {
	// Validate worktree exists (should have been created by CreateWorktree)
	info, err := s.fsys.Stat(st.WorktreePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.NewWithDetails(
				errors.EInternal,
				"worktree_path does not exist (WriteMeta called before CreateWorktree?)",
				map[string]string{
					"step":          "WriteMeta",
					"worktree_path": st.WorktreePath,
				},
			)
		}
		return errors.WrapWithDetails(
			errors.EInternal,
			"failed to stat worktree_path",
			err,
			map[string]string{
				"step":          "WriteMeta",
				"worktree_path": st.WorktreePath,
			},
		)
	}
	if !info.IsDir() {
		return errors.NewWithDetails(
			errors.EInternal,
			"worktree_path is not a directory",
			map[string]string{
				"step":          "WriteMeta",
				"worktree_path": st.WorktreePath,
			},
		)
	}

	// Create a store for the run operations
	st2 := store.NewStore(s.fsys, st.DataDir, s.nowFunc)

	// Create run directory (exclusive semantics) + logs subdirectory
	_, err = st2.EnsureRunDir(st.RepoID, st.RunID)
	if err != nil {
		return err
	}

	// Create initial meta (runner name was resolved in LoadAgencyConfig)
	meta := store.NewRunMeta(
		st.RunID,
		st.RepoID,
		st.Title,
		st.Runner,
		st.ResolvedRunnerCmd,
		st.ParentBranch,
		st.Branch,
		st.WorktreePath,
		s.nowFunc(),
	)

	// Write meta.json atomically
	if err := st2.WriteInitialMeta(st.RepoID, st.RunID, meta); err != nil {
		return err
	}

	return nil
}

// RunSetup executes the setup script with timeout.
// Not implemented in this PR (PR-07).
func (s *Service) RunSetup(ctx context.Context, st *pipeline.PipelineState) error {
	return errors.NewWithDetails(
		errors.ENotImplemented,
		"RunSetup not implemented (PR-07)",
		map[string]string{"step": "RunSetup"},
	)
}

// StartTmux creates the tmux session with the runner command.
// Not implemented in this PR (PR-08).
func (s *Service) StartTmux(ctx context.Context, st *pipeline.PipelineState) error {
	return errors.NewWithDetails(
		errors.ENotImplemented,
		"StartTmux not implemented (PR-08)",
		map[string]string{"step": "StartTmux"},
	)
}
