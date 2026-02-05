// Package commands implements agency CLI commands.
// This file implements integration worktree commands (Slice 8 PR-01, PR-06).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"time"

	"github.com/google/uuid"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// WorktreeCreateOpts holds options for the worktree create command.
type WorktreeCreateOpts struct {
	Name         string
	ParentBranch string
	Open         bool
	Editor       string
}

// WorktreeCreate creates a new integration worktree.
// PR-06: Routes through daemon for single-writer ownership.
func WorktreeCreate(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeCreateOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Validate repo context (basic check - daemon does full validation)
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}

	// Check parent tree is clean (daemon doesn't check this)
	clean, err := git.IsClean(ctx, cr, repoRoot.Path)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New(errors.EParentDirty, "working tree has uncommitted changes; commit or stash before creating a worktree")
	}

	// Auto-start daemon if not running
	client := daemonclient.NewClient(dirs.DataDir + "/agencyd.sock")
	if !client.IsRunning(ctx) {
		if err := daemonclient.AutoStartDaemon(ctx, dirs.DataDir); err != nil {
			return errors.Wrap(errors.EDaemonStartFailed, "failed to auto-start daemon", err)
		}
		// Wait for daemon to be ready
		if err := client.WaitForReady(ctx, 10*time.Second); err != nil {
			return err
		}
	}

	// Check API version compatibility
	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// Generate idempotency key
	idempotencyKey := uuid.New().String()

	// Call daemon to create worktree
	result, err := client.WorktreeCreate(ctx, daemonclient.WorktreeCreateOpts{
		RepoRoot:       repoRoot.Path,
		Name:           opts.Name,
		ParentBranch:   opts.ParentBranch,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return err
	}

	if !result.OK {
		return errors.NewWithDetails(
			errors.Code(result.ErrorCode),
			result.Message,
			map[string]string{"hint": result.Hint},
		)
	}

	// Output result
	_, _ = fmt.Fprintf(stdout, "Created integration worktree '%s'\n", opts.Name)
	_, _ = fmt.Fprintf(stdout, "  worktree_id: %s\n", result.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:      %s\n", result.Branch)
	_, _ = fmt.Fprintf(stdout, "  path:        %s\n", result.TreePath)

	// Open in editor if requested
	if opts.Open {
		editorName := opts.Editor
		userCfg, found, _ := config.LoadUserConfig(fsys, dirs.ConfigDir)
		if found && editorName == "" {
			editorName = userCfg.Defaults.Editor
		}

		editorCmd, err := config.ResolveEditorCmd(cr, fsys, dirs.ConfigDir, userCfg, editorName)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: could not resolve editor: %v\n", err)
		} else {
			cmd := osexec.Command(editorCmd, result.TreePath)
			cmd.Dir = result.TreePath
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			_ = cmd.Run()
		}
	}

	return nil
}

// WorktreeLSOpts holds options for the worktree ls command.
type WorktreeLSOpts struct {
	RepoPath string // deprecated: use RepoFlag
	RepoFlag string // PR-A: --repo <repo_id>
	AllRepos bool   // PR-A: --all-repos
	All      bool
	JSON     bool
}

// WorktreeLS lists integration worktrees.
// PR-12: Routes through daemon read API - CLI never reads store directly.
// PR-A: Supports --repo / --all-repos for CWD-less operation.
func WorktreeLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeLSOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// PR-A: Resolve repo context via daemon
	// Support legacy --repo path as well as new --repo <id>
	repoFlag := opts.RepoFlag
	if repoFlag == "" && opts.RepoPath != "" {
		// Legacy path-based --repo: register it and get the repo_id
		result, regErr := client.RegisterRepo(ctx, opts.RepoPath)
		if regErr != nil {
			return regErr
		}
		repoFlag = result.RepoID
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      repoFlag,
		AllRepos:      opts.AllRepos,
		AllowAllRepos: true,
		CmdName:       "worktree ls",
	})
	if err != nil {
		return err
	}

	// Call daemon ListWorktrees endpoint
	state := "present"
	if opts.All {
		state = "all"
	}

	var repoID string
	if !repoCtx.AllRepos {
		repoID = repoCtx.RepoID
	}

	result, err := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{
		RepoID: repoID,
		State:  state,
	})
	if err != nil {
		return err
	}

	// Output
	if opts.JSON {
		return writeWorktreeLSJSONFromDTO(stdout, result.Worktrees)
	}

	return writeWorktreeLSHumanFromDTO(stdout, result.Worktrees)
}

