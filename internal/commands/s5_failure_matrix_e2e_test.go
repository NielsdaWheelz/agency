//go:build e2e

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestS5E2EWorktreePRSyncMergeFailureMatrix(t *testing.T) {
	ctx := context.Background()

	t.Run("not_ready_invocation", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, fsys := setupAgentTestEnvShort(t, "s5-not-ready")

		cr := newS5E2ECommandRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoFlag:        repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeS5E2EMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.ENoPR), payload["error_code"])
		assertS5E2EHasRequestID(t, payload)
	})

	t.Run("missing_pr", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupS5E2EMergeReadyInvocation(t, "s5-missing-pr")
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

		cr := newS5E2ECommandRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoFlag:        repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeS5E2EMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.ENoPR), payload["error_code"])
		assertS5E2EHasRequestID(t, payload)
	})

	t.Run("closed_pr", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupS5E2EMergeReadyInvocation(t, "s5-closed-pr")
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

		cr := newS5E2ECommandRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoFlag:        repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeS5E2EMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EPRNotOpen), payload["error_code"])
		assertS5E2EHasRequestID(t, payload)
	})

	t.Run("mergeability_failure", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupS5E2EMergeReadyInvocation(t, "s5-mergeability")
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

		cr := newS5E2ECommandRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoFlag:        repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeS5E2EMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EPRNotMergeable), payload["error_code"])
		assertS5E2EHasRequestID(t, payload)
	})

	t.Run("confirmation_failure", func(t *testing.T) {
		_, dataDir, repoID, _, _, fsys := setupAgentTestEnvShort(t, "s5-confirmation")
		st := store.NewStore(fsys, dataDir, time.Now)
		client := daemonclient.NewClient(st.DaemonSocketPath())

		resp, err := client.WorktreePRMerge(ctx, "wt-any", repoID, daemonclient.WorktreePRMergeOpts{
			Strategy:         "squash",
			ConfirmationMode: "yes",
			Confirmed:        false,
		})
		require.NoError(t, err)
		assert.False(t, resp.OK)
		assert.Equal(t, string(errors.EConfirmationRequired), resp.ErrorCode)
		assert.NotEmpty(t, resp.RequestID)
	})

	t.Run("bounded_input_handling", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "s5-bounded-input")

		integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
		reportDir := filepath.Join(integrationTree, ".agency")
		require.NoError(t, os.MkdirAll(reportDir, 0o755))
		oversized := "## summary\n" + strings.Repeat("x", 2*1024*1024) + "\n\n## how to test\n- go test ./...\n"
		require.NoError(t, os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(oversized), 0o644))

		branch := "agency/s5-bounded-input-abcd"

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

		cr := newS5E2ECommandRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRSync(ctx, cr, fsys, repoDir, WorktreePRSyncOpts{
			WorktreeRef:     worktreeID,
			RepoFlag:        repoID,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeS5E2EMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EReportOversized), payload["error_code"])
		assertS5E2EHasRequestID(t, payload)
		assert.NotContains(t, daemonRunner.Calls, "gh pr edit 81 --body-file")
	})

	t.Run("merge_log_persistence_failure", func(t *testing.T) {
		repoDir, dataDir, repoID, worktreeID, _, branch, daemonRunner, fsys := setupS5E2EMergeReadyInvocation(t, "s5-merge-log-failure")
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

		cr := newS5E2ECommandRunner(repoDir)
		var stdout, stderr bytes.Buffer
		err := WorktreePRMerge(ctx, cr, fsys, repoDir, WorktreePRMergeOpts{
			WorktreeRef:     worktreeID,
			RepoFlag:        repoID,
			Yes:             true,
			JSON:            true,
			DataDirOverride: dataDir,
		}, &stdout, &stderr)
		require.NoError(t, err)

		payload := decodeS5E2EMutationPayload(t, stdout.Bytes())
		assert.Equal(t, false, payload["ok"])
		assert.Equal(t, string(errors.EPersistFailed), payload["error_code"])
		assertS5E2EHasRequestID(t, payload)
	})
}

func setupS5E2EMergeReadyInvocation(
	t *testing.T,
	worktreeName string,
) (repoDir, dataDir, repoID, worktreeID, invocationID, branch string, daemonRunner *testutil.FakeCommandRunner, fsys fs.FS) {
	t.Helper()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys = setupAgentTestEnvShort(t, worktreeName)
	invocationID = ""

	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	reportDir := filepath.Join(integrationTree, ".agency")
	require.NoError(t, os.MkdirAll(reportDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(
		"## summary\nmerge-ready report\n\n## how to test\ngo test ./...\n",
	), 0o644))
	writeWorktreeMergeScriptsAndConfig(t, integrationTree)
	writeWorktreeMergeRepoRecord(t, dataDir, repoID, repoDir)

	branch = "agency/" + worktreeName + "-abcd"
	return repoDir, dataDir, repoID, worktreeID, invocationID, branch, daemonRunner, fsys
}

func newS5E2ECommandRunner(repoDir string) *testutil.FakeCommandRunner {
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}
	return cr
}

func decodeS5E2EMutationPayload(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	return payload
}

func assertS5E2EHasRequestID(t *testing.T, payload map[string]any) {
	t.Helper()
	requestID, ok := payload["request_id"].(string)
	require.True(t, ok, "request_id must be present")
	assert.NotEmpty(t, strings.TrimSpace(requestID))
}
