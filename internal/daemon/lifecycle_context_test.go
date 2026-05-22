package daemon

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDaemonOwnedContextIgnoresCallerCancellationAndPreservesValues(t *testing.T) {
	t.Parallel()

	type contextKey string
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey("marker"), "value"))
	cancel()

	owned := daemonOwnedContext(ctx)

	select {
	case <-owned.Done():
		t.Fatal("daemon-owned context must not inherit caller cancellation")
	default:
	}
	require.NoError(t, owned.Err())
	if got := owned.Value(contextKey("marker")); got != "value" {
		t.Fatalf("context marker = %v, want %q", got, "value")
	}
}
