package pipeline

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

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
	mock := &mockRunService{
		checkRepoSafeErr: errors.New(errors.EParentDirty, "working tree has uncommitted changes"),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Title: "test"})

	// Should return error
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// runID should still be returned
	if runID == "" {
		t.Error("expected runID to be set even on error")
	}

	// Error code should be preserved
	code := errors.GetCode(err)
	if code != errors.EParentDirty {
		t.Errorf("expected error code %s, got %s", errors.EParentDirty, code)
	}

	// Only CheckRepoSafe should have been called
	if len(mock.called) != 1 {
		t.Errorf("expected 1 step called, got %d: %v", len(mock.called), mock.called)
	}
	if len(mock.called) > 0 && mock.called[0] != StepCheckRepoSafe {
		t.Errorf("expected first step to be %s, got %s", StepCheckRepoSafe, mock.called[0])
	}
}

// TestReachesThirdStepReturnsNotImplemented tests that the pipeline reaches
// the third step and returns E_NOT_IMPLEMENTED with the step name in details.
func TestReachesThirdStepReturnsNotImplemented(t *testing.T) {
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

	runID, err := p.Run(context.Background(), RunPipelineOpts{Title: "test"})

	// Should return error
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// runID should still be returned
	if runID == "" {
		t.Error("expected runID to be set even on error")
	}

	// Error code should be E_NOT_IMPLEMENTED
	code := errors.GetCode(err)
	if code != errors.ENotImplemented {
		t.Errorf("expected error code %s, got %s", errors.ENotImplemented, code)
	}

	// Check error message
	ae, ok := errors.AsAgencyError(err)
	if !ok {
		t.Fatal("expected AgencyError")
	}
	if ae.Msg != "not implemented" {
		t.Errorf("expected message 'not implemented', got %q", ae.Msg)
	}

	// Check details contain step name
	if ae.Details == nil {
		t.Fatal("expected details to be set")
	}
	if ae.Details["step"] != StepCreateWorktree {
		t.Errorf("expected step detail %q, got %q", StepCreateWorktree, ae.Details["step"])
	}

	// Only first three steps should have been called
	expected := []string{StepCheckRepoSafe, StepLoadAgencyConfig, StepCreateWorktree}
	if len(mock.called) != len(expected) {
		t.Errorf("expected %d steps called, got %d: %v", len(expected), len(mock.called), mock.called)
	}
}

// TestWrapsNonAgencyError tests that non-AgencyError errors are wrapped
// into E_INTERNAL with the step name in details.
func TestWrapsNonAgencyError(t *testing.T) {
	mock := &mockRunService{
		checkRepoSafeErr: stderrors.New("boom"),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Title: "test"})

	// Should return error
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// runID should still be returned
	if runID == "" {
		t.Error("expected runID to be set even on error")
	}

	// Error should be wrapped as E_INTERNAL
	code := errors.GetCode(err)
	if code != errors.EInternal {
		t.Errorf("expected error code %s, got %s", errors.EInternal, code)
	}

	// Check it's an AgencyError
	ae, ok := errors.AsAgencyError(err)
	if !ok {
		t.Fatal("expected AgencyError")
	}

	// Check message
	if ae.Msg != "internal error" {
		t.Errorf("expected message 'internal error', got %q", ae.Msg)
	}

	// Check cause is preserved
	if ae.Cause == nil {
		t.Error("expected cause to be set")
	} else if ae.Cause.Error() != "boom" {
		t.Errorf("expected cause 'boom', got %q", ae.Cause.Error())
	}

	// Check details contain step name
	if ae.Details == nil {
		t.Fatal("expected details to be set")
	}
	if ae.Details["step"] != StepCheckRepoSafe {
		t.Errorf("expected step detail %q, got %q", StepCheckRepoSafe, ae.Details["step"])
	}
}

// TestSuccessPath tests that the pipeline returns runID without error
// when all steps succeed.
func TestSuccessPath(t *testing.T) {
	mock := &mockRunService{} // all methods succeed (return nil)

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	runID, err := p.Run(context.Background(), RunPipelineOpts{Title: "test"})

	// Should succeed
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// runID should be set
	if runID == "" {
		t.Error("expected runID to be set")
	}

	// All 6 steps should have been called in order
	expected := []string{
		StepCheckRepoSafe,
		StepLoadAgencyConfig,
		StepCreateWorktree,
		StepWriteMeta,
		StepRunSetup,
		StepStartTmux,
	}
	if len(mock.called) != len(expected) {
		t.Errorf("expected %d steps called, got %d: %v", len(expected), len(mock.called), mock.called)
	}
	for i, step := range expected {
		if i < len(mock.called) && mock.called[i] != step {
			t.Errorf("step %d: expected %q, got %q", i, step, mock.called[i])
		}
	}
}

