// Package commands implements agency CLI commands.
// This file implements integration worktree commands (Slice 8 PR-01, PR-06).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
		openErr := openCreatedWorktree(ctx, cr, fsys, dirs.ConfigDir, result.TreePath, opts.Editor)
		emitOpenOnCreateStatus(stdout, stderr, openErr)
	}

	return nil
}

func openCreatedWorktree(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, configDir, treePath, editorName string) error {
	userCfg, _, err := config.LoadUserConfig(fsys, configDir)
	if err != nil {
		return err
	}
	if editorName == "" {
		editorName = userCfg.Defaults.Editor
	}
	if editorName == "" {
		editorName = os.Getenv("EDITOR")
	}

	editorCmd, err := config.ResolveEditorCmd(cr, fsys, configDir, userCfg, editorName)
	if err != nil {
		return err
	}
	runResult, runErr := exec.RunAttached(ctx, editorCmd, []string{treePath}, exec.AttachedRunOpts{
		Dir:    treePath,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if runErr != nil {
		return runErr
	}
	if runResult.ExitCode != 0 {
		return fmt.Errorf("editor exited with code %d", runResult.ExitCode)
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

	// Watch mode (PR-B): re-render on interval with ANSI clear-screen.
	Watch    bool
	Interval time.Duration // default 500ms, min 250ms, max 5s

	// SleepFn overrides time.Sleep for testing. If nil, uses time.Sleep.
	SleepFn func(time.Duration)

	// MaxIterations limits watch iterations for testing. 0 = unlimited.
	MaxIterations int
}

// WorktreeLS lists integration worktrees.
// PR-12: Routes through daemon read API - CLI never reads store directly.
// PR-A: Supports --repo / --all-repos for CWD-less operation.
// PR-B: Supports --watch for ANSI clear-screen redraw polling.
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

	// Non-watch mode
	if !opts.Watch {
		result, fetchErr := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{
			RepoID: repoID,
			State:  state,
		})
		if fetchErr != nil {
			return fetchErr
		}
		if opts.JSON {
			return writeWorktreeLSJSONFromDTO(stdout, result.Worktrees)
		}
		return writeWorktreeLSHumanFromDTO(stdout, result.Worktrees)
	}

	// Watch mode (PR-B)
	fetchAndRender := func(w io.Writer) error {
		result, fetchErr := client.ListWorktrees(ctx, daemonclient.ListWorktreesOpts{
			RepoID: repoID,
			State:  state,
		})
		if fetchErr != nil {
			return fetchErr
		}
		return writeWorktreeLSHumanFromDTO(w, result.Worktrees)
	}
	return watchLoop(ctx, stdout, stderr, opts.Interval, opts.SleepFn, opts.MaxIterations, fetchAndRender)
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

	result, err := client.GetWorktreeRich(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return err
	}

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
// S2-PR03: Routes through shared navigation kernel for daemon-first resolution.
func WorktreePath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePathOpts, stdout, stderr io.Writer) error {
	ns, err := setupWorktreeNav(ctx, fsys)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "worktree path")
	intent := NavigationIntent{
		CommandFamily: "worktree",
		Verb:          "path",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            opts.WorktreeRef,
		},
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, result.ResolvedPath)
	return nil
}

// WorktreeOpenOpts holds options for the worktree open command.
type WorktreeOpenOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
	Editor      string
}

// WorktreeOpen opens an integration worktree in the configured editor.
// S2-PR03: Routes through shared navigation kernel for daemon-first resolution.
func WorktreeOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeOpenOpts, stdout, stderr io.Writer) error {
	ns, err := setupWorktreeNav(ctx, fsys)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "worktree open")
	intent := NavigationIntent{
		CommandFamily: "worktree",
		Verb:          "open",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            opts.WorktreeRef,
		},
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	treePath := result.ResolvedPath

	userCfg, found, _ := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
	editorName := opts.Editor
	if found && editorName == "" {
		editorName = userCfg.Defaults.Editor
	}

	editorCmd, err := config.ResolveEditorCmd(cr, fsys, ns.dirs.ConfigDir, userCfg, editorName)
	if err != nil {
		return err
	}

	runResult, runErr := exec.RunAttached(ctx, editorCmd, []string{treePath}, exec.AttachedRunOpts{
		Dir:    treePath,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "failed to run editor command", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
	}

	return nil
}

