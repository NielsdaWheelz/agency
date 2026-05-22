package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunRejectsNonPositiveTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{name: "zero", timeout: 0},
		{name: "negative", timeout: -time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			logPath := filepath.Join(tmpDir, "verify.log")

			record, err := Run(context.Background(), RunConfig{
				RepoID:  "repo-1",
				RunID:   "run-1",
				WorkDir: tmpDir,
				Script:  filepath.Join(tmpDir, "verify.sh"),
				Timeout: tt.timeout,
				LogPath: logPath,
			})

			require.Error(t, err)
			if !strings.Contains(err.Error(), "verify timeout must be positive") {
				t.Fatalf("error = %q, want timeout message", err.Error())
			}
			require.NotNil(t, record.Error)
			if *record.Error != "verify timeout must be positive" {
				t.Fatalf("record error = %q, want %q", *record.Error, "verify timeout must be positive")
			}
			if record.TimeoutMS != tt.timeout.Milliseconds() {
				t.Fatalf("timeout_ms = %d, want %d", record.TimeoutMS, tt.timeout.Milliseconds())
			}
			if _, statErr := os.Stat(logPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("log file should not exist, stat error = %v", statErr)
			}
		})
	}
}