// writeWorktreeLSJSONFromDTO outputs worktree list as JSON from daemon DTOs.
// PR-12: CLI renders daemon-provided data - no local derivation.
func writeWorktreeLSJSONFromDTO(w io.Writer, worktrees []daemon.WorktreeDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(worktrees)
}

// writeWorktreeLSHumanFromDTO outputs worktree list in human-readable format from daemon DTOs.
// PR-12: CLI renders daemon-provided data.
func writeWorktreeLSHumanFromDTO(w io.Writer, worktrees []daemon.WorktreeDTO) error {
	if len(worktrees) == 0 {
		_, _ = fmt.Fprintln(w, "No integration worktrees found.")
		return nil
	}

	for _, wt := range worktrees {
		state := ""
		if wt.State == "archived" {
			state = " [archived]"
		}
		_, _ = fmt.Fprintf(w, "%s  %s  %s%s\n", wt.WorktreeID, wt.Name, wt.Branch, state)
	}

	return nil
}

// WorktreeShowOpts holds options for the worktree show command.
type WorktreeShowOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
	JSON        bool
}

// WorktreeShow shows details of an integration worktree.
// PR-12: Routes through daemon read API - CLI never reads store directly.
// PR-A: Supports --repo for CWD-less operation.
func WorktreeShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShowOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Ensure daemon is running
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	// PR-A: Resolve repo context via daemon (single-ref command, no --all-repos)
	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "worktree show",
	})
	if err != nil {
		return err
	}

	result, err := client.GetWorktree(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	// Output
	if opts.JSON {
		return writeWorktreeShowJSONFromDTO(stdout, &result.Worktree)
	}

	return writeWorktreeShowHumanFromDTO(stdout, &result.Worktree)
}

// writeWorktreeShowJSONFromDTO outputs worktree details as JSON from daemon DTO.
// PR-12: CLI renders daemon-provided data.
func writeWorktreeShowJSONFromDTO(w io.Writer, wt *daemon.WorktreeDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(wt)
}

// writeWorktreeShowHumanFromDTO outputs worktree details in human-readable format from daemon DTO.
// PR-12: CLI renders daemon-provided data.
func writeWorktreeShowHumanFromDTO(w io.Writer, wt *daemon.WorktreeDTO) error {
	_, _ = fmt.Fprintf(w, "worktree_id:   %s\n", wt.WorktreeID)
	_, _ = fmt.Fprintf(w, "name:          %s\n", wt.Name)
	_, _ = fmt.Fprintf(w, "repo_id:       %s\n", wt.RepoID)
	_, _ = fmt.Fprintf(w, "branch:        %s\n", wt.Branch)
	_, _ = fmt.Fprintf(w, "parent_branch: %s\n", wt.ParentBranch)
	_, _ = fmt.Fprintf(w, "state:         %s\n", wt.State)
	_, _ = fmt.Fprintf(w, "created_at:    %s\n", wt.CreatedAt)
	_, _ = fmt.Fprintf(w, "tree_path:     %s\n", wt.TreePath)
	return nil
}

// WorktreePathOpts holds options for the worktree path command.
type WorktreePathOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
}

// WorktreePath outputs the path to an integration worktree.
// PR-A: Supports --repo for CWD-less operation.
func WorktreePath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePathOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "worktree path",
	})
	if err != nil {
		return err
	}

	result, err := client.GetWorktree(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, result.Worktree.TreePath)
	return nil
}

// WorktreeOpenOpts holds options for the worktree open command.
type WorktreeOpenOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
	Editor      string
}

