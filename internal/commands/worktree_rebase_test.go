package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestWorktreeRebase_JSONSuccessIncludesIdentityFields(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "rebase-json")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git rebase origin/main"] = testutil.FakeResponse{ExitCode: 0}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreeRebase(context.Background(), cr, fsys, repoDir, WorktreeRebaseOpts{
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
	assert.Equal(t, "agency/rebase-json-abcd", payload["branch"])
	assert.NotEmpty(t, payload["request_id"])
	_, hasInvocationID := payload["invocation_id"]
	assert.False(t, hasInvocationID, "worktree rebase should not return invocation_id")
}

func TestWorktreeRebase_JSONFailureIncludesDaemonRequestID(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "rebase-json-failure")

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := WorktreeRebase(context.Background(), cr, fsys, repoDir, WorktreeRebaseOpts{
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
