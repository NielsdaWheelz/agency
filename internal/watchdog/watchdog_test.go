package watchdog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckStall(t *testing.T) {
	t.Parallel()

	threshold := 15 * time.Minute

	tests := []struct {
		name          string
		signals       ActivitySignals
		wantIsStalled bool
	}{
		{
			name: "no tmux session",
			signals: ActivitySignals{
				TmuxSessionExists: false,
				StatusFileModTime: ptr(time.Now().Add(-1 * time.Hour)),
			},
			wantIsStalled: false,
		},
		{
			name: "no status file",
			signals: ActivitySignals{
				TmuxSessionExists: true,
				StatusFileModTime: nil,
			},
			wantIsStalled: false,
		},
		{
			name: "recent status file",
			signals: ActivitySignals{
				TmuxSessionExists: true,
				StatusFileModTime: ptr(time.Now().Add(-5 * time.Minute)),
			},
			wantIsStalled: false,
		},
		{
			name: "exactly at threshold",
			signals: ActivitySignals{
				TmuxSessionExists: true,
				StatusFileModTime: ptr(time.Now().Add(-15 * time.Minute)),
			},
			wantIsStalled: true,
		},
		{
			name: "beyond threshold",
			signals: ActivitySignals{
				TmuxSessionExists: true,
				StatusFileModTime: ptr(time.Now().Add(-30 * time.Minute)),
			},
			wantIsStalled: true,
		},
		{
			name: "just before threshold",
			signals: ActivitySignals{
				TmuxSessionExists: true,
				StatusFileModTime: ptr(time.Now().Add(-14*time.Minute - 59*time.Second)),
			},
			wantIsStalled: false,
		},
		{
			name: "no tmux with old status file",
			signals: ActivitySignals{
				TmuxSessionExists: false,
				StatusFileModTime: ptr(time.Now().Add(-1 * time.Hour)),
			},
			wantIsStalled: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := CheckStall(tt.signals, threshold)
			assert.Equal(t, tt.wantIsStalled, result.IsStalled)
		})
	}
}

func TestCheckStall_StalledDuration(t *testing.T) {
	t.Parallel()

	threshold := 15 * time.Minute
	stalledTime := time.Now().Add(-30 * time.Minute)

	signals := ActivitySignals{
		TmuxSessionExists: true,
		StatusFileModTime: &stalledTime,
	}

	result := CheckStall(signals, threshold)
	require.True(t, result.IsStalled, "expected IsStalled = true")

	// Allow some tolerance for test execution time
	assert.True(t, result.StalledDuration >= 29*time.Minute && result.StalledDuration <= 31*time.Minute,
		"StalledDuration = %v, want ~30m", result.StalledDuration)
}

func TestCheckStallWithDefault(t *testing.T) {
	t.Parallel()

	// Just verify it uses the default threshold
	signals := ActivitySignals{
		TmuxSessionExists: true,
		StatusFileModTime: ptr(time.Now().Add(-20 * time.Minute)),
	}

	result := CheckStallWithDefault(signals)
	assert.True(t, result.IsStalled, "expected IsStalled = true with 20m old status file")

	// Recent file should not be stalled
	signals.StatusFileModTime = ptr(time.Now().Add(-5 * time.Minute))
	result = CheckStallWithDefault(signals)
	assert.False(t, result.IsStalled, "expected IsStalled = false with 5m old status file")
}

func TestDefaultStallThreshold(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 15*time.Minute, DefaultStallThreshold)
}

// ptr is a helper to create a pointer to a time.Time value.
func ptr(t time.Time) *time.Time {
	return &t
}
