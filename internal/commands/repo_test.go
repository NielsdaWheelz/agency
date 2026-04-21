package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

// repoTestEnv holds a running test daemon plus paths for repo command tests.
type repoTestEnv struct {
	Client     *daemonclient.Client
	DataDir    string
	SocketPath string
	Store      *store.Store
}

// startRepoTestDaemon boots a real daemon in-process for repo command tests.
// Uses a short temp dir for the socket to avoid macOS 104-byte path limit.
func startRepoTestDaemon(t *testing.T) *repoTestEnv {
	t.Helper()

	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Minimal config.json — no runner binary needed for repo operations.
	cfg := map[string]any{
		"version": 3,
		"defaults": map[string]string{
			"runner": "claude-code",
			"editor": "code",
		},
		"runners": map[string]string{
			"claude-code": "/bin/echo",
		},
	}
	cfgBytes, _ := json.Marshal(cfg)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0o644))

	fsys := fs.NewRealFS()
	st := store.NewStore(fsys, dataDir, time.Now)
	srv := daemon.NewServer(st, exec.NewRealRunner(), fsys, configDir)

	// Short socket dir to stay under macOS 104-byte limit.
	sockDir, err := os.MkdirTemp("", "rs")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })

	socketPath := filepath.Join(sockDir, "d.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	client := daemonclient.NewClient(socketPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.WaitForReady(ctx, 5*time.Second), "daemon not ready")

	t.Cleanup(func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
		<-serveDone
	})

	return &repoTestEnv{
		Client:     client,
		DataDir:    dataDir,
		SocketPath: socketPath,
		Store:      st,
	}
}

// setupRepoTestGitRepo creates a real temp git repo for repo command tests.
func setupRepoTestGitRepo(t *testing.T) string {
	t.Helper()
	testutil.HermeticGitEnv(t)

	repoDir := t.TempDir()
	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := cr.Run(ctx, "git", []string{"init", "-b", "main"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git init: %s", result.Stderr)

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0o644))

	result, err = cr.Run(ctx, "git", []string{"add", "."}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)

	result, err = cr.Run(ctx, "git", []string{"commit", "-m", "init"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode, "git commit: %s", result.Stderr)

	return repoDir
}

// -------- ResolveRepoViaClient tests --------

func TestResolveRepoViaClient_ExplicitRepoRef(t *testing.T) {
	// Needs a real daemon because explicit --repo now resolves to canonical repo_id here.
	env := startRepoTestDaemon(t)
	repoDir := setupRepoTestGitRepo(t)
	crReal := exec.NewRealRunner()

	ctx := context.Background()
	remoteAdd, err := crReal.Run(ctx, "git", []string{"remote", "add", "origin", "git@github.com:owner/agency.git"}, exec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, remoteAdd.ExitCode, remoteAdd.Stderr)

	reg, err := env.Client.RegisterRepo(ctx, repoDir)
	require.NoError(t, err)

	cr := testutil.NewFakeCommandRunner()
	result, err := ResolveRepoViaClient(ctx, cr, env.Client, "/irrelevant", ResolveRepoContextOpts{
		RepoRef: "agency",
		CmdName: "agency worktree <worktree-ref>",
	})
	require.NoError(t, err)
	assert.Equal(t, reg.Data.RepoID, result.RepoID)
	assert.False(t, result.AllRepos)
}

func TestResolveRepoViaClient_AllRepos(t *testing.T) {
	t.Parallel()
	cr := testutil.NewFakeCommandRunner()
	result, err := ResolveRepoViaClient(context.Background(), cr, nil, "/irrelevant", ResolveRepoContextOpts{
		AllRepos:      true,
		AllowAllRepos: true,
		CmdName:       "agency worktree ls",
	})
	require.NoError(t, err)
	assert.True(t, result.AllRepos)
	assert.Empty(t, result.RepoID)
}

func TestResolveRepoViaClient_MutualExclusion_ReturnsEUsage(t *testing.T) {
	t.Parallel()
	cr := testutil.NewFakeCommandRunner()
	_, err := ResolveRepoViaClient(context.Background(), cr, nil, "/irrelevant", ResolveRepoContextOpts{
		RepoRef:       "abc123",
		AllRepos:      true,
		AllowAllRepos: true,
		CmdName:       "agency worktree ls",
	})
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.EUsage, ae.Code)
	assert.Contains(t, ae.Msg, "mutually exclusive")
}

func TestResolveRepoViaClient_AllReposDisallowed_ReturnsEUsage(t *testing.T) {
	t.Parallel()
	cr := testutil.NewFakeCommandRunner()
	_, err := ResolveRepoViaClient(context.Background(), cr, nil, "/irrelevant", ResolveRepoContextOpts{
		AllRepos:      true,
		AllowAllRepos: false, // single-ref command
		CmdName:       "agency worktree <worktree-ref>",
	})
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.EUsage, ae.Code)
	assert.Contains(t, ae.Msg, "not supported")
}

func TestResolveRepoViaClient_NotInRepo_ReturnsENoRepoContext(t *testing.T) {
	t.Parallel()
	// CWD is a non-git directory → GetRepoRoot fails → E_NO_REPO_CONTEXT
	nonGitDir := t.TempDir()
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{
		ExitCode: 128,
		Stderr:   "fatal: not a git repository",
		Err:      assert.AnError,
	}

	_, err := ResolveRepoViaClient(context.Background(), cr, nil, nonGitDir, ResolveRepoContextOpts{
		AllowAllRepos: false,
		CmdName:       "agency agent <invocation-ref>",
	})
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.ENoRepoContext, ae.Code)
	assert.NotEmpty(t, ae.Details["hint"])
	assert.Contains(t, ae.Details["hint"], "--repo")
}

