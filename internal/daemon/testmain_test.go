package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestMain(m *testing.M) {
	if err := testutil.UnsetGitEnv(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Compile the fake runner binary once for all tests.
	tmpDir, err := os.MkdirTemp("", "fakerunner-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create temp dir for fakerunner:", err)
		os.Exit(1)
	}

	fakeRunnerBin := filepath.Join(tmpDir, "claude-code")
	cmd := exec.Command("go", "build", "-o", fakeRunnerBin, "./testdata/fakerunner")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to build fakerunner:", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	// Expose the path via env so package daemon_test (external test package) can read it.
	if err := os.Setenv("TEST_FAKE_RUNNER_PATH", fakeRunnerBin); err != nil {
		fmt.Fprintln(os.Stderr, "failed to set TEST_FAKE_RUNNER_PATH:", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	code := m.Run()

	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}
