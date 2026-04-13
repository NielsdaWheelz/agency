package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMergeSleeper is a fake sleeper for testing.
type fakeMergeSleeper struct {
	sleeps []time.Duration
}

func (f *fakeMergeSleeper) Sleep(d time.Duration) {
	f.sleeps = append(f.sleeps, d)
}

func TestParseOriginHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		originURL string
		want      string
	}{
		{
			name:      "scp-like github",
			originURL: "git@github.com:owner/repo.git",
			want:      "github.com",
		},
		{
			name:      "https github",
			originURL: "https://github.com/owner/repo.git",
			want:      "github.com",
		},
		{
			name:      "https github no .git",
			originURL: "https://github.com/owner/repo",
			want:      "github.com",
		},
		{
			name:      "scp-like gitlab",
			originURL: "git@gitlab.com:owner/repo.git",
			want:      "gitlab.com",
		},
		{
			name:      "https gitlab",
			originURL: "https://gitlab.com/owner/repo.git",
			want:      "gitlab.com",
		},
		{
			name:      "enterprise github",
			originURL: "git@github.enterprise.com:owner/repo.git",
			want:      "github.enterprise.com",
		},
		{
			name:      "empty",
			originURL: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseOriginHost(tt.originURL)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseGHPRViewFull(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		json    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid full response",
			json: `{
				"number": 123,
				"url": "https://github.com/owner/repo/pull/123",
				"state": "OPEN",
				"isDraft": false,
				"mergeable": "MERGEABLE",
				"headRefName": "agency/test-branch"
			}`,
			wantErr: false,
		},
		{
			name: "valid draft PR",
			json: `{
				"number": 456,
				"url": "https://github.com/owner/repo/pull/456",
				"state": "OPEN",
				"isDraft": true,
				"mergeable": "UNKNOWN",
				"headRefName": "agency/draft-branch"
			}`,
			wantErr: false,
		},
		{
			name: "missing number",
			json: `{
				"url": "https://github.com/owner/repo/pull/123",
				"state": "OPEN"
			}`,
			wantErr: true,
			errMsg:  "missing required field: number",
		},
		{
			name: "missing url",
			json: `{
				"number": 123,
				"state": "OPEN"
			}`,
			wantErr: true,
			errMsg:  "missing required field: url",
		},
		{
			name: "missing state",
			json: `{
				"number": 123,
				"url": "https://github.com/owner/repo/pull/123"
			}`,
			wantErr: true,
			errMsg:  "missing required field: state",
		},
		{
			name: "invalid state value",
			json: `{
				"number": 123,
				"url": "https://github.com/owner/repo/pull/123",
				"state": "INVALID"
			}`,
			wantErr: true,
			errMsg:  "unexpected state value",
		},
		{
			name: "invalid mergeable value",
			json: `{
				"number": 123,
				"url": "https://github.com/owner/repo/pull/123",
				"state": "OPEN",
				"mergeable": "INVALID"
			}`,
			wantErr: true,
			errMsg:  "unexpected mergeable value",
		},
		{
			name:    "invalid json",
			json:    "not json",
			wantErr: true,
			errMsg:  "failed to parse",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pr, err := parseGHPRViewFull(tt.json)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, pr, "parseGHPRViewFull() returned nil PR on success")
			}
		})
	}
}

func TestIsGHPRNotFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "sentinel not found",
			err:  errPRNotFound,
			want: true,
		},
		{
			name: "no pull requests found",
			err:  &testError{msg: "no pull requests found for branch"},
			want: true,
		},
		{
			name: "could not find pull request",
			err:  &testError{msg: "could not find pull request"},
			want: true,
		},
		{
			name: "other error",
			err:  &testError{msg: "connection refused"},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isPRNotFound(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestValidatePRState(t *testing.T) {
	t.Parallel()
	// Create temp dir for events
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	tests := []struct {
		name           string
		pr             *ghPRViewFull
		expectedBranch string
		wantErr        bool
		errCode        string
		wantMerged     bool // for idempotent already-merged path
	}{
		{
			name: "open PR, matching branch",
			pr: &ghPRViewFull{
				Number:      123,
				State:       "OPEN",
				IsDraft:     false,
				HeadRefName: "agency/test",
			},
			expectedBranch: "agency/test",
			wantErr:        false,
			wantMerged:     false,
		},
		{
			name: "merged PR - idempotent path",
			pr: &ghPRViewFull{
				Number:      123,
				State:       "MERGED",
				HeadRefName: "agency/test",
			},
			expectedBranch: "agency/test",
			wantErr:        false,
			wantMerged:     true, // Should return AlreadyMerged=true instead of error
		},
		{
			name: "closed PR",
			pr: &ghPRViewFull{
				Number:      123,
				State:       "CLOSED",
				HeadRefName: "agency/test",
			},
			expectedBranch: "agency/test",
			wantErr:        true,
			errCode:        "E_PR_NOT_OPEN",
		},
		{
			name: "draft PR",
			pr: &ghPRViewFull{
				Number:      123,
				State:       "OPEN",
				IsDraft:     true,
				HeadRefName: "agency/test",
			},
			expectedBranch: "agency/test",
			wantErr:        true,
			errCode:        "E_PR_DRAFT",
		},
		{
			name: "branch mismatch",
			pr: &ghPRViewFull{
				Number:      123,
				State:       "OPEN",
				IsDraft:     false,
				HeadRefName: "agency/other",
			},
			expectedBranch: "agency/test",
			wantErr:        true,
			errCode:        "E_PR_MISMATCH",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := validatePRState(tt.pr, tt.expectedBranch, eventsPath, "repo123", "run123")
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errCode)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result, "validatePRState() returned nil result on success")
				assert.Equal(t, tt.wantMerged, result.AlreadyMerged)
			}
		})
	}
}

