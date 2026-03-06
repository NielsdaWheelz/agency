//go:build e2e

package commands_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

type cliRunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type agentMutationResponse struct {
	OK           bool   `json:"ok"`
	ErrorCode    string `json:"error_code"`
	Message      string `json:"message"`
	InvocationID string `json:"invocation_id"`
	SandboxPath  string `json:"sandbox_path"`
}

type invocationShowResponse struct {
	Runner string `json:"runner"`
}

type launchCapture struct {
	Args []string `json:"args"`
	CWD  string   `json:"cwd"`
	Mode string   `json:"mode"`
}

func TestAgentStartCLIE2E_HeadlessLaunchMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("AGENCY_LOCAL_E2E") == "" {
		t.Skip("set AGENCY_LOCAL_E2E=1 to enable local CLI e2e tests")
	}

	repoRoot := repoRootFromCaller(t)
	agencyBin, fakeRunnerBin := buildE2EBinaries(t, repoRoot)

	tests := []struct {
		name            string
		runnerInput     string
		canonicalRunner string
		runnerArg       string
		prompt          string
	}{
		{
			name:            "claude alias",
			runnerInput:     "claude",
			canonicalRunner: "claude-code",
			runnerArg:       "--model=opus",
			prompt:          "cli e2e matrix claude alias",
		},
		{
			name:            "claude-code canonical",
			runnerInput:     "claude-code",
			canonicalRunner: "claude-code",
			runnerArg:       "--model=opus",
			prompt:          "cli e2e matrix claude canonical",
		},
		{
			name:            "codex canonical",
			runnerInput:     "codex",
			canonicalRunner: "codex",
			runnerArg:       "--model=gpt-5",
			prompt:          "cli e2e matrix codex canonical",
		},
		{
			name:            "amp canonical",
			runnerInput:     "amp",
			canonicalRunner: "amp",
			runnerArg:       "--model=amp-fast",
			prompt:          "cli e2e matrix amp canonical",
		},
		{
			name:            "opencode canonical",
			runnerInput:     "opencode",
			canonicalRunner: "opencode",
			runnerArg:       "--model=open",
			prompt:          "cli e2e matrix opencode canonical",
		},
		{
			name:            "cursor canonical",
			runnerInput:     "cursor",
			canonicalRunner: "cursor",
			runnerArg:       "--profile=default",
			prompt:          "cli e2e matrix cursor canonical",
		},
		{
			name:            "cursor-cli alias",
			runnerInput:     "cursor-cli",
			canonicalRunner: "cursor",
			runnerArg:       "--profile=default",
			prompt:          "cli e2e matrix cursor alias",
		},
		{
			name:            "droid canonical",
			runnerInput:     "droid",
			canonicalRunner: "droid",
			runnerArg:       "--agent=android",
			prompt:          "cli e2e matrix droid canonical",
		},
	}

	for i, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repoDir := testutil.SetupGitRepo(t)

			tmpDir := mustMkdirTemp(t, "agency-e2e-*")
			dataDir := filepath.Join(tmpDir, "data")
			configDir := filepath.Join(tmpDir, "config")
			cacheDir := filepath.Join(tmpDir, "cache")
			require.NoError(t, os.MkdirAll(dataDir, 0o755))
			require.NoError(t, os.MkdirAll(configDir, 0o755))
			require.NoError(t, os.MkdirAll(cacheDir, 0o755))
			writeE2EConfig(t, configDir, fakeRunnerBin)

			worktreeName := fmt.Sprintf("e2e-launch-%02d", i)
			capturePath := filepath.Join(tmpDir, "launch_capture.json")
			env := map[string]string{
				"AGENCY_DATA_DIR":            dataDir,
				"AGENCY_CONFIG_DIR":          configDir,
				"AGENCY_CACHE_DIR":           cacheDir,
				"FAKE_RUNNER_MODE":           "exit-ok",
				"FAKE_RUNNER_CAPTURE_PATH":   capturePath,
				"GIT_CONFIG_NOSYSTEM":        os.Getenv("GIT_CONFIG_NOSYSTEM"),
				"GIT_CONFIG_GLOBAL":          os.Getenv("GIT_CONFIG_GLOBAL"),
				"GIT_AUTHOR_NAME":            os.Getenv("GIT_AUTHOR_NAME"),
				"GIT_AUTHOR_EMAIL":           os.Getenv("GIT_AUTHOR_EMAIL"),
				"GIT_COMMITTER_NAME":         os.Getenv("GIT_COMMITTER_NAME"),
				"GIT_COMMITTER_EMAIL":        os.Getenv("GIT_COMMITTER_EMAIL"),
				"GIT_TERMINAL_PROMPT":        "0",
				"GH_PROMPT_DISABLED":         "1",
				"AGENCY_LOCAL_E2E":           "1",
				"AGENCY_LOCAL_E2E_TEST_CASE": tc.name,
			}

			create := runAgencyCLI(t, agencyBin, repoDir, env, "worktree", "create", "--name", worktreeName)
			require.Equalf(t, 0, create.ExitCode, "worktree create failed\nstdout:\n%s\nstderr:\n%s", create.Stdout, create.Stderr)

			start := runAgencyCLI(t, agencyBin, repoDir, env,
				"agent", "start",
				"--worktree", worktreeName,
				"--runner", tc.runnerInput,
				"--headless",
				"--prompt", tc.prompt,
				"--runner-arg", tc.runnerArg,
				"--json",
			)
			require.Equalf(t, 0, start.ExitCode, "agent start failed\nstdout:\n%s\nstderr:\n%s", start.Stdout, start.Stderr)

			var startResp agentMutationResponse
			require.NoError(t, json.Unmarshal([]byte(start.Stdout), &startResp), "invalid start json: %s", start.Stdout)
			require.Truef(t, startResp.OK, "expected ok=true, got error_code=%s message=%s", startResp.ErrorCode, startResp.Message)
			require.NotEmpty(t, startResp.InvocationID)
			require.NotEmpty(t, startResp.SandboxPath)

			show := runAgencyCLI(t, agencyBin, repoDir, env, "agent", "show", startResp.InvocationID, "--json")
			require.Equalf(t, 0, show.ExitCode, "agent show failed\nstdout:\n%s\nstderr:\n%s", show.Stdout, show.Stderr)

			var showResp invocationShowResponse
			require.NoError(t, json.Unmarshal([]byte(show.Stdout), &showResp), "invalid show json: %s", show.Stdout)
			assert.Equal(t, tc.canonicalRunner, showResp.Runner)

			capture := readLaunchCapture(t, capturePath)
			assert.Equal(t, "exit-ok", capture.Mode)
			assert.Equal(t, normalizePathForAssert(t, startResp.SandboxPath), normalizePathForAssert(t, capture.CWD))

			wantArgs := expectedHeadlessArgs(tc.canonicalRunner, tc.runnerArg, tc.prompt, startResp.SandboxPath)
			if tc.canonicalRunner == "codex" {
				require.GreaterOrEqual(t, len(capture.Args), 3)
				assert.Equal(t, normalizePathForAssert(t, startResp.SandboxPath), normalizePathForAssert(t, capture.Args[2]))
				wantArgs[2] = capture.Args[2]
			}
			assert.Equal(t, wantArgs, capture.Args)

			_ = runAgencyCLI(t, agencyBin, repoDir, env, "daemon", "stop", "--force")
		})
	}
}

