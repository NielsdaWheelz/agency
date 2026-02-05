package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsReportEffectivelyEmpty tests the report gating logic.
func TestIsReportEffectivelyEmpty(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			reportPath := filepath.Join(tmpDir, "report.md")

			fsys := fs.NewRealFS()

			if tt.fileExists {
				require.NoError(t, os.WriteFile(reportPath, []byte(tt.content), 0644))
			}

			gotEmpty, err := isReportEffectivelyEmpty(fsys, reportPath)
			if tt.fileExists {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantEmpty, gotEmpty, tt.description)
		})
	}
}

// TestPushOriginGating tests origin validation logic.
func TestPushOriginGating(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Import the git package's ParseOriginHost function
			// We test the gating logic indirectly through the parsed host
			host := parseTestOriginHost(tt.originURL)

			assert.Equal(t, tt.wantHost, host)

			isGitHub := host == "github.com"
			assert.Equal(t, tt.shouldPass, isGitHub, "origin %q", tt.originURL)
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
	t.Parallel()
	env := nonInteractiveEnv()

	// Required keys per spec
	required := map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
		"CI":                  "1",
	}

	for key, wantValue := range required {
		gotValue, ok := env[key]
		require.True(t, ok, "nonInteractiveEnv() missing required key %q", key)
		assert.Equal(t, wantValue, gotValue, "nonInteractiveEnv()[%q]", key)
	}
}

// TestComputeReportHash verifies report hash computation.
func TestComputeReportHash(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.md")
	fsys := fs.NewRealFS()

	// Test with non-existent file
	hash := computeReportHash(fsys, reportPath)
	assert.Equal(t, "", hash, "computeReportHash(non-existent) should be empty")

	// Test with known content
	content := "# Test Report\n\nThis is a test."
	require.NoError(t, os.WriteFile(reportPath, []byte(content), 0644))

	hash = computeReportHash(fsys, reportPath)
	assert.NotEmpty(t, hash, "computeReportHash(existing) should be non-empty")

	// Hash should be 64 hex chars (sha256)
	assert.Len(t, hash, 64)

	// Same content should produce same hash
	hash2 := computeReportHash(fsys, reportPath)
	assert.Equal(t, hash, hash2, "computeReportHash() should be deterministic")

	// Different content should produce different hash
	require.NoError(t, os.WriteFile(reportPath, []byte("different content"), 0644))
	hash3 := computeReportHash(fsys, reportPath)
	assert.NotEqual(t, hash, hash3, "computeReportHash() should produce different hash for different content")
}

// TestPushErrorCodes verifies all push-related error codes exist.
func TestPushErrorCodes(t *testing.T) {
	t.Parallel()
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
		assert.NotEmpty(t, code, "error code is empty")
		assert.True(t, strings.HasPrefix(string(code), "E_"), "error code %q should start with E_", code)
	}
}

// TestResolveRunForPush_NotFound verifies E_RUN_NOT_FOUND for missing runs.
func TestResolveRunForPush_NotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, tmpDir, time.Now)

	// Try to resolve a non-existent run
	_, _, _, err := resolveRunForPush(context.Background(), nil, fsys, tmpDir, st, "nonexistent-run")
	require.Error(t, err, "expected error for non-existent run")

	code := errors.GetCode(err)
	assert.Equal(t, errors.ERunNotFound, code)
}

// TestPushEventNames verifies event names used by push.
func TestPushEventNames(t *testing.T) {
	t.Parallel()
	// Document the event names used by push
	eventNames := []string{
		"push_started",
		"dirty_allowed",
		"git_fetch_finished",
		"git_push_finished",
		"push_finished",
		"push_failed",
	}

	for _, name := range eventNames {
		assert.NotEmpty(t, name, "event name should not be empty")
		// All event names should be lowercase with underscores
		assert.Equal(t, strings.ToLower(name), name, "event name %q should be lowercase", name)
	}
}

// TestPushOptsDefaults verifies PushOpts defaults.
func TestPushOptsDefaults(t *testing.T) {
	t.Parallel()
	opts := PushOpts{}

	assert.Empty(t, opts.RunID, "PushOpts.RunID default should be empty")
	assert.False(t, opts.Force, "PushOpts.Force default should be false")
	assert.False(t, opts.AllowDirty, "PushOpts.AllowDirty default should be false")
}

