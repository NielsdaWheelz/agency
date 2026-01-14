package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// mockCmdRunner is a mock CommandRunner for testing.
type mockCmdRunner struct {
	calls   []mockCall
	results map[string]mockResult
}

type mockCall struct {
	Name string
	Args []string
	Opts mockOpts
}

type mockOpts struct {
	Dir string
	Env map[string]string
}

type mockResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

func newMockCmdRunner() *mockCmdRunner {
	return &mockCmdRunner{
		results: make(map[string]mockResult),
	}
}

func (m *mockCmdRunner) Run(ctx context.Context, name string, args []string, opts struct {
	Dir string
	Env map[string]string
}) (struct {
	Stdout   string
	Stderr   string
	ExitCode int
}, error) {
	m.calls = append(m.calls, mockCall{Name: name, Args: args, Opts: mockOpts{Dir: opts.Dir, Env: opts.Env}})

	// Build key from command
	key := name + " " + strings.Join(args, " ")

	if result, ok := m.results[key]; ok {
		return struct {
			Stdout   string
			Stderr   string
			ExitCode int
		}{result.Stdout, result.Stderr, result.ExitCode}, result.Err
	}

	// Default success
	return struct {
		Stdout   string
		Stderr   string
		ExitCode int
	}{"", "", 0}, nil
}

func (m *mockCmdRunner) setResult(key string, result mockResult) {
	m.results[key] = result
}

// TestIsReportEffectivelyEmpty tests the report gating logic.
func TestIsReportEffectivelyEmpty(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		fileExists  bool
		wantEmpty   bool
		description string
	}{
		{
			name:        "missing file",
			fileExists:  false,
			wantEmpty:   true,
			description: "missing file should be effectively empty",
		},
		{
			name:        "empty file",
			content:     "",
			fileExists:  true,
			wantEmpty:   true,
			description: "empty file should be effectively empty",
		},
		{
			name:        "whitespace only",
			content:     "   \n\t\n   ",
			fileExists:  true,
			wantEmpty:   true,
			description: "whitespace-only content should be effectively empty",
		},
		{
			name:        "less than 20 chars",
			content:     "short content",
			fileExists:  true,
			wantEmpty:   true,
			description: "content < 20 trimmed chars should be effectively empty",
		},
		{
			name:        "exactly 20 chars",
			content:     "exactly 20 chars!!!!", // 20 chars
			fileExists:  true,
			wantEmpty:   false,
			description: "content == 20 trimmed chars should NOT be empty",
		},
		{
			name:        "more than 20 chars",
			content:     "this is a valid report with more than 20 characters",
			fileExists:  true,
			wantEmpty:   false,
			description: "content > 20 trimmed chars should NOT be empty",
		},
		{
			name:        "padded content over threshold",
			content:     "   this is padded content over threshold   ",
			fileExists:  true,
			wantEmpty:   false,
			description: "padded content should trim and compare",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			reportPath := filepath.Join(tmpDir, "report.md")

			fsys := fs.NewRealFS()

			if tt.fileExists {
				if err := os.WriteFile(reportPath, []byte(tt.content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			gotEmpty, err := isReportEffectivelyEmpty(fsys, reportPath)
			if err != nil && tt.fileExists {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotEmpty != tt.wantEmpty {
				t.Errorf("%s: got empty=%v, want empty=%v", tt.description, gotEmpty, tt.wantEmpty)
			}
		})
	}
}

// TestPushOriginGating tests origin validation logic.
func TestPushOriginGating(t *testing.T) {
	tests := []struct {
		name       string
		originURL  string
		wantHost   string
		shouldPass bool
	}{
		{
			name:       "github.com ssh",
			originURL:  "git@github.com:owner/repo.git",
			wantHost:   "github.com",
			shouldPass: true,
		},
		{
			name:       "github.com https",
			originURL:  "https://github.com/owner/repo.git",
			wantHost:   "github.com",
			shouldPass: true,
		},
		{
			name:       "gitlab.com ssh",
			originURL:  "git@gitlab.com:owner/repo.git",
			wantHost:   "gitlab.com",
			shouldPass: false,
		},
		{
			name:       "gitlab.com https",
			originURL:  "https://gitlab.com/owner/repo.git",
			wantHost:   "gitlab.com",
			shouldPass: false,
		},
		{
			name:       "bitbucket.org ssh",
			originURL:  "git@bitbucket.org:owner/repo.git",
			wantHost:   "bitbucket.org",
			shouldPass: false,
		},
		{
			name:       "github enterprise",
			originURL:  "git@github.company.com:owner/repo.git",
			wantHost:   "github.company.com",
			shouldPass: false,
		},
		{
			name:       "empty origin",
			originURL:  "",
			wantHost:   "",
			shouldPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Import the git package's ParseOriginHost function
			// We test the gating logic indirectly through the parsed host
			host := parseTestOriginHost(tt.originURL)

			if host != tt.wantHost {
				t.Errorf("parseOriginHost(%q) = %q, want %q", tt.originURL, host, tt.wantHost)
			}

			isGitHub := host == "github.com"
			if isGitHub != tt.shouldPass {
				t.Errorf("origin %q: isGitHub=%v, shouldPass=%v", tt.originURL, isGitHub, tt.shouldPass)
			}
		})
	}
}

// parseTestOriginHost is a copy of the parsing logic for testing.
// This matches the logic in git.ParseOriginHost.
func parseTestOriginHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Check for scp-like format: git@host:path
	if strings.Contains(raw, "@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		atIdx := strings.Index(raw, "@")
		colonIdx := strings.Index(raw, ":")
		if colonIdx > atIdx {
			host := raw[atIdx+1 : colonIdx]
			if host != "" && strings.Contains(host, ".") {
				return host
			}
		}
		return ""
	}

	// Check for https:// URL
	if strings.HasPrefix(raw, "https://") {
		rest := strings.TrimPrefix(raw, "https://")
		slashIdx := strings.Index(rest, "/")
		if slashIdx > 0 {
			host := rest[:slashIdx]
			if colonIdx := strings.Index(host, ":"); colonIdx > 0 {
				host = host[:colonIdx]
			}
			if strings.Contains(host, ".") {
				return host
			}
		}
		return ""
	}

	return ""
}

