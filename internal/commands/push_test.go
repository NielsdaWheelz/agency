package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/report"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonInteractiveEnv(t *testing.T) {
	t.Parallel()

	env := nonInteractiveEnv()
	assert.Equal(t, "0", env["GIT_TERMINAL_PROMPT"])
	assert.Equal(t, "1", env["GH_PROMPT_DISABLED"])
	assert.Equal(t, "1", env["CI"])
}

func TestComputeReportHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.md")
	fsys := fs.NewRealFS()

	hash := computeReportHash(fsys, reportPath)
	assert.Empty(t, hash)

	require.NoError(t, os.WriteFile(reportPath, []byte("# test report\n"), 0o644))
	hash = computeReportHash(fsys, reportPath)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64)

	hash2 := computeReportHash(fsys, reportPath)
	assert.Equal(t, hash, hash2)

	require.NoError(t, os.WriteFile(reportPath, []byte("different content\n"), 0o644))
	hash3 := computeReportHash(fsys, reportPath)
	assert.NotEqual(t, hash, hash3)
}

func TestPreparePushReportBody_UsesJSONCanonicalBodyAndConflictDiagnostic(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{
  "schema_version": "1.0",
  "summary": "json summary",
  "how_to_test": "go test ./..."
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
markdown summary

## how to test
go test ./internal/...
`), 0o644))

	stderr := &bytes.Buffer{}
	bodyPath, bodyHash, usable := preparePushReportBody(fs.NewRealFS(), worktree, stderr)

	require.True(t, usable)
	assert.Equal(t, filepath.Join(worktree, ".agency", "tmp", "push_report_v2.md"), bodyPath)
	assert.NotEmpty(t, bodyHash)

	content, err := os.ReadFile(bodyPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "json summary")
	assert.NotContains(t, string(content), "markdown summary")
	assert.Contains(t, stderr.String(), "[report_conflict_json_precedence]")
}

func TestPreparePushReportBody_JSONViolationFallsBackToGeneratedBody(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.json"), []byte(`{"schema_version":"1.0","summary":`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
markdown summary

## how to test
go test ./...
`), 0o644))

	stderr := &bytes.Buffer{}
	bodyPath, bodyHash, usable := preparePushReportBody(fs.NewRealFS(), worktree, stderr)

	assert.False(t, usable)
	assert.Empty(t, bodyPath)
	assert.Empty(t, bodyHash)
	assert.Contains(t, stderr.String(), "warning: report malformed; using auto-generated PR body")
}

func TestPreparePushReportBody_MarkdownOnlyUsesReportMarkdown(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	agencyDir := filepath.Join(worktree, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agencyDir, "report.md"), []byte(`## summary
markdown summary

## how to test
go test ./...
`), 0o644))

	stderr := &bytes.Buffer{}
	bodyPath, bodyHash, usable := preparePushReportBody(fs.NewRealFS(), worktree, stderr)

	require.True(t, usable)
	assert.Equal(t, filepath.Join(worktree, ".agency", "report.md"), bodyPath)
	assert.NotEmpty(t, bodyHash)
	assert.Empty(t, stderr.String())
}

func TestPrintPushReportViolationWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		violation *report.Violation
		want      string
	}{
		{
			name:      "nil violation writes nothing",
			violation: nil,
			want:      "",
		},
		{
			name: "missing",
			violation: &report.Violation{
				Code: report.ViolationMissing,
			},
			want: "warning: report file missing; using auto-generated PR body",
		},
		{
			name: "malformed",
			violation: &report.Violation{
				Code: report.ViolationMalformed,
			},
			want: "warning: report malformed; using auto-generated PR body",
		},
		{
			name: "oversized",
			violation: &report.Violation{
				Code: report.ViolationOversized,
			},
			want: "warning: report exceeds",
		},
		{
			name: "schema incompatible",
			violation: &report.Violation{
				Code: report.ViolationSchemaIncompatible,
			},
			want: "warning: report schema incompatible; using auto-generated PR body",
		},
		{
			name: "incomplete includes message",
			violation: &report.Violation{
				Code:    report.ViolationIncomplete,
				Message: "missing summary",
			},
			want: "warning: report incomplete (missing summary); using auto-generated PR body",
		},
		{
			name: "unknown violation falls back to generic warning",
			violation: &report.Violation{
				Code:    report.ViolationCode("unknown"),
				Message: "unknown",
			},
			want: "warning: report invalid (unknown); using auto-generated PR body",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stderr := &bytes.Buffer{}
			printPushReportViolationWarning(stderr, tt.violation)
			if tt.want == "" {
				assert.Empty(t, stderr.String())
				return
			}
			assert.Contains(t, stderr.String(), tt.want)
		})
	}
}

func TestResolveRunForPush_NotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, tmpDir, time.Now)

	_, _, _, err := resolveRunForPush(context.Background(), nil, fsys, tmpDir, st, "nonexistent-run")
	require.Error(t, err)
	assert.Equal(t, errors.ERunNotFound, errors.GetCode(err))
}

func TestParsePRURL(t *testing.T) {
	t.Parallel()

	stderr := `a pull request for branch "agency/test" into branch "main" already exists:
https://github.com/owner/repo/pull/80`

	url, number, ok := parsePRURL(stderr)
	require.True(t, ok)
	assert.Equal(t, "https://github.com/owner/repo/pull/80", url)
	assert.Equal(t, 80, number)
}

func TestParsePRURL_NoMatch(t *testing.T) {
	t.Parallel()

	url, number, ok := parsePRURL("no pull request URL in output")
	assert.False(t, ok)
	assert.Empty(t, url)
	assert.Zero(t, number)
}

func TestIsPRAlreadyExistsError(t *testing.T) {
	t.Parallel()

	assert.True(t, isPRAlreadyExistsError("a pull request for branch already exists"))
	assert.True(t, isPRAlreadyExistsError("Pull Request ALREADY EXISTS"))
	assert.False(t, isPRAlreadyExistsError("gh pr create failed: permission denied"))
}

func TestViewPRByBranchUsesOwnerRepo(t *testing.T) {
	t.Parallel()

	cr := &pushTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			require.Equal(t, "gh", name)
			assert.Equal(t, "owner:branch", argValue(args, "--head"))
			assert.Equal(t, "owner/repo", argValue(args, "-R"))
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

func TestViewPRByBranchFallsBackToUnqualifiedHead(t *testing.T) {
	t.Parallel()

	var heads []string
	cr := &pushTestCommandRunner{
		runFunc: func(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
			require.Equal(t, "gh", name)
			head := argValue(args, "--head")
			heads = append(heads, head)
			switch head {
			case "owner:branch":
				return exec.CmdResult{ExitCode: 0, Stdout: `[]`}, nil
			case "branch":
				return exec.CmdResult{
					ExitCode: 0,
					Stdout:   `[{"number":7,"url":"https://github.com/owner/repo/pull/7","state":"OPEN"}]`,
				}, nil
			default:
				return exec.CmdResult{ExitCode: 1, Stderr: "unexpected head"}, nil
			}
		},
	}

	pr, err := viewPRByBranch(context.Background(), cr, "/tmp", "branch", ghRepoRef{
		NameWithOwner: "owner/repo",
		Owner:         "owner",
	})
	require.NoError(t, err)
	assert.Equal(t, 7, pr.Number)
	assert.Equal(t, []string{"owner:branch", "branch"}, heads)
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
			require.Equal(t, "gh", name)
			head := argValue(args, "--head")
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
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	require.Len(t, lines, 3)
	for _, line := range lines {
		assert.Contains(t, string(line), `"event":"pr_resolution_attempt"`)
	}
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

func argValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return strings.TrimSpace(args[i+1])
		}
	}
	return ""
}
