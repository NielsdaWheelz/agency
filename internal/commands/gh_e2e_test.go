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
	configDir := filepath.Join(tmpDir, "config")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", configDir)
	t.Setenv("AGENCY_CACHE_DIR", filepath.Join(tmpDir, "cache"))
	require.NoError(t, os.MkdirAll(dataDir, 0o700), "mkdir data dir")
	require.NoError(t, os.MkdirAll(configDir, 0o700), "mkdir config dir")
	userConfig, err := json.MarshalIndent(map[string]any{
		"version": 4,
		"defaults": map[string]any{
			"runner":            "claude-code",
			"editor":            "code",
			"execution_profile": "personal",
		},
		"runners": map[string]any{
			"claude-code": "claude",
		},
		"editors": map[string]any{
			"code": "code",
		},
		"execution_profiles": map[string]any{
			"personal": map[string]any{
				"env": map[string]any{
					"GH_TOKEN": token,
				},
			},
		},
	}, "", "  ")
	require.NoError(t, err, "marshal user config")
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), append(userConfig, '\n'), 0o600), "write user config")

	repoRoot := filepath.Join(tmpDir, "repo")
	requireGHAuth(t, ctx, cr)
	runCmd(t, ctx, cr, "", "gh", "auth", "setup-git")
	runCmd(t, ctx, cr, "", "gh", "repo", "clone", repo, repoRoot)

	defaultBranch := resolveDefaultBranch(t, ctx, cr, repoRoot, repo)

	runID, err := core.NewRunID(time.Now())
	require.NoError(t, err, "runID")

	agencyJSON := `{
  "version": 4,
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
  },
  "execution": {
    "profile": "personal",
    "checkout_root": "repo-sibling"
  }
}
`
	repoScriptsDir := filepath.Join(repoRoot, "scripts")
	require.NoError(t, os.MkdirAll(repoScriptsDir, 0o755), "mkdir repo scripts")
	writeScript(t, filepath.Join(repoScriptsDir, "agency_setup.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(repoScriptsDir, "agency_verify.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(repoScriptsDir, "agency_archive.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "agency.json"), []byte(agencyJSON), 0o644), "write repo agency.json")

	st := store.NewStore(fsys, dataDir, time.Now)
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
	registeredRepo, err := client.RegisterRepo(ctx, repoRoot)
	require.NoError(t, err, "RegisterRepo")
	repoID := registeredRepo.Data.RepoID
	require.NotEmpty(t, repoID, "registered repo id")
	createResp, err := client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:   repoRoot,
		Name:       "e2e-" + runID,
		BaseBranch: defaultBranch,
	})
	require.NoError(t, err, "WorktreeCreate")
	require.True(t, createResp.OK, "worktree create response")
	require.Equal(t, repoID, createResp.RepoID, "worktree repo id")
	require.Equal(t, "personal", createResp.ExecutionProfile, "worktree execution profile")
	require.Equal(t, filepath.Join(filepath.Dir(registeredRepo.Data.PreferredRoot), ".agency", "checkouts", repoID), createResp.CheckoutRoot, "worktree checkout root")
	worktreeID := createResp.WorktreeID
	branch := createResp.Branch
	worktreePath := createResp.TreePath
	require.NotEmpty(t, worktreeID, "worktree id")
	require.NotEmpty(t, branch, "worktree branch")
	require.Equal(t, filepath.Join(createResp.CheckoutRoot, "worktrees", "e2e-"+runID+"-"+core.ShortID(worktreeID)), worktreePath, "worktree path")

	// Runner status lives under .agency/state/ and is read locally by PR sync.
	stateDir := filepath.Join(worktreePath, ".agency", "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755), "mkdir runner status")
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "runner_status.json"), []byte(`{
  "schema_version": "2.0",
  "state": "succeeded",
  "updated_at": "2026-04-20T00:00:00Z",
  "summary": "e2e runner status: verifying agent pr sync + merge works",
  "questions": [],
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

	// Repo-shared config lives at the registered canonical repo root; the PR
	// branch only carries unique run data plus runner status when it is tracked.
	addPaths := []string{
		fmt.Sprintf("e2e/%s/", runID),
	}
	if !runnerStatusIgnored {
		addPaths = append(addPaths, ".agency/state/runner_status.json")
	}
	runCmd(t, ctx, cr, worktreePath, "git", append([]string{"add"}, addPaths...)...)
	runCmd(t, ctx, cr, worktreePath, "git", "commit", "-m", "e2e: "+runID)

	var prSyncStdout, prSyncStderr bytes.Buffer
	require.NoError(t, WorktreePRSync(ctx, cr, fsys, repoRoot, WorktreePRSyncOpts{
		WorktreeRef:     worktreeID,
		RepoRef:         repoID,
		JSON:            true,
		DataDirOverride: dataDir,
	}, &prSyncStdout, &prSyncStderr), "worktree pr sync failed\nstderr:\n%s", prSyncStderr.String())

	var prSyncPayload struct {
		OK        bool   `json:"ok"`
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
		Hint      string `json:"hint"`
		PRNumber  int    `json:"pr_number"`
	}
	require.NoError(t, json.Unmarshal(prSyncStdout.Bytes(), &prSyncPayload), "decode pr sync JSON")
	require.True(t, prSyncPayload.OK, "pr sync failed: code=%s message=%s hint=%s stdout=%s stderr=%s", prSyncPayload.ErrorCode, prSyncPayload.Message, prSyncPayload.Hint, prSyncStdout.String(), prSyncStderr.String())
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
		RepoRef:         repoID,
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