func TestAgentStartCLIE2E_ReservedArgConflictJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("AGENCY_LOCAL_E2E") == "" {
		t.Skip("set AGENCY_LOCAL_E2E=1 to enable local CLI e2e tests")
	}

	repoRoot := repoRootFromCaller(t)
	agencyBin, fakeRunnerBin := buildE2EBinaries(t, repoRoot)

	repoDir := testutil.SetupGitRepo(t)

	tmpDir := mustMkdirTemp(t, "agency-e2e-*")
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "config")
	cacheDir := filepath.Join(tmpDir, "cache")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	writeE2EConfig(t, configDir, fakeRunnerBin)

	env := map[string]string{
		"AGENCY_DATA_DIR":     dataDir,
		"AGENCY_CONFIG_DIR":   configDir,
		"AGENCY_CACHE_DIR":    cacheDir,
		"FAKE_RUNNER_MODE":    "exit-ok",
		"GIT_CONFIG_NOSYSTEM": os.Getenv("GIT_CONFIG_NOSYSTEM"),
		"GIT_CONFIG_GLOBAL":   os.Getenv("GIT_CONFIG_GLOBAL"),
		"GIT_AUTHOR_NAME":     os.Getenv("GIT_AUTHOR_NAME"),
		"GIT_AUTHOR_EMAIL":    os.Getenv("GIT_AUTHOR_EMAIL"),
		"GIT_COMMITTER_NAME":  os.Getenv("GIT_COMMITTER_NAME"),
		"GIT_COMMITTER_EMAIL": os.Getenv("GIT_COMMITTER_EMAIL"),
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
		"AGENCY_LOCAL_E2E":    "1",
	}

	create := runAgencyCLI(t, agencyBin, repoDir, env, "worktree", "create", "--name", "e2e-conflict")
	require.Equalf(t, 0, create.ExitCode, "worktree create failed\nstdout:\n%s\nstderr:\n%s", create.Stdout, create.Stderr)

	start := runAgencyCLI(t, agencyBin, repoDir, env,
		"agent", "start",
		"--worktree", "e2e-conflict",
		"--runner", "cursor",
		"--headless",
		"--prompt", "reserved arg conflict",
		"--runner-arg", "-p",
		"--json",
	)
	require.Equalf(t, 0, start.ExitCode, "expected json envelope command to exit 0\nstdout:\n%s\nstderr:\n%s", start.Stdout, start.Stderr)

	var startResp agentMutationResponse
	require.NoError(t, json.Unmarshal([]byte(start.Stdout), &startResp), "invalid start json: %s", start.Stdout)
	assert.False(t, startResp.OK)
	assert.Equal(t, "E_RUNNER_ARG_CONFLICT", startResp.ErrorCode)
	assert.Contains(t, startResp.Message, "reserved flag")

	_ = runAgencyCLI(t, agencyBin, repoDir, env, "daemon", "stop", "--force")
}

