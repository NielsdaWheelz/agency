package commands

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestTaskStartHeadlessPromptRequiredBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name: "feature",
		Mode: "headless",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EPromptRequired, errors.GetCode(err))
}

func TestTaskStartHeadedPromptRejectedBeforeDaemon(t *testing.T) {
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
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}

func TestTaskStartInvalidModeBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskStart(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskStartOpts{
		Name:   "feature",
		Mode:   "bogus",
		Prompt: "do it",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestTaskRetryHeadedPromptRejectedBeforeDaemon(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := TaskRetry(context.Background(), testutil.NewFakeCommandRunner(), nil, "", TaskRetryOpts{
		TaskRef:  "task-1",
		Mode:     "headed",
		Prompt:   "do it",
		Detached: true,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EUsage, errors.GetCode(err))
}