// WorktreeOpen opens an integration worktree in the configured editor.
// PR-A: Supports --repo for CWD-less operation.
func WorktreeOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeOpenOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "worktree open",
	})
	if err != nil {
		return err
	}

	// Resolve worktree via local store (for tree path)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)
	record, err := svc.Resolve(repoCtx.RepoID, opts.WorktreeRef, false)
	if err != nil {
		return err
	}

	if record.Broken || record.Meta == nil {
		return errors.NewWithDetails(
			errors.EWorktreeBroken,
			"worktree exists but meta.json is unreadable or invalid",
			map[string]string{"worktree_id": record.WorktreeID},
		)
	}

	treePath := record.Meta.TreePath

	// Resolve editor
	userCfg, found, _ := config.LoadUserConfig(fsys, dirs.ConfigDir)
	editorName := opts.Editor
	if found && editorName == "" {
		editorName = userCfg.Defaults.Editor
	}

	editorCmd, err := config.ResolveEditorCmd(cr, fsys, dirs.ConfigDir, userCfg, editorName)
	if err != nil {
		return err
	}

	cmd := osexec.Command(editorCmd, treePath)
	cmd.Dir = treePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			return errors.WithExitCode(
				errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", exitErr.ExitCode())),
				exitErr.ExitCode(),
			)
		}
		return errors.Wrap(errors.EInternal, "failed to run editor command", err)
	}

	return nil
}

// WorktreeShellOpts holds options for the worktree shell command.
type WorktreeShellOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
}

// WorktreeShell opens a shell in an integration worktree.
// PR-A: Supports --repo for CWD-less operation.
func WorktreeShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShellOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "worktree shell",
	})
	if err != nil {
		return err
	}

	// Resolve worktree via local store (for tree path)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)
	record, err := svc.Resolve(repoCtx.RepoID, opts.WorktreeRef, false)
	if err != nil {
		return err
	}

	if record.Broken || record.Meta == nil {
		return errors.NewWithDetails(
			errors.EWorktreeBroken,
			"worktree exists but meta.json is unreadable or invalid",
			map[string]string{"worktree_id": record.WorktreeID},
		)
	}

	treePath := record.Meta.TreePath

	// Get shell from environment
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	// Spawn shell as child process
	cmd := osexec.Command(shell, "-l")
	cmd.Dir = treePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			return errors.WithExitCode(
				errors.New(errors.EInternal, fmt.Sprintf("shell exited with code %d", exitErr.ExitCode())),
				exitErr.ExitCode(),
			)
		}
		return errors.Wrap(errors.EInternal, "failed to run shell", err)
	}

	return nil
}

// WorktreeRmOpts holds options for the worktree rm command.
type WorktreeRmOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
	Force       bool
}

// WorktreeRm removes an integration worktree.
// PR-06: Routes through daemon for single-writer ownership.
// PR-A: Supports --repo for CWD-less operation.
func WorktreeRm(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeRmOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return err
	}

	if err := client.CheckAPIVersion(ctx); err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "worktree rm",
	})
	if err != nil {
		return err
	}

	// Resolve worktree locally first to get worktree name for display
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)
	record, err := svc.Resolve(repoCtx.RepoID, opts.WorktreeRef, false)
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EWorktreeBroken,
			"worktree exists but meta.json is unreadable or invalid; remove manually",
			map[string]string{
				"worktree_id":  record.WorktreeID,
				"worktree_dir": record.WorktreeDir,
			},
		)
	}

	worktreeName := record.Meta.Name
	worktreeID := record.WorktreeID

	// Call daemon to remove worktree
	result, err := client.WorktreeRm(ctx, repoCtx.RepoID, worktreeID, opts.Force)
	if err != nil {
		return err
	}

	if !result.OK {
		return errors.NewWithDetails(
			errors.Code(result.ErrorCode),
			result.Message,
			map[string]string{"hint": result.Hint},
		)
	}

	_, _ = fmt.Fprintf(stdout, "Removed integration worktree '%s' (%s)\n", worktreeName, worktreeID)
	return nil
}
