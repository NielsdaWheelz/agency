// Package commands implements agency CLI commands.
// This file tests worktree command convergence (S2 PR-03).
// All tests are non-parallel due to AGENCY_DATA_DIR env overrides.
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Setup helpers (non-parallel tests only — env mutation via t.Setenv)
// ---------------------------------------------------------------------------

type worktreeTestEnv struct {
	DataDir    string
	RepoID     string
	WorktreeID string
	TreePath   string
}

func setupWorktreeEnv(t *testing.T, name string) worktreeTestEnv {
	t.Helper()

	dataTmp, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID := "20260131120000-abcd"

	treePath := createWorktreeInStore(t, dataTmp, repoID, wtID, name,
		"agency/"+name+"-abcd", "main")

	startTestDaemonForWorktree(t, dataTmp)

	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	return worktreeTestEnv{
		DataDir:    dataTmp,
		RepoID:     repoID,
		WorktreeID: wtID,
		TreePath:   treePath,
	}
}

func createWorktreeInStore(t *testing.T, dataDir, repoID, wtID, name, branch, parentBranch string) string {
	t.Helper()

	wtDir := filepath.Join(dataDir, "repos", repoID, "integration_worktrees", wtID)
	treePath := filepath.Join(wtDir, "tree")
	require.NoError(t, os.MkdirAll(treePath, 0755))

	agencyDir := filepath.Join(treePath, ".agency")
	require.NoError(t, os.MkdirAll(agencyDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(agencyDir, integrationworktree.IntegrationMarkerFileName),
		[]byte("# Integration worktree\n"), 0644))

	meta := &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    wtID,
		Name:          name,
		RepoID:        repoID,
		Branch:        branch,
		ParentBranch:  parentBranch,
		TreePath:      treePath,
		CreatedAt:     "2026-01-31T12:00:00Z",
		State:         store.WorktreeStatePresent,
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(wtDir, "meta.json"), metaBytes, 0644))

	return treePath
}

func startTestDaemonForWorktree(t *testing.T, dataDir string) {
	t.Helper()

	fsys := fs.NewRealFS()
	cr := testutil.NewFakeCommandRunner()
	st := store.NewStore(fsys, dataDir, time.Now)
	configDir := filepath.Join(dataDir, "config")
	srv := daemon.NewServer(st, cr, fsys, configDir)

	socketPath := st.DaemonSocketPath()
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-serveDone
	})

	client := daemonclient.NewClient(socketPath)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second), "daemon not ready")
}

func createShimScript(t *testing.T) (shimPath, recordFile string) {
	t.Helper()
	shimDir := t.TempDir()
	recordFile = filepath.Join(shimDir, "record.txt")
	shimPath = filepath.Join(shimDir, "shim")
	script := fmt.Sprintf("#!/bin/sh\npwd > '%s'\necho \"$@\" >> '%s'\n", recordFile, recordFile)
	require.NoError(t, os.WriteFile(shimPath, []byte(script), 0755))
	return shimPath, recordFile
}

func readShimRecord(t *testing.T, recordFile string) (cwd, args string) {
	t.Helper()
	data, err := os.ReadFile(recordFile)
	require.NoError(t, err, "shim record file should exist after dispatch")
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	require.GreaterOrEqual(t, len(lines), 1)
	cwd = lines[0]
	if len(lines) > 1 {
		args = lines[1]
	}
	return cwd, args
}

// ---------------------------------------------------------------------------
// Acceptance 1: worktree ls/show daemon-of-record read behavior
// ---------------------------------------------------------------------------

func TestWorktreeLS_DaemonOfRecord_RendersDaemonDTO(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, env.WorktreeID)
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "agency/alpha-abcd")
}

func TestWorktreeShow_DaemonOfRecord_RendersDaemonDTO(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreeShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShowOpts{WorktreeRef: env.WorktreeID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "worktree_id:   "+env.WorktreeID)
	assert.Contains(t, out, "name:          alpha")
	assert.Contains(t, out, "repo_id:       "+env.RepoID)
	assert.Contains(t, out, "branch:        agency/alpha-abcd")
	assert.Contains(t, out, "parent_branch: main")
	assert.Contains(t, out, "state:         present")
	assert.Contains(t, out, "tree_path:     "+env.TreePath)
}