func buildE2EBinaries(t *testing.T, repoRoot string) (agencyBin string, fakeRunnerBin string) {
	t.Helper()

	binDir := mustMkdirTemp(t, "agency-e2e-bin-*")
	agencyBin = filepath.Join(binDir, "agency")
	fakeRunnerBin = filepath.Join(binDir, "fakerunner")

	buildCommand(t, repoRoot, agencyBin, "./cmd/agency")
	buildCommand(t, repoRoot, fakeRunnerBin, "./internal/daemon/testdata/fakerunner")

	return agencyBin, fakeRunnerBin
}

func buildCommand(t *testing.T, repoRoot, outputPath, target string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := osexec.CommandContext(ctx, "go", "build", "-o", outputPath, target)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		require.FailNowf(t, "go build timed out", "target=%s", target)
	}
	require.NoErrorf(t, err, "go build failed for %s: %s", target, string(out))
}

func runAgencyCLI(t *testing.T, agencyBin, cwd string, env map[string]string, args ...string) cliRunResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := osexec.CommandContext(ctx, agencyBin, args...)
	cmd.Dir = cwd
	cmd.Env = mergeEnv(os.Environ(), env)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *osexec.ExitError
		if ok := stderrors.As(err, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			require.NoError(t, err)
		}
	}
	if ctx.Err() != nil {
		require.FailNowf(t, "agency CLI command timed out", "args=%v", args)
	}

	return cliRunResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	envMap := map[string]string{}
	for _, kv := range base {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		envMap[key] = value
	}
	for k, v := range overrides {
		envMap[k] = v
	}
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+envMap[k])
	}
	return out
}

func readLaunchCapture(t *testing.T, capturePath string) launchCapture {
	t.Helper()
	var capture launchCapture
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(capturePath)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			return false
		}
		return json.Unmarshal(data, &capture) == nil
	}, 5*time.Second, 50*time.Millisecond, "launch capture not written: %s", capturePath)
	return capture
}

func expectedHeadlessArgs(canonicalRunner, runnerArg, prompt, sandboxPath string) []string {
	switch canonicalRunner {
	case "claude-code":
		return []string{"-p", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions", runnerArg, prompt}
	case "codex":
		return []string{"exec", "--cd", sandboxPath, "--json", "--full-auto", runnerArg, prompt}
	case "amp":
		return []string{"-x", runnerArg, prompt}
	case "opencode":
		return []string{"run", "--mode", "auto", runnerArg, prompt}
	case "cursor":
		return []string{"-p", runnerArg, prompt}
	case "droid":
		return []string{"exec", runnerArg, prompt}
	default:
		return nil
	}
}

func writeE2EConfig(t *testing.T, configDir, fakeRunnerBin string) {
	t.Helper()

	cfg := map[string]any{
		"version": 1,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude":      fakeRunnerBin,
			"claude-code": fakeRunnerBin,
			"codex":       fakeRunnerBin,
			"amp":         fakeRunnerBin,
			"opencode":    fakeRunnerBin,
			"cursor":      fakeRunnerBin,
			"cursor-cli":  fakeRunnerBin,
			"droid":       fakeRunnerBin,
		},
	}

	payload, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), payload, 0o644))
}

func mustMkdirTemp(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", pattern)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = agencyfs.SafeRemoveAll(dir, os.TempDir())
	})
	return dir
}

func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func normalizePathForAssert(t *testing.T, path string) string {
	t.Helper()
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return clean
	}
	return filepath.Clean(resolved)
}
