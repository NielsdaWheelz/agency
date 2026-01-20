package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// cleanFakeCommandRunner is a test double for exec.CommandRunner with configurable responses.
type cleanFakeCommandRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

func (f *cleanFakeCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)

	if resp, ok := f.responses[key]; ok {
		if resp.err != nil {
			return exec.CmdResult{}, resp.err
		}
		return exec.CmdResult{
			Stdout:   resp.stdout,
			Stderr:   resp.stderr,
			ExitCode: resp.exitCode,
		}, nil
	}

	// Default response for git commands
	if name == "git" {
		return exec.CmdResult{Stdout: "", ExitCode: 0}, nil
	}

	return exec.CmdResult{}, nil
}

func (f *cleanFakeCommandRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

// setupCleanTestEnv creates a temporary test environment for clean tests.
func setupCleanTestEnv(t *testing.T, runID string, setupMeta bool, setupWorktree bool) (string, string, string, *cleanFakeCommandRunner, fs.FS, *store.RunMeta) {
	t.Helper()

	// Create temp directories
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	dataDir := filepath.Join(tempDir, "data")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	originURL := "git@github.com:test/repo.git"

	// Compute repo_id the same way the real code does
	repoIdentity := identity.DeriveRepoIdentity(repoDir, originURL)
	repoID := repoIdentity.RepoID

	// Create fake command runner
	cr := &cleanFakeCommandRunner{
		responses: map[string]fakeResponse{
			"git rev-parse --show-toplevel":      {stdout: repoDir + "\n"},
			"git config --get remote.origin.url": {stdout: originURL + "\n"},
		},
	}

	fsys := fs.NewRealFS()

	var meta *store.RunMeta
	if setupMeta {
		// Create store directories
		runDir := filepath.Join(dataDir, "repos", repoID, "runs", runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(runDir, "logs"), 0755); err != nil {
			t.Fatal(err)
		}

		worktreePath := filepath.Join(dataDir, "repos", repoID, "worktrees", runID)

		// Create worktree directory if needed
		if setupWorktree {
			if err := os.MkdirAll(worktreePath, 0755); err != nil {
				t.Fatal(err)
			}
			// Create .agency directory
			if err := os.MkdirAll(filepath.Join(worktreePath, ".agency"), 0755); err != nil {
				t.Fatal(err)
			}
		}

		// Write meta.json
		meta = &store.RunMeta{
			SchemaVersion:   "1.0",
			RunID:           runID,
			RepoID:          repoID,
			Name:            "test-run",
			Runner:          "claude",
			RunnerCmd:       "claude",
			ParentBranch:    "main",
			Branch:          "agency/test-run-" + runID[:4],
			WorktreePath:    worktreePath,
			CreatedAt:       "2026-01-10T12:00:00Z",
			TmuxSessionName: tmux.SessionName(runID),
			PRNumber:        123,
			PRURL:           "https://github.com/test/repo/pull/123",
		}
		metaBytes, _ := json.MarshalIndent(meta, "", "  ")
		metaPath := filepath.Join(runDir, "meta.json")
		if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Set environment for data dir resolution
	t.Setenv("AGENCY_DATA_DIR", dataDir)

	return repoDir, dataDir, repoID, cr, fsys, meta
}

func TestClean_DeleteBranch_LocalBranchDeleted(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses for branch deletion
	cr.responses["git -C "+repoDir+" branch -D "+branch] = fakeResponse{exitCode: 0}
	cr.responses["git -C "+repoDir+" config --get remote.origin.url"] = fakeResponse{
		stdout: "git@github.com:test/repo.git\n",
	}
	cr.responses["git -C "+repoDir+" push origin --delete "+branch] = fakeResponse{exitCode: 0}
	cr.responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = fakeResponse{exitCode: 0}

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	// Create input for confirmation
	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	// Force interactive mode for testing by using CleanWithTmux directly
	// and temporarily modifying tty detection
	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Clean() error = %v, want nil", err)
	}

	// Verify local branch deletion was called
	foundLocalDelete := false
	for _, call := range cr.calls {
		if strings.Contains(call, "branch -D "+branch) {
			foundLocalDelete = true
			break
		}
	}
	if !foundLocalDelete {
		t.Errorf("expected local branch deletion call, got calls: %v", cr.calls)
	}

	// Verify output shows branch was deleted
	output := stdout.String()
	if !strings.Contains(output, "local_branch: deleted") {
		t.Errorf("stdout = %q, want contains 'local_branch: deleted'", output)
	}

	// Verify event was logged
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"branch_deleted"`) {
		t.Error("expected branch_deleted event in events.jsonl")
	}
}

func TestClean_DeleteBranch_RemoteBranchDeleted(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses for branch deletion
	cr.responses["git -C "+repoDir+" branch -D "+branch] = fakeResponse{exitCode: 0}
	cr.responses["git -C "+repoDir+" config --get remote.origin.url"] = fakeResponse{
		stdout: "git@github.com:test/repo.git\n",
	}
	cr.responses["git -C "+repoDir+" push origin --delete "+branch] = fakeResponse{exitCode: 0}
	cr.responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = fakeResponse{exitCode: 0}

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Clean() error = %v, want nil", err)
	}

	// Verify remote branch deletion was called
	foundRemoteDelete := false
	for _, call := range cr.calls {
		if strings.Contains(call, "push origin --delete "+branch) {
			foundRemoteDelete = true
			break
		}
	}
	if !foundRemoteDelete {
		t.Errorf("expected remote branch deletion call, got calls: %v", cr.calls)
	}

	// Verify output shows remote branch was deleted
	output := stdout.String()
	if !strings.Contains(output, "remote_branch: deleted") {
		t.Errorf("stdout = %q, want contains 'remote_branch: deleted'", output)
	}

	// Verify event was logged
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	// Should have two branch_deleted events (local and remote)
	if strings.Count(string(eventsData), `"event":"branch_deleted"`) < 2 {
		t.Error("expected two branch_deleted events in events.jsonl")
	}
}

func TestClean_DeleteBranch_PRClosed(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, dataDir, repoID, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses for branch deletion and PR close
	cr.responses["git -C "+repoDir+" branch -D "+branch] = fakeResponse{exitCode: 0}
	cr.responses["git -C "+repoDir+" config --get remote.origin.url"] = fakeResponse{
		stdout: "git@github.com:test/repo.git\n",
	}
	cr.responses["git -C "+repoDir+" push origin --delete "+branch] = fakeResponse{exitCode: 0}
	cr.responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = fakeResponse{exitCode: 0}

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Clean() error = %v, want nil", err)
	}

	// Verify PR close was called
	foundPRClose := false
	for _, call := range cr.calls {
		if strings.Contains(call, "gh pr close 123") {
			foundPRClose = true
			break
		}
	}
	if !foundPRClose {
		t.Errorf("expected gh pr close call, got calls: %v", cr.calls)
	}

	// Verify output shows PR was closed
	output := stdout.String()
	if !strings.Contains(output, "pr: closed #123") {
		t.Errorf("stdout = %q, want contains 'pr: closed #123'", output)
	}

	// Verify event was logged
	st := store.NewStore(fsys, dataDir, nil)
	eventsPath := filepath.Join(st.RunDir(repoID, runID), "events.jsonl")
	eventsData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	if !strings.Contains(string(eventsData), `"event":"pr_closed"`) {
		t.Error("expected pr_closed event in events.jsonl")
	}
}

func TestClean_WithoutDeleteBranch_NoBranchDeletion(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Add responses but they should NOT be called
	cr.responses["git -C "+repoDir+" branch -D "+branch] = fakeResponse{exitCode: 0}

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: false, // Explicitly false
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Clean() error = %v, want nil", err)
	}

	// Verify NO branch deletion was called
	for _, call := range cr.calls {
		if strings.Contains(call, "branch -D") {
			t.Errorf("unexpected branch deletion call: %s", call)
		}
		if strings.Contains(call, "push origin --delete") {
			t.Errorf("unexpected remote branch deletion call: %s", call)
		}
		if strings.Contains(call, "gh pr close") {
			t.Errorf("unexpected PR close call: %s", call)
		}
	}

	// Verify output does NOT show branch deletion
	output := stdout.String()
	if strings.Contains(output, "local_branch:") {
		t.Errorf("stdout = %q, should NOT contain 'local_branch:'", output)
	}
}

func TestClean_DeleteBranch_LocalBranchFailure_NonFatal(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Local branch deletion fails
	cr.responses["git -C "+repoDir+" branch -D "+branch] = fakeResponse{
		exitCode: 1,
		stderr:   "error: branch 'agency/test-run-2026' not found.",
	}
	cr.responses["git -C "+repoDir+" config --get remote.origin.url"] = fakeResponse{
		stdout: "git@github.com:test/repo.git\n",
	}
	// Remote should still be attempted
	cr.responses["git -C "+repoDir+" push origin --delete "+branch] = fakeResponse{exitCode: 0}
	cr.responses["gh pr close 123 -R test/repo --comment Closed via `agency clean --delete-branch`"] = fakeResponse{exitCode: 0}

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	// Should still succeed despite local branch deletion failure
	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Clean() error = %v, want nil (branch failure should be non-fatal)", err)
	}

	// Verify warning was logged
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "warning: failed to delete local branch") {
		t.Errorf("stderr = %q, want contains warning about local branch failure", stderrStr)
	}

	// Output should still show clean succeeded
	output := stdout.String()
	if !strings.Contains(output, "cleaned:") {
		t.Errorf("stdout = %q, want contains 'cleaned:'", output)
	}
}

func TestClean_DeleteBranch_NonGitHubOrigin_SkipsRemote(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys, meta := setupCleanTestEnv(t, runID, true, true)

	branch := meta.Branch

	// Local branch deletion succeeds
	cr.responses["git -C "+repoDir+" branch -D "+branch] = fakeResponse{exitCode: 0}
	// Non-GitHub origin
	cr.responses["git -C "+repoDir+" config --get remote.origin.url"] = fakeResponse{
		stdout: "git@gitlab.com:test/repo.git\n",
	}

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Clean() error = %v, want nil", err)
	}

	// Verify remote branch deletion was NOT called (non-GitHub origin)
	for _, call := range cr.calls {
		if strings.Contains(call, "push origin --delete") {
			t.Errorf("unexpected remote branch deletion call for non-GitHub origin: %s", call)
		}
		if strings.Contains(call, "gh pr close") {
			t.Errorf("unexpected PR close call for non-GitHub origin: %s", call)
		}
	}

	// Output should show local branch deleted but not remote
	output := stdout.String()
	if !strings.Contains(output, "local_branch: deleted") {
		t.Errorf("stdout = %q, want contains 'local_branch: deleted'", output)
	}
	if strings.Contains(output, "remote_branch:") {
		t.Errorf("stdout = %q, should NOT contain 'remote_branch:'", output)
	}
}

func TestClean_RunNotFound(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys, _ := setupCleanTestEnv(t, runID, false, false) // no meta

	fakeTmux := &fakeTmuxClient{}

	stdin := strings.NewReader("clean\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err == nil {
		t.Fatal("Clean() error = nil, want E_RUN_NOT_FOUND")
	}

	code := errors.GetCode(err)
	if code != errors.ERunNotFound {
		t.Errorf("error code = %q, want %q", code, errors.ERunNotFound)
	}
}

func TestClean_ConfirmationRequired(t *testing.T) {
	runID := "20260110120000-a3f2"
	repoDir, _, _, cr, fsys, _ := setupCleanTestEnv(t, runID, true, true)

	fakeTmux := &fakeTmuxClient{
		hasSessionResult: false,
	}

	// Wrong confirmation
	stdin := strings.NewReader("no\n")
	var stdout, stderr bytes.Buffer

	opts := CleanOpts{
		RunID:        runID,
		DeleteBranch: true,
	}

	origIsInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origIsInteractive })

	err := CleanWithTmux(context.Background(), cr, fsys, fakeTmux, repoDir, opts, stdin, &stdout, &stderr)
	if err == nil {
		t.Fatal("Clean() error = nil, want E_ABORTED")
	}

	code := errors.GetCode(err)
	if code != errors.EAborted {
		t.Errorf("error code = %q, want %q", code, errors.EAborted)
	}
}