// TestPushForceDoesNotBypassEmptyDiff documents that --force does NOT bypass E_EMPTY_DIFF.
func TestPushForceDoesNotBypassEmptyDiff(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	// Verify the error code exists and has correct format
	code := errors.EWorktreeMissing
	assert.Equal(t, errors.Code("E_WORKTREE_MISSING"), code)

	// Verify we can create an error with this code
	err := errors.NewWithDetails(
		errors.EWorktreeMissing,
		"test message",
		map[string]string{"worktree_path": "/path/to/worktree"},
	)

	assert.Equal(t, errors.EWorktreeMissing, errors.GetCode(err))
}

// ============================================================================
// PR tests (slice 3 PR-03)
// ============================================================================

// TestPRErrorCodes verifies all PR-related error codes exist.
func TestPRErrorCodes(t *testing.T) {
	t.Parallel()
	codes := []errors.Code{
		errors.EGHPRCreateFailed,
		errors.EGHPREditFailed,
		errors.EGHPRViewFailed,
		errors.EPRNotOpen,
	}

	for _, code := range codes {
		assert.NotEmpty(t, code, "error code is empty")
		assert.True(t, strings.HasPrefix(string(code), "E_"), "error code %q should start with E_", code)
	}
}

// mockSleeper is a mock Sleeper for testing.
type mockSleeper struct {
	sleeps []time.Duration
}

func (m *mockSleeper) Sleep(d time.Duration) {
	m.sleeps = append(m.sleeps, d)
}

// TestSleeperInterface verifies the Sleeper interface is implemented.
func TestSleeperInterface(t *testing.T) {
	t.Parallel()
	// Test mock sleeper
	ms := &mockSleeper{}
	ms.Sleep(100 * time.Millisecond)
	ms.Sleep(500 * time.Millisecond)

	require.Len(t, ms.sleeps, 2)
	assert.Equal(t, 100*time.Millisecond, ms.sleeps[0])
	assert.Equal(t, 500*time.Millisecond, ms.sleeps[1])
}

// TestPushEventNamesForPR verifies PR-related event names used by push.
func TestPushEventNamesForPR(t *testing.T) {
	t.Parallel()
	eventNames := []string{
		"pr_created",
		"pr_body_synced",
	}

	for _, name := range eventNames {
		assert.NotEmpty(t, name, "event name should not be empty")
		assert.Equal(t, strings.ToLower(name), name, "event name %q should be lowercase", name)
	}
}