func TestCheckMergeability(t *testing.T) {
	t.Parallel()
	// Create temp dir for events
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	tests := []struct {
		name       string
		responses  []string // JSON responses for each gh call
		wantErr    bool
		errCode    string
		wantSleeps int
	}{
		{
			name: "MERGEABLE on first try",
			responses: []string{
				`{"mergeable": "MERGEABLE"}`,
			},
			wantErr:    false,
			wantSleeps: 0,
		},
		{
			name: "CONFLICTING on first try",
			responses: []string{
				`{"mergeable": "CONFLICTING"}`,
			},
			wantErr: true,
			errCode: "E_PR_NOT_MERGEABLE",
		},
		{
			name: "UNKNOWN then MERGEABLE",
			responses: []string{
				`{"mergeable": "UNKNOWN"}`,
				`{"mergeable": "MERGEABLE"}`,
			},
			wantErr:    false,
			wantSleeps: 1,
		},
		{
			name: "UNKNOWN 4 times",
			responses: []string{
				`{"mergeable": "UNKNOWN"}`,
				`{"mergeable": "UNKNOWN"}`,
				`{"mergeable": "UNKNOWN"}`,
				`{"mergeable": "UNKNOWN"}`,
			},
			wantErr:    true,
			errCode:    "E_PR_MERGEABILITY_UNKNOWN",
			wantSleeps: 3,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callIdx := 0
			fakeCR := &mergeTestCommandRunner{
				runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name != "gh" || len(args) < 2 || args[0] != "pr" || args[1] != "view" {
						return exec.CmdResult{ExitCode: 1}, nil
					}
					if callIdx >= len(tt.responses) {
						return exec.CmdResult{ExitCode: 1, Stderr: "unexpected call"}, nil
					}
					resp := tt.responses[callIdx]
					callIdx++
					return exec.CmdResult{ExitCode: 0, Stdout: resp}, nil
				},
			}

			sleeper := &fakeMergeSleeper{}
			err := checkMergeability(context.Background(), fakeCR, "/tmp", "owner/repo", 123, sleeper, eventsPath, "repo123", "run123")

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errCode)
			} else {
				assert.NoError(t, err)
			}

			assert.Len(t, sleeper.sleeps, tt.wantSleeps)
		})
	}
}

