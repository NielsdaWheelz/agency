package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

func TestGHE2EPushMerge(t *testing.T) {
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

	ctx := context.Background()
	cr := exec.NewRealRunner()
	fsys := fs.NewRealFS()

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	t.Setenv("AGENCY_DATA_DIR", dataDir)
	t.Setenv("AGENCY_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("AGENCY_CACHE_DIR", filepath.Join(tmpDir, "cache"))

	repoRoot := filepath.Join(tmpDir, "repo")
	runCmd(t, ctx, cr, "", "gh", "auth", "status")
	runCmd(t, ctx, cr, "", "gh", "auth", "setup-git")
	runCmd(t, ctx, cr, "", "gh", "repo", "clone", repo, repoRoot)

	runCmd(t, ctx, cr, repoRoot, "git", "config", "user.email", "agency-e2e@users.noreply.github.com")
	runCmd(t, ctx, cr, repoRoot, "git", "config", "user.name", "agency-e2e")

	runID, err := core.NewRunID(time.Now())
	if err != nil {
		t.Fatalf("runID: %v", err)
	}
	branch := fmt.Sprintf("agency/e2e-%s", runID)

	originInfo := git.GetOriginInfo(ctx, cr, repoRoot)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot, originInfo.URL)
	if repoIdentity.RepoID == "" {
		t.Fatal("repoID empty")
	}

	worktreePath := filepath.Join(dataDir, "repos", repoIdentity.RepoID, "worktrees", runID)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}
	runCmd(t, ctx, cr, repoRoot, "git", "fetch", "origin", "main")
	runCmd(t, ctx, cr, repoRoot, "git", "worktree", "add", "-b", branch, worktreePath, "origin/main")

	scriptsDir := filepath.Join(worktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}

	writeScript(t, filepath.Join(scriptsDir, "agency_setup.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(scriptsDir, "agency_verify.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(scriptsDir, "agency_archive.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")

	agencyJSON := `{
  "version": 1,
  "defaults": {
    "parent_branch": "main",
    "runner": "claude"
  },
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
  },
  "runners": {
    "claude": "claude"
  }
}
`
	if err := os.WriteFile(filepath.Join(worktreePath, "agency.json"), []byte(agencyJSON), 0o644); err != nil {
		t.Fatalf("write agency.json: %v", err)
	}

	reportDir := filepath.Join(worktreePath, ".agency")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report: %v", err)
	}
	report := "e2e report: verifying push/merge works"
	if err := os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(report), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	changePath := filepath.Join(worktreePath, "e2e.txt")
	if err := os.WriteFile(changePath, []byte(runID+"\n"), 0o644); err != nil {
		t.Fatalf("write change: %v", err)
	}
	runCmd(t, ctx, cr, worktreePath, "git", "add", "e2e.txt")
	runCmd(t, ctx, cr, worktreePath, "git", "commit", "-m", "e2e: "+runID)

	st := store.NewStore(fsys, dataDir, time.Now)
	if _, err := st.EnsureRunDir(repoIdentity.RepoID, runID); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	meta := store.NewRunMeta(runID, repoIdentity.RepoID, "e2e", "claude", "claude", "main", branch, worktreePath, time.Now())
	if err := st.WriteInitialMeta(repoIdentity.RepoID, runID, meta); err != nil {
		t.Fatalf("WriteInitialMeta: %v", err)
	}

	var pushStdout, pushStderr bytes.Buffer
	if err := Push(ctx, cr, fsys, worktreePath, PushOpts{RunID: runID}, &pushStdout, &pushStderr); err != nil {
		t.Fatalf("push failed: %v\nstderr:\n%s", err, pushStderr.String())
	}

	meta, err = st.ReadMeta(repoIdentity.RepoID, runID)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	prNumber := meta.PRNumber
	if prNumber == 0 {
		t.Fatal("pr_number not recorded")
	}

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

	origInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = origInteractive })

	var mergeStdout, mergeStderr bytes.Buffer
	mergeOpts := MergeOpts{
		RunID:      runID,
		TmuxClient: noopTmuxClient{},
	}
	if err := Merge(ctx, cr, fsys, worktreePath, mergeOpts, strings.NewReader("merge\n"), &mergeStdout, &mergeStderr); err != nil {
		t.Fatalf("merge failed: %v\nstderr:\n%s", err, mergeStderr.String())
	}

	merged = true
	runCmd(t, ctx, cr, repoRoot, "git", "push", "origin", "--delete", branch)
}

func runCmd(t *testing.T, ctx context.Context, cr exec.CommandRunner, dir, name string, args ...string) {
	t.Helper()
	result, err := cr.Run(ctx, name, args, exec.RunOpts{
		Dir: dir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("%s %s exited %d: %s", name, strings.Join(args, " "), result.ExitCode, result.Stderr)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type noopTmuxClient struct{}

func (noopTmuxClient) HasSession(context.Context, string) (bool, error) { return false, nil }
func (noopTmuxClient) NewSession(context.Context, string, string, []string) error {
	return nil
}
func (noopTmuxClient) Attach(context.Context, string) error { return nil }
func (noopTmuxClient) KillSession(context.Context, string) error {
	return nil
}
func (noopTmuxClient) SendKeys(context.Context, string, []tmux.Key) error {
	return nil
}