func TestWorktreeLS_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoFlag: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dtos []daemon.WorktreeDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dtos))
	require.Len(t, dtos, 1)

	assert.Equal(t, env.WorktreeID, dtos[0].WorktreeID)
	assert.Equal(t, "alpha", dtos[0].Name)
	assert.Equal(t, env.RepoID, dtos[0].RepoID)
	assert.Equal(t, "agency/alpha-abcd", dtos[0].Branch)
	assert.Equal(t, "main", dtos[0].ParentBranch)
	assert.Equal(t, env.TreePath, dtos[0].TreePath)
	assert.Equal(t, "present", dtos[0].State)
}

func TestWorktreeShow_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreeShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShowOpts{WorktreeRef: env.WorktreeID, RepoFlag: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dto daemon.WorktreeDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dto))

	assert.Equal(t, env.WorktreeID, dto.WorktreeID)
	assert.Equal(t, "alpha", dto.Name)
	assert.Equal(t, env.RepoID, dto.RepoID)
	assert.Equal(t, env.TreePath, dto.TreePath)
}

func TestWorktreeShow_AmbiguousPreservesCandidates(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	wtID1 := "20260201000000-aaaa"
	wtID2 := "20260201000000-bbbb"
	createWorktreeInStore(t, dataTmp, repoID, wtID1, "feat-a", "agency/a", "main")
	createWorktreeInStore(t, dataTmp, repoID, wtID2, "feat-b", "agency/b", "main")
	startTestDaemonForWorktree(t, dataTmp)

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataTmp, "config"))

	var stdout, stderr bytes.Buffer
	err = WorktreeShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShowOpts{WorktreeRef: "20260201000000", RepoFlag: repoID}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeIDAmbiguous, errors.GetCode(err),
		"worktree show must return entity-specific ambiguity code, not E_AMBIGUOUS")

	dre, ok := daemonclient.AsDaemonReadError(err)
	require.True(t, ok, "error must be DaemonReadError with rich details")
	candidates := dre.Candidates()
	assert.Len(t, candidates, 2, "daemon should return both candidate IDs")
}

// ---------------------------------------------------------------------------
// Acceptance 2: worktree path/open/shell daemon-first navigation
// ---------------------------------------------------------------------------

func TestWorktreePath_UsesNavigationKernelDaemonResolution(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreePath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreePathOpts{WorktreeRef: env.WorktreeID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, env.TreePath+"\n", stdout.String(),
		"stdout must be exactly the daemon-resolved tree_path plus newline")
}

func TestWorktreeOpen_UsesNavigationKernelDaemonPath_NoLocalResolve(t *testing.T) {
	env := setupWorktreeEnv(t, "open-test")
	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	err := WorktreeOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeOpenOpts{
			WorktreeRef: env.WorktreeID,
			RepoFlag:    env.RepoID,
			Editor:      shimPath,
		}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.TreePath, cwd,
		"editor dispatch cwd must equal daemon-resolved tree_path")
	assert.Contains(t, args, env.TreePath,
		"editor must receive daemon-resolved tree_path as argument")
}

func TestWorktreeShell_UsesNavigationKernelDaemonPath_NoLocalResolve(t *testing.T) {
	env := setupWorktreeEnv(t, "shell-test")
	shimPath, recordFile := createShimScript(t)

	t.Setenv("SHELL", shimPath)

	var stdout, stderr bytes.Buffer
	err := WorktreeShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShellOpts{WorktreeRef: env.WorktreeID, RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.TreePath, cwd,
		"shell cwd must equal daemon-resolved tree_path")
	assert.Equal(t, "-l", args,
		"shell should be invoked with -l (login)")
}

