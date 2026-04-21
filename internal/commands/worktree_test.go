// Package commands implements agency CLI commands.
// This file tests worktree command convergence (S2 PR-03).
// All tests are non-parallel due to AGENCY_DATA_DIR env overrides.
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
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

	repoRoot := filepath.Join(dataTmp, "repos", repoID, "root")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))
	st := store.NewStore(fs.NewRealFS(), dataTmp, time.Now)
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"path:" + repoID: {
				RepoID:     repoID,
				Paths:      []string{repoRoot},
				LastSeenAt: "2026-01-31T12:00:00Z",
			},
		},
	}))
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "path:" + repoID,
		RepoID:           repoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    false,
		CreatedAt:        "2026-01-31T12:00:00Z",
		UpdatedAt:        "2026-01-31T12:00:00Z",
	}))

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

func seedRepoIndexForWorktreeAmbiguityTests(t *testing.T, dataDir, repoID string) {
	t.Helper()

	repoRoot := filepath.Join(dataDir, "repos", repoID, "root")
	require.NoError(t, os.MkdirAll(repoRoot, 0755))

	st := store.NewStore(fs.NewRealFS(), dataDir, time.Now)
	require.NoError(t, st.SaveRepoIndex(store.RepoIndex{
		SchemaVersion: store.SchemaVersion,
		Repos: map[string]store.RepoIndexEntry{
			"path:" + repoID: {
				RepoID:     repoID,
				Paths:      []string{repoRoot},
				LastSeenAt: "2026-01-31T12:00:00Z",
			},
		},
	}))
	require.NoError(t, st.SaveRepoRecord(store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          "path:" + repoID,
		RepoID:           repoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    false,
		CreatedAt:        "2026-01-31T12:00:00Z",
		UpdatedAt:        "2026-01-31T12:00:00Z",
	}))
}

func createWorktreeInStore(t *testing.T, dataDir, repoID, wtID, name, branch, baseBranch string) string {
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
		BaseBranch:    baseBranch,
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
	startTestDaemonForWorktreeWithRunner(t, dataDir, testutil.NewFakeCommandRunner())
}

func startTestDaemonForWorktreeWithRunner(t *testing.T, dataDir string, cr agencyexec.CommandRunner) {
	t.Helper()

	fsys := fs.NewRealFS()
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
		WorktreeLSOpts{RepoRef: env.RepoID}, &stdout, &stderr)
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
		WorktreeShowOpts{WorktreeRef: env.WorktreeID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "worktree:    alpha ("+env.WorktreeID+")")
	assert.Contains(t, out, "repo:        root ("+env.RepoID+")")
	assert.Contains(t, out, "branch:        agency/alpha-abcd")
	assert.Contains(t, out, "base_branch: main")
	assert.Contains(t, out, "state:         present")
	assert.Contains(t, out, "tree_path:     "+env.TreePath)
}

func TestWorktreeLS_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoRef: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dtos []daemon.WorktreeDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dtos))
	require.Len(t, dtos, 1)

	assert.Equal(t, env.WorktreeID, dtos[0].WorktreeID)
	assert.Equal(t, "alpha", dtos[0].WorktreeName)
	assert.Equal(t, env.RepoID, dtos[0].RepoID)
	assert.Equal(t, "root", dtos[0].RepoName)
	assert.Equal(t, "agency/alpha-abcd", dtos[0].Branch)
	assert.Equal(t, "main", dtos[0].BaseBranch)
	assert.Equal(t, env.TreePath, dtos[0].TreePath)
	assert.Equal(t, "present", dtos[0].State)
}

func TestWorktreeShow_JSONOutput_DirectDaemonDTO(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreeShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShowOpts{WorktreeRef: env.WorktreeID, RepoRef: env.RepoID, JSON: true}, &stdout, &stderr)
	require.NoError(t, err)

	var dto daemon.WorktreeDTO
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &dto))

	assert.Equal(t, env.WorktreeID, dto.WorktreeID)
	assert.Equal(t, "alpha", dto.WorktreeName)
	assert.Equal(t, env.RepoID, dto.RepoID)
	assert.Equal(t, "root", dto.RepoName)
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
	seedRepoIndexForWorktreeAmbiguityTests(t, dataTmp, repoID)
	startTestDaemonForWorktree(t, dataTmp)

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataTmp, "config"))

	var stdout, stderr bytes.Buffer
	err = WorktreeShow(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShowOpts{WorktreeRef: "20260201000000", RepoRef: repoID}, &stdout, &stderr)

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