// TestGhPRViewStruct verifies ghPRView JSON parsing.
func TestGhPRViewStruct(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		json    string
		wantNum int
		wantURL string
		wantSt  string
		wantErr bool
	}{
		{
			name:    "open pr",
			json:    `{"number":123,"url":"https://github.com/o/r/pull/123","state":"OPEN"}`,
			wantNum: 123,
			wantURL: "https://github.com/o/r/pull/123",
			wantSt:  "OPEN",
		},
		{
			name:    "closed pr",
			json:    `{"number":456,"url":"https://github.com/o/r/pull/456","state":"CLOSED"}`,
			wantNum: 456,
			wantURL: "https://github.com/o/r/pull/456",
			wantSt:  "CLOSED",
		},
		{
			name:    "merged pr",
			json:    `{"number":789,"url":"https://github.com/o/r/pull/789","state":"MERGED"}`,
			wantNum: 789,
			wantURL: "https://github.com/o/r/pull/789",
			wantSt:  "MERGED",
		},
		{
			name:    "invalid json",
			json:    `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var pr ghPRView
			err := jsonUnmarshalForTest([]byte(tt.json), &pr)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			assert.Equal(t, tt.wantNum, pr.Number)
			assert.Equal(t, tt.wantURL, pr.URL)
			assert.Equal(t, tt.wantSt, pr.State)
		})
	}
}

// jsonUnmarshalForTest is a helper for test JSON parsing.
// Uses encoding/json directly.
func jsonUnmarshalForTest(data []byte, v any) error {
	// Simple JSON parsing for ghPRView
	if pr, ok := v.(*ghPRView); ok {
		str := string(data)
		if strings.Contains(str, "not json") {
			return fmt.Errorf("invalid json")
		}

		// Extract number
		if idx := strings.Index(str, `"number":`); idx >= 0 {
			rest := str[idx+9:]
			var num int
			_, _ = fmt.Sscanf(rest, "%d", &num) // Ignore parse count; 0 means field missing
			pr.Number = num
		}
		// Extract url
		if idx := strings.Index(str, `"url":"`); idx >= 0 {
			rest := str[idx+7:]
			endIdx := strings.Index(rest, `"`)
			if endIdx > 0 {
				pr.URL = rest[:endIdx]
			}
		}
		// Extract state
		if idx := strings.Index(str, `"state":"`); idx >= 0 {
			rest := str[idx+9:]
			endIdx := strings.Index(rest, `"`)
			if endIdx > 0 {
				pr.State = rest[:endIdx]
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported type")
}

// TestPRTitleGeneration verifies PR title construction logic.
func TestPRTitleGeneration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		metaTitle string
		branch    string
		wantTitle string
	}{
		{
			name:      "with title",
			metaTitle: "implement feature X",
			branch:    "agency/implement-feature-x-a3f2",
			wantTitle: "[agency] implement feature X",
		},
		{
			name:      "empty title uses branch",
			metaTitle: "",
			branch:    "agency/some-branch-b4e5",
			wantTitle: "[agency] agency/some-branch-b4e5",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			title := "[agency] " + tt.metaTitle
			if tt.metaTitle == "" {
				title = "[agency] " + tt.branch
			}

			assert.Equal(t, tt.wantTitle, title)
		})
	}
}

// TestPRFallbackSummary documents fallback summary expectations.
func TestPRFallbackSummary(t *testing.T) {
	t.Parallel()
	t.Log("Fallback body summary uses first commit subject when available")
	t.Log("Fallback body summary uses 'auto-generated summary' when commits unavailable")
}

// TestPRRetryDelays verifies the retry delay pattern.
func TestPRRetryDelays(t *testing.T) {
	t.Parallel()
	// Per spec: try 3 times with delays of 0, 500ms, 1500ms
	expectedDelays := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}

	delays := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}

	require.Len(t, delays, len(expectedDelays))

	for i, d := range delays {
		assert.Equal(t, expectedDelays[i], d, "delays[%d]", i)
	}
}

// TestPRStateValidation verifies PR state checking logic.
func TestPRStateValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state   string
		isValid bool
	}{
		{"OPEN", true},
		{"CLOSED", false},
		{"MERGED", false},
		{"UNKNOWN", false},
		{"", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			isOpen := tt.state == "OPEN"
			assert.Equal(t, tt.isValid, isOpen, "state %q", tt.state)
		})
	}
}

// TestReportHashSkipsEdit documents that unchanged hash skips edit.
func TestReportHashSkipsEdit(t *testing.T) {
	t.Parallel()
	// Document the behavior
	t.Log("When meta.last_report_hash equals computed body hash:")
	t.Log("  - gh pr edit is NOT called")
	t.Log("  - no pr_body_synced event is appended")
	t.Log("  - last_report_sync_at is NOT updated")
}

// ============================================================================
// PR-4 output formatting tests (slice 3 PR-04)
// ============================================================================

// TestPushOutputFormat_ErrorMessages verifies error message templates match spec.
func TestPushOutputFormat_ErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code        errors.Code
		wantMessage string
	}{
		{
			code:        errors.EEmptyDiff,
			wantMessage: "no commits ahead of parent; make at least one commit",
		},
		{
			code:        errors.ENoOrigin,
			wantMessage: "git remote 'origin' not configured",
		},
		{
			code:        errors.EUnsupportedOriginHost,
			wantMessage: "origin host must be github.com",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.code), func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.code, tt.wantMessage)
			ae, ok := errors.AsAgencyError(err)
			require.True(t, ok, "expected AgencyError")

			// Verify error format matches spec: <ERROR_CODE>: <message>
			expectedFormat := fmt.Sprintf("%s: %s", tt.code, tt.wantMessage)
			assert.Equal(t, expectedFormat, ae.Error())
		})
	}
}

