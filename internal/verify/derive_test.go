package verify

import "testing"

// intPtr returns a pointer to an int for use in tests.
func intPtr(i int) *int {
	return &i
}

func TestDeriveOK_Precedence(t *testing.T) {
	tests := []struct {
		name      string
		timedOut  bool
		cancelled bool
		exitCode  *int
		vj        *VerifyJSON
		want      bool
	}{
		// 1. Timeout/cancel always => ok=false
		{
			name:      "timedOut overrides everything",
			timedOut:  true,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true},
			want:      false,
		},
		{
			name:      "cancelled overrides everything",
			timedOut:  false,
			cancelled: true,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true},
			want:      false,
		},
		{
			name:      "timedOut with nil exit code",
			timedOut:  true,
			cancelled: false,
			exitCode:  nil,
			vj:        nil,
			want:      false,
		},
		// 2. exit_code nil => ok=false
		{
			name:      "nil exit code without timeout/cancel",
			timedOut:  false,
			cancelled: false,
			exitCode:  nil,
			vj:        nil,
			want:      false,
		},
		{
			name:      "nil exit code with verify.json ok=true",
			timedOut:  false,
			cancelled: false,
			exitCode:  nil,
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true},
			want:      false,
		},
		// 3. exit_code != 0 => ok=false
		{
			name:      "exit code 1",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(1),
			vj:        nil,
			want:      false,
		},
		{
			name:      "exit code 127",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(127),
			vj:        nil,
			want:      false,
		},
		{
			name:      "exit code non-zero ignores verify.json ok=true",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(1),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true},
			want:      false,
		},
		// 4. exit_code == 0 and verify.json valid => ok = verify.json.ok
		{
			name:      "exit 0 with verify.json ok=true",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true},
			want:      true,
		},
		{
			name:      "exit 0 with verify.json ok=false (downgrade)",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: false},
			want:      false,
		},
		// 5. exit_code == 0 and verify.json absent/invalid => ok=true
		{
			name:      "exit 0 without verify.json",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        nil,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveOK(tt.timedOut, tt.cancelled, tt.exitCode, tt.vj)
			if got != tt.want {
				t.Errorf("DeriveOK(%v, %v, %v, %v) = %v, want %v",
					tt.timedOut, tt.cancelled, tt.exitCode, tt.vj, got, tt.want)
			}
		})
	}
}

func TestDeriveSummary(t *testing.T) {
	tests := []struct {
		name      string
		timedOut  bool
		cancelled bool
		exitCode  *int
		vj        *VerifyJSON
		want      string
	}{
		// verify.json summary wins when provided
		{
			name:      "verify.json summary wins",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true, Summary: "all 42 tests passed"},
			want:      "all 42 tests passed",
		},
		{
			name:      "verify.json summary wins even on failure",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(1),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: false, Summary: "3 tests failed"},
			want:      "3 tests failed",
		},
		// Generic messages when no verify.json summary
		{
			name:      "timedOut",
			timedOut:  true,
			cancelled: false,
			exitCode:  nil,
			vj:        nil,
			want:      "verify timed out",
		},
		{
			name:      "cancelled",
			timedOut:  false,
			cancelled: true,
			exitCode:  nil,
			vj:        nil,
			want:      "verify cancelled",
		},
		{
			name:      "nil exit code",
			timedOut:  false,
			cancelled: false,
			exitCode:  nil,
			vj:        nil,
			want:      "verify failed (no exit code)",
		},
		{
			name:      "exit 0 success",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        nil,
			want:      "verify succeeded",
		},
		{
			name:      "exit 0 with verify.json no summary",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true},
			want:      "verify succeeded",
		},
		{
			name:      "exit 1",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(1),
			vj:        nil,
			want:      "verify failed (exit 1)",
		},
		{
			name:      "exit 127",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(127),
			vj:        nil,
			want:      "verify failed (exit 127)",
		},
		// Empty summary in verify.json falls back to generic
		{
			name:      "verify.json with empty summary",
			timedOut:  false,
			cancelled: false,
			exitCode:  intPtr(0),
			vj:        &VerifyJSON{SchemaVersion: "1.0", OK: true, Summary: ""},
			want:      "verify succeeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveSummary(tt.timedOut, tt.cancelled, tt.exitCode, tt.vj)
			if got != tt.want {
				t.Errorf("DeriveSummary(%v, %v, %v, %v) = %q, want %q",
					tt.timedOut, tt.cancelled, tt.exitCode, tt.vj, got, tt.want)
			}
		})
	}
}
