package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRun_NilLoader_ReturnsEInternal(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), nil, RunOptions{})
	require.Error(t, err)
	assert.Equal(t, errors.EInternal, errors.GetCode(err))
}
