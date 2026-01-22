package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
)

type prBodyCommandRunner struct{}

func (r *prBodyCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if name != "git" || len(args) < 2 {
		return exec.CmdResult{ExitCode: 1, Stderr: "unexpected command"}, nil
	}

	switch args[0] {
	case "log":
		return exec.CmdResult{
			ExitCode: 0,
			Stdout:   "feat: add thing\nfix: handle edge case\n",
		}, nil
	case "diff":
		if args[1] == "--stat" {
			return exec.CmdResult{
				ExitCode: 0,
				Stdout:   "file1.go | 2 +-\nfile2.go | 1 +\n2 files changed, 2 insertions(+), 1 deletion(-)\n",
			}, nil
		}
		if args[1] == "--name-only" {
			return exec.CmdResult{
				ExitCode: 0,
				Stdout:   "file1.go\nfile2.go\n",
			}, nil
		}
	}

	return exec.CmdResult{ExitCode: 1, Stderr: "unexpected args"}, nil
}

func (r *prBodyCommandRunner) LookPath(file string) (string, error) {
	return "", nil
}

func TestWriteFallbackPRBody(t *testing.T) {
	workDir := t.TempDir()
	realFS := fs.NewRealFS()
	meta := &store.RunMeta{
		RunID:        "run123",
		Name:         "test-run",
		Branch:       "agency/test-run-1234",
		ParentBranch: "main",
	}

	path, hash, err := writeFallbackPRBody(context.Background(), &prBodyCommandRunner{}, realFS, workDir, "main", meta.Branch, meta)
	if err != nil {
		t.Fatalf("writeFallbackPRBody() error = %v", err)
	}
	if path == "" {
		t.Fatal("expected pr body path")
	}
	if hash == "" {
		t.Fatal("expected non-empty body hash")
	}

	contentBytes, err := realFS.ReadFile(path)
	if err != nil {
		t.Fatalf("read pr body: %v", err)
	}
	content := string(contentBytes)

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
		if !strings.Contains(content, snippet) {
			t.Errorf("expected pr body to contain %q", snippet)
		}
	}
}
