//go:build e2e

package commands_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
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

type repoAddResponse struct {
	RepoID string `json:"repo_id"`
}

type launchCapture struct {
	Args []string `json:"args"`
	CWD  string   `json:"cwd"`
	Mode string   `json:"mode"`
}

func writeCommittedAgencyConfig(t *testing.T, repoRoot string) {
	t.Helper()

	scriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755))
	for _, script := range []string{"agency_setup.sh", "agency_verify.sh", "agency_archive.sh"} {
		require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, script), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(`{
  "version": 4,
  "scripts": {
    "setup": { "path": "scripts/agency_setup.sh", "timeout": "10m" },
    "verify": { "path": "scripts/agency_verify.sh", "timeout": "30m" },
    "archive": { "path": "scripts/agency_archive.sh", "timeout": "5m" }
  },
  "execution": {
    "profile": "personal",
    "checkout_root": "repo-sibling"
  }
}`), 0o644))

	runner := agencyexec.NewRealRunner()
	result, err := runner.Run(context.Background(), "git", []string{"add", "agency.json", "scripts"}, agencyexec.RunOpts{Dir: repoRoot})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git add agency config failed: %s", result.Stderr)
	result, err = runner.Run(context.Background(), "git", []string{"commit", "-m", "Add agency config"}, agencyexec.RunOpts{Dir: repoRoot})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git commit agency config failed: %s", result.Stderr)
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
			name:            "claude-code canonical",
			runnerInput:     "claude-code",
			canonicalRunner: "claude-code",
			runnerArg:       "--allowed-extra=claude",
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
			writeCommittedAgencyConfig(t, repoDir)

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

			nonGitCWD := mustMkdirTemp(t, "agency-e2e-cwd-*")
			add := runAgencyCLI(t, agencyBin, nonGitCWD, env, "repo", "add", repoDir, "--json")
			require.Equalf(t, 0, add.ExitCode, "repo add failed\nstdout:\n%s\nstderr:\n%s", add.Stdout, add.Stderr)
			var repo repoAddResponse
			require.NoError(t, json.Unmarshal([]byte(add.Stdout), &repo), "invalid repo add json: %s", add.Stdout)
			require.NotEmpty(t, repo.RepoID)

			create := runAgencyCLI(t, agencyBin, nonGitCWD, env,
				"worktree", "create",
				"--repo", repo.RepoID,
				worktreeName,
				"--base", "main",
			)
			require.Equalf(t, 0, create.ExitCode, "worktree create failed\nstdout:\n%s\nstderr:\n%s", create.Stdout, create.Stderr)

			start := runAgencyCLI(t, agencyBin, nonGitCWD, env,
				"agent", "start",
				"--repo", repo.RepoID,
				"--worktree", worktreeName,
				"--runner", tc.runnerInput,
				"--mode", "headless",
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

			show := runAgencyCLI(t, agencyBin, repoDir, env, "agent", startResp.InvocationID, "--json")
			require.Equalf(t, 0, show.ExitCode, "agent show failed\nstdout:\n%s\nstderr:\n%s", show.Stdout, show.Stderr)

			var showResp invocationShowResponse
			require.NoError(t, json.Unmarshal([]byte(show.Stdout), &showResp), "invalid show json: %s", show.Stdout)
			assert.Equal(t, tc.canonicalRunner, showResp.Runner)

			capture := readLaunchCapture(t, capturePath)
			assert.Equal(t, "exit-ok", capture.Mode)
			assert.Equal(t, normalizePathForAssert(t, startResp.SandboxPath), normalizePathForAssert(t, capture.CWD))

			wantArgs := expectedHeadlessArgs(tc.canonicalRunner, tc.runnerArg, tc.prompt, startResp.SandboxPath)
			if tc.canonicalRunner == "codex" {
				require.GreaterOrEqual(t, len(capture.Args), 7)
				assert.Equal(t, normalizePathForAssert(t, startResp.SandboxPath), normalizePathForAssert(t, capture.Args[6]))
				wantArgs[6] = capture.Args[6]
			}
			assert.Equal(t, wantArgs, capture.Args)

			_ = runAgencyCLI(t, agencyBin, repoDir, env, "daemon", "stop", "--force")
		})
	}
}

