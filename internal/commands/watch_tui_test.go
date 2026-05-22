package commands

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestWatch_NotInteractiveReturnsENotInteractive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Watch(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", WatchOpts{
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)

	require.Error(t, err, "expected watch to require an interactive terminal")
	if got := errors.GetCode(err); got != errors.ENotInteractive {
		t.Fatalf("error code = %s, want %s", got, errors.ENotInteractive)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}
