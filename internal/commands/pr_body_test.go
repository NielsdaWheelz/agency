package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type prBodyCommandRunner struct{}

func (r *prBodyCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if name != "git" || len(args) < 2 {
		return exec.CmdResult{ExitCode: 1, Stderr: "unexpected command"}, nil
	}

	switch args[0] {
	case "rev-list":
		return exec.CmdResult{
			ExitCode: 0,
			Stdout:   "2\n",
		}, nil
	case "log":
		return exec.CmdResult{
			ExitCode: 0,
			Stdout:   "feat: add thing\nfix: handle edge case\n",
		}, nil
	case "diff":
		if args[1] == "--shortstat" {
			return exec.CmdResult{
				ExitCode: 0,
				Stdout:   "2 files changed, 2 insertions(+), 1 deletion(-)\n",
			}, nil
		}
		if strings.HasPrefix(args[1], "--stat=120,80,21") {
			return exec.CmdResult{
				ExitCode: 0,
				Stdout:   "file1.go | 2 +-\nfile2.go | 1 +\n2 files changed, 2 insertions(+), 1 deletion(-)\n",
			}, nil
		}
	}

	return exec.CmdResult{ExitCode: 1, Stderr: "unexpected args"}, nil
}

func (r *prBodyCommandRunner) LookPath(file string) (string, error) {
	return "", nil
}

func TestWriteFallbackPRBody(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	realFS := fs.NewRealFS()
	meta := &store.RunMeta{
		RunID:        "run123",
		Name:         "test-run",
		Branch:       "agency/test-run-1234",
		ParentBranch: "main",
	}

	path, hash, err := writeFallbackPRBody(context.Background(), &prBodyCommandRunner{}, realFS, workDir, "main", meta.Branch, meta)
	require.NoError(t, err)
	require.NotEmpty(t, path, "expected pr body path")
	require.NotEmpty(t, hash, "expected non-empty body hash")

	contentBytes, err := realFS.ReadFile(path)
	require.NoError(t, err, "read pr body")
	content := string(contentBytes)
	info, err := os.Stat(path)
	require.NoError(t, err, "stat pr body")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "fallback pr body should be private")

	wantSnippets := []string{
		"# test-run",
		"## summary",
		"- feat: add thing",
		"## commits",
		"- feat: add thing",
		"- fix: handle edge case",
		"## changes",
		"file1.go | 2 +-",
		"## files",
		"- file1.go",
		"- file2.go",
		"## tests",
		"- not run (report missing or incomplete)",
		"## meta",
		"- run_id: run123",
		"- branch: agency/test-run-1234",
		"- parent: main",
	}

	for _, snippet := range wantSnippets {
		assert.Contains(t, content, snippet)
	}
}

type boundedPRBodyCommandRunner struct {
	calls [][]string
}

func (r *boundedPRBodyCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if name != "git" {
		return exec.CmdResult{ExitCode: 1, Stderr: "unexpected command"}, nil
	}
	r.calls = append(r.calls, append([]string{}, args...))
	if len(args) < 2 {
		return exec.CmdResult{ExitCode: 1, Stderr: "unexpected args"}, nil
	}

	switch args[0] {
	case "rev-list":
		return exec.CmdResult{ExitCode: 0, Stdout: "15\n"}, nil
	case "log":
		if !containsAllArgs(args, "-n", "11") {
			return exec.CmdResult{ExitCode: 1, Stderr: "missing bounded -n 11 for git log"}, nil
		}
		lines := make([]string, 0, 11)
		for i := 1; i <= 11; i++ {
			lines = append(lines, fmt.Sprintf("commit %02d", i))
		}
		return exec.CmdResult{ExitCode: 0, Stdout: strings.Join(lines, "\n") + "\n"}, nil
	case "diff":
		if args[1] == "--shortstat" {
			return exec.CmdResult{ExitCode: 0, Stdout: " 25 files changed, 100 insertions(+), 2 deletions(-)\n"}, nil
		}
		if len(args) > 1 && strings.HasPrefix(args[1], "--stat=120,80,21") {
			var b strings.Builder
			for i := 1; i <= 21; i++ {
				_, _ = b.WriteString(fmt.Sprintf("file-%02d.go | 1 +\n", i))
			}
			_, _ = b.WriteString("21 files changed, 21 insertions(+)\n")
			return exec.CmdResult{ExitCode: 0, Stdout: b.String()}, nil
		}
	}

	return exec.CmdResult{ExitCode: 1, Stderr: "unexpected args"}, nil
}

