package commands

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func awaitConfirmationLineBeforeEOF(t *testing.T, input string, run func(io.Reader) error) error {
	t.Helper()

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pw.Close()
		_ = pr.Close()
	})

	done := make(chan error, 1)
	go func() {
		done <- run(pr)
	}()

	_, err := pw.Write([]byte(input))
	require.NoError(t, err)

	select {
	case err := <-done:
		return err
	case <-time.After(750 * time.Millisecond):
		t.Fatal("confirmation prompt waited for EOF instead of returning after one line")
		return nil
	}
}