func TestWorktreePath_AmbiguityUsesEAmbiguous(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	createWorktreeInStore(t, dataTmp, repoID, "20260201000000-aaaa", "feat-a", "agency/a", "main")
	createWorktreeInStore(t, dataTmp, repoID, "20260201000000-bbbb", "feat-b", "agency/b", "main")
	startTestDaemonForWorktree(t, dataTmp)

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataTmp, "config"))

	var stdout, stderr bytes.Buffer
	err = WorktreePath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreePathOpts{WorktreeRef: "20260201000000", RepoFlag: repoID}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err),
		"navigation ambiguity must return E_AMBIGUOUS, not entity-specific code")

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	require.NotNil(t, ae.Details)
	assert.Equal(t, "worktree", ae.Details["target_kind"])
	assert.Equal(t, "2", ae.Details["candidate_count"])
}

func TestWorktreeOpen_AmbiguityUsesEAmbiguous_NoDispatch(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	createWorktreeInStore(t, dataTmp, repoID, "20260201000000-aaaa", "feat-a", "agency/a", "main")
	createWorktreeInStore(t, dataTmp, repoID, "20260201000000-bbbb", "feat-b", "agency/b", "main")
	startTestDaemonForWorktree(t, dataTmp)

	configDir := filepath.Join(dataTmp, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)

	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	err = WorktreeOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeOpenOpts{
			WorktreeRef: "20260201000000",
			RepoFlag:    repoID,
			Editor:      shimPath,
		}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EAmbiguous, errors.GetCode(err),
		"navigation ambiguity must return E_AMBIGUOUS")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr),
		"editor shim must NOT be executed on ambiguous target")
}

// ---------------------------------------------------------------------------
// Acceptance 3: deterministic identity/output for script-driven selection
// ---------------------------------------------------------------------------

func TestWorktreeLS_JSONOutput_PreservesRepoScopedIDs(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repo1 := "r1"
	repo2 := "r2"
	createWorktreeInStore(t, dataTmp, repo1, "20260131000000-aaaa", "alpha", "agency/alpha", "main")
	createWorktreeInStore(t, dataTmp, repo2, "20260131000000-bbbb", "bravo", "agency/bravo", "main")

	repoIndex := store.RepoIndex{
		SchemaVersion: "1.0",
		Repos: map[string]store.RepoIndexEntry{
			"key1": {RepoID: repo1, Paths: []string{"/r1"}, LastSeenAt: "2026-01-31T12:00:00Z"},
			"key2": {RepoID: repo2, Paths: []string{"/r2"}, LastSeenAt: "2026-01-31T12:00:00Z"},
		},
	}
	idxBytes, _ := json.MarshalIndent(repoIndex, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dataTmp, "repo_index.json"), idxBytes, 0644))

	startTestDaemonForWorktree(t, dataTmp)

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataTmp, "config"))

	var stdout, stderr bytes.Buffer
	err = WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{AllRepos: true, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dtos []daemon.WorktreeDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dtos))
	require.Len(t, dtos, 2)

	repoIDs := map[string]bool{}
	for _, dto := range dtos {
		repoIDs[dto.RepoID] = true
		assert.NotEmpty(t, dto.WorktreeID, "each row must preserve worktree_id")
	}
	assert.True(t, repoIDs[repo1], "repo1 must appear in JSON output")
	assert.True(t, repoIDs[repo2], "repo2 must appear in JSON output")
}

