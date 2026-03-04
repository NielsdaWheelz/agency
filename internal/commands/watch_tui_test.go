package commands

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func TestWatch_NotInteractive_ReturnsENotInteractive(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Watch(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), "", WatchOpts{
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "interactive terminal")
}
