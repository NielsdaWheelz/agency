package commands

import (
	"bytes"
	"context"
	"encoding/json"
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

	defaultBranch := resolveDefaultBranch(t, ctx, cr, repoRoot, repo)

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
	runCmd(t, ctx, cr, repoRoot, "git", "fetch", "origin", defaultBranch)
	runCmd(t, ctx, cr, repoRoot, "git", "worktree", "add", "-b", branch, worktreePath, "origin/"+defaultBranch)

	scriptsDir := filepath.Join(worktreePath, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}

	writeScript(t, filepath.Join(scriptsDir, "agency_setup.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(scriptsDir, "agency_verify.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")
	writeScript(t, filepath.Join(scriptsDir, "agency_archive.sh"), "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n")

	agencyJSON := `{
  "version": 1,
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
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
	// Report must include ## summary and ## how to test sections per S7 spec4
	report := `# e2e test

## summary
e2e report: verifying push/merge works

## how to test
This is an automated e2e test - no manual testing required.
`
	if err := os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(report), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	e2eDir := filepath.Join(worktreePath, "e2e")
	if err := os.MkdirAll(e2eDir, 0o755); err != nil {
		t.Fatalf("mkdir e2e dir: %v", err)
	}
	logPath := filepath.Join(e2eDir, "gh_e2e.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open e2e log: %v", err)
	}
	if _, err := fmt.Fprintf(logFile, "%s %s\n", runID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = logFile.Close()
		t.Fatalf("write e2e log: %v", err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatalf("close e2e log: %v", err)
	}

	result, err := cr.Run(ctx, "git", []string{"check-ignore", "-q", ".agency/report.md"}, exec.RunOpts{
		Dir: worktreePath,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		t.Fatalf("git check-ignore .agency/report.md: %v", err)
	}
	reportIgnored := false
	switch result.ExitCode {
	case 0:
		reportIgnored = true
	case 1:
		reportIgnored = false
	default:
		t.Fatalf("git check-ignore .agency/report.md exited %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}

	addPaths := []string{
		"agency.json",
		"scripts/agency_setup.sh",
		"scripts/agency_verify.sh",
		"scripts/agency_archive.sh",
		"e2e/gh_e2e.log",
	}
	if !reportIgnored {
		addPaths = append(addPaths, ".agency/report.md")
	}
	runCmd(t, ctx, cr, worktreePath, "git", append([]string{"add"}, addPaths...)...)
	runCmd(t, ctx, cr, worktreePath, "git", "commit", "-m", "e2e: add agency config")

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
	meta := store.NewRunMeta(runID, repoIdentity.RepoID, "e2e", "claude", "claude", defaultBranch, branch, worktreePath, time.Now())
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
	runCmdAllowMissingRemoteRef(t, ctx, cr, repoRoot, "git", "push", "origin", "--delete", branch)
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

func runCmdAllowMissingRemoteRef(t *testing.T, ctx context.Context, cr exec.CommandRunner, dir, name string, args ...string) {
	t.Helper()
	result, err := cr.Run(ctx, name, args, exec.RunOpts{
		Dir: dir,
		Env: nonInteractiveEnv(),
	})
	if err != nil {
		t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	if result.ExitCode != 0 {
		msg := result.Stderr + result.Stdout
		if strings.Contains(msg, "remote ref does not exist") {
			return
		}
		t.Fatalf("%s %s exited %d: %s", name, strings.Join(args, " "), result.ExitCode, result.Stderr)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
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