func TestAgentStartCLIE2E_ReservedRunnerArgRejectedJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("AGENCY_LOCAL_E2E") == "" {
		t.Skip("set AGENCY_LOCAL_E2E=1 to enable local CLI e2e tests")
	}

	repoRoot := repoRootFromCaller(t)
	agencyBin, fakeRunnerBin := buildE2EBinaries(t, repoRoot)

	repoDir := testutil.SetupGitRepo(t)
	writeCommittedAgencyConfig(t, repoDir)

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

	nonGitCWD := mustMkdirTemp(t, "agency-e2e-cwd-*")
	add := runAgencyCLI(t, agencyBin, nonGitCWD, env, "repo", "add", repoDir, "--json")
	require.Equalf(t, 0, add.ExitCode, "repo add failed\nstdout:\n%s\nstderr:\n%s", add.Stdout, add.Stderr)
	var repo repoAddResponse
	require.NoError(t, json.Unmarshal([]byte(add.Stdout), &repo), "invalid repo add json: %s", add.Stdout)
	require.NotEmpty(t, repo.RepoID)

	create := runAgencyCLI(t, agencyBin, nonGitCWD,
		env,
		"worktree", "create",
		"--repo", repo.RepoID,
		"e2e-conflict",
		"--base", "main",
	)
	require.Equalf(t, 0, create.ExitCode, "worktree create failed\nstdout:\n%s\nstderr:\n%s", create.Stdout, create.Stderr)

	start := runAgencyCLI(t, agencyBin, nonGitCWD, env,
		"agent", "start",
		"--repo", repo.RepoID,
		"--worktree", "e2e-conflict",
		"--runner", "cursor",
		"--mode", "headless",
		"--prompt", "reserved arg passthrough",
		"--runner-arg", "-p",
		"--json",
	)
	require.Equalf(t, 0, start.ExitCode, "agent start failed\nstdout:\n%s\nstderr:\n%s", start.Stdout, start.Stderr)

	var startResp agentMutationResponse
	require.NoError(t, json.Unmarshal([]byte(start.Stdout), &startResp), "invalid start json: %s", start.Stdout)
	assert.False(t, startResp.OK)
	assert.Equal(t, "E_RUNNER_ARG_CONFLICT", startResp.ErrorCode)
	assert.Contains(t, startResp.Message, "reserved flag '-p'")

	_ = runAgencyCLI(t, agencyBin, repoDir, env, "daemon", "stop", "--force")
}

func TestAgentStartCLIE2E_CWDFallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("AGENCY_LOCAL_E2E") == "" {
		t.Skip("set AGENCY_LOCAL_E2E=1 to enable local CLI e2e tests")
	}

	repoRoot := repoRootFromCaller(t)
	agencyBin, fakeRunnerBin := buildE2EBinaries(t, repoRoot)
	repoDir := testutil.SetupGitRepo(t)
	writeCommittedAgencyConfig(t, repoDir)

	tmpDir := mustMkdirTemp(t, "agency-e2e-*")
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "config")
	cacheDir := filepath.Join(tmpDir, "cache")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	writeE2EConfig(t, configDir, fakeRunnerBin)

	capturePath := filepath.Join(tmpDir, "launch_capture.json")
	env := map[string]string{
		"AGENCY_DATA_DIR":          dataDir,
		"AGENCY_CONFIG_DIR":        configDir,
		"AGENCY_CACHE_DIR":         cacheDir,
		"FAKE_RUNNER_MODE":         "exit-ok",
		"FAKE_RUNNER_CAPTURE_PATH": capturePath,
		"GIT_CONFIG_NOSYSTEM":      os.Getenv("GIT_CONFIG_NOSYSTEM"),
		"GIT_CONFIG_GLOBAL":        os.Getenv("GIT_CONFIG_GLOBAL"),
		"GIT_AUTHOR_NAME":          os.Getenv("GIT_AUTHOR_NAME"),
		"GIT_AUTHOR_EMAIL":         os.Getenv("GIT_AUTHOR_EMAIL"),
		"GIT_COMMITTER_NAME":       os.Getenv("GIT_COMMITTER_NAME"),
		"GIT_COMMITTER_EMAIL":      os.Getenv("GIT_COMMITTER_EMAIL"),
		"GIT_TERMINAL_PROMPT":      "0",
		"GH_PROMPT_DISABLED":       "1",
		"AGENCY_LOCAL_E2E":         "1",
	}

	create := runAgencyCLI(t, agencyBin, repoDir, env, "worktree", "create", "cwd-fallback")
	require.Equalf(t, 0, create.ExitCode, "worktree create failed\nstdout:\n%s\nstderr:\n%s", create.Stdout, create.Stderr)

	path := runAgencyCLI(t, agencyBin, repoDir, env, "worktree", "cwd-fallback", "path")
	require.Equalf(t, 0, path.ExitCode, "worktree path failed\nstdout:\n%s\nstderr:\n%s", path.Stdout, path.Stderr)
	worktreePath := strings.TrimSpace(path.Stdout)
	require.NotEmpty(t, worktreePath)

	fromRepo := runAgencyCLI(t, agencyBin, repoDir, env,
		"agent", "start",
		"--worktree", "cwd-fallback",
		"--runner", "claude-code",
		"--mode", "headless",
		"--prompt", "start from repo cwd",
		"--json",
	)
	require.Equalf(t, 0, fromRepo.ExitCode, "agent start from repo cwd failed\nstdout:\n%s\nstderr:\n%s", fromRepo.Stdout, fromRepo.Stderr)

	fromWorktree := runAgencyCLI(t, agencyBin, worktreePath, env,
		"agent", "start",
		"--runner", "claude-code",
		"--mode", "headless",
		"--prompt", "start from integration worktree cwd",
		"--json",
	)
	require.Equalf(t, 0, fromWorktree.ExitCode, "agent start from worktree cwd failed\nstdout:\n%s\nstderr:\n%s", fromWorktree.Stdout, fromWorktree.Stderr)

	_ = runAgencyCLI(t, agencyBin, repoDir, env, "daemon", "stop", "--force")
}

