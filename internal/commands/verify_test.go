package commands

import (
	"testing"

	"github.com/NielsdaWheelz/agency/internal/store"
)

func TestDeriveFailureReason(t *testing.T) {
	tests := []struct {
		name   string
		record *store.VerifyRecord
		want   string
	}{
		{
			name: "timed out",
			record: &store.VerifyRecord{
				TimedOut: true,
			},
			want: "timed out",
		},
		{
			name: "cancelled",
			record: &store.VerifyRecord{
				Cancelled: true,
			},
			want: "cancelled",
		},
		{
			name: "exec failed (error set, no exit code)",
			record: &store.VerifyRecord{
				Error:    strPtr("failed to start verify script"),
				ExitCode: nil,
			},
			want: "exec failed",
		},
		{
			name: "exit code 1",
			record: &store.VerifyRecord{
				ExitCode: intPtr(1),
			},
			want: "exit 1",
		},
		{
			name: "exit code 127",
			record: &store.VerifyRecord{
				ExitCode: intPtr(127),
			},
			want: "exit 127",
		},
		{
			name: "verify.json ok=false (exit 0)",
			record: &store.VerifyRecord{
				ExitCode: intPtr(0),
			},
			want: "verify.json ok=false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveFailureReason(tt.record)
			if got != tt.want {
				t.Errorf("deriveFailureReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeRecordPath(t *testing.T) {
	tests := []struct {
		name    string
		record  *store.VerifyRecord
		wantSfx string // suffix we expect the path to end with
	}{
		{
			name: "derives from log path",
			record: &store.VerifyRecord{
				LogPath: "/data/repos/abc123/runs/20260110-a3f2/logs/verify.log",
			},
			wantSfx: "/data/repos/abc123/runs/20260110-a3f2/verify_record.json",
		},
		{
			name: "empty log path",
			record: &store.VerifyRecord{
				LogPath: "",
			},
			wantSfx: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeRecordPath(tt.record)
			if got != tt.wantSfx {
				t.Errorf("computeRecordPath() = %q, want %q", got, tt.wantSfx)
			}
		})
	}
}

// helpers
func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }
