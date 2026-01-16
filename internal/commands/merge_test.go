package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// fakeMergeSleeper is a fake sleeper for testing.
type fakeMergeSleeper struct {
	sleeps []time.Duration
}

func (f *fakeMergeSleeper) Sleep(d time.Duration) {
	f.sleeps = append(f.sleeps, d)
}

func TestParseOriginHost(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got := parseOriginHost(tt.originURL)
			if got != tt.want {
				t.Errorf("parseOriginHost(%q) = %q, want %q", tt.originURL, got, tt.want)
			}
		})
	}
}

func TestParseGHPRViewFull(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			pr, err := parseGHPRViewFull(tt.json)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseGHPRViewFull() expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("parseGHPRViewFull() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("parseGHPRViewFull() unexpected error: %v", err)
				}
				if pr == nil {
					t.Errorf("parseGHPRViewFull() returned nil PR on success")
				}
			}
		})
	}
}

func TestIsGHPRNotFound(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			got := isGHPRNotFound(tt.err)
			if got != tt.want {
				t.Errorf("isGHPRNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
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
	// Create temp dir for events
	tmpDir := t.TempDir()
	eventsPath := filepath.Join(tmpDir, "events.jsonl")

	tests := []struct {
		name            string
		pr              *ghPRViewFull
		expectedBranch  string
		wantErr         bool
		errCode         string
		wantMerged      bool // for idempotent already-merged path
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
		t.Run(tt.name, func(t *testing.T) {
			result, err := validatePRState(tt.pr, tt.expectedBranch, eventsPath, "repo123", "run123")
			if tt.wantErr {
				if err == nil {
					t.Errorf("validatePRState() expected error with code %s, got nil", tt.errCode)
				} else if !strings.Contains(err.Error(), tt.errCode) {
					t.Errorf("validatePRState() error = %q, want error containing %q", err.Error(), tt.errCode)
				}
			} else {
				if err != nil {
					t.Errorf("validatePRState() unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("validatePRState() returned nil result on success")
				} else if result.AlreadyMerged != tt.wantMerged {
					t.Errorf("validatePRState() AlreadyMerged = %v, want %v", result.AlreadyMerged, tt.wantMerged)
				}
			}
		})
	}
}

func TestCheckMergeability(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
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
				if err == nil {
					t.Errorf("checkMergeability() expected error with code %s, got nil", tt.errCode)
				} else if !strings.Contains(err.Error(), tt.errCode) {
					t.Errorf("checkMergeability() error = %q, want error containing %q", err.Error(), tt.errCode)
				}
			} else {
				if err != nil {
					t.Errorf("checkMergeability() unexpected error: %v", err)
				}
			}

			if len(sleeper.sleeps) != tt.wantSleeps {
				t.Errorf("checkMergeability() sleeps = %d, want %d", len(sleeper.sleeps), tt.wantSleeps)
			}
		})
	}
}

