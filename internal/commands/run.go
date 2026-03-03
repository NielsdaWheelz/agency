// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/pipeline"
	"github.com/NielsdaWheelz/agency/internal/runservice"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// RunOpts holds options for the run command.
type RunOpts struct {
	// Name is the run name (required, validated).
	Name string

	// RepoPath is the optional --repo flag to target a specific repo.
	RepoPath string

	// Runner is the runner name (empty = use user config default).
	Runner string

	// Parent is the parent branch (empty = use current branch).
	Parent string

	// Attach indicates whether to attach after tmux creation.
	Attach bool

	// Open opens the created workspace after creation and skips auto-attach.
	Open bool
}

// RunResult holds the result of a successful run for output formatting.
type RunResult struct {
	RunID           string
	Name            string
	Runner          string
	Parent          string
	Branch          string
	WorktreePath    string
	TmuxSessionName string
	Warnings        []pipeline.Warning
}

type runExecutionDeps struct {
	executePipeline func(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, targetCwd string, opts RunOpts) (string, error)
	loadResult      func(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, runID string) (*RunResult, error)
	openWorkspace   func(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, worktreePath string) error
	attachSession   func(ctx context.Context, sessionName string) error
}

func (d runExecutionDeps) withDefaults() runExecutionDeps {
	if d.executePipeline == nil {
		d.executePipeline = executeRunPipeline
	}
	if d.loadResult == nil {
		d.loadResult = getRunResult
	}
	if d.openWorkspace == nil {
		d.openWorkspace = openCreatedWorkspace
	}
	if d.attachSession == nil {
		d.attachSession = attachToTmuxSessionRun
	}
	return d
}

// Run executes the agency run command.
// Creates a workspace, runs setup, starts tmux session.
func Run(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts RunOpts, stdout, stderr io.Writer) error {
	targetCwd, err := resolveRunTargetCwd(ctx, cr, cwd, opts)
	if err != nil {
		return err
	}
	return runWithDeps(ctx, cr, fsys, targetCwd, opts, stdout, stderr, runExecutionDeps{})
}

func resolveRunTargetCwd(ctx context.Context, cr agencyexec.CommandRunner, cwd string, opts RunOpts) (string, error) {
	// Handle --repo path: if provided, use it instead of cwd
	targetCwd := cwd
	if opts.RepoPath != "" {
		// Validate the path exists
		info, err := os.Stat(opts.RepoPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", errors.NewWithDetails(
					errors.EInvalidRepoPath,
					fmt.Sprintf("--repo path does not exist: %s", opts.RepoPath),
					map[string]string{"path": opts.RepoPath},
				)
			}
			return "", errors.Wrap(errors.EInvalidRepoPath, "failed to stat --repo path", err)
		}
		if !info.IsDir() {
			return "", errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--repo path is not a directory: %s", opts.RepoPath),
				map[string]string{"path": opts.RepoPath},
			)
		}

		// Verify it's inside a git repo
		repoRoot, err := git.GetRepoRoot(ctx, cr, opts.RepoPath)
		if err != nil {
			return "", errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--repo path is not inside a git repository: %s", opts.RepoPath),
				map[string]string{"path": opts.RepoPath},
			)
		}
		targetCwd = repoRoot.Path
	}
	return targetCwd, nil
}

func executeRunPipeline(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, targetCwd string, opts RunOpts) (string, error) {
	// Create the run service with explicit dependencies and working directory.
	svc := runservice.NewWithDeps(cr, fsys)
	svc.SetWorkingDir(targetCwd)

	// Create the pipeline
	p := pipeline.NewPipeline(svc)

	// Execute the pipeline
	pipelineOpts := pipeline.RunPipelineOpts{
		Name:   opts.Name,
		Runner: opts.Runner,
		Parent: opts.Parent,
		Attach: opts.Attach,
	}

	return p.Run(ctx, pipelineOpts)
}

func runWithDeps(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, targetCwd string, opts RunOpts, stdout, stderr io.Writer, deps runExecutionDeps) error {
	deps = deps.withDefaults()

	runID, err := deps.executePipeline(ctx, cr, fsys, targetCwd, opts)
	if err != nil {
		// Print error details for failures after worktree creation
		printRunError(ctx, cr, stderr, err, runID, targetCwd, fsys)
		return err
	}

	// Get final state from metadata
	result, err := deps.loadResult(ctx, cr, fsys, targetCwd, runID)
	if err != nil {
		// Pipeline succeeded but couldn't read result - internal error
		return errors.Wrap(errors.EInternal, "failed to read run result", err)
	}

	attachAfterCreate := opts.Attach && !opts.Open

	// Print success output (show "next:" hint whenever we are not auto-attaching).
	printRunSuccess(stdout, result, !attachAfterCreate)

	// Print warnings to stderr
	for _, w := range result.Warnings {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", w.Message)
	}

	if opts.Open {
		openErr := deps.openWorkspace(ctx, cr, fsys, result.WorktreePath)
		emitOpenOnCreateStatus(stdout, stderr, openErr)
	}

	// Handle attach (default) - skip if --detached was specified
	if attachAfterCreate && result.TmuxSessionName != "" {
		return deps.attachSession(ctx, result.TmuxSessionName)
	}

	return nil
}