func TestAgentStartCLIE2E_TargetFirstActionGrammar(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("AGENCY_LOCAL_E2E") == "" {
		t.Skip("set AGENCY_LOCAL_E2E=1 to enable local CLI e2e tests")
	}

	repoRoot := repoRootFromCaller(t)
	agencyBin, _ := buildE2EBinaries(t, repoRoot)

	tmpDir := mustMkdirTemp(t, "agency-e2e-grammar-*")
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "config")
	cacheDir := filepath.Join(tmpDir, "cache")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))

	env := map[string]string{
		"AGENCY_DATA_DIR":     dataDir,
		"AGENCY_CONFIG_DIR":   configDir,
		"AGENCY_CACHE_DIR":    cacheDir,
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
	cwd := mustMkdirTemp(t, "agency-e2e-cwd-*")

	historyHelp := runAgencyCLI(t, agencyBin, cwd, env, "agent", "abc", "history", "--help")
	require.Equalf(t, 0, historyHelp.ExitCode, "agent history help failed\nstdout:\n%s\nstderr:\n%s", historyHelp.Stdout, historyHelp.Stderr)
	assert.Contains(t, historyHelp.Stdout, "agency agent <invocation-ref> history")
	assert.Contains(t, historyHelp.Stdout, "agency agent <invocation-ref> history logs")
	assert.Contains(t, historyHelp.Stdout, "history uses --json, --last, --limit, and --cursor")

	mergeHelp := runAgencyCLI(t, agencyBin, cwd, env, "worktree", "foo", "pr", "merge", "--help")
	require.Equalf(t, 0, mergeHelp.ExitCode, "worktree pr merge help failed\nstdout:\n%s\nstderr:\n%s", mergeHelp.Stdout, mergeHelp.Stderr)
	assert.Contains(t, mergeHelp.Stdout, "agency worktree <worktree-ref> pr merge")
	assert.Contains(t, mergeHelp.Stdout, "--agency-config string")
	assert.Contains(t, mergeHelp.Stdout, "--no-delete-branch")
	assert.Contains(t, mergeHelp.Stdout, "pr merge uses --squash, --merge, --rebase, --no-delete-branch, --yes, and --agency-config")

	landRequests, landDecodeErrors := startE2ELandDaemon(t, dataDir)
	land := runAgencyCLI(t, agencyBin, cwd, env,
		"agent", "inv-1", "land",
		"--repo", "repo-1",
		"--apply",
		"--require-base",
		"--json",
	)
	require.Equalf(t, 0, land.ExitCode, "agent land failed\nstdout:\n%s\nstderr:\n%s", land.Stdout, land.Stderr)
	assert.NotContains(t, land.Stderr, "unknown flag")

	var landResp agentMutationResponse
	require.NoError(t, json.Unmarshal([]byte(land.Stdout), &landResp), "invalid land json: %s", land.Stdout)
	assert.True(t, landResp.OK)
	assert.Equal(t, "inv-1", landResp.InvocationID)

	select {
	case req := <-landRequests:
		assert.True(t, req.Apply)
		assert.True(t, req.RequireBase)
	case <-time.After(3 * time.Second):
		t.Fatal("fake daemon did not receive land request")
	}
	select {
	case err := <-landDecodeErrors:
		require.NoError(t, err)
	default:
	}
}