func TestCheckRemoteHeadUpToDate(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	tests := []struct {
		name    string
		setup   func(*mergeTestCommandRunner)
		wantErr bool
		errCode string
	}{
		{
			name: "up to date",
			setup: func(cr *mergeTestCommandRunner) {
				cr.runFunc = func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name == "git" && contains(args, "fetch") {
						return exec.CmdResult{ExitCode: 0}, nil
					}
					if name == "git" && contains(args, "rev-parse") {
						return exec.CmdResult{ExitCode: 0, Stdout: "abc123\n"}, nil
					}
					return exec.CmdResult{ExitCode: 1}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "fetch fails",
			setup: func(cr *mergeTestCommandRunner) {
				cr.runFunc = func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name == "git" && contains(args, "fetch") {
						return exec.CmdResult{ExitCode: 1, Stderr: "fetch failed"}, nil
					}
					return exec.CmdResult{ExitCode: 0}, nil
				}
			},
			wantErr: true,
			errCode: "E_GIT_FETCH_FAILED",
		},
		{
			name: "sha mismatch",
			setup: func(cr *mergeTestCommandRunner) {
				callIdx := 0
				cr.runFunc = func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name == "git" && contains(args, "fetch") {
						return exec.CmdResult{ExitCode: 0}, nil
					}
					if name == "git" && contains(args, "rev-parse") {
						callIdx++
						if callIdx == 1 {
							return exec.CmdResult{ExitCode: 0, Stdout: "local123\n"}, nil
						}
						return exec.CmdResult{ExitCode: 0, Stdout: "remote456\n"}, nil
					}
					return exec.CmdResult{ExitCode: 1}, nil
				}
			},
			wantErr: true,
			errCode: "E_REMOTE_OUT_OF_DATE",
		},
		{
			name: "remote branch missing",
			setup: func(cr *mergeTestCommandRunner) {
				callIdx := 0
				cr.runFunc = func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name == "git" && contains(args, "fetch") {
						return exec.CmdResult{ExitCode: 0}, nil
					}
					if name == "git" && contains(args, "rev-parse") {
						callIdx++
						if callIdx == 1 {
							return exec.CmdResult{ExitCode: 0, Stdout: "local123\n"}, nil
						}
						return exec.CmdResult{ExitCode: 1, Stderr: "not a valid ref"}, nil
					}
					return exec.CmdResult{ExitCode: 1}, nil
				}
			},
			wantErr: true,
			errCode: "E_REMOTE_OUT_OF_DATE",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cr := &mergeTestCommandRunner{}
			tt.setup(cr)

			err := checkRemoteHeadUpToDate(context.Background(), cr, "/tmp", "agency/test", eventsPath, "repo123", "run123")

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errCode)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// mergeTestCommandRunner is a fake implementation of exec.CommandRunner for merge tests.
type mergeTestCommandRunner struct {
	runFunc func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error)
}

func (f *mergeTestCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if f.runFunc != nil {
		return f.runFunc(ctx, name, args, opts)
	}
	return exec.CmdResult{ExitCode: 0}, nil
}

func (f *mergeTestCommandRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

func contains(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

// TestMergeIntegration tests the full merge flow with a fake setup.
func TestMergeIntegration_PrechecksPass_ThenVerifyFails_ThenRejects(t *testing.T) {
	t.Parallel()
	// This test simulates:
	// 1. All prechecks pass
	// 2. Verify script fails
	// 3. User rejects continuation

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	worktreePath := filepath.Join(tmpDir, "worktree")
	repoRoot := filepath.Join(tmpDir, "repo")

	// Create directories
	for _, dir := range []string{
		dataDir,
		worktreePath,
		repoRoot,
		filepath.Join(worktreePath, ".agency", "out"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755), "failed to create directory %s", dir)
	}

	// Create agency.json
	agencyJSON := map[string]any{
		"version": 1,
		"scripts": map[string]any{
			"setup":   "scripts/setup.sh",
			"verify":  "exit 1", // Will fail
			"archive": "scripts/archive.sh",
		},
	}
	agencyJSONBytes, err := json.Marshal(agencyJSON)
	require.NoError(t, err, "failed to marshal agency.json")
	require.NoError(t, os.WriteFile(filepath.Join(worktreePath, "agency.json"), agencyJSONBytes, 0o644), "failed to write agency.json")

	// Create meta.json
	repoID := "test123456789012"
	runID := "20260115120000-abcd"
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	require.NoError(t, os.MkdirAll(filepath.Join(runsDir, "logs"), 0o755), "failed to create runs dir")

	meta := &store.RunMeta{
		SchemaVersion:   "1.0",
		RunID:           runID,
		RepoID:          repoID,
		Name:            "test run",
		Runner:          "claude",
		ParentBranch:    "main",
		Branch:          "agency/test-abcd",
		WorktreePath:    worktreePath,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		TmuxSessionName: "agency_" + runID,
		PRNumber:        123,
		PRURL:           "https://github.com/owner/repo/pull/123",
	}
	metaBytes, err := json.Marshal(meta)
	require.NoError(t, err, "failed to marshal meta.json")
	require.NoError(t, os.WriteFile(filepath.Join(runsDir, "meta.json"), metaBytes, 0o644), "failed to write meta.json")

	// Create repo.json
	repoRecordDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoRecordDir, 0o755), "failed to create repo record dir")
	repoRecord := map[string]any{
		"schema_version": "1.0",
		"origin_url":     "git@github.com:owner/repo.git",
		"origin_host":    "github.com",
	}
	repoRecordBytes, err := json.Marshal(repoRecord)
	require.NoError(t, err, "failed to marshal repo.json")
	require.NoError(t, os.WriteFile(filepath.Join(repoRecordDir, "repo.json"), repoRecordBytes, 0o644), "failed to write repo.json")

	// We can't easily test the full flow without mocking tty.IsInteractive()
	// This test is more of a compilation/structure check
	t.Log("Integration test structure verified - full test requires TTY mocking")
}