// getRunResult reads the run metadata and constructs the result.
func getRunResult(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, runID string) (*RunResult, error) {
	// Resolve repo root
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return nil, err
	}

	// Get origin info for repo identity
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)

	// Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Compute repo identity
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)
	repoID := repoIdentity.RepoID

	// Create store and read meta
	st := store.NewStore(fsys, dataDir, nil)
	meta, err := st.ReadMeta(repoID, runID)
	if err != nil {
		return nil, err
	}

	return &RunResult{
		RunID:           meta.RunID,
		Name:            meta.Name,
		Runner:          meta.Runner,
		Parent:          meta.ParentBranch,
		Branch:          meta.Branch,
		WorktreePath:    meta.WorktreePath,
		TmuxSessionName: meta.TmuxSessionName,
	}, nil
}

// printRunSuccess prints the success output in the required format.
// All writes use explicit error ignoring since this is informational output
// where write failures cannot be meaningfully handled.
// The detached parameter controls whether to print the "next:" hint.
func printRunSuccess(w io.Writer, result *RunResult, detached bool) {
	_, _ = fmt.Fprintf(w, "run_id: %s\n", result.RunID)
	_, _ = fmt.Fprintf(w, "name: %s\n", result.Name)
	_, _ = fmt.Fprintf(w, "runner: %s\n", result.Runner)
	_, _ = fmt.Fprintf(w, "parent: %s\n", result.Parent)
	_, _ = fmt.Fprintf(w, "branch: %s\n", result.Branch)
	_, _ = fmt.Fprintf(w, "worktree: %s\n", result.WorktreePath)
	_, _ = fmt.Fprintf(w, "tmux: %s\n", result.TmuxSessionName)
	if detached {
		_, _ = fmt.Fprintf(w, "next: agency attach %s\n", result.Name)
	}
}

// printRunError prints error details for run failures.
// All writes use explicit error ignoring since this is informational output
// where write failures cannot be meaningfully handled.
func printRunError(ctx context.Context, cr agencyexec.CommandRunner, w io.Writer, err error, runID string, cwd string, fsys fs.FS) {
	ae, ok := errors.AsAgencyError(err)
	if !ok {
		_, _ = fmt.Fprintf(w, "error: %s\n", err.Error())
		return
	}

	// Print error line
	_, _ = fmt.Fprintf(w, "error: %s: %s\n", ae.Code, ae.Msg)

	// Print run_id if we have one (means worktree was likely created)
	if runID != "" {
		_, _ = fmt.Fprintf(w, "run_id: %s\n", runID)
	}

	// Print evidence paths if available in details
	if ae.Details != nil {
		if wp := ae.Details["worktree_path"]; wp != "" {
			_, _ = fmt.Fprintf(w, "worktree: %s\n", wp)
		}
		if lp := ae.Details["log_path"]; lp != "" {
			_, _ = fmt.Fprintf(w, "setup_log: %s\n", lp)
		}
	}

	// Try to get worktree path from meta if we have a run_id
	if runID != "" && ae.Details["worktree_path"] == "" {
		if result, err := tryGetRunMeta(ctx, cr, cwd, runID, fsys); err == nil {
			_, _ = fmt.Fprintf(w, "worktree: %s\n", result.WorktreePath)
		}
	}
}

// tryGetRunMeta attempts to read run metadata for error reporting.
func tryGetRunMeta(ctx context.Context, cr agencyexec.CommandRunner, cwd, runID string, fsys fs.FS) (*store.RunMeta, error) {
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return nil, err
	}

	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)

	// Compute repo identity
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)
	repoID := repoIdentity.RepoID

	// Resolve data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	dataDir := dirs.DataDir

	// Read meta
	st := store.NewStore(fsys, dataDir, nil)
	return st.ReadMeta(repoID, runID)
}

// attachToTmuxSessionRun attaches to a tmux session for the run command.
func attachToTmuxSessionRun(ctx context.Context, sessionName string) error {
	result, err := agencyexec.RunAttached(ctx, "tmux", []string{"attach", "-t", sessionName}, agencyexec.AttachedRunOpts{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return errors.Wrap(errors.ETmuxAttachFailed, "tmux attach failed", err)
	}
	if result.ExitCode != 0 {
		return errors.New(errors.ETmuxAttachFailed, fmt.Sprintf("tmux attach failed with exit code %d", result.ExitCode))
	}
	return nil
}

func openCreatedWorkspace(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, worktreePath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	userCfg, _, err := config.LoadUserConfig(fsys, dirs.ConfigDir)
	if err != nil {
		return err
	}

	editorName := userCfg.Defaults.Editor
	if editorName == "" {
		editorName = os.Getenv("EDITOR")
	}

	editorCmd, err := config.ResolveEditorCmd(cr, fsys, dirs.ConfigDir, userCfg, editorName)
	if err != nil {
		return err
	}

	result, runErr := agencyexec.RunAttached(ctx, editorCmd, []string{worktreePath}, agencyexec.AttachedRunOpts{
		Dir:    worktreePath,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if runErr != nil {
		return errors.Wrap(errors.EInternal, "failed to run editor command", runErr)
	}
	if result.ExitCode != 0 {
		return errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", result.ExitCode))
	}
	return nil
}
