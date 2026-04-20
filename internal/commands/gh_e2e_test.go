//go:build e2e

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

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestGHE2EWorktreePRSyncMerge(t *testing.T) {
	if os.Getenv("AGENCY_GH_E2E") == "" {
		t.Skip("set AGENCY_GH_E2E=1 to enable GH e2e")
	}

	repo := os.Getenv("AGENCY_GH_REPO")
	if repo == "" {
		t.Skip("set AGENCY_GH_REPO=owner/repo to enable GH e2e")
	}

	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		t.Skip("set GH_TOKEN or GITHUB_TOKEN to enable GH e2e")
	}
	t.Setenv("GH_TOKEN", token)
	testutil.HermeticGitEnv(t)

	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()

	// Keep this path short: macOS unix sockets fail around ~104 bytes.
	tmpDir, err := os.MkdirTemp("", "age2e")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	dataDir := filepath.Join(tmpDir, "data")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("AGENCY_CACHE_DIR", filepath.Join(tmpDir, "cache"))

	repoRoot := filepath.Join(tmpDir, "repo")
	requireGHAuth(t, ctx, cr)
	runCmd(t, ctx, cr, "", "gh", "auth", "setup-git")
	runCmd(t, ctx, cr, "", "gh", "repo", "clone", repo, repoRoot)

	defaultBranch := resolveDefaultBranch(t, ctx, cr, repoRoot, repo)

	runID, err := core.NewRunID(time.Now())
	require.NoError(t, err, "runID")
	branch := fmt.Sprintf("agency/e2e-%s", runID)

	originInfo := git.GetOriginInfo(ctx, cr, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)
	require.NotEmpty(t, repoIdentity.RepoID, "repoID empty")

	worktreePath := filepath.Join(dataDir, "repos", repoIdentity.RepoID, "worktrees", runID)
	require.NoError(t, os.MkdirAll(filepath.Dir(worktreePath), 0o755), "mkdir worktrees")
	runCmd(t, ctx, cr, repoRoot, "git", "fetch", "origin", defaultBranch)
	runCmd(t, ctx, cr, repoRoot, "git", "worktree", "add", "-b", branch, worktreePath, "origin/"+defaultBranch)

	// Infrastructure files (agency.json, scripts/) use FIXED content so concurrent
	// test runs don't conflict - Git auto-merges identical content.
	// Only the e2e/<runID>/ directory contains unique-per-run data.
	scriptsDir := filepath.Join(worktreePath, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0o755), "mkdir scripts")

	// Fixed script content - same every run
	writeScript(t, filepath.Join(scriptsDir, "agency_setup.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(scriptsDir, "agency_verify.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(scriptsDir, "agency_archive.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")

	// Fixed agency.json content - same every run
	agencyJSON := `{
  "version": 2,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(worktreePath, "agency.json"), []byte(agencyJSON), 0o644), "write agency.json")

	// Runner status lives under .agency/state/ and is read locally by PR sync.
	stateDir := filepath.Join(worktreePath, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755), "mkdir runner status")
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), []byte(`{
  "schema_version": "1.0",
  "status": "ready",
  "updated_at": "2026-04-20T00:00:00Z",
  "summary": "e2e runner status: verifying agent pr sync + merge works",
  "questions": [],
  "blockers": [],
  "how_to_test": "This is an automated e2e test - no manual testing required.",
  "risks": []
}`), 0o644), "write runner status")

	// Unique test data under e2e/<runID>/ - this is the only unique-per-run content
	e2eRunDir := filepath.Join(worktreePath, "e2e", runID)
	require.NoError(t, os.MkdirAll(e2eRunDir, 0o755), "mkdir e2e run dir")
	logPath := filepath.Join(e2eRunDir, "log.txt")
	logContent := fmt.Sprintf("%s %s\n", runID, time.Now().UTC().Format(time.RFC3339))
	require.NoError(t, os.WriteFile(logPath, []byte(logContent), 0o644), "write e2e log")

	result, err := cr.Run(ctx, "git", []string{"check-ignore", "-q", ".agency/state/runner_status.json"}, exec.RunOpts{
		Dir: worktreePath,
		Env: nonInteractiveEnv(),
	})
	require.NoError(t, err, "git check-ignore .agency/state/runner_status.json")
	runnerStatusIgnored := false
	switch result.ExitCode {
	case 0:
		runnerStatusIgnored = true
	case 1:
		runnerStatusIgnored = false
	default:
		require.Fail(t, "git check-ignore .agency/state/runner_status.json unexpected exit code", "exited %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}

	// Add fixed infrastructure files + unique run data
	addPaths := []string{
		"agency.json",
		"scripts/agency_setup.sh",
		"scripts/agency_verify.sh",
		"scripts/agency_archive.sh",
		fmt.Sprintf("e2e/%s/", runID),
	}
	if !runnerStatusIgnored {
		addPaths = append(addPaths, ".agency/state/runner_status.json")
	}
	runCmd(t, ctx, cr, worktreePath, "git", append([]string{"add"}, addPaths...)...)
	runCmd(t, ctx, cr, worktreePath, "git", "commit", "-m", "e2e: "+runID)

	st := store.NewStore(fsys, dataDir, time.Now)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	repoRecord := store.RepoRecord{
		SchemaVersion:    store.SchemaVersion,
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoIdentity.RepoID,
		RepoRootLastSeen: repoRoot,
		PreferredRoot:    repoRoot,
		AgencyJSONPath:   filepath.Join(repoRoot, "agency.json"),
		OriginPresent:    true,
		OriginURL:        originInfo.URL,
		OriginHost:       originInfo.Host,
		Capabilities: store.Capabilities{
			GitHubOrigin: repoIdentity.GitHubFlowAvailable,
			OriginHost:   originInfo.Host,
			GhAuthed:     true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, st.SaveRepoRecord(repoRecord), "SaveRepoRecord")

	worktreeID := runID
	_, err = st.EnsureIntegrationWorktreeDir(repoIdentity.RepoID, worktreeID)
	require.NoError(t, err, "EnsureIntegrationWorktreeDir")
	require.NoError(t, st.WriteIntegrationWorktreeMeta(repoIdentity.RepoID, worktreeID, &store.IntegrationWorktreeMeta{
		SchemaVersion: "1.0",
		WorktreeID:    worktreeID,
		Name:          "e2e-" + runID,
		RepoID:        repoIdentity.RepoID,
		Branch:        branch,
		BaseBranch:    defaultBranch,
		TreePath:      worktreePath,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		State:         store.WorktreeStatePresent,
	}), "WriteIntegrationWorktreeMeta")

	invocationID := runID
	_, err = st.EnsureInvocationDir(repoIdentity.RepoID, invocationID)
	require.NoError(t, err, "EnsureInvocationDir")
	require.NoError(t, st.WriteInvocationMeta(repoIdentity.RepoID, invocationID, &store.InvocationMeta{
		SchemaVersion:         "1.0",
		InvocationID:          invocationID,
		InvocationName:        "e2e-" + runID,
		IntegrationWorktreeID: worktreeID,
		SandboxPath:           worktreePath,
		SandboxBranch:         "agency/sandbox-" + runID,
		BaseCommit:            "",
		Runner:                "claude-code",
		Mode:                  store.RunnerModeHeadless,
		StartedAt:             time.Now().UTC().Format(time.RFC3339),
		FinishedAt:            time.Now().UTC().Format(time.RFC3339),
		Status:                store.InvocationStatusFinished,
		ExitReason:            "exited",
		LandingStatus:         store.LandingStatusLanded,
	}), "WriteInvocationMeta")

	configDir := filepath.Join(tmpDir, "config")
	srv := daemon.NewServer(st, cr, fsys, configDir)
	listener, err := net.Listen("unix", st.DaemonSocketPath())
	require.NoError(t, err, "listen daemon socket")
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(listener) }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-serveDone
	})
	client := daemonclient.NewClient(st.DaemonSocketPath())
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	require.NoError(t, client.WaitForReady(waitCtx, 5*time.Second), "daemon not ready")

	var prSyncStdout, prSyncStderr bytes.Buffer
	require.NoError(t, WorktreePRSync(ctx, cr, fsys, repoRoot, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoIdentity.RepoID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &prSyncStdout, &prSyncStderr), "worktree pr sync failed\nstderr:\n%s", prSyncStderr.String())

	var prSyncPayload struct {
		PRNumber int `json:"pr_number"`
	}
	require.NoError(t, json.Unmarshal(prSyncStdout.Bytes(), &prSyncPayload), "decode pr sync JSON")
	prNumber := prSyncPayload.PRNumber
	require.NotZero(t, prNumber, "pr_number not recorded")

	merged := false
	t.Cleanup(func() {
		if !merged && prNumber != 0 {
			_, _ = cr.Run(ctx, "gh", []string{"pr", "close", fmt.Sprintf("%d", prNumber), "-R", repo}, exec.RunOpts{
				Env: nonInteractiveEnv(),
			})
		}
		_, _ = cr.Run(ctx, "git", []string{"-C", repoRoot, "push", "origin", "--delete", branch}, exec.RunOpts{
			Env: nonInteractiveEnv(),
		})
	})

	var mergeStdout, mergeStderr bytes.Buffer
	require.NoError(t, WorktreePRMerge(ctx, cr, fsys, repoRoot, WorktreePRMergeOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoIdentity.RepoID,
		Yes:             true,
		DataDirOverride: dataDir,
	}, &mergeStdout, &mergeStderr), "worktree pr merge failed\nstderr:\n%s", mergeStderr.String())

	merged = true
	runCmdAllowMissingRemoteRef(t, ctx, cr, repoRoot, "git", "push", "origin", "--delete", branch)
}

func nonInteractiveEnv() map[string]string {
	return map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PROMPT_DISABLED":  "1",
		"CI":                  "1",
	}
}

func runCmd(t *testing.T, ctx context.Context, cr exec.CommandRunner, dir, name string, args ...string) {
	t.Helper()
	result, err := cr.Run(ctx, name, args, exec.RunOpts{
		Dir: dir,
		Env: nonInteractiveEnv(),
	})
	require.NoError(t, err, "%s %s", name, strings.Join(args, " "))
	require.Equal(t, 0, result.ExitCode, "%s %s exited %d: %s", name, strings.Join(args, " "), result.ExitCode, result.Stderr)
}

func requireGHAuth(t *testing.T, ctx context.Context, cr exec.CommandRunner) {
	t.Helper()

	statusResult, err := cr.Run(ctx, "gh", []string{"auth", "status"}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	require.NoError(t, err, "gh auth status")
	if statusResult.ExitCode == 0 {
		return
	}

	combined := strings.ToLower(statusResult.Stdout + "\n" + statusResult.Stderr)
	if strings.Contains(combined, "active account: true") && strings.Contains(combined, "logged in to github.com") {
		t.Logf("gh auth status exited %d but reports active account; continuing", statusResult.ExitCode)
		return
	}

	tokenResult, tokenErr := cr.Run(ctx, "gh", []string{"auth", "token"}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	require.NoError(t, tokenErr, "gh auth token")
	if tokenResult.ExitCode == 0 && strings.TrimSpace(tokenResult.Stdout) != "" {
		t.Logf("gh auth status exited %d but gh auth token succeeded; continuing", statusResult.ExitCode)
		return
	}

	require.Equal(t, 0, statusResult.ExitCode, "gh auth status exited %d: %s", statusResult.ExitCode, statusResult.Stderr)
}

func runCmdAllowMissingRemoteRef(t *testing.T, ctx context.Context, cr exec.CommandRunner, dir, name string, args ...string) {
	t.Helper()
	result, err := cr.Run(ctx, name, args, exec.RunOpts{
		Dir: dir,
		Env: nonInteractiveEnv(),
	})
	require.NoError(t, err, "%s %s", name, strings.Join(args, " "))
	if result.ExitCode != 0 {
		msg := result.Stderr + result.Stdout
		if strings.Contains(msg, "remote ref does not exist") {
			return
		}
		require.Equal(t, 0, result.ExitCode, "%s %s exited %d: %s", name, strings.Join(args, " "), result.ExitCode, result.Stderr)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755), "write %s", path)
}

func resolveDefaultBranch(t *testing.T, ctx context.Context, cr exec.CommandRunner, repoRoot, repo string) string {
	t.Helper()

	result, err := cr.Run(ctx, "git", []string{
		"-C", repoRoot,
		"branch", "--show-current",
	}, exec.RunOpts{})
	if err == nil && result.ExitCode == 0 {
		branch := strings.TrimSpace(result.Stdout)
		if branch != "" {
			return branch
		}
	}

	result, err = cr.Run(ctx, "gh", []string{
		"repo", "view", repo,
		"--json", "defaultBranchRef",
	}, exec.RunOpts{
		Env: nonInteractiveEnv(),
	})
	if err == nil && result.ExitCode == 0 {
		var payload struct {
			DefaultBranchRef struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
		}
		if json.Unmarshal([]byte(result.Stdout), &payload) == nil && payload.DefaultBranchRef.Name != "" {
			return payload.DefaultBranchRef.Name
		}
	}

	result, err = cr.Run(ctx, "git", []string{
		"-C", repoRoot,
		"ls-remote", "--symref", "origin", "HEAD",
	}, exec.RunOpts{})
	if err == nil && result.ExitCode == 0 {
		for _, line := range strings.Split(result.Stdout, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "ref:" && fields[len(fields)-1] == "HEAD" {
				ref := fields[1]
				if strings.HasPrefix(ref, "refs/heads/") {
					return strings.TrimPrefix(ref, "refs/heads/")
				}
			}
		}
	}

	result, err = cr.Run(ctx, "git", []string{
		"-C", repoRoot,
		"remote", "show", "origin",
	}, exec.RunOpts{})
	if err == nil && result.ExitCode == 0 {
		for _, line := range strings.Split(result.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "HEAD branch:") {
				branch := strings.TrimSpace(strings.TrimPrefix(line, "HEAD branch:"))
				if branch != "" && branch != "(unknown)" {
					return branch
				}
			}
		}
	}

	result, err = cr.Run(ctx, "git", []string{
		"-C", repoRoot,
		"ls-remote", "--heads", "origin",
	}, exec.RunOpts{})
	if err == nil && result.ExitCode == 0 {
		branches := parseRemoteBranches(result.Stdout)
		if branch := pickDefaultBranch(branches); branch != "" {
			return branch
		}
	}

	return "main"
}

func parseRemoteBranches(output string) []string {
	var branches []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := fields[1]
		if strings.HasPrefix(ref, "refs/heads/") {
			branches = append(branches, strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	return branches
}

func pickDefaultBranch(branches []string) string {
	preferred := []string{"main", "master", "trunk"}
	branchSet := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		branchSet[branch] = struct{}{}
	}
	for _, branch := range preferred {
		if _, ok := branchSet[branch]; ok {
			return branch
		}
	}
	if len(branches) > 0 {
		return branches[0]
	}
	return ""
}
