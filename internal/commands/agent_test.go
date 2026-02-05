// Package commands implements agency CLI commands.
// This file tests agent commands for headed execution (Slice 8 PR-03).
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAgentTestEnv creates a test environment with integration worktree for agent tests.
func setupAgentTestEnv(t *testing.T, worktreeName string) (string, string, string, string, *testutil.FakeCommandRunner, fs.FS) {
	t.Helper()

	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	// Initialize git repo (minimal)
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".git"), 0755))

	originURL := "git@github.com:test/agent-repo.git"
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	// Create fake command runner
	cr := testutil.NewFakeCommandRunner()
	cr.Responses["git rev-parse --show-toplevel"] = testutil.FakeResponse{Stdout: repoDir + "\n"}
	cr.Responses["git config --get remote.origin.url"] = testutil.FakeResponse{Stdout: originURL + "\n"}

	fsys := fs.NewRealFS()

	// Create store directories
	repoStoreDir := filepath.Join(dataDir, "repos", repoID)
	require.NoError(t, os.MkdirAll(repoStoreDir, 0755))

	// Create integration worktree
	worktreeID := "20260131120000-abcd"
	worktreeDir := filepath.Join(repoStoreDir, "integration_worktrees", worktreeID)
	worktreeTreeDir := filepath.Join(worktreeDir, "tree")
	require.NoError(t, os.MkdirAll(worktreeTreeDir, 0755))

	// Write integration marker
	agencyDir := filepath.Join(worktreeTreeDir, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	markerPath := filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName)
	require.NoError(t, os.WriteFile(markerPath, []byte("# Integration worktree\n"), 0644))

	// Write integration worktree meta.json
	wtMeta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    worktreeID,
		Name:          worktreeName,
		RepoID:        repoID,
		Branch:        "agency/" + worktreeName + "-abcd",
		ParentBranch:  "main",
		TreePath:      worktreeTreeDir,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	metaBytes, _ := json.MarshalIndent(wtMeta, "", "  ")
	metaPath := filepath.Join(worktreeDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))

	return repoDir, dataDir, repoID, worktreeID, cr, fsys
}

// createTestInvocation creates a test invocation for testing attach/stop/kill.
func createTestInvocation(t *testing.T, dataDir, repoID, worktreeID, invocationID string, mode store.RunnerMode, status store.InvocationStatus) {
	t.Helper()

	invDir := filepath.Join(dataDir, "repos", repoID, "invocations", invocationID)
	require.NoError(t, os.MkdirAll(invDir, 0755))

	sandboxDir := filepath.Join(dataDir, "repos", repoID, "sandboxes", invocationID)
	sandboxTreeDir := filepath.Join(sandboxDir, "tree")
	require.NoError(t, os.MkdirAll(sandboxTreeDir, 0755))

	meta := &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		IntegrationWorktreeID: worktreeID,
		SandboxPath:           sandboxTreeDir,
		SandboxBranch:         "agency/sandbox-" + invocationID,
		BaseCommit:            "abc123def456",
		Runner:                "claude",
		Mode:                  mode,
		StartedAt:             time.Now().UTC().Format(time.RFC3339),
		Status:                status,
	}
	if mode == store.RunnerModeHeaded {
		meta.TmuxSession = tmux.SessionName(invocationID)
	}

	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	metaPath := filepath.Join(invDir, "meta.json")
	require.NoError(t, os.WriteFile(metaPath, metaBytes, 0644))
}

func TestAgentAttach_HeadlessInvocation_ReturnsInvalidMode(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headless invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeadless, store.InvocationStatusRunning)

	fakeTmux := testutil.NewFakeTmuxClient()

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef:   invocationID,
		TmuxClient:      fakeTmux,
		IsInteractive:   func() bool { return true },
		DataDirOverride: dataDir,
	}

	err := AgentAttach(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	require.Error(t, err, "AgentAttach error = nil, want E_INVOCATION_INVALID_MODE")

	assert.Equal(t, errors.EInvocationInvalidMode, errors.GetCode(err))
}

func TestAgentAttach_HeadedInvocation_SessionMissing(t *testing.T) {
	t.Parallel()
	repoDir, dataDir, repoID, worktreeID, cr, fsys := setupAgentTestEnv(t, "test-feature")
	invocationID := "20260131130000-efgh"

	// Create a headed invocation
	createTestInvocation(t, dataDir, repoID, worktreeID, invocationID, store.RunnerModeHeaded, store.InvocationStatusRunning)

	fakeTmux := testutil.NewFakeTmuxClient()
	// default: Sessions map is empty, so HasSession returns false

	var stdout, stderr bytes.Buffer
	opts := AgentAttachOpts{
		InvocationRef:   invocationID,
		TmuxClient:      fakeTmux,
		IsInteractive:   func() bool { return true },
		DataDirOverride: dataDir,
	}

	err := AgentAttach(context.Background(), cr, fsys, repoDir, opts, &stdout, &stderr)
	require.Error(t, err, "AgentAttach error = nil, want E_SESSION_ENDED")

	assert.Equal(t, errors.ESessionEnded, errors.GetCode(err))
}
