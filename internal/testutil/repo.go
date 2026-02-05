package testutil

import (
	"os/exec"
	"testing"
)

// SetupGitRepo creates a real git repo with an initial commit.
// Returns the repo root path. Cleanup is automatic via t.TempDir().
func SetupGitRepo(t *testing.T) string {
	t.Helper()
	HermeticGitEnv(t)
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "initial")
	return dir
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}
