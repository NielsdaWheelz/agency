package tmux

import "testing"

func TestSessionName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		invocationID string
		want         string
	}{
		{"abc123", "agency_abc123"},
		{"20260110-a3f2", "agency_20260110-a3f2"},
		{"test", "agency_test"},
	}

	for _, tt := range tests {
		t.Run(tt.invocationID, func(t *testing.T) {
			t.Parallel()
			got := SessionName(tt.invocationID)
			if got != tt.want {
				t.Fatalf("SessionName(%q) = %q, want %q", tt.invocationID, got, tt.want)
			}
		})
	}
}
