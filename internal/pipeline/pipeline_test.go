package pipeline

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

// fixedTime returns a fixed time for deterministic run_id generation.
func fixedTime() time.Time {
	return time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
}

// mockRunService is a test implementation of RunService.
// Each method can be configured to succeed, fail with an error, or track calls.
type mockRunService struct {
	// Errors to return (nil = success)
	checkRepoSafeErr    error
	loadAgencyConfigErr error
	createWorktreeErr   error
	writeMetaErr        error
	runSetupErr         error
	startTmuxErr        error

	// Track which methods were called
	called []string
}

func (m *mockRunService) CheckRepoSafe(_ context.Context, _ *PipelineState) error {
	m.called = append(m.called, StepCheckRepoSafe)
	return m.checkRepoSafeErr
}

func (m *mockRunService) LoadAgencyConfig(_ context.Context, _ *PipelineState) error {
	m.called = append(m.called, StepLoadAgencyConfig)
	return m.loadAgencyConfigErr
}

func (m *mockRunService) CreateWorktree(_ context.Context, _ *PipelineState) error {
	m.called = append(m.called, StepCreateWorktree)
	return m.createWorktreeErr
}

func (m *mockRunService) WriteMeta(_ context.Context, _ *PipelineState) error {
	m.called = append(m.called, StepWriteMeta)
	return m.writeMetaErr
}

func (m *mockRunService) RunSetup(_ context.Context, _ *PipelineState) error {
	m.called = append(m.called, StepRunSetup)
	return m.runSetupErr
}

func (m *mockRunService) StartTmux(_ context.Context, _ *PipelineState) error {
	m.called = append(m.called, StepStartTmux)
	return m.startTmuxErr
}

// TestShortCircuitPreservesErrorCode tests that the pipeline short-circuits
// on first step error and preserves AgencyError codes.
func TestShortCircuitPreservesErrorCode(t *testing.T) {
	t.Parallel()
	mock := &mockRunService{
		checkRepoSafeErr: errors.New(errors.EParentDirty, "working tree has uncommitted changes"),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Name: "test"})

	// Should return error
	require.Error(t, err)

	// runID should still be returned
	assert.NotEmpty(t, runID, "expected runID to be set even on error")

	// Error code should be preserved
	code := errors.GetCode(err)
	assert.Equal(t, errors.EParentDirty, code)

	// Only CheckRepoSafe should have been called
	assert.Len(t, mock.called, 1)
	require.NotEmpty(t, mock.called)
	assert.Equal(t, StepCheckRepoSafe, mock.called[0])
}

// TestReachesThirdStepReturnsNotImplemented tests that the pipeline reaches
// the third step and returns E_NOT_IMPLEMENTED with the step name in details.
func TestReachesThirdStepReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	mock := &mockRunService{
		// First two succeed, third fails
		createWorktreeErr: errors.NewWithDetails(
			errors.ENotImplemented,
			"not implemented",
			map[string]string{"step": StepCreateWorktree},
		),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Name: "test"})

	// Should return error
	require.Error(t, err)

	// runID should still be returned
	assert.NotEmpty(t, runID, "expected runID to be set even on error")

	// Error code should be E_NOT_IMPLEMENTED
	code := errors.GetCode(err)
	assert.Equal(t, errors.ENotImplemented, code)

	// Check it's an AgencyError
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError")
	assert.Equal(t, "not implemented", ae.Msg)

	// Check details contain step name
	require.NotNil(t, ae.Details)
	assert.Equal(t, StepCreateWorktree, ae.Details["step"])

	// Only first three steps should have been called
	expected := []string{StepCheckRepoSafe, StepLoadAgencyConfig, StepCreateWorktree}
	assert.Len(t, mock.called, len(expected))
}

// TestWrapsNonAgencyError tests that non-AgencyError errors are wrapped
// into E_INTERNAL with the step name in details.
func TestWrapsNonAgencyError(t *testing.T) {
	t.Parallel()
	mock := &mockRunService{
		checkRepoSafeErr: stderrors.New("boom"),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Name: "test"})

	// Should return error
	require.Error(t, err)

	// runID should still be returned
	assert.NotEmpty(t, runID, "expected runID to be set even on error")

	// Error should be wrapped as E_INTERNAL
	code := errors.GetCode(err)
	assert.Equal(t, errors.EInternal, code)

	// Check it's an AgencyError
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError")

	// Check message
	assert.Equal(t, "internal error", ae.Msg)

	// Check cause is preserved
	require.NotNil(t, ae.Cause)
	assert.Equal(t, "boom", ae.Cause.Error())

	// Check details contain step name
	require.NotNil(t, ae.Details)
	assert.Equal(t, StepCheckRepoSafe, ae.Details["step"])
}

// TestSuccessPath tests that the pipeline returns runID without error
// when all steps succeed.
func TestSuccessPath(t *testing.T) {
	t.Parallel()
	mock := &mockRunService{} // all methods succeed (return nil)

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Name: "test"})

	// Should succeed
	require.NoError(t, err)

	// runID should be set
	assert.NotEmpty(t, runID, "expected runID to be set")

	// All 6 steps should have been called in order
	expected := []string{
		StepCheckRepoSafe,
		StepLoadAgencyConfig,
		StepCreateWorktree,
		StepWriteMeta,
		StepRunSetup,
		StepStartTmux,
	}
	require.Len(t, mock.called, len(expected))
	for i, step := range expected {
		assert.Equal(t, step, mock.called[i], "step %d", i)
	}
}

