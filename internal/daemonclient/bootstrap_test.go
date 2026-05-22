package daemonclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestStartDaemonBackgroundRefusesGoTestBinary(t *testing.T) {
	exePath, err := os.Executable()
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(filepath.Base(exePath), ".test"), "test requires the standard Go test binary name")

	logPath := filepath.Join(t.TempDir(), "daemon.log")

	err = StartDaemonBackground(context.Background(), logPath)
	require.Error(t, err)
	if got := errors.GetCode(err); got != errors.EDaemonStartFailed {
		t.Fatalf("error code = %s, want %s", got, errors.EDaemonStartFailed)
	}
	if !strings.Contains(err.Error(), "refusing to autostart daemon from Go test binary") {
		t.Fatalf("error = %q, want Go test binary refusal", err.Error())
	}

	require.NoFileExists(t, logPath)
}
