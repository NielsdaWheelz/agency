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
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
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
	RepoPath string
	All      bool
	JSON     bool
}

// WorktreeLS lists integration worktrees.
func WorktreeLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeLSOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Determine repo scope
	var repoID string
	if opts.RepoPath != "" {
		repoRoot, repoIDFromPath, err := ResolveRepoContext(ctx, cr, cwd, opts.RepoPath)
		if err != nil {
			return err
		}
		repoID = repoIDFromPath
		_ = repoRoot
	} else {
		// Try CWD-based repo discovery
		repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
		if err != nil {
			return errors.New(errors.ENoRepo, "not inside a git repository; use --repo to specify")
		}
		originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
		repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)
		repoID = repoIdentity.RepoID
	}

	// Scan worktrees
	records, err := store.ScanIntegrationWorktreesForRepo(dirs.DataDir, repoID)
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to scan integration worktrees", err)
	}

	// Filter by state unless --all
	var filtered []store.IntegrationWorktreeRecord
	for _, r := range records {
		if r.Broken {
			if opts.All {
				filtered = append(filtered, r)
			}
			continue
		}
		if r.Meta.State == store.WorktreeStateArchived && !opts.All {
			continue
		}
		filtered = append(filtered, r)
	}

	// Output
	if opts.JSON {
		return writeWorktreeLSJSON(stdout, filtered)
	}

	return writeWorktreeLSHuman(stdout, filtered)
}

func writeWorktreeLSJSON(w io.Writer, records []store.IntegrationWorktreeRecord) error {
	type jsonRecord struct {
		WorktreeID   string `json:"worktree_id"`
		Name         string `json:"name,omitempty"`
		Branch       string `json:"branch,omitempty"`
		ParentBranch string `json:"parent_branch,omitempty"`
		TreePath     string `json:"tree_path,omitempty"`
		State        string `json:"state,omitempty"`
		CreatedAt    string `json:"created_at,omitempty"`
		Broken       bool   `json:"broken,omitempty"`
	}

	out := make([]jsonRecord, len(records))
	for i, r := range records {
		jr := jsonRecord{
			WorktreeID: r.WorktreeID,
			Broken:     r.Broken,
		}
		if r.Meta != nil {
			jr.Name = r.Meta.Name
			jr.Branch = r.Meta.Branch
			jr.ParentBranch = r.Meta.ParentBranch
			jr.TreePath = r.Meta.TreePath
			jr.State = string(r.Meta.State)
			jr.CreatedAt = r.Meta.CreatedAt
		}
		out[i] = jr
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeWorktreeLSHuman(w io.Writer, records []store.IntegrationWorktreeRecord) error {
	if len(records) == 0 {
		_, _ = fmt.Fprintln(w, "No integration worktrees found.")
		return nil
	}

	for _, r := range records {
		if r.Broken {
			_, _ = fmt.Fprintf(w, "%s  [broken]\n", r.WorktreeID)
			continue
		}
		state := ""
		if r.Meta.State == store.WorktreeStateArchived {
			state = " [archived]"
		}
		_, _ = fmt.Fprintf(w, "%s  %s  %s%s\n", r.WorktreeID, r.Meta.Name, r.Meta.Branch, state)
	}

	return nil
}

// WorktreeShowOpts holds options for the worktree show command.
type WorktreeShowOpts struct {
	WorktreeRef string
	JSON        bool
}

// WorktreeShow shows details of an integration worktree.
func WorktreeShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShowOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)

	record, err := svc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, true)
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EWorktreeBroken,
			"worktree exists but meta.json is unreadable or invalid",
			map[string]string{
				"worktree_id":  record.WorktreeID,
				"worktree_dir": record.WorktreeDir,
				"hint":         "inspect or remove the directory manually",
			},
		)
	}

	// Output
	if opts.JSON {
		return writeWorktreeShowJSON(stdout, record)
	}

	return writeWorktreeShowHuman(stdout, record)
}

func writeWorktreeShowJSON(w io.Writer, r *store.IntegrationWorktreeRecord) error {
	type jsonRecord struct {
		WorktreeID   string `json:"worktree_id"`
		Name         string `json:"name"`
		RepoID       string `json:"repo_id"`
		Branch       string `json:"branch"`
		ParentBranch string `json:"parent_branch"`
		TreePath     string `json:"tree_path"`
		State        string `json:"state"`
		CreatedAt    string `json:"created_at"`
		WorktreeDir  string `json:"worktree_dir"`
	}

	out := jsonRecord{
		WorktreeID:   r.WorktreeID,
		Name:         r.Meta.Name,
		RepoID:       r.Meta.RepoID,
		Branch:       r.Meta.Branch,
		ParentBranch: r.Meta.ParentBranch,
		TreePath:     r.Meta.TreePath,
		State:        string(r.Meta.State),
		CreatedAt:    r.Meta.CreatedAt,
		WorktreeDir:  r.WorktreeDir,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeWorktreeShowHuman(w io.Writer, r *store.IntegrationWorktreeRecord) error {
	_, _ = fmt.Fprintf(w, "worktree_id:   %s\n", r.WorktreeID)
	_, _ = fmt.Fprintf(w, "name:          %s\n", r.Meta.Name)
	_, _ = fmt.Fprintf(w, "branch:        %s\n", r.Meta.Branch)
	_, _ = fmt.Fprintf(w, "parent_branch: %s\n", r.Meta.ParentBranch)
	_, _ = fmt.Fprintf(w, "state:         %s\n", r.Meta.State)
	_, _ = fmt.Fprintf(w, "created_at:    %s\n", r.Meta.CreatedAt)
	_, _ = fmt.Fprintf(w, "tree_path:     %s\n", r.Meta.TreePath)
	return nil
}

// WorktreePathOpts holds options for the worktree path command.
type WorktreePathOpts struct {
	WorktreeRef string
}

// WorktreePath outputs the path to an integration worktree.
func WorktreePath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePathOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)

	record, err := svc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
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

	// Output just the path
	_, _ = fmt.Fprintln(stdout, record.Meta.TreePath)
	return nil
}

// WorktreeOpenOpts holds options for the worktree open command.
type WorktreeOpenOpts struct {
	WorktreeRef string
	Editor      string
}

// WorktreeOpen opens an integration worktree in the configured editor.
func WorktreeOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeOpenOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)

	record, err := svc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
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
}

// WorktreeShell opens a shell in an integration worktree.
func WorktreeShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShellOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)

	record, err := svc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
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
	Force       bool
}

// WorktreeRm removes an integration worktree.
// PR-06: Routes through daemon for single-writer ownership.
func WorktreeRm(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeRmOpts, stdout, stderr io.Writer) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree locally first to get worktree name for display
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	svc := integrationworktree.NewService(st, cr, fsys, time.Now)

	record, err := svc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
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

	// Call daemon to remove worktree
	result, err := client.WorktreeRm(ctx, repoIdentity.RepoID, worktreeID, opts.Force)
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