func TestWorktreePath_UsesDaemonResolution(t *testing.T) {
	env := setupWorktreeEnv(t, "alpha")

	var stdout, stderr bytes.Buffer
	err := WorktreePath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreePathOpts{WorktreeRef: env.WorktreeID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	assert.Equal(t, env.TreePath+"\n", stdout.String(),
		"stdout must be exactly the daemon-resolved tree_path plus newline")
}

func TestWorktreeOpen_UsesDaemonResolution_NoLocalResolve(t *testing.T) {
	env := setupWorktreeEnv(t, "open-test")
	shimPath, recordFile := createShimScript(t)

	var stdout, stderr bytes.Buffer
	err := WorktreeOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeOpenOpts{
			WorktreeRef: env.WorktreeID,
			RepoRef:     env.RepoID,
			Editor:      shimPath,
		}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.TreePath, cwd,
		"editor dispatch cwd must equal daemon-resolved tree_path")
	assert.Contains(t, args, env.TreePath,
		"editor must receive daemon-resolved tree_path as argument")
}

func TestWorktreeShell_UsesDaemonResolution_NoLocalResolve(t *testing.T) {
	env := setupWorktreeEnv(t, "shell-test")
	shimPath, recordFile := createShimScript(t)

	t.Setenv("SHELL", shimPath)

	var stdout, stderr bytes.Buffer
	err := WorktreeShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShellOpts{WorktreeRef: env.WorktreeID, RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	cwd, args := readShimRecord(t, recordFile)

	assert.Equal(t, env.TreePath, cwd,
		"shell cwd must equal daemon-resolved tree_path")
	assert.Equal(t, "-l", args,
		"shell should be invoked with -l (login)")
}

func TestWorktreePath_ArchivedExactIDFails(t *testing.T) {
	env := setupWorktreeEnv(t, "archived-path")

	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	require.NoError(t, st.UpdateIntegrationWorktreeMeta(env.RepoID, env.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.State = store.WorktreeStateArchived
	}))

	var stdout, stderr bytes.Buffer
	err := WorktreePath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreePathOpts{WorktreeRef: env.WorktreeID, RepoRef: env.RepoID}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "archived")
}

func TestWorktreeOpen_ArchivedExactIDFailsWithoutDispatch(t *testing.T) {
	env := setupWorktreeEnv(t, "archived-open")
	shimPath, recordFile := createShimScript(t)

	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	require.NoError(t, st.UpdateIntegrationWorktreeMeta(env.RepoID, env.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.State = store.WorktreeStateArchived
	}))

	var stdout, stderr bytes.Buffer
	err := WorktreeOpen(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeOpenOpts{
			WorktreeRef: env.WorktreeID,
			RepoRef:     env.RepoID,
			Editor:      shimPath,
		}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "archived")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr), "editor shim must not run for archived worktrees")
}

func TestWorktreeShell_ArchivedExactIDFailsWithoutDispatch(t *testing.T) {
	env := setupWorktreeEnv(t, "archived-shell")
	shimPath, recordFile := createShimScript(t)
	t.Setenv("SHELL", shimPath)

	st := store.NewStore(fs.NewRealFS(), env.DataDir, time.Now)
	require.NoError(t, st.UpdateIntegrationWorktreeMeta(env.RepoID, env.WorktreeID, func(meta *store.IntegrationWorktreeMeta) {
		meta.State = store.WorktreeStateArchived
	}))

	var stdout, stderr bytes.Buffer
	err := WorktreeShell(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeShellOpts{WorktreeRef: env.WorktreeID, RepoRef: env.RepoID}, &stdout, &stderr)

	require.Error(t, err)
	assert.Equal(t, errors.EWorktreeNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "archived")

	_, readErr := os.ReadFile(recordFile)
	assert.True(t, os.IsNotExist(readErr), "shell shim must not run for archived worktrees")
}