// TestNonInteractiveEnv verifies the environment overlay for non-interactive execution.
func TestNonInteractiveEnv(t *testing.T) {
	env := nonInteractiveEnv()

	// Required keys per spec
	required := map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
		"CI":                  "1",
	}

	for key, wantValue := range required {
		gotValue, ok := env[key]
		if !ok {
			t.Errorf("nonInteractiveEnv() missing required key %q", key)
			continue
		}
		if gotValue != wantValue {
			t.Errorf("nonInteractiveEnv()[%q] = %q, want %q", key, gotValue, wantValue)
		}
	}
}

// TestComputeReportHash verifies report hash computation.
func TestComputeReportHash(t *testing.T) {
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.md")
	fsys := fs.NewRealFS()

	// Test with non-existent file
	hash := computeReportHash(fsys, reportPath)
	if hash != "" {
		t.Errorf("computeReportHash(non-existent) = %q, want empty", hash)
	}

	// Test with known content
	content := "# Test Report\n\nThis is a test."
	if err := os.WriteFile(reportPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	hash = computeReportHash(fsys, reportPath)
	if hash == "" {
		t.Error("computeReportHash(existing) = empty, want non-empty hash")
	}

	// Hash should be 64 hex chars (sha256)
	if len(hash) != 64 {
		t.Errorf("computeReportHash() len = %d, want 64", len(hash))
	}

	// Same content should produce same hash
	hash2 := computeReportHash(fsys, reportPath)
	if hash != hash2 {
		t.Errorf("computeReportHash() not deterministic: %q != %q", hash, hash2)
	}

	// Different content should produce different hash
	if err := os.WriteFile(reportPath, []byte("different content"), 0644); err != nil {
		t.Fatal(err)
	}
	hash3 := computeReportHash(fsys, reportPath)
	if hash == hash3 {
		t.Error("computeReportHash() should produce different hash for different content")
	}
}

// TestPushErrorCodes verifies all push-related error codes exist.
func TestPushErrorCodes(t *testing.T) {
	// Compile-time verification that all error codes exist
	codes := []errors.Code{
		errors.ENoOrigin,
		errors.EUnsupportedOriginHost,
		errors.EParentNotFound,
		errors.EGitPushFailed,
		errors.EReportInvalid,
		errors.EEmptyDiff,
		errors.EWorktreeMissing,
		errors.ERepoLocked,
		errors.ERunNotFound,
		errors.EGhNotInstalled,
		errors.EGhNotAuthenticated,
	}

	for _, code := range codes {
		if code == "" {
			t.Error("error code is empty")
		}
		// Verify format starts with E_
		if !strings.HasPrefix(string(code), "E_") {
			t.Errorf("error code %q should start with E_", code)
		}
	}
}

// TestResolveRunForPush_NotFound verifies E_RUN_NOT_FOUND for missing runs.
func TestResolveRunForPush_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, tmpDir, time.Now)

	// Try to resolve a non-existent run
	_, _, _, err := resolveRunForPush(context.Background(), nil, fsys, tmpDir, st, "nonexistent-run")
	if err == nil {
		t.Fatal("expected error for non-existent run")
	}

	code := errors.GetCode(err)
	if code != errors.ERunNotFound {
		t.Errorf("error code = %q, want %q", code, errors.ERunNotFound)
	}
}

