package daemonclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartDaemonBackgroundRefusesGoTestBinary(t *testing.T) {
	exePath, err := os.Executable()
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(filepath.Base(exePath), ".test"), "test requires the standard Go test binary name")

	logPath := filepath.Join(t.TempDir(), "daemon.log")

	err = StartDaemonBackground(logPath)
	require.Error(t, err)
	assert.Equal(t, errors.EDaemonStartFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "refusing to autostart daemon from Go test binary")

	_, statErr := os.Stat(logPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
