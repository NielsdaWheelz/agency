package cobra

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestWatchCmd_InvalidIntervalReturnsInvalidArgument(t *testing.T) {
	_, _, err := executeCmd("watch", "--interval", "soon")
	require.Error(t, err, "expected invalid interval to be rejected")
	if got := errors.GetCode(err); got != errors.EInvalidArgument {
		t.Fatalf("error code = %s, want %s", got, errors.EInvalidArgument)
	}
}

func TestWatchCmd_OutOfRangeIntervalReturnsInvalidArgument(t *testing.T) {
	_, _, err := executeCmd("watch", "--interval", "10s")
	require.Error(t, err, "expected out-of-range interval to be rejected")
	if got := errors.GetCode(err); got != errors.EInvalidArgument {
		t.Fatalf("error code = %s, want %s", got, errors.EInvalidArgument)
	}
	if !strings.Contains(err.Error(), "250ms and 5s") {
		t.Fatalf("error = %q, want interval bounds", err.Error())
	}
}
