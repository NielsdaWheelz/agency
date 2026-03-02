package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
)

func TestAgentPRSync_JSONCreatedOutcomeIncludesIdentityFields(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-json")
	invocationID := "20260302190000-prsync1"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFinished)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
	}))

	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	require.NoError(t, os.MkdirAll(filepath.Join(integrationTree, ".agency"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(integrationTree, ".agency", "report.md"), []byte(
		"## summary\nlanded invocation changes\n\n## how to test\ngo test ./...\n",
	), 0o644))

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git rev-list --count main..agency/prsync-json-abcd"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	daemonRunner.Responses["git push -u origin agency/prsync-json-abcd"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:agency/prsync-json-abcd --state all --json number,url,state"] = testutil.FakeResponse{Stdout: "[]", ExitCode: 0}
	daemonRunner.Responses["gh pr create --base main --head agency/prsync-json-abcd --title [agency] prsync-json --body-file "+filepath.Join(integrationTree, ".agency", "report.md")] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh pr list --head agency/prsync-json-abcd --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":42,"url":"https://github.com/test/agent-repo/pull/42","state":"OPEN"}]`,
		ExitCode: 0,
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentPRSync(context.Background(), cr, fsys, repoDir, AgentPRSyncOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, invocationID, payload["invocation_id"])
	assert.Equal(t, repoID, payload["repo_id"])
	assert.Equal(t, worktreeID, payload["integration_worktree_id"])
	assert.Equal(t, "agency/prsync-json-abcd", payload["branch"])
	assert.Equal(t, "created", payload["pr_action"])
	assert.Equal(t, "https://github.com/test/agent-repo/pull/42", payload["pr_url"])
}

func TestAgentPRSync_DirtyWorktreeRejectedWithoutAllowDirty(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-dirty")
	invocationID := "20260302191000-prsync2"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFinished)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
	}))

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{
		Stdout: " M README.md\n",
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	err := AgentPRSync(context.Background(), cr, fsys, repoDir, AgentPRSyncOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		DataDirOverride: dataDir,
	}, ioDiscard{}, ioDiscard{})
	require.Error(t, err)
	assert.Equal(t, errors.EDirtyWorktree, errors.GetCode(err))
}

func TestAgentPRSync_NonFastForwardReturnsForceWithLeaseHint(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-ff")
	invocationID := "20260302192000-prsync3"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFinished)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
	}))

	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	require.NoError(t, os.MkdirAll(filepath.Join(integrationTree, ".agency"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(integrationTree, ".agency", "report.md"), []byte(
		"## summary\nfast-forward rejection case\n\n## how to test\ngo test ./...\n",
	), 0o644))

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
	err := AgentPRSync(context.Background(), cr, fsys, repoDir, AgentPRSyncOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EGitPushFailed, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "--force-with-lease")
}

func TestAgentPRSync_ForceWithLeaseUsesPushPolicy(t *testing.T) {
	t.Parallel()

	repoDir, dataDir, repoID, worktreeID, daemonRunner, fsys := setupAgentTestEnvShort(t, "prsync-fwl")
	invocationID := "20260302193000-prsync4"
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusFinished)

	st := store.NewStore(fsys, dataDir, time.Now)
	require.NoError(t, st.UpdateInvocationMeta(repoID, invocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFinished
		meta.LandingStatus = store.LandingStatusLanded
	}))

	integrationTree := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", worktreeID, "tree")
	require.NoError(t, os.MkdirAll(filepath.Join(integrationTree, ".agency"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(integrationTree, ".agency", "report.md"), []byte(
		"## summary\nforce-with-lease case\n\n## how to test\ngo test ./...\n",
	), 0o644))

	daemonRunner.Responses["git status --porcelain --untracked-files=all"] = testutil.FakeResponse{Stdout: "", ExitCode: 0}
	daemonRunner.Responses["gh --version"] = testutil.FakeResponse{Stdout: "gh version 2.0.0\n", ExitCode: 0}
	daemonRunner.Responses["gh auth status"] = testutil.FakeResponse{Stdout: "ok\n", ExitCode: 0}
	daemonRunner.Responses["git fetch origin"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git show-ref --verify --quiet refs/heads/main"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["git rev-list --count main..agency/prsync-fwl-abcd"] = testutil.FakeResponse{Stdout: "1\n", ExitCode: 0}
	daemonRunner.Responses["git push --force-with-lease -u origin agency/prsync-fwl-abcd"] = testutil.FakeResponse{ExitCode: 0}
	daemonRunner.Responses["gh pr list --head test:agency/prsync-fwl-abcd --state all --json number,url,state"] = testutil.FakeResponse{
		Stdout:   `[{"number":43,"url":"https://github.com/test/agent-repo/pull/43","state":"OPEN"}]`,
		ExitCode: 0,
	}
	daemonRunner.Responses["gh pr edit 43 --body-file "+filepath.Join(integrationTree, ".agency", "report.md")] = testutil.FakeResponse{
		ExitCode: 0,
	}

	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: "git@github.com:test/agent-repo.git\n"}

	var stdout, stderr bytes.Buffer
	err := AgentPRSync(context.Background(), cr, fsys, repoDir, AgentPRSyncOpts{
		InvocationRef:   invocationID,
		RepoFlag:        repoID,
		ForceWithLease:  true,
		DataDirOverride: dataDir,
	}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Contains(t, daemonRunner.Calls, "git push --force-with-lease -u origin agency/prsync-fwl-abcd")
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