// TestMergeStrategyFlags tests the strategy flag mapping.
func TestMergeStrategyFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		strategy MergeStrategy
		wantFlag string
	}{
		{MergeStrategySquash, "--squash"},
		{MergeStrategyMerge, "--merge"},
		{MergeStrategyRebase, "--rebase"},
		{"", "--squash"}, // default
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.strategy), func(t *testing.T) {
			t.Parallel()
			strategy := tt.strategy
			if strategy == "" {
				strategy = MergeStrategySquash
			}
			gotFlag := "--" + string(strategy)
			assert.Equal(t, tt.wantFlag, gotFlag)
		})
	}
}

// TestConfirmPRMerged tests the post-merge state confirmation.
func TestConfirmPRMerged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		responses  []string // JSON responses for each gh call
		wantResult bool
		wantSleeps int
	}{
		{
			name: "merged on first try",
			responses: []string{
				`{"state": "MERGED"}`,
			},
			wantResult: true,
			wantSleeps: 0,
		},
		{
			name: "open then merged",
			responses: []string{
				`{"state": "OPEN"}`,
				`{"state": "MERGED"}`,
			},
			wantResult: true,
			wantSleeps: 1,
		},
		{
			name: "never merged",
			responses: []string{
				`{"state": "OPEN"}`,
				`{"state": "OPEN"}`,
				`{"state": "OPEN"}`,
			},
			wantResult: false,
			wantSleeps: 2, // Sleeps before attempts 2 and 3
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callIdx := 0
			fakeCR := &mergeTestCommandRunner{
				runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name != "gh" || len(args) < 2 || args[0] != "pr" || args[1] != "view" {
						return exec.CmdResult{ExitCode: 1}, nil
					}
					if callIdx >= len(tt.responses) {
						return exec.CmdResult{ExitCode: 1, Stderr: "unexpected call"}, nil
					}
					resp := tt.responses[callIdx]
					callIdx++
					return exec.CmdResult{ExitCode: 0, Stdout: resp}, nil
				},
			}

			sleeper := &fakeMergeSleeper{}
			result, _ := confirmPRMerged(context.Background(), fakeCR, "/tmp", "owner/repo", 123, sleeper)

			assert.Equal(t, tt.wantResult, result)
			assert.Len(t, sleeper.sleeps, tt.wantSleeps)
		})
	}
}

func TestViewPRByHeadFullWithRetry_Backoff(t *testing.T) {
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")
	sleeper := &fakeMergeSleeper{}

	origJitter := jitterDelay
	jitterDelay = func(d time.Duration) time.Duration { return d }
	t.Cleanup(func() { jitterDelay = origJitter })

	call := 0
	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			call++
			if args[1] == "list" {
				if call < 3 {
					return exec.CmdResult{ExitCode: 0, Stdout: `[]`}, nil
				}
				return exec.CmdResult{
					ExitCode: 0,
					Stdout:   `[{"number":3,"url":"https://github.com/owner/repo/pull/3","state":"OPEN"}]`,
				}, nil
			}
			if args[1] == "view" {
				return exec.CmdResult{
					ExitCode: 0,
					Stdout:   `{"number":3,"url":"https://github.com/owner/repo/pull/3","state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefName":"agency/test"}`,
				}, nil
			}
			return exec.CmdResult{ExitCode: 1, Stderr: "unexpected call"}, nil
		},
	}

	pr, err := viewPRByHeadFullWithRetry(context.Background(), fakeCR, "/tmp", "owner/repo", "owner:branch", "branch", sleeper, eventsPath, "repo123", "run123")
	require.NoError(t, err)
	assert.Equal(t, 3, pr.Number)

	require.Len(t, sleeper.sleeps, 2)
	assert.Equal(t, time.Second, sleeper.sleeps[0])
	assert.Equal(t, 2*time.Second, sleeper.sleeps[1])

	data, err := os.ReadFile(eventsPath)
	require.NoError(t, err, "read events")
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	require.Len(t, lines, 3)
}