// WorktreeShellOpts holds options for the worktree shell command.
type WorktreeShellOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
}

// WorktreeShell opens a shell in an integration worktree.
// S2-PR03: Routes through shared navigation kernel for daemon-first resolution.
func WorktreeShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeShellOpts, stdout, stderr io.Writer) error {
	ns, err := setupWorktreeNav(ctx, fsys)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "worktree shell")
	intent := NavigationIntent{
		CommandFamily: "worktree",
		Verb:          "shell",
		Selection: NavigationSelection{
			SelectorSource: SelectorExplicitRef,
			TargetKind:     TargetWorktree,
			Ref:            opts.WorktreeRef,
		},
		Interactive: true,
	}

	result, err := ResolveNavigation(ctx, intent, deps)
	if err != nil {
		return err
	}

	treePath := result.ResolvedPath

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	runResult, runErr := exec.RunAttached(ctx, shell, []string{"-l"}, exec.AttachedRunOpts{
		Dir:    treePath,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "failed to run shell", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("shell exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Shared navigation kernel setup for worktree path/open/shell (S2-PR03)
// ---------------------------------------------------------------------------

type worktreeNavSetup struct {
	dirs   paths.Dirs
	client *daemonclient.Client
}

func setupWorktreeNav(ctx context.Context, fsys fs.FS) (*worktreeNavSetup, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	socketPath := st.DaemonSocketPath()
	logPath := st.DaemonLogPath()

	client, err := daemonclient.EnsureDaemonRunning(ctx, socketPath, logPath)
	if err != nil {
		return nil, err
	}

	return &worktreeNavSetup{dirs: dirs, client: client}, nil
}

func (ns *worktreeNavSetup) buildNavDeps(cr exec.CommandRunner, cwd, repoFlag, cmdName string) NavigationDeps {
	return NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			return ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
				RepoFlag:      repoFlag,
				AllowAllRepos: false,
				CmdName:       cmdName,
			})
		},
		EnsureDaemon:    func(ctx context.Context) error { return nil },
		CheckAPIVersion: func(ctx context.Context) error { return ns.client.CheckAPIVersion(ctx) },
		GetWorktree: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			result, err := ns.client.GetWorktreeRich(ctx, ref, repoID)
			if err != nil {
				return nil, err
			}
			return &NavigationResult{
				TargetKind:       TargetWorktree,
				ResolvedRepoID:   result.Worktree.RepoID,
				ResolvedID:       result.Worktree.WorktreeID,
				ResolvedPath:     result.Worktree.TreePath,
				ResolutionSource: "daemon_get_worktree",
			}, nil
		},
		IsInteractive: func() bool { return false },
	}
}

// WorktreeRmOpts holds options for the worktree rm command.
type WorktreeRmOpts struct {
	WorktreeRef string
	RepoFlag    string // PR-A: --repo <repo_id>
	Force       bool
	Yes         bool

	// IsInteractive reports whether stdin/stderr are interactive terminals.
	// If nil, defaults to checking os.Stdin + os.Stderr.
	IsInteractive func() bool

	// ConfirmationIn provides interactive confirmation input.
	// If nil, defaults to os.Stdin.
	ConfirmationIn io.Reader
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

	if !opts.Yes {
		isInteractiveFn := opts.IsInteractive
		if isInteractiveFn == nil {
			isInteractiveFn = func() bool { return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()) }
		}
		if !isInteractiveFn() {
			return errors.NewWithDetails(
				errors.EConfirmationRequired,
				"non-interactive worktree remove requires explicit confirmation",
				map[string]string{
					"hint": "re-run with --yes",
				},
			)
		}

		_, _ = fmt.Fprint(stderr, "confirm: type 'rm' to proceed: ")
		confirmationIn := opts.ConfirmationIn
		if confirmationIn == nil {
			confirmationIn = os.Stdin
		}
		token, err := readBoundedMergeConfirmationToken(confirmationIn, maxMergeConfirmationBytes)
		if err != nil {
			return err
		}
		if token != "rm" {
			return errors.New(errors.EAborted, "worktree remove confirmation failed; expected 'rm'")
		}
	}

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
