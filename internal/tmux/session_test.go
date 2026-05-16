package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		runID string
		want  string
	}{
		{"abc123", "agency_abc123:0.0"},
		{"20260110-a3f2", "agency_20260110-a3f2:0.0"},
		{"test", "agency_test:0.0"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.runID, func(t *testing.T) {
			t.Parallel()
			got := SessionTarget(tt.runID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSessionName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		runID string
		want  string
	}{
		{"abc123", "agency_abc123"},
		{"20260110-a3f2", "agency_20260110-a3f2"},
		{"test", "agency_test"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.runID, func(t *testing.T) {
			t.Parallel()
			got := SessionName(tt.runID)
			assert.Equal(t, tt.want, got)
		})
	}
}
