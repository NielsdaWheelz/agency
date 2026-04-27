package testutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
)

// SetupGitRepo creates a real git repo with a tracked README and initial commit.
// Returns the repo root path. Cleanup is automatic via t.TempDir().
func SetupGitRepo(t *testing.T) string {
	t.Helper()
	HermeticGitEnv(t)
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "Initial commit")
	return dir
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	result, err := agencyexec.NewRealRunner().Run(context.Background(), name, args, agencyexec.RunOpts{Dir: dir})
	if err != nil {
		t.Fatalf("%s %v failed to start: %v", name, args, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("%s %v exited %d\n%s", name, args, result.ExitCode, strings.TrimSpace(result.Stdout+"\n"+result.Stderr))
	}
}