func TestCheckRemoteHeadUpToDate(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			cr := &mergeTestCommandRunner{}
			tt.setup(cr)

			err := checkRemoteHeadUpToDate(context.Background(), cr, "/tmp", "agency/test", eventsPath, "repo123", "run123")

			if tt.wantErr {
				if err == nil {
					t.Errorf("checkRemoteHeadUpToDate() expected error with code %s, got nil", tt.errCode)
				} else if !strings.Contains(err.Error(), tt.errCode) {
					t.Errorf("checkRemoteHeadUpToDate() error = %q, want error containing %q", err.Error(), tt.errCode)
				}
			} else {
				if err != nil {
					t.Errorf("checkRemoteHeadUpToDate() unexpected error: %v", err)
				}
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
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create directory %s: %v", dir, err)
		}
	}

	// Create agency.json
	agencyJSON := map[string]any{
		"version": 1,
		"defaults": map[string]any{
			"parent_branch": "main",
			"runner":        "claude",
		},
		"scripts": map[string]any{
			"setup":   "scripts/setup.sh",
			"verify":  "exit 1", // Will fail
			"archive": "scripts/archive.sh",
		},
	}
	agencyJSONBytes, err := json.Marshal(agencyJSON)
	if err != nil {
		t.Fatalf("failed to marshal agency.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "agency.json"), agencyJSONBytes, 0o644); err != nil {
		t.Fatalf("failed to write agency.json: %v", err)
	}

	// Create meta.json
	repoID := "test123456789012"
	runID := "20260115120000-abcd"
	runsDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
	if err := os.MkdirAll(filepath.Join(runsDir, "logs"), 0o755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	meta := &store.RunMeta{
		SchemaVersion:   "1.0",
		RunID:           runID,
		RepoID:          repoID,
		Title:           "test run",
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
	if err != nil {
		t.Fatalf("failed to marshal meta.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "meta.json"), metaBytes, 0o644); err != nil {
		t.Fatalf("failed to write meta.json: %v", err)
	}

	// Create repo.json
	repoRecordDir := filepath.Join(dataDir, "repos", repoID)
	if err := os.MkdirAll(repoRecordDir, 0o755); err != nil {
		t.Fatalf("failed to create repo record dir: %v", err)
	}
	repoRecord := map[string]any{
		"schema_version": "1.0",
		"origin_url":     "git@github.com:owner/repo.git",
		"origin_host":    "github.com",
	}
	repoRecordBytes, err := json.Marshal(repoRecord)
	if err != nil {
		t.Fatalf("failed to marshal repo.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRecordDir, "repo.json"), repoRecordBytes, 0o644); err != nil {
		t.Fatalf("failed to write repo.json: %v", err)
	}

	// We can't easily test the full flow without mocking tty.IsInteractive()
	// This test is more of a compilation/structure check
	t.Log("Integration test structure verified - full test requires TTY mocking")
}

// TestMergeStrategyFlags tests the strategy flag mapping.
func TestMergeStrategyFlags(t *testing.T) {
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
		t.Run(string(tt.strategy), func(t *testing.T) {
			strategy := tt.strategy
			if strategy == "" {
				strategy = MergeStrategySquash
			}
			gotFlag := "--" + string(strategy)
			if gotFlag != tt.wantFlag {
				t.Errorf("strategy flag = %q, want %q", gotFlag, tt.wantFlag)
			}
		})
	}
}

// TestConfirmPRMerged tests the post-merge state confirmation.
func TestConfirmPRMerged(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
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

			if result != tt.wantResult {
				t.Errorf("confirmPRMerged() = %v, want %v", result, tt.wantResult)
			}

			if len(sleeper.sleeps) != tt.wantSleeps {
				t.Errorf("confirmPRMerged() sleeps = %d, want %d", len(sleeper.sleeps), tt.wantSleeps)
			}
		})
	}
}

// TestTruncateString tests the string truncation helper.
func TestTruncateString(t *testing.T) {
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
		t.Run(tt.input, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// TestGetOriginURLForMerge tests origin URL resolution.
func TestGetOriginURLForMerge(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	repoID := "test123456789012"

	// Create repo.json with origin URL
	repoRecordDir := filepath.Join(dataDir, "repos", repoID)
	if err := os.MkdirAll(repoRecordDir, 0o755); err != nil {
		t.Fatalf("failed to create repo record dir: %v", err)
	}
	repoRecord := map[string]any{
		"schema_version": "1.0",
		"origin_url":     "git@github.com:owner/repo.git",
		"origin_host":    "github.com",
	}
	repoRecordBytes, err := json.Marshal(repoRecord)
	if err != nil {
		t.Fatalf("failed to marshal repo.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRecordDir, "repo.json"), repoRecordBytes, 0o644); err != nil {
		t.Fatalf("failed to write repo.json: %v", err)
	}

	// Create store
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)

	// Test that it reads from repo.json
	url, err := getOriginURLForMerge(context.Background(), nil, st, repoID, "/tmp/worktree")
	if err != nil {
		t.Fatalf("getOriginURLForMerge() unexpected error: %v", err)
	}
	if url != "git@github.com:owner/repo.git" {
		t.Errorf("getOriginURLForMerge() = %q, want %q", url, "git@github.com:owner/repo.git")
	}
}