func TestWorktreePath_OutputsDaemonResolvedPath(t *testing.T) {
	env := setupWorktreeEnv(t, "pathout")

	var stdout, stderr bytes.Buffer
	err := WorktreePath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreePathOpts{WorktreeRef: "pathout", RepoFlag: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	printedPath := strings.TrimSpace(stdout.String())
	assert.Equal(t, env.TreePath, printedPath,
		"printed path must equal daemon DTO tree_path (not re-derived)")
}

func TestWorktreeHumanOutput_RemainsHumanOriented_ScriptContractViaJSON(t *testing.T) {
	env := setupWorktreeEnv(t, "human")

	var humanOut, jsonOut, stderr bytes.Buffer

	err := WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoFlag: env.RepoID}, &humanOut, &stderr)
	require.NoError(t, err)

	err = WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoFlag: env.RepoID, JSON: true}, &jsonOut, &stderr)
	require.NoError(t, err)

	humanStr := humanOut.String()
	assert.NotContains(t, humanStr, `"worktree_id"`,
		"human output must not introduce JSON machine token grammar")
	assert.Contains(t, humanStr, env.WorktreeID,
		"human output must still include worktree ID for readability")

	var dtos []daemon.WorktreeDTO
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &dtos),
		"JSON output must decode to daemon DTO slice (canonical script-safe format)")
	require.Len(t, dtos, 1)
	assert.Equal(t, env.RepoID, dtos[0].RepoID)
	assert.Equal(t, env.WorktreeID, dtos[0].WorktreeID)
}

func TestWorktreeNavigation_DoesNotReturnEWorktreeBrokenForTargetResolution(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		env := setupWorktreeEnv(t, "brk-open")
		shimPath, _ := createShimScript(t)

		var stdout, stderr bytes.Buffer
		err := WorktreeOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
			WorktreeOpenOpts{
				WorktreeRef: "nonexistent-worktree",
				RepoFlag:    env.RepoID,
				Editor:      shimPath,
			}, &stdout, &stderr)

		require.Error(t, err)
		code := errors.GetCode(err)
		assert.NotEqual(t, errors.EWorktreeBroken, code,
			"navigation target resolution must not return E_WORKTREE_BROKEN after PR-03 migration")
		assert.Equal(t, errors.EWorktreeNotFound, code,
			"expected daemon-first E_WORKTREE_NOT_FOUND for missing target")
	})

	t.Run("shell", func(t *testing.T) {
		env := setupWorktreeEnv(t, "brk-shell")
		shimPath, _ := createShimScript(t)
		t.Setenv("SHELL", shimPath)

		var stdout, stderr bytes.Buffer
		err := WorktreeShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
			WorktreeShellOpts{WorktreeRef: "nonexistent-worktree", RepoFlag: env.RepoID}, &stdout, &stderr)

		require.Error(t, err)
		code := errors.GetCode(err)
		assert.NotEqual(t, errors.EWorktreeBroken, code,
			"navigation target resolution must not return E_WORKTREE_BROKEN after PR-03 migration")
		assert.Equal(t, errors.EWorktreeNotFound, code,
			"expected daemon-first E_WORKTREE_NOT_FOUND for missing target")
	})
}

func TestWorktreeRm_NonInteractiveWithoutYes_ReturnsEConfirmationRequired(t *testing.T) {
	env := setupWorktreeEnv(t, "rm-confirm")

	var stdout, stderr bytes.Buffer
	err := WorktreeRm(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", WorktreeRmOpts{
		WorktreeRef:   env.WorktreeID,
		RepoFlag:      env.RepoID,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))
}

func TestWorktreeRm_NonInteractiveWithYes_Proceeds(t *testing.T) {
	env := setupWorktreeEnv(t, "rm-yes")

	var stdout, stderr bytes.Buffer
	err := WorktreeRm(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", WorktreeRmOpts{
		WorktreeRef:   env.WorktreeID,
		RepoFlag:      env.RepoID,
		Yes:           true,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.NotEqual(t, errors.EConfirmationRequired, errors.GetCode(err))
	assert.NotEqual(t, errors.EAborted, errors.GetCode(err))
}

func TestWorktreeRm_InteractiveConfirmationRejected_ReturnsEAborted(t *testing.T) {
	env := setupWorktreeEnv(t, "rm-reject")

	var stdout, stderr bytes.Buffer
	err := WorktreeRm(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", WorktreeRmOpts{
		WorktreeRef:    env.WorktreeID,
		RepoFlag:       env.RepoID,
		IsInteractive:  func() bool { return true },
		ConfirmationIn: strings.NewReader("no\n"),
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EAborted, errors.GetCode(err))
}
