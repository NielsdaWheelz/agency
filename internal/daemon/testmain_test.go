package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
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
	result, err := agencyexec.NewRealRunner().Run(context.Background(), "go", []string{"build", "-o", fakeRunnerBin, "./testdata/fakerunner"}, agencyexec.RunOpts{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to start fakerunner build:", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}
	if result.ExitCode != 0 {
		fmt.Fprintf(os.Stderr, "failed to build fakerunner:\n%s%s", result.Stdout, result.Stderr)
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

func writeTestUserConfig(t *testing.T, configDir string) {
	t.Helper()
	requireTestNoError(t, os.MkdirAll(configDir, 0o755))
	runnerPath := os.Getenv("TEST_FAKE_RUNNER_PATH")
	if runnerPath == "" {
		runnerPath = "/bin/echo"
	}
	cfg := map[string]any{
		"version": 4,
		"defaults": map[string]string{
			"runner":            "claude-code",
			"editor":            "code",
			"execution_profile": "personal",
		},
		"runners": map[string]string{
			"claude-code": runnerPath,
			"codex":       runnerPath,
			"amp":         runnerPath,
			"opencode":    runnerPath,
			"cursor":      runnerPath,
			"droid":       runnerPath,
		},
		"execution_profiles": map[string]any{
			"personal": map[string]any{"env": map[string]string{}},
			"work":     map[string]any{"env": map[string]string{}},
		},
	}
	data, err := json.Marshal(cfg)
	requireTestNoError(t, err)
	requireTestNoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644))
}

func writeTestAgencyConfig(t *testing.T, repoRoot string) {
	t.Helper()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	requireTestNoError(t, os.MkdirAll(scriptsDir, 0o755))
	for _, script := range []string{"setup", "verify", "archive"} {
		requireTestNoError(t, os.WriteFile(filepath.Join(scriptsDir, script+".sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755))
	}
	agencyJSON := `{"version":4,"scripts":{"setup":{"path":"scripts/setup.sh","timeout":"10m"},"verify":{"path":"scripts/verify.sh","timeout":"30m"},"archive":{"path":"scripts/archive.sh","timeout":"5m"}},"execution":{"checkout_root":"repo-sibling"}}`
	requireTestNoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0o644))
}

func requireTestNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