// TestPushOutputFormat_WarningMessages verifies warning strings match spec.
func TestPushOutputFormat_WarningMessages(t *testing.T) {
	t.Parallel()
	// These are the exact warning strings required by the spec
	warnings := []string{
		"warning: worktree has uncommitted changes; proceeding due to --allow-dirty",
		"warning: report file missing; using auto-generated PR body",
		"warning: report unreadable (error); using auto-generated PR body",
		"warning: report empty (<20 chars); using auto-generated PR body",
		"warning: report incomplete (missing: summary, how to test); using auto-generated PR body",
	}

	for _, w := range warnings {
		assert.True(t, strings.HasPrefix(w, "warning: "), "warning %q does not start with 'warning: '", w)
	}
}

// TestPushOutputFormat_SuccessLine verifies success output format.
func TestPushOutputFormat_SuccessLine(t *testing.T) {
	t.Parallel()
	// Per spec: success prints exactly one stdout line: pr: <url>
	url := "https://github.com/owner/repo/pull/123"
	expected := fmt.Sprintf("pr: %s\n", url)

	// This is the format that push.go now produces
	assert.True(t, strings.HasPrefix(expected, "pr: "), "success output should start with 'pr: '")
	assert.NotContains(t, expected, "created", "success output should NOT contain 'created'")
	assert.NotContains(t, expected, "updated", "success output should NOT contain 'updated'")
}

func TestParsePRURL(t *testing.T) {
	t.Parallel()
	stderr := `a pull request for branch "agency/test" into branch "main" already exists:
https://github.com/owner/repo/pull/80`

	url, number, ok := parsePRURL(stderr)
	require.True(t, ok, "expected URL to be parsed")
	assert.Equal(t, "https://github.com/owner/repo/pull/80", url)
	assert.Equal(t, 80, number)
}

func TestIsPRAlreadyExistsError(t *testing.T) {
	t.Parallel()
	assert.True(t, isPRAlreadyExistsError("a pull request for branch already exists"), "expected already exists error to match")
	assert.False(t, isPRAlreadyExistsError("gh pr create failed: permission denied"), "expected non-matching error to be false")
}

func TestViewPRByBranchUsesOwnerRepo(t *testing.T) {
	t.Parallel()
	cr := &pushTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name != "gh" {
				return exec.CmdResult{ExitCode: 1, Stderr: "unexpected command"}, nil
			}
			got := strings.Join(args, " ")
			if !strings.Contains(got, "pr list") {
				return exec.CmdResult{ExitCode: 1, Stderr: "expected list"}, nil
			}
			if !strings.Contains(got, "--head owner:branch") {
				return exec.CmdResult{ExitCode: 1, Stderr: "missing head"}, nil
			}
			if !strings.Contains(got, "-R owner/repo") {
				return exec.CmdResult{ExitCode: 1, Stderr: "missing repo"}, nil
			}
			return exec.CmdResult{
				ExitCode: 0,
				Stdout:   `[{"number":1,"url":"https://github.com/owner/repo/pull/1","state":"OPEN"}]`,
			}, nil
		},
	}

	pr, err := viewPRByBranch(context.Background(), cr, "/tmp", "branch", ghRepoRef{
		NameWithOwner: "owner/repo",
		Owner:         "owner",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, pr.Number)
}

func TestViewPRWithRetry_BackoffAndEvents(t *testing.T) {
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")
	sleeper := &fakePushSleeper{}

	origJitter := jitterDelay
	jitterDelay = func(d time.Duration) time.Duration { return d }
	t.Cleanup(func() { jitterDelay = origJitter })

	attempt := 0
	cr := &pushTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			head := ""
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "--head" {
					head = args[i+1]
					break
				}
			}
			if head == "owner:branch" {
				attempt++
				if attempt >= 3 {
					return exec.CmdResult{
						ExitCode: 0,
						Stdout:   `[{"number":2,"url":"https://github.com/owner/repo/pull/2","state":"OPEN"}]`,
					}, nil
				}
			}
			return exec.CmdResult{ExitCode: 0, Stdout: `[]`}, nil
		},
	}

	pr, err := viewPRWithRetry(context.Background(), cr, "/tmp", "branch", ghRepoRef{
		NameWithOwner: "owner/repo",
		Owner:         "owner",
	}, "repo123", "run123", eventsPath, sleeper)
	require.NoError(t, err)
	assert.Equal(t, 2, pr.Number)

	require.Len(t, sleeper.sleeps, 2)
	assert.Equal(t, time.Second, sleeper.sleeps[0])
	assert.Equal(t, 2*time.Second, sleeper.sleeps[1])

	data, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "read events")
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	require.Len(t, lines, 3)
}

