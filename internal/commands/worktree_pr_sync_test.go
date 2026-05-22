package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestWorktreePRSync_JSONCreatedOutcomeIncludesIdentityFields(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-json")

	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	stateDir := filepath.Join(integrationTree, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), []byte(`{
  "schema_version": "2.0",
  "state": "succeeded",
  "updated_at": "2026-02-05T12:00:00Z",
  "summary": "landed invocation changes",
  "questions": [],
  "how_to_test": "go test ./..."
}`), 0o644))
	prBodyPath := filepath.Join(integrationTree, ".agency", "tmp", "pr_body.md")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git rev-list --count main..agency/prsync-json-abcd"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	daemonRunner.Responses["git push -u origin agency/prsync-json-abcd"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:agency/prsync-json-abcd --state all --json number,url,state"] = testutil.FakeResponse{Stdout: "[]", ExitCode: 0}
	daemonRunner.Responses["gh pr create --base main --head agency/prsync-json-abcd --title [agency] prsync-json --body-file "+prBodyPath] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh pr list --head agency/prsync-json-abcd --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":42,"url":"https://github.com/test/agent-repo/pull/42","state":"OPEN"}]`,
		ExitCode: 0,
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreePRSync(context.Background(), cr, fsys, repoDir, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, repoID, payload["repo_id"])
	assert.Equal(t, worktreeID, payload["integration_worktree_id"])
	assert.Equal(t, "agency/prsync-json-abcd", payload["branch"])
	assert.Equal(t, "created", payload["pr_action"])
	assert.Equal(t, "https://github.com/test/agent-repo/pull/42", payload["pr_url"])
	assert.NotEmpty(t, payload["request_id"])
	_, hasInvocationID := payload["invocation_id"]
	assert.False(t, hasInvocationID, "worktree pr sync should not return invocation_id")

	prBody, err := os.ReadFile(prBodyPath)
	require.NoError(t, err)
	assert.Equal(t, "## summary\nlanded invocation changes\n\n## how to test\ngo test ./...\n", string(prBody))
}

func TestWorktreePRSync_DirtyWorktreeRejectedWithoutAllowDirty(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-dirty")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	err := WorktreePRSync(context.Background(), cr, fsys, repoDir, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		DataDirOverride: dataDir,
	}, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Equal(t, errors.EDirtyWorktree, errors.GetCode(err))
}

func TestWorktreePRSync_JSONFailureIncludesDaemonRequestID(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-json-failure")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreePRSync(context.Background(), cr, fsys, repoDir, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "json mode should emit deterministic envelope for daemon-declared failure")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, string(errors.EDirtyWorktree), payload["error_code"])
	assert.NotEmpty(t, payload["request_id"])
}

func TestWorktreePRSync_NonFastForwardReturnsForceWithLeaseHint(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-ff")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git rev-list --count main..agency/prsync-ff-abcd"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	daemonRunner.Responses["git push -u origin agency/prsync-ff-abcd"] = testutil.FakeResponse{
		ExitCode: 1,
		Stderr:   "! [rejected]        agency/prsync-ff-abcd -> agency/prsync-ff-abcd (non-fast-forward)",
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreePRSync(context.Background(), cr, fsys, repoDir, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EGitPushFailed, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "--force-with-lease")
}

func TestWorktreePRSync_ForceWithLeaseUsesPushPolicy(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-fwl")
	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	prBodyPath := filepath.Join(integrationTree, ".agency", "tmp", "pr_body.md")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git rev-list --count main..agency/prsync-fwl-abcd"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	daemonRunner.Responses["git push -u origin agency/prsync-fwl-abcd"] = testutil.FakeResponse{ExitCode: 1, Stderr: "rejected non-fast-forward"}
	daemonRunner.Responses["git push --force-with-lease -u origin agency/prsync-fwl-abcd"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:agency/prsync-fwl-abcd --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":43,"url":"https://github.com/test/agent-repo/pull/43","state":"OPEN"}]`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr edit 43 --body-file "+prBodyPath] = testutil.FakeResponse{
		ExitCode: 0,
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreePRSync(context.Background(), cr, fsys, repoDir, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		ForceWithLease:  true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	prBody, readErr := os.ReadFile(prBodyPath)
	require.NoError(t, readErr)
	assert.Equal(t, "## summary\nSummary not provided.\n\n## how to test\nHow to test not provided.\n", string(prBody))
}