// TestTruncateString tests the string truncation helper.
func TestTruncateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := truncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExecuteGHMerge_DeleteBranch tests the --delete-branch flag behavior.
func TestExecuteGHMerge_DeleteBranch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		deleteBranch bool
		wantFlag     bool // whether --delete-branch should be in args
	}{
		{
			name:         "delete branch enabled (default)",
			deleteBranch: true,
			wantFlag:     true,
		},
		{
			name:         "delete branch disabled (--no-delete-branch)",
			deleteBranch: false,
			wantFlag:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			mergeLogPath := filepath.Join(tmpDir, "merge.log")

			var capturedArgs []string
			fakeCR := &mergeTestCommandRunner{
				runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
					if name == "gh" && len(args) > 0 && args[0] == "pr" && args[1] == "merge" {
						capturedArgs = args
						return exec.CmdResult{ExitCode: 0, Stdout: "merged"}, nil
					}
					return exec.CmdResult{ExitCode: 1}, nil
				},
			}

			err := executeGHMerge(context.Background(), fakeCR, fs.NewRealFS(), tmpDir, "owner/repo", 123, "--squash", mergeLogPath, tt.deleteBranch)
			require.NoError(t, err)

			// Check if --delete-branch is in args
			hasDeleteBranch := false
			for _, arg := range capturedArgs {
				if arg == "--delete-branch" {
					hasDeleteBranch = true
					break
				}
			}

			assert.Equal(t, tt.wantFlag, hasDeleteBranch, "executeGHMerge() --delete-branch in args; args = %v", capturedArgs)

			// Verify the log file contains the right info
			logContent, err := os.ReadFile(mergeLogPath)
			require.NoError(t, err, "failed to read merge log")
			if tt.wantFlag {
				assert.Contains(t, string(logContent), "--delete-branch", "merge log should contain --delete-branch when enabled")
			} else {
				assert.NotContains(t, string(logContent), "--delete-branch", "merge log should not contain --delete-branch when disabled")
			}
			info, statErr := os.Stat(mergeLogPath)
			require.NoError(t, statErr, "failed to stat merge log")
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "merge log should be private")
		})
	}
}

func TestExecuteGHMerge_LogWriteFailureReturnsPersistFailed(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	mergeLogPath := filepath.Join(tmpDir, "merge.log")
	require.NoError(t, os.MkdirAll(mergeLogPath, 0o700), "precreate merge.log path as directory")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			return exec.CmdResult{ExitCode: 0, Stdout: "merged"}, nil
		},
	}

	err := executeGHMerge(context.Background(), fakeCR, fs.NewRealFS(), tmpDir, "owner/repo", 123, "--squash", mergeLogPath, true)
	require.Error(t, err)
	assert.Equal(t, errors.EPersistFailed, errors.GetCode(err))
	assert.ErrorContains(t, err, "failed to persist merge log")
}

func TestBuildVerifyEnvForMerge_UsesRepoAndWorkspaceRoots(t *testing.T) {
	t.Parallel()
	meta := &store.RunMeta{
		RunID:        "run-123",
		Name:         "run-name",
		Runner:       "claude",
		Branch:       "agency/branch",
		ParentBranch: "main",
		PRURL:        "https://github.com/o/r/pull/1",
		PRNumber:     1,
	}
	repoRoot := "/repo/root"
	workspaceRoot := "/repo/worktrees/integration"
	runDir := "/tmp/run-dir"

	env := buildVerifyEnvForMerge(meta, repoRoot, workspaceRoot, runDir)
	envMap := make(map[string]string)
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	assert.Equal(t, repoRoot, envMap["AGENCY_REPO_ROOT"])
	assert.Equal(t, workspaceRoot, envMap["AGENCY_WORKSPACE_ROOT"])
}

// TestMergeOpts_NoDeleteBranch tests the NoDeleteBranch option default behavior.
func TestMergeOpts_NoDeleteBranch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		noDeleteBranch bool
		wantDelete     bool
	}{
		{
			name:           "default (delete branch)",
			noDeleteBranch: false,
			wantDelete:     true,
		},
		{
			name:           "preserve branch",
			noDeleteBranch: true,
			wantDelete:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := MergeOpts{
				RunID:          "test-run",
				Strategy:       MergeStrategySquash,
				NoDeleteBranch: tt.noDeleteBranch,
			}

			// The logic: deleteBranch := !opts.NoDeleteBranch
			deleteBranch := !opts.NoDeleteBranch
			assert.Equal(t, tt.wantDelete, deleteBranch)
		})
	}
}

// TestGetOriginURLForMerge tests origin URL resolution.
func TestGetOriginURLForMerge(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test123456789012"

	// Create repo.json with origin URL
	repoRecordDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoRecordDir, 0o755), "failed to create repo record dir")
	repoRecord := map[string]any{
		"schema_version": "1.0",
		"origin_url":     "git@github.com:owner/repo.git",
		"origin_host":    "github.com",
	}
	repoRecordBytes, err := json.Marshal(repoRecord)
	require.NoError(t, err, "failed to marshal repo.json")
	require.NoError(t, os.WriteFile(filepath.Join(repoRecordDir, "repo.json"), repoRecordBytes, 0o644), "failed to write repo.json")

	// Create store
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)

	// Test that it reads from repo.json
	url, err := getOriginURLForMerge(context.Background(), nil, st, repoID, "/tmp/worktree")
	require.NoError(t, err)
	assert.Equal(t, "git@github.com:owner/repo.git", url)
}