func startE2ELandDaemon(t *testing.T, dataDir string) (<-chan daemon.LandRequest, <-chan error) {
	t.Helper()

	st := store.NewStore(agencyfs.NewRealFS(), dataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0o755))
	_ = os.Remove(socketPath)

	landRequests := make(chan daemon.LandRequest, 1)
	decodeErrors := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			_ = json.NewEncoder(w).Encode(daemon.HealthResponse{
				OK:         true,
				APIVersion: daemon.APIVersion,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/repo-1":
			_ = json.NewEncoder(w).Encode(daemon.APIResponse{
				OK:         true,
				APIVersion: daemon.APIVersion,
				RequestID:  "repo-req",
				Data: daemon.RepoDTO{
					RepoID:                  "repo-1",
					RepoName:                "repo",
					RepoKey:                 "github.com/test/repo",
					PreferredRootAccessible: true,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/invocations/inv-1/land" && r.URL.Query().Get("repo_id") == "repo-1":
			var req daemon.LandRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				decodeErrors <- err
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			landRequests <- req
			_ = json.NewEncoder(w).Encode(daemon.LandResponse{
				OK:                    true,
				APIVersion:            daemon.APIVersion,
				RequestID:             "land-req",
				InvocationID:          "inv-1",
				AppliedMode:           daemon.LandingModeApplyPatch,
				IntegrationHeadBefore: "1111111111111111111111111111111111111111",
				IntegrationHeadAfter:  "2222222222222222222222222222222222222222",
				CommitsLanded:         1,
			})
		default:
			http.NotFound(w, r)
		}
	})

	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	server := &http.Server{Handler: handler}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		err := <-done
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("fake daemon serve failed: %v", err)
		}
		_ = os.Remove(socketPath)
	})

	return landRequests, decodeErrors
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

	result, err := agencyexec.NewRealRunner().Run(ctx, "go", []string{"build", "-o", outputPath, target}, agencyexec.RunOpts{Dir: repoRoot})
	if ctx.Err() != nil {
		require.FailNowf(t, "go build timed out", "target=%s", target)
	}
	require.NoErrorf(t, err, "go build failed to start for %s", target)
	require.Equalf(t, 0, result.ExitCode, "go build failed for %s\nstdout:\n%s\nstderr:\n%s", target, result.Stdout, result.Stderr)
}

func runAgencyCLI(t *testing.T, agencyBin, cwd string, env map[string]string, args ...string) cliRunResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := agencyexec.NewRealRunner().Run(ctx, agencyBin, args, agencyexec.RunOpts{
		Dir: cwd,
		Env: env,
	})
	if ctx.Err() != nil {
		require.FailNowf(t, "agency CLI command timed out", "args=%v", args)
	}
	require.NoError(t, err)

	return cliRunResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}
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
		return []string{"-p", "--output-format", "stream-json", "--input-format", "text", "--verbose", runnerArg, "--permission-mode", "bypassPermissions", prompt}
	case "codex":
		return []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "exec", "--cd", sandboxPath, "--json", runnerArg, "--disable", "unified_exec", prompt}
	case "amp":
		return []string{"-x", "--stream-json", "--stream-json-input", runnerArg}
	case "opencode":
		return []string{"run", "--mode", "auto", runnerArg, prompt}
	case "cursor":
		return []string{"-p", "--output-format", "stream-json", "--force", "--workspace", sandboxPath, runnerArg, prompt}
	case "droid":
		return []string{"exec", "--output-format", "stream-json", "--input-format", "stream-json", runnerArg}
	default:
		return nil
	}
}

func writeE2EConfig(t *testing.T, configDir, fakeRunnerBin string) {
	t.Helper()

	cfg := map[string]any{
		"version": 4,
		"defaults": map[string]string{
			"runner":            "claude-code",
			"editor":            "code",
			"execution_profile": "personal",
		},
		"runners": map[string]string{
			"claude-code": fakeRunnerBin,
			"codex":       fakeRunnerBin,
			"amp":         fakeRunnerBin,
			"opencode":    fakeRunnerBin,
			"cursor":      fakeRunnerBin,
			"droid":       fakeRunnerBin,
		},
		"execution_profiles": map[string]any{
			"personal": map[string]any{
				"env": map[string]string{},
			},
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