// TestRunIDGeneratedBeforeSteps tests that run_id is generated before
// any steps execute, and is available even if the first step fails.
func TestRunIDGeneratedBeforeSteps(t *testing.T) {
	var capturedRunID string

	mock := &mockRunService{}
	// Override CheckRepoSafe to capture the runID from state
	originalCheckRepoSafe := mock.CheckRepoSafe
	_ = originalCheckRepoSafe

	// Use a custom mock that captures state
	type capturingMock struct {
		mockRunService
		capturedState *PipelineState
	}
	cm := &capturingMock{}
	cm.checkRepoSafeErr = errors.New(errors.EParentDirty, "dirty")

	// We need a way to capture state. Let's use a different approach.
	// Create a mock that captures the runID when CheckRepoSafe is called.
	stateCapturer := &stateCapturingMock{
		err: errors.New(errors.EParentDirty, "dirty"),
	}

	p := NewPipeline(stateCapturer)
	p.SetNowFunc(fixedTime)

	runID, _ := p.Run(context.Background(), RunPipelineOpts{})

	capturedRunID = stateCapturer.capturedRunID

	// Returned runID should match what was in state
	if capturedRunID == "" {
		t.Error("expected RunID to be set in state before step execution")
	}
	if runID != capturedRunID {
		t.Errorf("returned runID %q doesn't match state runID %q", runID, capturedRunID)
	}
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
	var capturedState *PipelineState

	captureMock := &optCapturingMock{
		capture: func(st *PipelineState) {
			capturedState = st
		},
	}

	p := NewPipeline(captureMock)
	p.SetNowFunc(fixedTime)

	opts := RunPipelineOpts{
		Title:  "my title",
		Runner: "claude",
		Parent: "main",
		Attach: true,
	}

	_, err := p.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedState == nil {
		t.Fatal("expected state to be captured")
	}
	if capturedState.Title != opts.Title {
		t.Errorf("expected Title %q, got %q", opts.Title, capturedState.Title)
	}
	if capturedState.Runner != opts.Runner {
		t.Errorf("expected Runner %q, got %q", opts.Runner, capturedState.Runner)
	}
	if capturedState.Parent != opts.Parent {
		t.Errorf("expected Parent %q, got %q", opts.Parent, capturedState.Parent)
	}
	if capturedState.Attach != opts.Attach {
		t.Errorf("expected Attach %v, got %v", opts.Attach, capturedState.Attach)
	}
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
func (m *optCapturingMock) CreateWorktree(_ context.Context, _ *PipelineState) error  { return nil }
func (m *optCapturingMock) WriteMeta(_ context.Context, _ *PipelineState) error       { return nil }
func (m *optCapturingMock) RunSetup(_ context.Context, _ *PipelineState) error        { return nil }
func (m *optCapturingMock) StartTmux(_ context.Context, _ *PipelineState) error       { return nil }

// TestStepsExecuteInOrder tests that steps execute in the expected fixed order.
func TestStepsExecuteInOrder(t *testing.T) {
	mock := &mockRunService{}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	_, err := p.Run(context.Background(), RunPipelineOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		StepCheckRepoSafe,
		StepLoadAgencyConfig,
		StepCreateWorktree,
		StepWriteMeta,
		StepRunSetup,
		StepStartTmux,
	}

	if len(mock.called) != len(expected) {
		t.Fatalf("expected %d steps, got %d", len(expected), len(mock.called))
	}
	for i, step := range expected {
		if mock.called[i] != step {
			t.Errorf("step %d: expected %q, got %q", i, step, mock.called[i])
		}
	}
}

// TestMiddleStepFailure tests that failure in a middle step short-circuits correctly.
func TestMiddleStepFailure(t *testing.T) {
	mock := &mockRunService{
		runSetupErr: errors.New(errors.EScriptFailed, "setup script failed"),
	}

	p := NewPipeline(mock)
	p.SetNowFunc(fixedTime)

	_, err := p.Run(context.Background(), RunPipelineOpts{})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	code := errors.GetCode(err)
	if code != errors.EScriptFailed {
		t.Errorf("expected error code %s, got %s", errors.EScriptFailed, code)
	}

	// Steps up to and including RunSetup should have been called
	expected := []string{
		StepCheckRepoSafe,
		StepLoadAgencyConfig,
		StepCreateWorktree,
		StepWriteMeta,
		StepRunSetup,
	}
	if len(mock.called) != len(expected) {
		t.Errorf("expected %d steps called, got %d: %v", len(expected), len(mock.called), mock.called)
	}
}