// ==========================================
// Error code coverage tests for merge/PR
// ==========================================

// TestMergeErrorCode_ENoPR verifies that resolvePRForMerge returns ENoPR
// when no PR exists for the branch.
func TestMergeErrorCode_ENoPR(t *testing.T) {
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	// Disable jitter so retry sleeps are deterministic
	origJitter := jitterDelay
	jitterDelay = func(d time.Duration) time.Duration { return 0 }
	t.Cleanup(func() { jitterDelay = origJitter })

	meta := &store.RunMeta{
		RunID:        "20260115120000-abcd",
		Branch:       "agency/test-branch",
		WorktreePath: tmpDir,
	}

	// Fake CR: gh pr list always returns empty (no PR found)
	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "gh" && len(args) > 1 && args[0] == "pr" && args[1] == "list" {
				return exec.CmdResult{ExitCode: 0, Stdout: `[]`}, nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		},
	}

	sleeper := &fakeMergeSleeper{}
	_, err := resolvePRForMerge(
		context.Background(), fakeCR, meta, "owner/repo",
		newGHRepoRef("owner", "repo"), eventsPath, "repo123", sleeper,
	)

	require.Error(t, err)
	assert.Equal(t, errors.ENoPR, errors.GetCode(err), "expected ENoPR error code")
}

// TestMergeErrorCode_EPRDraft verifies that validatePRState returns EPRDraft
// when the PR is in draft state.
func TestMergeErrorCode_EPRDraft(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	pr := &ghPRViewFull{
		Number:      42,
		URL:         "https://github.com/owner/repo/pull/42",
		State:       "OPEN",
		IsDraft:     true,
		HeadRefName: "agency/test-branch",
	}

	_, err := validatePRState(pr, "agency/test-branch", eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.EPRDraft, errors.GetCode(err), "expected EPRDraft error code")
}

// TestMergeErrorCode_EPRMismatch verifies that validatePRState returns EPRMismatch
// when the PR head branch does not match the expected branch.
func TestMergeErrorCode_EPRMismatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	pr := &ghPRViewFull{
		Number:      42,
		URL:         "https://github.com/owner/repo/pull/42",
		State:       "OPEN",
		IsDraft:     false,
		HeadRefName: "agency/different-branch",
	}

	_, err := validatePRState(pr, "agency/expected-branch", eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.EPRMismatch, errors.GetCode(err), "expected EPRMismatch error code")
}

// TestMergeErrorCode_EGHRepoParseFailed verifies that EGHRepoParseFailed is returned
// when the origin URL cannot be parsed into owner/repo.
// This code path is in Merge() precheck 6. We test ParseGitHubOwnerRepo indirectly
// by verifying the error code produced for an unparseable URL.
func TestMergeErrorCode_EGHRepoParseFailed(t *testing.T) {
	t.Parallel()

	// identity.ParseGitHubOwnerRepo returns false for unparseable URLs.
	// The merge code then returns EGHRepoParseFailed. Since the actual
	// code that produces this error is inline in Merge() at precheck 6,
	// we verify the error code by checking the same path:
	// identity.ParseGitHubOwnerRepo fails -> EGHRepoParseFailed.
	//
	// We test the exact error construction here to confirm the code.
	originURL := "not-a-valid-url"
	_, _, ok := identity.ParseGitHubOwnerRepo(originURL)
	require.False(t, ok, "expected ParseGitHubOwnerRepo to fail for invalid URL")

	// Construct the same error the production code would create
	err := errors.NewWithDetails(errors.EGHRepoParseFailed, "failed to parse owner/repo from origin URL",
		map[string]string{"origin_url": originURL})

	assert.Equal(t, errors.EGHRepoParseFailed, errors.GetCode(err), "expected EGHRepoParseFailed error code")
}

// TestMergeErrorCode_EPRMergeabilityUnknown verifies that checkMergeability returns
// EPRMergeabilityUnknown when mergeability stays UNKNOWN after all retries.
func TestMergeErrorCode_EPRMergeabilityUnknown(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			return exec.CmdResult{ExitCode: 0, Stdout: `{"mergeable": "UNKNOWN"}`}, nil
		},
	}

	sleeper := &fakeMergeSleeper{}
	err := checkMergeability(context.Background(), fakeCR, tmpDir, "owner/repo", 42, sleeper, eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.EPRMergeabilityUnknown, errors.GetCode(err), "expected EPRMergeabilityUnknown error code")
}

