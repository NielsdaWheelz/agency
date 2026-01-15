package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run([]string{}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error for no args")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
	if !strings.Contains(stdout.String(), "usage:") {
		t.Error("expected usage in stdout")
	}
}

func TestRun_Help(t *testing.T) {
	tests := []string{"-h", "--help"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "usage:") {
				t.Error("expected usage in stdout")
			}
		})
	}
}

func TestRun_Version(t *testing.T) {
	tests := []string{"-v", "--version"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "agency") {
				t.Error("expected 'agency' in version output")
			}
		})
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run([]string{"nope"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Error("expected unknown command name in error")
	}
	if !strings.Contains(stdout.String(), "usage:") {
		t.Error("expected usage in stdout")
	}
}

func TestRun_InitHelp(t *testing.T) {
	tests := []string{"-h", "--help"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{"init", arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "agency init") {
				t.Error("expected init usage in stdout")
			}
		})
	}
}

func TestRun_DoctorHelp(t *testing.T) {
	tests := []string{"-h", "--help"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{"doctor", arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "agency doctor") {
				t.Error("expected doctor usage in stdout")
			}
		})
	}
}

// TestRun_InitNotInRepo tests that init fails when not in a git repo.
// Note: The actual init implementation is tested in internal/commands/init_test.go.
// This test verifies the CLI wiring works.
func TestRun_InitNotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(originalWd)

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run([]string{"init"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

// TestRun_DoctorNotInRepo tests that doctor fails when not in a git repo.
// Note: The actual doctor implementation is tested in internal/commands/doctor_test.go.
// This test verifies the CLI wiring works.
func TestRun_DoctorNotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(originalWd)

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run([]string{"doctor"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

func TestRun_RunHelp(t *testing.T) {
	tests := []string{"-h", "--help"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{"run", arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "agency run") {
				t.Error("expected run usage in stdout")
			}
			// Verify flags are documented
			if !strings.Contains(stdout.String(), "--title") {
				t.Error("expected --title in run usage")
			}
			if !strings.Contains(stdout.String(), "--runner") {
				t.Error("expected --runner in run usage")
			}
			if !strings.Contains(stdout.String(), "--parent") {
				t.Error("expected --parent in run usage")
			}
			if !strings.Contains(stdout.String(), "--attach") {
				t.Error("expected --attach in run usage")
			}
		})
	}
}

// TestRun_RunNotInRepo tests that run fails when not in a git repo.
// This test verifies the CLI wiring works.
func TestRun_RunNotInRepo(t *testing.T) {
	// Save and restore cwd
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(originalWd)

	// Change to temp dir that is NOT a git repo
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run([]string{"run"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error when not in git repo")
	}
	// The run command goes through the pipeline which will fail at CheckRepoSafe
	// with E_NO_REPO
	if errors.GetCode(err) != errors.ENoRepo {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.ENoRepo)
	}
}

func TestRun_AttachHelp(t *testing.T) {
	tests := []string{"-h", "--help"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{"attach", arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "agency attach") {
				t.Error("expected attach usage in stdout")
			}
		})
	}
}

func TestRun_AttachMissingRunID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run([]string{"attach"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error when run_id is missing")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

func TestRun_VerifyHelp(t *testing.T) {
	tests := []string{"-h", "--help"}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run([]string{"verify", arg}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), "agency verify") {
				t.Error("expected verify usage in stdout")
			}
			// Verify flags are documented
			if !strings.Contains(stdout.String(), "--timeout") {
				t.Error("expected --timeout in verify usage")
			}
		})
	}
}

func TestRun_VerifyMissingRunID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run([]string{"verify"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error when run_id is missing")
	}
	if errors.GetCode(err) != errors.EUsage {
		t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
	}
}

func TestRun_VerifyInvalidTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
	}{
		{"invalid format", "not-a-duration"},
		{"zero", "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			// Put flags before positional argument (Go flag package stops at first non-flag)
			err := Run([]string{"verify", "--timeout", tt.timeout, "some-run-id"}, &stdout, &stderr)

			if err == nil {
				t.Fatal("expected error for invalid timeout")
			}
			if errors.GetCode(err) != errors.EUsage {
				t.Errorf("code = %q, want %q", errors.GetCode(err), errors.EUsage)
			}
		})
	}
}

// Note: Negative timeout like "-30m" can't be easily tested because it looks like a flag to the parser.
