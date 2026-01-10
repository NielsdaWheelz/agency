// Package pipeline provides the run pipeline orchestrator for agency slice 1.
// The pipeline executes steps in a fixed order, short-circuits on first error,
// and preserves AgencyError codes.
package pipeline

import (
	"context"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

// RunPipelineOpts contains the inputs for running a pipeline.
type RunPipelineOpts struct {
	// Title is the run title (may be empty; defaults applied in later PRs).
	Title string

	// Runner is the runner name (may be empty; defaults applied in later PRs).
	Runner string

	// Parent is the parent branch name (may be empty; defaults applied in later PRs).
	Parent string

	// Attach indicates whether to attach to tmux after creation (used in later PRs).
	Attach bool
}

// Warning represents a non-fatal warning emitted during pipeline execution.
type Warning struct {
	// Code is a stable warning identifier.
	Code string

	// Message is a human-readable description.
	Message string
}

// PipelineState accumulates state during pipeline execution.
// Fields are populated by steps as they execute.
type PipelineState struct {
	// From opts (copied at start)
	Title  string
	Runner string
	Parent string
	Attach bool

	// Generated immediately
	RunID string

	// Populated by CheckRepoSafe
	RepoRoot  string
	RepoID    string
	RepoKey   string
	OriginURL string
	DataDir   string

	// Populated by LoadAgencyConfig
	ResolvedRunnerCmd string
	SetupScript       string
	ParentBranch      string // resolved from config if Parent was empty

	// Populated by CreateWorktree
	Branch       string
	WorktreePath string

	// Accumulated warnings (non-fatal)
	Warnings []Warning
}

// RunService defines the step implementations for the run pipeline.
// Each method corresponds to a pipeline step executed in order.
// Implementations are injected to allow testing without real git/tmux/fs.
type RunService interface {
	// CheckRepoSafe verifies repo safety (clean working tree, parent branch exists, etc.)
	CheckRepoSafe(ctx context.Context, st *PipelineState) error

	// LoadAgencyConfig loads and validates agency.json, populates runner/setup info
	LoadAgencyConfig(ctx context.Context, st *PipelineState) error

	// CreateWorktree creates the git worktree and .agency/ directories
	CreateWorktree(ctx context.Context, st *PipelineState) error

	// WriteMeta writes the initial meta.json for the run
	WriteMeta(ctx context.Context, st *PipelineState) error

	// RunSetup executes the setup script with timeout
	RunSetup(ctx context.Context, st *PipelineState) error

	// StartTmux creates the tmux session with the runner command
	StartTmux(ctx context.Context, st *PipelineState) error
}

// Pipeline orchestrates the execution of run steps in a fixed order.
type Pipeline struct {
	svc     RunService
	nowFunc func() time.Time
}

// NewPipeline creates a pipeline with the given service implementation.
func NewPipeline(svc RunService) *Pipeline {
	return &Pipeline{
		svc:     svc,
		nowFunc: time.Now,
	}
}

// SetNowFunc overrides the time source for testing.
func (p *Pipeline) SetNowFunc(fn func() time.Time) {
	p.nowFunc = fn
}

// Run executes the pipeline steps in fixed order:
//  1. CheckRepoSafe
//  2. LoadAgencyConfig
//  3. CreateWorktree
//  4. WriteMeta
//  5. RunSetup
//  6. StartTmux
//
// Behavior:
//   - Generates run_id immediately and stores it in state
//   - Executes steps in order; short-circuits on first error
//   - If error is *AgencyError, preserves code/message/details exactly
//   - If error is not *AgencyError, wraps into *AgencyError with:
//     Code = E_INTERNAL, Message = "internal error", Cause = original error,
//     Details = map[string]string{"step": "<StepName>"}
//   - Returns runID even on error (after run_id generation)
func (p *Pipeline) Run(ctx context.Context, opts RunPipelineOpts) (string, error) {
	// Initialize state with opts
	st := &PipelineState{
		Title:  opts.Title,
		Runner: opts.Runner,
		Parent: opts.Parent,
		Attach: opts.Attach,
	}

	// Generate run_id immediately
	now := p.nowFunc()
	runID, err := core.NewRunID(now)
	if err != nil {
		// Extremely rare: crypto/rand failure
		return "", errors.Wrap(errors.EInternal, "failed to generate run_id", err)
	}
	st.RunID = runID

	// Execute steps in fixed order
	if err := p.svc.CheckRepoSafe(ctx, st); err != nil {
		return st.RunID, wrapStepError(err, StepCheckRepoSafe)
	}

	if err := p.svc.LoadAgencyConfig(ctx, st); err != nil {
		return st.RunID, wrapStepError(err, StepLoadAgencyConfig)
	}

	if err := p.svc.CreateWorktree(ctx, st); err != nil {
		return st.RunID, wrapStepError(err, StepCreateWorktree)
	}

	if err := p.svc.WriteMeta(ctx, st); err != nil {
		return st.RunID, wrapStepError(err, StepWriteMeta)
	}

	if err := p.svc.RunSetup(ctx, st); err != nil {
		return st.RunID, wrapStepError(err, StepRunSetup)
	}

	if err := p.svc.StartTmux(ctx, st); err != nil {
		return st.RunID, wrapStepError(err, StepStartTmux)
	}

	return st.RunID, nil
}

// wrapStepError ensures the error is an *AgencyError.
// If already *AgencyError, returns it unchanged.
// Otherwise wraps it with E_INTERNAL and step name in details.
func wrapStepError(err error, stepName string) error {
	if err == nil {
		return nil
	}

	// Check if already an AgencyError
	if _, ok := errors.AsAgencyError(err); ok {
		return err
	}

	// Wrap non-AgencyError into E_INTERNAL
	return errors.WrapWithDetails(
		errors.EInternal,
		"internal error",
		err,
		map[string]string{"step": stepName},
	)
}

// Step name constants.
const (
	StepCheckRepoSafe    = "CheckRepoSafe"
	StepLoadAgencyConfig = "LoadAgencyConfig"
	StepCreateWorktree   = "CreateWorktree"
	StepWriteMeta        = "WriteMeta"
	StepRunSetup         = "RunSetup"
	StepStartTmux        = "StartTmux"
)