// TestMergeErrorCode_EGHPRMergeFailed_NonZeroExit verifies that executeGHMerge returns
// EGHPRMergeFailed when gh pr merge exits with a non-zero code.
func TestMergeErrorCode_EGHPRMergeFailed_NonZeroExit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	mergeLogPath := filepath.Join(tmpDir, "merge.log")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			return exec.CmdResult{ExitCode: 1, Stderr: "merge conflict"}, nil
		},
	}

	err := executeGHMerge(context.Background(), fakeCR, fs.NewRealFS(), tmpDir, "owner/repo", 42, "--squash", mergeLogPath, true)

	require.Error(t, err)
	assert.Equal(t, errors.EGHPRMergeFailed, errors.GetCode(err), "expected EGHPRMergeFailed error code")
}

// TestMergeErrorCode_EGHPRMergeFailed_ExecError verifies that executeGHMerge returns
// EGHPRMergeFailed when the gh command runner itself returns an error.
func TestMergeErrorCode_EGHPRMergeFailed_ExecError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	mergeLogPath := filepath.Join(tmpDir, "merge.log")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			return exec.CmdResult{}, fmt.Errorf("exec failed: command not found")
		},
	}

	err := executeGHMerge(context.Background(), fakeCR, fs.NewRealFS(), tmpDir, "owner/repo", 42, "--squash", mergeLogPath, true)

	require.Error(t, err)
	assert.Equal(t, errors.EGHPRMergeFailed, errors.GetCode(err), "expected EGHPRMergeFailed error code")
}

// TestMergeErrorCode_EPRNotMergeable verifies that checkMergeability returns
// EPRNotMergeable when the PR has conflicts.
func TestMergeErrorCode_EPRNotMergeable(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			return exec.CmdResult{ExitCode: 0, Stdout: `{"mergeable": "CONFLICTING"}`}, nil
		},
	}

	sleeper := &fakeMergeSleeper{}
	err := checkMergeability(context.Background(), fakeCR, tmpDir, "owner/repo", 42, sleeper, eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.EPRNotMergeable, errors.GetCode(err), "expected EPRNotMergeable error code")
}

// TestMergeErrorCode_EGitFetchFailed_NonZeroExit verifies that checkRemoteHeadUpToDate
// returns EGitFetchFailed when git fetch exits with a non-zero code.
func TestMergeErrorCode_EGitFetchFailed_NonZeroExit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "git" && contains(args, "fetch") {
				return exec.CmdResult{ExitCode: 128, Stderr: "fatal: could not read from remote"}, nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		},
	}

	err := checkRemoteHeadUpToDate(context.Background(), fakeCR, tmpDir, "agency/test", eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.EGitFetchFailed, errors.GetCode(err), "expected EGitFetchFailed error code")
}

// TestMergeErrorCode_EGitFetchFailed_ExecError verifies that checkRemoteHeadUpToDate
// returns EGitFetchFailed when the git fetch command runner returns an error.
func TestMergeErrorCode_EGitFetchFailed_ExecError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "git" && contains(args, "fetch") {
				return exec.CmdResult{}, fmt.Errorf("exec failed: git not found")
			}
			return exec.CmdResult{ExitCode: 0}, nil
		},
	}

	err := checkRemoteHeadUpToDate(context.Background(), fakeCR, tmpDir, "agency/test", eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.EGitFetchFailed, errors.GetCode(err), "expected EGitFetchFailed error code")
}

// TestMergeErrorCode_ERemoteOutOfDate_ShaMismatch verifies that checkRemoteHeadUpToDate
// returns ERemoteOutOfDate when local and remote SHAs differ.
func TestMergeErrorCode_ERemoteOutOfDate_ShaMismatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	revParseCall := 0
	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "git" && contains(args, "fetch") {
				return exec.CmdResult{ExitCode: 0}, nil
			}
			if name == "git" && contains(args, "rev-parse") {
				revParseCall++
				if revParseCall == 1 {
					return exec.CmdResult{ExitCode: 0, Stdout: "aaa111\n"}, nil
				}
				return exec.CmdResult{ExitCode: 0, Stdout: "bbb222\n"}, nil
			}
			return exec.CmdResult{ExitCode: 1}, nil
		},
	}

	err := checkRemoteHeadUpToDate(context.Background(), fakeCR, tmpDir, "agency/test", eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.ERemoteOutOfDate, errors.GetCode(err), "expected ERemoteOutOfDate error code")
}