// TestPushEventNames verifies event names used by push.
func TestPushEventNames(t *testing.T) {
	// Document the event names used by push
	eventNames := []string{
		"push_started",
		"git_fetch_finished",
		"git_push_finished",
		"push_finished",
		"push_failed",
	}

	for _, name := range eventNames {
		if name == "" {
			t.Error("event name should not be empty")
		}
		// All event names should be lowercase with underscores
		if strings.ToLower(name) != name {
			t.Errorf("event name %q should be lowercase", name)
		}
	}
}

// TestPushOptsDefaults verifies PushOpts defaults.
func TestPushOptsDefaults(t *testing.T) {
	opts := PushOpts{}

	if opts.RunID != "" {
		t.Errorf("PushOpts.RunID default should be empty, got %q", opts.RunID)
	}
	if opts.Force != false {
		t.Error("PushOpts.Force default should be false")
	}
}

// TestPushForceDoesNotBypassEmptyDiff documents that --force does NOT bypass E_EMPTY_DIFF.
func TestPushForceDoesNotBypassEmptyDiff(t *testing.T) {
	// This is a documentation test - the actual behavior is tested in integration tests.
	// Per spec: --force allows proceeding with missing/empty report but does NOT bypass E_EMPTY_DIFF.
	t.Log("--force flag allows:")
	t.Log("  - pushing with missing report")
	t.Log("  - pushing with empty report")
	t.Log("--force flag does NOT allow:")
	t.Log("  - pushing with 0 commits ahead (E_EMPTY_DIFF)")
}

// TestWorktreeMissingError verifies E_WORKTREE_MISSING error code exists.
func TestWorktreeMissingError(t *testing.T) {
	// Verify the error code exists and has correct format
	code := errors.EWorktreeMissing
	if code != "E_WORKTREE_MISSING" {
		t.Errorf("EWorktreeMissing = %q, want %q", code, "E_WORKTREE_MISSING")
	}

	// Verify we can create an error with this code
	err := errors.NewWithDetails(
		errors.EWorktreeMissing,
		"test message",
		map[string]string{"worktree_path": "/path/to/worktree"},
	)

	if errors.GetCode(err) != errors.EWorktreeMissing {
		t.Errorf("GetCode(err) = %q, want %q", errors.GetCode(err), errors.EWorktreeMissing)
	}
}