// TestRunIDGeneratedBeforeSteps tests that run_id is generated before
// any steps execute, and is available even if the first step fails.
func TestRunIDGeneratedBeforeSteps(t *testing.T) {
	t.Parallel()
	var capturedRunID string

	// Create a mock that captures the runID when CheckRepoSafe is called.
	stateCapturer := &stateCapturingMock{
		err: errors.New(errors.EParentDirty, "dirty"),
	}

	p := NewPipeline(stateCapturer)
	p.SetNowFunc(fixedTime)

	runID, _ := p.Run(context.Background(), RunPipelineOpts{Name: "test-run"})

	capturedRunID = stateCapturer.capturedRunID

	// Returned runID should match what was in state
	assert.NotEmpty(t, capturedRunID, "expected RunID to be set in state before step execution")
	assert.Equal(t, capturedRunID, runID)
}

// stateCapturingMock captures the PipelineState.RunID when CheckRepoSafe is called.
type stateCapturingMock struct {
	capturedRunID string
	err           error
}

func (m *stateCapturingMock) CheckRepoSafe(_ context.Context, st *PipelineState) error {
	m.capturedRunID = st.RunID
	return m.err
}
func (m *stateCapturingMock) LoadAgencyConfig(_ context.Context, _ *PipelineState) error {
	return nil
}
func (m *stateCapturingMock) CreateWorktree(_ context.Context, _ *PipelineState) error {
	return nil
}
func (m *stateCapturingMock) WriteMeta(_ context.Context, _ *PipelineState) error { return nil }
func (m *stateCapturingMock) RunSetup(_ context.Context, _ *PipelineState) error  { return nil }
func (m *stateCapturingMock) StartTmux(_ context.Context, _ *PipelineState) error { return nil }

// TestOptsPassedToState tests that RunPipelineOpts are correctly copied
// into the pipeline state.
func TestOptsPassedToState(t *testing.T) {
	t.Parallel()
	var capturedState *PipelineState

	captureMock := &optCapturingMock{
		capture: func(st *PipelineState) {
			capturedState = st
		},
	}

	p := NewPipeline(captureMock)
	p.SetNowFunc(fixedTime)

	opts := RunPipelineOpts{
		Name:   "my-feature",
		Runner: "claude",
		Parent: "main",
		Attach: true,
	}

	_, err := p.Run(context.Background(), opts)
	require.NoError(t, err)

	require.NotNil(t, capturedState)
	assert.Equal(t, opts.Name, capturedState.Name)
	assert.Equal(t, opts.Runner, capturedState.Runner)
	assert.Equal(t, opts.Parent, capturedState.Parent)
	assert.Equal(t, opts.Attach, capturedState.Attach)
}

// optCapturingMock captures the PipelineState for inspection.
type optCapturingMock struct {
	capture func(*PipelineState)
}

func (m *optCapturingMock) CheckRepoSafe(_ context.Context, st *PipelineState) error {
	if m.capture != nil {
		m.capture(st)
	}
	return nil
}
func (m *optCapturingMock) LoadAgencyConfig(_ context.Context, _ *PipelineState) error { return nil }
func (m *optCapturingMock) CreateWorktree(_ context.Context, _ *PipelineState) error   { return nil }
func (m *optCapturingMock) WriteMeta(_ context.Context, _ *PipelineState) error        { return nil }
func (m *optCapturingMock) RunSetup(_ context.Context, _ *PipelineState) error         { return nil }
func (m *optCapturingMock) StartTmux(_ context.Context, _ *PipelineState) error        { return nil }

// TestStepsExecuteInOrder tests that steps execute in the expected fixed order.
func TestStepsExecuteInOrder(t *testing.T) {
	t.Parallel()
	mock := &mockRunService{}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	_, err := p.Run(context.Background(), RunPipelineOpts{Name: "test-run"})
	require.NoError(t, err)

	expected := []string{
		StepCheckRepoSafe,
		StepLoadAgencyConfig,
		StepCreateWorktree,
		StepWriteMeta,
		StepRunSetup,
		StepStartTmux,
	}

	require.Len(t, mock.called, len(expected))
	for i, step := range expected {
		assert.Equal(t, step, mock.called[i], "step %d", i)
	}
}

// TestMiddleStepFailure tests that failure in a middle step short-circuits correctly.
func TestMiddleStepFailure(t *testing.T) {
	t.Parallel()
	mock := &mockRunService{
		runSetupErr: errors.New(errors.EScriptFailed, "setup script failed"),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	_, err := p.Run(context.Background(), RunPipelineOpts{Name: "test-run"})

	require.Error(t, err)

	code := errors.GetCode(err)
	assert.Equal(t, errors.EScriptFailed, code)

	// Steps up to and including RunSetup should have been called
	expected := []string{
		StepCheckRepoSafe,
		StepLoadAgencyConfig,
		StepCreateWorktree,
		StepWriteMeta,
		StepRunSetup,
	}
	assert.Len(t, mock.called, len(expected))
}