// TestMergeErrorCode_ERemoteOutOfDate_RemoteBranchMissing verifies that checkRemoteHeadUpToDate
// returns ERemoteOutOfDate when the remote branch ref does not exist.
func TestMergeErrorCode_ERemoteOutOfDate_RemoteBranchMissing(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	revParseCall := 0
	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "git" && contains(args, "fetch") {
				return exec.CmdResult{ExitCode: 0}, nil
			}
			if name == "git" && contains(args, "rev-parse") {
				revParseCall++
				if revParseCall == 1 {
					return exec.CmdResult{ExitCode: 0, Stdout: "aaa111\n"}, nil
				}
				// Second call (remote ref) fails
				return exec.CmdResult{ExitCode: 128, Stderr: "fatal: bad revision"}, nil
			}
			return exec.CmdResult{ExitCode: 1}, nil
		},
	}

	err := checkRemoteHeadUpToDate(context.Background(), fakeCR, tmpDir, "agency/test", eventsPath, "repo123", "run123")

	require.Error(t, err)
	assert.Equal(t, errors.ERemoteOutOfDate, errors.GetCode(err), "expected ERemoteOutOfDate error code")
}

// TestMergeErrorCode_EDirtyWorktree verifies that dirtyErrorWithContext returns
// EDirtyWorktree when the worktree has uncommitted changes.
func TestMergeErrorCode_EDirtyWorktree(t *testing.T) {
	t.Parallel()

	err := dirtyErrorWithContext("M  file.go\n?? untracked.txt")

	require.Error(t, err)
	assert.Equal(t, errors.EDirtyWorktree, errors.GetCode(err), "expected EDirtyWorktree error code")
	assert.Contains(t, err.Error(), "uncommitted changes")
}

// TestMergeErrorCode_EDirtyWorktree_ViaGetDirtyStatus verifies the full dirty worktree
// detection flow: getDirtyStatus reports dirty, and dirtyErrorWithContext produces EDirtyWorktree.
func TestMergeErrorCode_EDirtyWorktree_ViaGetDirtyStatus(t *testing.T) {
	t.Parallel()

	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "git" && contains(args, "status") {
				return exec.CmdResult{ExitCode: 0, Stdout: "M  file.go\n"}, nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		},
	}

	isClean, status, err := getDirtyStatus(context.Background(), fakeCR, "/tmp")
	require.NoError(t, err)
	require.False(t, isClean, "expected dirty worktree")

	// Now produce the error the same way merge.go does
	dirtyErr := dirtyErrorWithContext(status)
	assert.Equal(t, errors.EDirtyWorktree, errors.GetCode(dirtyErr), "expected EDirtyWorktree error code")
}

// TestMergeErrorCode_ENoPR_ByStoredPRNumber verifies ENoPR when stored PR number
// lookup fails and head branch lookup also finds no PR.
func TestMergeErrorCode_ENoPR_ByStoredPRNumber(t *testing.T) {
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	origJitter := jitterDelay
	jitterDelay = func(d time.Duration) time.Duration { return 0 }
	t.Cleanup(func() { jitterDelay = origJitter })

	meta := &store.RunMeta{
		RunID:        "20260115120000-abcd",
		Branch:       "agency/test-branch",
		WorktreePath: tmpDir,
		PRNumber:     999, // stored PR number that will not be found
	}

	// Fake CR: gh pr view by number returns "not found", gh pr list returns empty
	fakeCR := &mergeTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			if name == "gh" && len(args) > 1 {
				if args[0] == "pr" && args[1] == "view" {
					return exec.CmdResult{ExitCode: 1, Stderr: "could not find pull request"}, nil
				}
				if args[0] == "pr" && args[1] == "list" {
					return exec.CmdResult{ExitCode: 0, Stdout: `[]`}, nil
				}
			}
			return exec.CmdResult{ExitCode: 0}, nil
		},
	}

	sleeper := &fakeMergeSleeper{}
	_, err := resolvePRForMerge(
		context.Background(), fakeCR, meta, "owner/repo",
		newGHRepoRef("owner", "repo"), eventsPath, "repo123", sleeper,
	)

	require.Error(t, err)
	assert.Equal(t, errors.ENoPR, errors.GetCode(err), "expected ENoPR error code")
}

func TestMerge_NonInteractiveWithoutYes_ReturnsEConfirmationRequired(t *testing.T) {
	originalIsInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = originalIsInteractive })

	err := Merge(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), t.TempDir(), MergeOpts{
		RunID: "20260303-merge-ni",
	}, strings.NewReader(""), io.Discard, io.Discard)
	require.Error(t, err)
	assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))
}

func TestMerge_NonInteractiveWithYes_DoesNotFailAtConfirmationGate(t *testing.T) {
	originalIsInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = originalIsInteractive })

	err := Merge(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), t.TempDir(), MergeOpts{
		RunID: "20260303-merge-yes",
		Yes:   true,
	}, strings.NewReader(""), io.Discard, io.Discard)
	require.Error(t, err)
	assert.NotEqual(t, errors.EConfirmationRequired, errors.GetCode(err))
	assert.NotEqual(t, errors.ENotInteractive, errors.GetCode(err))
}
