package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestWorktreePRMerge_NonInteractiveRequiresYes(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "merge-noninteractive")

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	err := WorktreePRMerge(context.Background(), cr, fsys, repoDir, WorktreePRMergeOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		DataDirOverride: dataDir,
		IsInteractive:   func() bool { return false },
	}, ioDiscard{}, ioDiscard{})
	require.Error(t, err)
	assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))
}

func TestWorktreePRMerge_InteractiveConfirmationRejected(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "merge-confirm-reject")

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	err := awaitConfirmationLineBeforeEOF(t, "nope\n", func(confirmIn io.Reader) error {
		return WorktreePRMerge(context.Background(), cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			DataDirOverride: dataDir,
			IsInteractive:   func() bool { return true },
			ConfirmationIn:  confirmIn,
		}, ioDiscard{}, ioDiscard{})
	})
	require.Error(t, err)
	assert.Equal(t, errors.EAborted, errors.GetCode(err))
}

func TestWorktreePRMerge_InteractiveConfirmationTooLarge(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "merge-confirm-too-large")
	cr := testutil.NewFakeCommandRunner()

	longToken := strings.Repeat("x", maxConfirmationBytes+1) + "\n"
	err := WorktreePRMerge(context.Background(), cr, fsys, repoDir, WorktreePRMergeOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		DataDirOverride: dataDir,
		IsInteractive:   func() bool { return true },
		ConfirmationIn:  strings.NewReader(longToken),
	}, ioDiscard{}, ioDiscard{})
	require.Error(t, err)
	assert.Equal(t, errors.EInvalidArgument, errors.GetCode(err))
}

func TestWorktreePRMerge_JSONSuccessIncludesDurableMergeFields(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "merge-json")
	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")

	writeWorktreeMergeScriptsAndConfig(t, repoDir)
	writeWorktreeMergeRepoRecord(t, dataDir, repoID, repoDir)

	branch := "agency/merge-json-abcd"
	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
		Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefName":"` + branch + `"}`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr merge 77 -R test/agent-repo --squash --delete-branch"] = testutil.FakeResponse{
		Stdout:   "merged",
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr view 77 -R test/agent-repo --json state"] = testutil.FakeResponse{
		Stdout:   `{"state":"MERGED"}`,
		ExitCode: 0,
	}
	canonicalRepoDir := canonicalPathForTest(t, repoDir)
	archiveScript := filepath.Join(canonicalRepoDir, "scripts", "archive.sh")
	daemonRunner.Responses[archiveScript] = testutil.FakeResponse{Stdout: "archived\n", ExitCode: 0}
	daemonRunner.Responses["git -C "+canonicalRepoDir+" worktree remove --force "+integrationTree] = testutil.FakeResponse{
		ExitCode: 0,
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreePRMerge(context.Background(), cr, fsys, repoDir, WorktreePRMergeOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		Yes:             true,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, repoID, payload["repo_id"])
	assert.Equal(t, worktreeID, payload["integration_worktree_id"])
	assert.Equal(t, branch, payload["branch"])
	assert.Equal(t, float64(77), payload["pr_number"])
	assert.Equal(t, "https://github.com/test/agent-repo/pull/77", payload["pr_url"])
	assert.Equal(t, "squash", payload["strategy"])
	assert.Equal(t, true, payload["delete_branch"])
	assert.NotEmpty(t, payload["merge_log_path"])
	assert.NotEmpty(t, payload["verify_log_path"])
	assert.NotEmpty(t, payload["archive_log_path"])
	assert.Empty(t, stderr.String())
	assert.NotEmpty(t, payload["request_id"])
	archiveLogBytes, err := os.ReadFile(payload["archive_log_path"].(string))
	require.NoError(t, err)
	assert.Contains(t, string(archiveLogBytes), "=== "+archiveScript+" ===")
	assert.Contains(t, string(archiveLogBytes), "=== git -C "+canonicalRepoDir+" worktree remove --force "+integrationTree+" ===")
	canonicalRepoDir = canonicalPathForTest(t, repoDir)
	require.Contains(t, daemonRunner.Calls, "git -C "+canonicalRepoDir+" worktree remove --force "+integrationTree)
	_, hasInvocationID := payload["invocation_id"]
	assert.False(t, hasInvocationID, "worktree pr merge should not return invocation_id")
}