func (r *boundedPRBodyCommandRunner) LookPath(file string) (string, error) {
	return "", nil
}

func containsAllArgs(args []string, values ...string) bool {
	for _, want := range values {
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestWriteFallbackPRBody_BoundsCommitAndFileInputs(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	realFS := fs.NewRealFS()
	meta := &store.RunMeta{
		RunID:        "run123",
		Name:         "test-run",
		Branch:       "agency/test-run-1234",
		ParentBranch: "main",
	}
	runner := &boundedPRBodyCommandRunner{}

	path, _, err := writeFallbackPRBody(context.Background(), runner, realFS, workDir, "main", meta.Branch, meta)
	require.NoError(t, err)

	contentBytes, err := realFS.ReadFile(path)
	require.NoError(t, err)
	content := string(contentBytes)

	assert.Contains(t, content, "- commit 01")
	assert.Contains(t, content, "- commit 10")
	assert.NotContains(t, content, "- commit 11")
	assert.Contains(t, content, "- ... and 5 more")

	assert.Contains(t, content, "- file-01.go")
	assert.Contains(t, content, "- file-20.go")
	assert.NotContains(t, content, "- file-21.go")
	assert.Contains(t, content, "- ... and 5 more")
}

type unknownCountPRBodyCommandRunner struct{}

func (r *unknownCountPRBodyCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if name != "git" || len(args) < 2 {
		return exec.CmdResult{ExitCode: 1, Stderr: "unexpected command"}, nil
	}

	switch args[0] {
	case "rev-list":
		return exec.CmdResult{ExitCode: 1, Stderr: "count unavailable"}, nil
	case "log":
		lines := make([]string, 0, 11)
		for i := 1; i <= 11; i++ {
			lines = append(lines, fmt.Sprintf("commit %02d", i))
		}
		return exec.CmdResult{ExitCode: 0, Stdout: strings.Join(lines, "\n") + "\n"}, nil
	case "diff":
		if args[1] == "--shortstat" {
			return exec.CmdResult{ExitCode: 1, Stderr: "shortstat unavailable"}, nil
		}
		if len(args) > 1 && strings.HasPrefix(args[1], "--stat=120,80,21") {
			var b strings.Builder
			for i := 1; i <= 21; i++ {
				_, _ = b.WriteString(fmt.Sprintf("file-%02d.go | 1 +\n", i))
			}
			_, _ = b.WriteString("21 files changed, 21 insertions(+)\n")
			return exec.CmdResult{ExitCode: 0, Stdout: b.String()}, nil
		}
	}

	return exec.CmdResult{ExitCode: 1, Stderr: "unexpected args"}, nil
}

func (r *unknownCountPRBodyCommandRunner) LookPath(file string) (string, error) {
	return "", nil
}

func TestWriteFallbackPRBody_ShowsUnknownTruncationWhenCountsUnavailable(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	realFS := fs.NewRealFS()
	meta := &store.RunMeta{
		RunID:        "run123",
		Name:         "test-run",
		Branch:       "agency/test-run-1234",
		ParentBranch: "main",
	}

	path, _, err := writeFallbackPRBody(context.Background(), &unknownCountPRBodyCommandRunner{}, realFS, workDir, "main", meta.Branch, meta)
	require.NoError(t, err)

	contentBytes, err := realFS.ReadFile(path)
	require.NoError(t, err)
	content := string(contentBytes)
	assert.Contains(t, content, "- ... and more", "fallback should signal truncation even when total counts are unavailable")
}
