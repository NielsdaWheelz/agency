//go:build e2e

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestWorktreePRSyncMergeFailureMatrixE2E(t *testing.T) {
	ctx := context.Background()

	t.Run("merge_without_runner_status_still_checks_pr", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "pr-not-ready")

		branch := "agency/pr-not-ready-abcd"
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

		cr := newWorktreePRFailureMatrixRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeWorktreePRMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.ENoPR), payload["error_code"])
		assertWorktreePRHasRequestID(t, payload)
	})

	t.Run("missing_pr", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupWorktreePRMergeReadyInvocation(t, "missing-pr")
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

		cr := newWorktreePRFailureMatrixRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeWorktreePRMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.ENoPR), payload["error_code"])
		assertWorktreePRHasRequestID(t, payload)
	})

	t.Run("closed_pr", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupWorktreePRMergeReadyInvocation(t, "closed-pr")
		daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
		daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
		daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
		daemonRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
			Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
			ExitCode: 0,
		}
		daemonRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
			Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"CLOSED","isDraft":false,"mergeable":"MERGEABLE","headRefName":"` + branch + `"}`,
			ExitCode: 0,
		}

		cr := newWorktreePRFailureMatrixRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeWorktreePRMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EPRNotOpen), payload["error_code"])
		assertWorktreePRHasRequestID(t, payload)
	})

	t.Run("mergeability_failure", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupWorktreePRMergeReadyInvocation(t, "mergeability")
		daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
		daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
		daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
		daemonRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
			Stdout:   `[{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN"}]`,
			ExitCode: 0,
		}
		daemonRunner.Responses["gh pr view 77 -R test/agent-repo --json number,url,state,isDraft,mergeable,headRefName"] = testutil.FakeResponse{
			Stdout:   `{"number":77,"url":"https://github.com/test/agent-repo/pull/77","state":"OPEN","isDraft":false,"mergeable":"CONFLICTING","headRefName":"` + branch + `"}`,
			ExitCode: 0,
		}

		cr := newWorktreePRFailureMatrixRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeWorktreePRMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EPRNotMergeable), payload["error_code"])
		assertWorktreePRHasRequestID(t, payload)
	})

	t.Run("confirmation_failure", func(t *testing.T) {
		_, dataDir, repoID, _, _, fsys := setupAgentTestEnvShort(t, "confirmation-required")
		st := store.NewStore(fsys, dataDir, time.Now)
		client := daemonclient.NewClient(st.DaemonSocketPath())

		_, err := client.WorktreePRMerge(ctx, "wt-any", repoID, daemon.WorktreePRMergeRequest{
			Strategy:         "squash",
			ConfirmationMode: "yes",
			Confirmed:        false,
		})

		require.Error(t, err)
		assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))

		var dae *daemonclient.DaemonActionError
		require.True(t, stderrors.As(err, &dae), "expected daemon action error, got %v", err)

		var resp daemon.WorktreePRMergeResponse
		require.NoError(t, dae.DecodeResponse(&resp))
		assert.False(t, resp.OK)
		assert.Equal(t, string(errors.EConfirmationRequired), resp.ErrorCode)
		assert.NotEmpty(t, resp.RequestID)
	})

	t.Run("pr_sync_uses_default_body_when_runner_status_missing", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "missing-status-pr-body")

		integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
		prBodyPath := filepath.Join(integrationTree, ".agency", "tmp", "pr_body.md")

		branch := "agency/missing-status-pr-body-abcd"

		daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
		daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
		daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
		daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
		daemonRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
		daemonRunner.Responses["git rev-list --count main.."+branch] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
		daemonRunner.Responses["git push -u origin "+branch] = testutil.FakeResponse{ExitCode: 0}
		daemonRunner.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n", ExitCode: 0}
		daemonRunner.Responses["gh pr list --head test:"+branch+" --state all --json number,url,state"] = testutil.FakeResponse{
			Stdout:   `[{"number":81,"url":"https://github.com/test/agent-repo/pull/81","state":"OPEN"}]`,
			ExitCode: 0,
		}
		daemonRunner.Responses["gh pr edit 81 --body-file "+prBodyPath] = testutil.FakeResponse{ExitCode: 0}

		cr := newWorktreePRFailureMatrixRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRSync(ctx, cr, fsys, repoDir, WorktreePRSyncOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeWorktreePRMutationPayload(t, stdout.Bytes())
		assert.Equal(t, true, payload["ok"])
		assert.Equal(t, "updated", payload["pr_action"])
		assertWorktreePRHasRequestID(t, payload)

		prBody, readErr := os.ReadFile(prBodyPath)
		require.NoError(t, readErr)
		assert.Equal(t, "## summary\nSummary not provided.\n\n## how to test\nHow to test not provided.\n", string(prBody))
	})

	t.Run("merge_log_persistence_failure", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupWorktreePRMergeReadyInvocation(t, "merge-log-failure")
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

		mergeLogPath := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "logs", "merge.log")
		require.NoError(t, os.MkdirAll(mergeLogPath, 0o700))

		cr := newWorktreePRFailureMatrixRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoRef:         repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeWorktreePRMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EPersistFailed), payload["error_code"])
		assertWorktreePRHasRequestID(t, payload)
	})
}

func setupWorktreePRMergeReadyInvocation(
	t *testing.T,
	worktreeName string,
) (repoDir, dataDir, repoID, worktreeID, invocationID, branch string, daemonRunner *testutil.FakeCommandRunner, fsys fs.FS) {
	t.Helper()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys = setupAgentTestEnvShort(t, worktreeName)
	invocationID = ""

	writeWorktreeMergeScriptsAndConfig(t, repoDir)
	writeWorktreeMergeRepoRecord(t, dataDir, repoID, repoDir)

	branch = "agency/" + worktreeName + "-abcd"
	return repoDir, dataDir, repoID, worktreeID, invocationID, branch, daemonRunner, fsys
}

func newWorktreePRFailureMatrixRunner(repoDir string) *testutil.FakeCommandRunner {
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}
	return cr
}

func decodeWorktreePRMutationPayload(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	return payload
}

func assertWorktreePRHasRequestID(t *testing.T, payload map[string]any) {
	t.Helper()
	requestID, ok := payload["request_id"].(string)
	require.True(t, ok, "request_id must be present")
	assert.NotEmpty(t, strings.TrimSpace(requestID))
}