func TestWorktreePRMerge_HumanModeAttachesAndPrintsDurableProgress(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "merge-human-attach")
	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	startedPath := filepath.Join(integrationTree, ".agency", "out", "verify-started")
	releasePath := filepath.Join(integrationTree, ".agency", "out", "verify-release")

	verifyScript := `#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$PWD/.agency/out"
touch "$PWD/.agency/out/verify-started"
while [ ! -f "$PWD/.agency/out/verify-release" ]; do
  sleep 0.05
done
exit 0
`
	writeWorktreeMergeScriptsAndConfigWithVerifyScript(t, repoDir, verifyScript)
	writeWorktreeMergeRepoRecord(t, dataDir, repoID, repoDir)

	branch := "agency/merge-human-attach-abcd"
	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
		Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","headRefName":"` + branch + `"}`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr merge 77 -R test/agent-repo --squash --delete-branch"] = testutil.FakeResponse{
		Stdout:   "merged",
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr view 77 -R test/agent-repo --json state"] = testutil.FakeResponse{
		Stdout:   `{"state":"MERGED"}`,
		ExitCode: 0,
	}
	canonicalRepoDir := canonicalPathForTest(t, repoDir)
	archiveScript := filepath.Join(canonicalRepoDir, "scripts", "archive.sh")
	daemonRunner.Responses[archiveScript] = testutil.FakeResponse{Stdout: "archived\n", ExitCode: 0}
	daemonRunner.Responses["git -C "+canonicalRepoDir+" worktree remove --force "+integrationTree] = testutil.FakeResponse{
		ExitCode: 0,
	}

	client := daemonclient.NewClient(store.NewStore(fsys, dataDir, time.Now).DaemonSocketPath())
	startResp, err := client.WorktreePRMerge(context.Background(), worktreeID, repoID, daemonclient.WorktreePRMergeOpts{
		Strategy:         "squash",
		ConfirmationMode: "yes",
		Confirmed:        true,
	})
	require.NoError(t, err)
	require.True(t, startResp.OK)
	assert.Equal(t, "started", startResp.Action)
	assert.Equal(t, repoID, startResp.RepoID)
	assert.Equal(t, worktreeID, startResp.IntegrationWorktreeID)
	assert.NotEmpty(t, startResp.RequestID)
	require.NotNil(t, startResp.Merge)
	assert.Equal(t, startResp.RequestID, startResp.Merge.RequestID)
	assert.Equal(t, "running", startResp.Merge.State)
	assert.Equal(t, "preflight", startResp.Merge.Stage)
	assert.Equal(t, "preparing merge", startResp.Merge.StatusSummary)
	assert.Equal(t, branch, startResp.Merge.Branch)
	assert.Equal(t, "squash", startResp.Merge.Strategy)
	assert.True(t, startResp.Merge.DeleteBranch)
	assert.NotEmpty(t, startResp.Merge.AttemptID)
	assert.NotEmpty(t, startResp.Merge.MergeLogPath)
	assert.NotEmpty(t, startResp.Merge.VerifyLogPath)
	assert.NotEmpty(t, startResp.Merge.ArchiveLogPath)

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(startedPath)
		return statErr == nil
	}, 5*time.Second, 25*time.Millisecond)

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr lockedBuffer
	done := make(chan error, 1)
	go func() {
		done <- WorktreePRMerge(context.Background(), cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(stderr.String(), "merge: running verify")
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, os.WriteFile(releasePath, []byte("release\n"), 0o644))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attached merge command to finish")
	}

	finalMerge, err := client.GetWorktreeMerge(context.Background(), worktreeID, repoID)
	require.NoError(t, err)
	assert.Equal(t, startResp.Merge.AttemptID, finalMerge.Data.AttemptID)
	assert.Equal(t, startResp.RequestID, finalMerge.Data.RequestID)
	assert.Equal(t, "succeeded", finalMerge.Data.State)
	assert.Equal(t, "completed", finalMerge.Data.Stage)
	assert.Equal(t, "merge complete", finalMerge.Data.StatusSummary)

	assert.Contains(t, stderr.String(), "merge: running verify")
	assert.Contains(t, stderr.String(), "merge: merge complete")
	assert.Contains(t, stdout.String(), "worktree pr merge complete")
	assert.Contains(t, stdout.String(), "  worktree_id:     "+worktreeID)
	assert.Contains(t, stdout.String(), "  branch:          "+branch)
	assert.Contains(t, stdout.String(), "  strategy:        squash")
	assert.Contains(t, stdout.String(), "  pr_url:          https://github.com/test/agent-repo/pull/77")
	assert.Contains(t, stdout.String(), "  merge_log:       "+startResp.Merge.MergeLogPath)
	assert.Contains(t, stdout.String(), "  archive_log:     "+startResp.Merge.ArchiveLogPath)
	assert.Contains(t, stdout.String(), "  state:           archived")
}

func TestWorktreePRMerge_JSONFailureIncludesDaemonRequestID(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "merge-json-failure")

	writeWorktreeMergeScriptsAndConfig(t, repoDir)
	writeWorktreeMergeRepoRecord(t, dataDir, repoID, repoDir)

	branch := "agency/merge-json-failure-abcd"
	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[]`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr list --head "+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[]`,
		ExitCode: 0,
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreePRMerge(context.Background(), cr, fsys, repoDir, WorktreePRMergeOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		Yes:             true,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "json mode should emit deterministic envelope for daemon-declared failure")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.ENoPR), payload["error_code"])
	assert.Empty(t, stderr.String())
	assert.NotEmpty(t, payload["request_id"])
}

func writeWorktreeMergeScriptsAndConfig(t *testing.T, integrationTree string) {
	t.Helper()
	writeWorktreeMergeScriptsAndConfigWithVerifyScript(t, integrationTree, "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
}

func writeWorktreeMergeScriptsAndConfigWithVerifyScript(t *testing.T, integrationTree, verifyScript string) {
	t.Helper()

	scriptsDir := filepath.Join(integrationTree, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "setup.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "verify.sh"), []byte(verifyScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "archive.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755))

	agencyJSON := `{
  "version": 4,
  "scripts": {
    "setup": {
      "path": "scripts/setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/archive.sh",
      "timeout": "5m"
    }
  },
  "execution": {
    "profile": "personal",
    "checkout_root": "repo-sibling"
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(integrationTree, "agency.json"), []byte(agencyJSON), 0o644))
}

func writeWorktreeMergeRepoRecord(t *testing.T, dataDir, repoID, repoRoot string) {
	t.Helper()

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	record := store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "test/agent-repo",
		RepoID:           repoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    true,
		OriginURL:        "git@github.com:test/agent-repo.git",
		OriginHost:       "github.com",
		Capabilities: store.Capabilities{
			GitHubOrigin: true,
			OriginHost:   "github.com",
			GhAuthed:     true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, st.SaveRepoRecord(record))
}

func canonicalPathForTest(t *testing.T, path string) string {
	t.Helper()

	canonical := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonical = resolved
	}
	return canonical
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
