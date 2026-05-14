package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

type blockingFakeRunner struct {
	*testutil.FakeCommandRunner

	blockKey string

	startedOnce sync.Once
	started     chan struct{}

	releaseOnce sync.Once
	release     chan struct{}

	mu          sync.Mutex
	sawDeadline bool
}

func newBlockingFakeRunner(blockKey string) *blockingFakeRunner {
	return &blockingFakeRunner{
		FakeCommandRunner: testutil.NewFakeCommandRunner(),
		blockKey:          blockKey,
		started:           make(chan struct{}),
		release:           make(chan struct{}),
	}
}

func (r *blockingFakeRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}

	if key == r.blockKey {
		r.mu.Lock()
		r.sawDeadline = r.sawDeadline || deadlineSet(ctx)
		r.Calls = append(r.Calls, key)
		r.mu.Unlock()

		r.startedOnce.Do(func() { close(r.started) })
		<-r.release
		return exec.CmdResult{}, context.DeadlineExceeded
	}

	return r.FakeCommandRunner.Run(ctx, name, args, opts)
}

func (r *blockingFakeRunner) Release() {
	r.releaseOnce.Do(func() {
		close(r.release)
	})
}

func (r *blockingFakeRunner) SawDeadline() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sawDeadline
}

func deadlineSet(ctx context.Context) bool {
	_, ok := ctx.Deadline()
	return ok
}

func TestHandleWorktreeRm_BlocksGitRemoveWithDeadline(t *testing.T) {
	t.Parallel()

	env := setupWorktreeRmTimeoutEnv(t)
	runner := newBlockingFakeRunner(env.removeCmd)
	env.server.Runner = runner
	t.Cleanup(runner.Release)

	reqBody := []byte(`{"force":true}`)
	req := httptest.NewRequest(http.MethodPost, "/worktrees/"+env.worktreeID+"/rm?repo_id="+env.repoID, strings.NewReader(string(reqBody)))
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		env.server.handleWorktreeRm(w, req, env.worktreeID)
		close(done)
	}()

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("git worktree remove was not invoked")
	}

	require.True(t, runner.SawDeadline(), "expected git worktree remove context to carry a deadline")
	assert.Contains(t, runner.Calls, env.removeCmd)

	runner.Release()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worktree rm handler did not return after releasing blocked remove")
	}

	require.Equal(t, http.StatusInternalServerError, w.Code)

	var resp WorktreeRmResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.OK)
	assert.Equal(t, string(errors.EWorktreeRemoveFailed), resp.ErrorCode)
}

func TestHandleWorktreeMerge_BlocksArchiveCleanupRemoveWithDeadline(t *testing.T) {
	t.Parallel()

	env := setupReadTestEnv(t)
	fakeRunner := testutil.NewFakeCommandRunner()
	env.Server.Runner = fakeRunner

	workspaceRoot, repoRoot, worktreeID := setupWorktreeMergeReadyState(t, env, "")
	canonicalRepoRoot := canonicalTestPath(t, repoRoot)
	setWorktreeMergeHappyRunnerResponses(fakeRunner, "agency/alpha", canonicalRepoRoot, workspaceRoot)

	removeCmd := "git -C " + canonicalRepoRoot + " worktree remove --force " + workspaceRoot
	runner := newBlockingFakeRunner(removeCmd)
	runner.Responses = fakeRunner.Responses
	env.Server.Runner = runner
	t.Cleanup(runner.Release)

	req := httptest.NewRequest(
		http.MethodPost,
		"/worktrees/"+worktreeID+"/pr/merge?repo_id="+env.RepoID,
		strings.NewReader(`{"strategy":"squash","confirmation_mode":"yes","confirmed":true}`),
	)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		env.Server.handleWorktreePRMerge(w, req, worktreeID)
		close(done)
	}()

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("git worktree remove was not invoked during merge cleanup")
	}

	require.True(t, runner.SawDeadline(), "expected merge cleanup git worktree remove context to carry a deadline")
	assert.Contains(t, runner.Calls, filepath.Join(canonicalRepoRoot, "scripts", "archive.sh"))
	assert.Contains(t, runner.Calls, removeCmd)

	runner.Release()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worktree merge handler did not return after releasing blocked remove")
	}

	requireStartedWorktreeMergeResponse(t, w, env.RepoID, worktreeID)

	mergeMeta := requireEventuallyWorktreeMergeMeta(t, env, worktreeID, func(meta *store.IntegrationWorktreeMergeMeta) bool {
		return meta != nil && meta.Status == store.WorktreeMergeStatusFailed
	})
	assert.Equal(t, store.WorktreeMergeStageArchive, mergeMeta.Stage)
	assert.Equal(t, string(errors.EArchiveFailed), mergeMeta.ErrorCode)
}

type worktreeRmTimeoutEnv struct {
	server     *Server
	repoID     string
	worktreeID string
	removeCmd  string
}

func setupWorktreeRmTimeoutEnv(t *testing.T) *worktreeRmTimeoutEnv {
	t.Helper()

	dataDir := t.TempDir()
	configDir := t.TempDir()
	writeTestUserConfig(t, configDir)
	now := time.Now().UTC()
	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	srv := NewServer(st, exec.NewRealRunner(), fs.NewRealFS(), configDir)

	repoID := "repo-timeout"
	repoRoot := filepath.Join(t.TempDir(), "repo-root")
	worktreeID := "wt-timeout"
	treePath := filepath.Join(t.TempDir(), "integration-tree")

	require.NoError(t, os.MkdirAll(repoRoot, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(treePath, ".agency"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(treePath, ".agency", "INTEGRATION_MARKER"), []byte("integration\n"), 0o644))

	idx := store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			repoID: {
				RepoID:     repoID,
				Paths:      []string{repoRoot},
				LastSeenAt: now.Format(time.RFC3339),
			},
		},
	}
	require.NoError(t, st.SaveRepoIndex(idx))

	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "github:example/repo",
		RepoID:           repoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    true,
		OriginURL:        "git@github.com:example/repo.git",
		OriginHost:       "github.com",
		Capabilities: store.Capabilities{
			GitHubOrigin: true,
			OriginHost:   "github.com",
			GhAuthed:     true,
		},
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
	}))

	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoID, worktreeID, store.NewIntegrationWorktreeMeta(
		worktreeID,
		"timeout",
		repoID,
		"agency/timeout",
		"main",
		treePath,
		filepath.Join(repoRoot, ".agency", "checkouts", repoID),
		"work",
		now,
	)))

	return &worktreeRmTimeoutEnv{
		server:     srv,
		repoID:     repoID,
		worktreeID: worktreeID,
		removeCmd:  "git -C " + canonicalTestPath(t, repoRoot) + " worktree remove --force " + treePath,
	}
}