func TestWorktreePath_AmbiguityUsesEAmbiguous(t *testing.T) {
	dataTmp, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataTmp) })

	repoID := "r1"
	createWorktreeInStore(t, dataTmp, repoID, "20260201000000-aaaa", "feat-a", "agency/a", "main")
	createWorktreeInStore(t, dataTmp, repoID, "20260201000000-bbbb", "feat-b", "agency/b", "main")
	seedRepoIndexForWorktreeAmbiguityTests(t, dataTmp, repoID)
	startTestDaemonForWorktree(t, dataTmp)

	t.Setenv("AGENCY_DATA_DIR", dataTmp)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(dataTmp, "config"))

	var stdout, stderr bytes.Buffer
	err = WorktreePath(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreePathOpts{WorktreeRef: "20260201000000", RepoRef: repoID}, &stdout, &stderr)

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
	seedRepoIndexForWorktreeAmbiguityTests(t, dataTmp, repoID)
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
			RepoRef:     repoID,
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
		WorktreePathOpts{WorktreeRef: "pathout", RepoRef: env.RepoID}, &stdout, &stderr)
	require.NoError(t, err)

	printedPath := strings.TrimSpace(stdout.String())
	assert.Equal(t, env.TreePath, printedPath,
		"printed path must equal daemon DTO tree_path (not re-derived)")
}

func TestWorktreeHumanOutput_RemainsHumanOriented_ScriptContractViaJSON(t *testing.T) {
	env := setupWorktreeEnv(t, "human")

	var humanOut, jsonOut, stderr bytes.Buffer

	err := WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoRef: env.RepoID}, &humanOut, &stderr)
	require.NoError(t, err)

	err = WorktreeLS(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "",
		WorktreeLSOpts{RepoRef: env.RepoID, JSON: true}, &jsonOut, &stderr)
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
				RepoRef:     env.RepoID,
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
			WorktreeShellOpts{WorktreeRef: "nonexistent-worktree", RepoRef: env.RepoID}, &stdout, &stderr)

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
		RepoRef:       env.RepoID,
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EConfirmationRequired, errors.GetCode(err))
}

func TestWorktreeRm_InteractiveConfirmationRejected_ReturnsEAborted(t *testing.T) {
	env := setupWorktreeEnv(t, "rm-reject")

	var stdout, stderr bytes.Buffer
	err := awaitConfirmationLineBeforeEOF(t, "no\n", func(confirmIn io.Reader) error {
		return WorktreeRm(context.Background(), testutil.NewFakeCommandRunner(), fs.NewRealFS(), "", WorktreeRmOpts{
			WorktreeRef:    env.WorktreeID,
			RepoRef:        env.RepoID,
			IsInteractive:  func() bool { return true },
			ConfirmationIn: confirmIn,
		}, &stdout, &stderr)
	})
	require.Error(t, err)
	assert.Equal(t, errors.EAborted, errors.GetCode(err))
}

