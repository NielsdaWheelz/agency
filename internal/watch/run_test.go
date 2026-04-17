package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRun_NilClient_ReturnsEInternal(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), nil, RunOptions{})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
}

func TestRun_HistoryInitialPageRequiresInvocationAndRepo(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, nil))

	err := Run(context.Background(), client, RunOptions{InitialPage: InitialPageHistory})
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
	assert.Contains(t, err.Error(), "history page requires an invocation and repo")
}

func TestRun_UnknownInitialPageReturnsEInternal(t *testing.T) {
	t.Parallel()

	client := daemonclient.NewClient(startFakeDaemon(t, nil))

	err := Run(context.Background(), client, RunOptions{InitialPage: "bogus"})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
	assert.Contains(t, err.Error(), "unknown watch initial page")
}
