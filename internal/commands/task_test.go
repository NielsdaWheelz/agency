package commands

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestTaskStartHeadlessPromptRequired(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name: "feature",
		Mode: "headless",
	}, &stdout, &stderr)
	require.Error(t, err)
	if got := errors.GetCode(err); got != errors.EPromptRequired {
		t.Fatalf("error code = %s, want %s", got, errors.EPromptRequired)
	}
}

func TestTaskStartHeadedPromptRejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name:       "feature",
		Mode:       "headed",
		Prompt:     "do it",
		Detached:   true,
		Runner:     "claude-code",
		BaseBranch: "main",
	}, &stdout, &stderr)
	require.Error(t, err)
	if got := errors.GetCode(err); got != errors.EUsage {
		t.Fatalf("error code = %s, want %s", got, errors.EUsage)
	}
}

func TestTaskStartInvalidMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name:   "feature",
		Mode:   "bogus",
		Prompt: "do it",
	}, &stdout, &stderr)
	require.Error(t, err)
	if got := errors.GetCode(err); got != errors.EInvalidArgument {
		t.Fatalf("error code = %s, want %s", got, errors.EInvalidArgument)
	}
}

func TestTaskRetryHeadedPromptRejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskRetry(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskRetryOpts{
		TaskRef:  "task-1",
		Mode:     "headed",
		Prompt:   "do it",
		Detached: true,
	}, &stdout, &stderr)
	require.Error(t, err)
	if got := errors.GetCode(err); got != errors.EUsage {
		t.Fatalf("error code = %s, want %s", got, errors.EUsage)
	}
}