func TestWorktreeCreate_DefaultsRepoAndParentFromCWD(t *testing.T) {
	repoDir := testutil.SetupGitRepo(t)
	dataDir, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	startTestDaemonForWorktreeWithRunner(t, dataDir, agencyexec.NewRealRunner())

	var stdout, stderr bytes.Buffer
	err = WorktreeCreate(context.Background(), agencyexec.NewRealRunner(), fs.NewRealFS(), repoDir, WorktreeCreateOpts{
		Name: "default-context",
	}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Created integration worktree 'default-context'")

	client := daemonclient.NewClient(filepath.Join(dataDir, "agencyd.sock"))
	listResp, listErr := client.ListWorktrees(context.Background(), daemonclient.ListWorktreesOpts{State: "present"})
	require.NoError(t, listErr)
	require.Len(t, listResp.Data.Worktrees, 1)
	assert.Equal(t, "default-context", listResp.Data.Worktrees[0].WorktreeName)
	assert.Equal(t, "main", listResp.Data.Worktrees[0].BaseBranch)
	assert.Empty(t, stderr.String())
}

func TestWorktreeCreate_DefaultParentRequiresCurrentBranch(t *testing.T) {
	repoDir := testutil.SetupGitRepo(t)
	detach, err := agencyexec.NewRealRunner().Run(context.Background(), "git", []string{"checkout", "--detach"}, agencyexec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, detach.ExitCode, "git checkout --detach failed: %s", detach.Stderr)

	dataDir, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	startTestDaemonForWorktreeWithRunner(t, dataDir, agencyexec.NewRealRunner())

	var stdout, stderr bytes.Buffer
	err = WorktreeCreate(context.Background(), agencyexec.NewRealRunner(), fs.NewRealFS(), repoDir, WorktreeCreateOpts{
		Name: "detached-default-base",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EBaseBranchNotFound, errors.GetCode(err))
}

func TestWorktreeCreate_ExplicitMissingBaseBranchFailsBeforeCreate(t *testing.T) {
	repoDir := testutil.SetupGitRepo(t)
	dataDir, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	startTestDaemonForWorktreeWithRunner(t, dataDir, agencyexec.NewRealRunner())

	var stdout, stderr bytes.Buffer
	err = WorktreeCreate(context.Background(), agencyexec.NewRealRunner(), fs.NewRealFS(), repoDir, WorktreeCreateOpts{
		Name:       "missing-base",
		BaseBranch: "does-not-exist",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EBaseBranchNotFound, errors.GetCode(err))
	assert.Contains(t, err.Error(), "local base branch 'does-not-exist' was not found")
}

func TestWorktreeCreate_EmptyRepoFailsBeforeCreate(t *testing.T) {
	testutil.HermeticGitEnv(t)
	repoDir := t.TempDir()
	initResult, err := agencyexec.NewRealRunner().Run(context.Background(), "git", []string{"init", "-b", "main"}, agencyexec.RunOpts{Dir: repoDir})
	require.NoError(t, err)
	require.Equal(t, 0, initResult.ExitCode, "git init failed: %s", initResult.Stderr)

	dataDir, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	startTestDaemonForWorktreeWithRunner(t, dataDir, agencyexec.NewRealRunner())

	var stdout, stderr bytes.Buffer
	err = WorktreeCreate(context.Background(), agencyexec.NewRealRunner(), fs.NewRealFS(), repoDir, WorktreeCreateOpts{
		Name:       "empty-repo",
		BaseBranch: "main",
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.EEmptyRepo, errors.GetCode(err))
}

func TestWorktreeCreate_OpenFailureReportsFailedStatusAndPreservesCreation(t *testing.T) {
	repoDir := testutil.SetupGitRepo(t)
	dataDir, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	startTestDaemonForWorktreeWithRunner(t, dataDir, agencyexec.NewRealRunner())
	client := daemonclient.NewClient(filepath.Join(dataDir, "agencyd.sock"))
	reg, regErr := client.RegisterRepo(context.Background(), repoDir)
	require.NoError(t, regErr)

	editorPath := filepath.Join(t.TempDir(), "editor-fail.sh")
	require.NoError(t, os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 17\n"), 0o755))

	var stdout, stderr bytes.Buffer
	err = WorktreeCreate(context.Background(), agencyexec.NewRealRunner(), fs.NewRealFS(), repoDir, WorktreeCreateOpts{
		RepoRef:    reg.Data.RepoID,
		Name:       "open-fail",
		BaseBranch: "main",
		Open:       true,
		Editor:     editorPath,
	}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "open_status: failed")
	assert.Contains(t, stderr.String(), "warning: workspace created but open dispatch failed: editor exited with code 17")

	listResp, listErr := client.ListWorktrees(context.Background(), daemonclient.ListWorktreesOpts{
		RepoID: reg.Data.RepoID,
		State:  "present",
	})
	require.NoError(t, listErr)
	require.Len(t, listResp.Data.Worktrees, 1)
	assert.Equal(t, "open-fail", listResp.Data.Worktrees[0].WorktreeName)
}

func TestWorktreeCreate_OpenSuccessReportsOpenedStatus(t *testing.T) {
	repoDir := testutil.SetupGitRepo(t)
	dataDir, err := os.MkdirTemp("", "wd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	configDir := filepath.Join(dataDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	startTestDaemonForWorktreeWithRunner(t, dataDir, agencyexec.NewRealRunner())
	client := daemonclient.NewClient(filepath.Join(dataDir, "agencyd.sock"))
	reg, regErr := client.RegisterRepo(context.Background(), repoDir)
	require.NoError(t, regErr)

	editorPath := filepath.Join(t.TempDir(), "editor-ok.sh")
	require.NoError(t, os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	var stdout, stderr bytes.Buffer
	err = WorktreeCreate(context.Background(), agencyexec.NewRealRunner(), fs.NewRealFS(), repoDir, WorktreeCreateOpts{
		RepoRef:    reg.Data.RepoID,
		Name:       "open-ok",
		BaseBranch: "main",
		Open:       true,
		Editor:     editorPath,
	}, &stdout, &stderr)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "open_status: opened")
	assert.NotContains(t, stdout.String(), "open_status: failed")
	assert.Empty(t, stderr.String())
}