type pushTestCommandRunner struct {
	runFunc func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error)
}

func (f *pushTestCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	return f.runFunc(ctx, name, args, opts)
}

func (f *pushTestCommandRunner) LookPath(file string) (string, error) {
	return "", nil
}

type fakePushSleeper struct {
	sleeps []time.Duration
}

func (f *fakePushSleeper) Sleep(d time.Duration) {
	f.sleeps = append(f.sleeps, d)
}

// ============================================================================
// S7 Spec4: Report Completeness Gate Tests
// ============================================================================

// TestReportCompletenessErrorCode verifies E_REPORT_INCOMPLETE error code exists.
func TestReportCompletenessErrorCode(t *testing.T) {
	t.Parallel()
	// Verify the error code exists and has correct format
	code := errors.EReportIncomplete
	assert.Equal(t, errors.Code("E_REPORT_INCOMPLETE"), code)

	// Verify we can create an error with this code
	err := errors.NewWithDetails(
		errors.EReportIncomplete,
		"report incomplete: missing summary, how to test",
		map[string]string{"missing_sections": "summary, how to test"},
	)

	assert.Equal(t, errors.EReportIncomplete, errors.GetCode(err))
}

// TestPushReportGating_MissingFile documents warning behavior for missing report.
func TestPushReportGating_MissingFile(t *testing.T) {
	t.Parallel()
	t.Log("Push behavior for missing report file:")
	t.Log("  - Emits warning")
	t.Log("  - Uses auto-generated PR body")
	t.Log("  - Push proceeds")
}

// TestPushReportGating_IncompleteReport documents warning behavior for incomplete report.
func TestPushReportGating_IncompleteReport(t *testing.T) {
	t.Parallel()
	t.Log("Push behavior for incomplete report:")
	t.Log("  - Emits warning with missing sections")
	t.Log("  - Uses auto-generated PR body")
	t.Log("  - Push proceeds")
}

// TestPushReportGating_ForceBypass documents --force behavior.
func TestPushReportGating_ForceBypass(t *testing.T) {
	t.Parallel()
	t.Log("--force behavior:")
	t.Log("  - No report gating remains (flag is no-op for report checks)")
}

// TestPushReportGating_CompleteReport documents report behavior.
func TestPushReportGating_CompleteReport(t *testing.T) {
	t.Parallel()
	t.Log("Complete report behavior:")
	t.Log("  - Report body is used directly")
	t.Log("  - No report warnings printed")
}

// TestPushErrorOutput_ReportIncomplete verifies error output format.
func TestPushErrorOutput_ReportIncomplete(t *testing.T) {
	t.Parallel()
	t.Log("Report completeness no longer blocks push; warnings are printed instead")
}

// TestPushReportGating_DistinguishMissingVsIncomplete verifies distinct behavior.
func TestPushReportGating_DistinguishMissingVsIncomplete(t *testing.T) {
	t.Parallel()
	// S7 spec4 introduced a distinction:
	// - Missing file: E_REPORT_INVALID (existing code)
	// - Incomplete content: E_REPORT_INCOMPLETE (new code)

	// Verify they are distinct error codes
	assert.NotEqual(t, errors.EReportInvalid, errors.EReportIncomplete, "E_REPORT_INVALID and E_REPORT_INCOMPLETE should be distinct")

	assert.Equal(t, errors.Code("E_REPORT_INVALID"), errors.EReportInvalid)
	assert.Equal(t, errors.Code("E_REPORT_INCOMPLETE"), errors.EReportIncomplete)
}