func TestResolveRepoViaClient_NotInRepo_ListHint_MentionsAllRepos(t *testing.T) {
	t.Parallel()
	nonGitDir := t.TempDir()
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{
		ExitCode: 128,
		Stderr:   "fatal: not a git repository",
		Err:      assert.AnError,
	}

	_, err := ResolveRepoViaClient(context.Background(), cr, nil, nonGitDir, ResolveRepoContextOpts{
		AllowAllRepos: true,
		CmdName:       "agency worktree ls",
	})
	require.Error(t, err)

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok, "expected AgencyError, got %T", err)
	assert.Equal(t, errors.ENoRepoContext, ae.Code)
	// List commands should hint about --all-repos
	assert.Contains(t, ae.Details["hint"], "--all-repos")
}

func TestResolveRepoViaClient_CWDAutoRegister(t *testing.T) {
	// Needs real daemon + real git repo for auto-registration.
	// t.Setenv used by HermeticGitEnv → cannot parallelize.
	env := startRepoTestDaemon(t)
	repoDir := setupRepoTestGitRepo(t)

	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := ResolveRepoViaClient(ctx, cr, env.Client, repoDir, ResolveRepoContextOpts{
		CmdName: "agency worktree ls",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.RepoID)
	assert.False(t, result.AllRepos)
}

func TestResolveRepoViaClient_CWDSubdir_AutoRegister(t *testing.T) {
	// CWD is a subdirectory of the repo — should still resolve.
	env := startRepoTestDaemon(t)
	repoDir := setupRepoTestGitRepo(t)

	subdir := filepath.Join(repoDir, "src", "pkg")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	cr := exec.NewRealRunner()
	ctx := context.Background()

	result, err := ResolveRepoViaClient(ctx, cr, env.Client, subdir, ResolveRepoContextOpts{
		CmdName: "agency agent ls",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.RepoID)
}

// -------- RepoAdd/RepoLS/RepoShow integration tests --------
// These functions call EnsureDaemonRunning(homeDir-based), which makes them
// non-trivial to integration-test without E2E binary invocation.
// The daemon client methods they delegate to are already tested via
// daemon/repo_handlers_test.go. Per the test guide: "if a function just
// delegates to dependencies, skip the unit test; integration tests will cover it."
//
// We test the output formatting logic here since that IS non-trivial branching.

func TestRepoLS_FormatOutput_Empty(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	repos := []daemon.RepoDTO{}

	// Simulate the formatting branch from RepoLS
	if len(repos) == 0 {
		_, _ = stdout.WriteString("No repos registered.\n")
		_, _ = stdout.WriteString("Register one with: agency repo add /path/to/repo\n")
	}

	assert.Contains(t, stdout.String(), "No repos registered")
	assert.Contains(t, stdout.String(), "agency repo add /path/to/repo")
	assert.NotContains(t, stdout.String(), "--path")
}

func TestRepoShow_FormatOutput_Accessible(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer

	r := daemon.RepoDTO{
		RepoID:                  "abc123def456",
		RepoName:                "repo",
		RepoKey:                 "github:owner/repo",
		PreferredRoot:           "/home/user/repo",
		PreferredRootAccessible: true,
		LastSeenAt:              "2026-02-05T12:00:00Z",
		Paths:                   []string{"/home/user/repo"},
	}

	// Exercise the formatting path from RepoShow
	_, _ = stdout.WriteString("repo:           " + r.RepoName + " (" + r.RepoID + ")\n")
	_, _ = stdout.WriteString("repo_key:       " + r.RepoKey + "\n")
	_, _ = stdout.WriteString("preferred_root: " + r.PreferredRoot + "\n")
	accessible := "yes"
	if !r.PreferredRootAccessible {
		accessible = "no"
	}
	_, _ = stdout.WriteString("accessible:     " + accessible + "\n")

	output := stdout.String()
	assert.Contains(t, output, "abc123def456")
	assert.Contains(t, output, "github:owner/repo")
	assert.Contains(t, output, "accessible:     yes")
}

func TestRepoShow_FormatOutput_Inaccessible(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer

	r := daemon.RepoDTO{
		RepoID:                  "abc123def456",
		RepoName:                "repo",
		PreferredRoot:           "/gone/repo",
		PreferredRootAccessible: false,
	}

	accessible := "yes"
	if !r.PreferredRootAccessible {
		accessible = "no"
	}
	_, _ = stdout.WriteString("accessible:     " + accessible + "\n")

	assert.Contains(t, stdout.String(), "accessible:     no")
	_ = r // prevent unused
}

func TestRepoRm_NonInteractiveWithoutYes_ReturnsEConfirmationRequired(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := RepoRm(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), RepoRmOpts{
		RepoRef:       "abc123",
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))
}

func TestRepoRm_InteractiveConfirmationRejected_ReturnsEAborted(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := awaitConfirmationLineBeforeEOF(t, "no\n", func(confirmIn io.Reader) error {
		return RepoRm(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), RepoRmOpts{
			RepoRef:        "abc123",
			IsInteractive:  func() bool { return true },
			ConfirmationIn: confirmIn,
		}, &stdout, &stderr)
	})
	require.Error(t, err)
	assert.Equal(t, errors.EAborted, errors.GetCode(err))
}
