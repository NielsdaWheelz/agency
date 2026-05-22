package commands

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/watch"
)

// TaskStartOpts holds options for task start.
type TaskStartOpts struct {
	RepoRef            string
	Name               string
	BaseBranch         string
	Mode               string
	Runner             string
	InvocationName     string
	Detached           bool
	Prompt             string
	PromptFile         string
	AgencyConfigPath   string
	ExecutionProfile   string
	RunnerArgs         []string
	Model              string
	Effort             string
	PermissionMode     string
	JSON               bool
	NoIncludeUntracked bool

	IsInteractive func() bool
	TmuxAttachFn  func(context.Context, string) error
}

// TaskShowOpts holds options for showing one task.
type TaskShowOpts struct {
	TaskRef string
	RepoRef string
	JSON    bool
}

// TaskLSOpts holds options for listing tasks.
type TaskLSOpts struct {
	RepoRef  string
	AllRepos bool
	All      bool
	JSON     bool
}

// TaskArchiveOpts holds options for archiving one task.
type TaskArchiveOpts struct {
	TaskRef string
	RepoRef string
	JSON    bool
}

// TaskRetryOpts holds options for retrying one task.
type TaskRetryOpts struct {
	TaskRef            string
	RepoRef            string
	Mode               string
	Runner             string
	InvocationName     string
	Detached           bool
	Prompt             string
	PromptFile         string
	AgencyConfigPath   string
	ExecutionProfile   string
	RunnerArgs         []string
	Model              string
	Effort             string
	PermissionMode     string
	JSON               bool
	NoIncludeUntracked bool
	IsInteractive      func() bool
	TmuxAttachFn       func(context.Context, string) error
}

// TaskWatchOpts holds options for watching one task.
type TaskWatchOpts struct {
	TaskRef       string
	RepoRef       string
	IsInteractive func() bool
	Input         io.Reader
	Output        io.Writer
}

func TaskStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskStartOpts, stdout, stderr io.Writer) error {
	fail := commandFail(stdout, opts.JSON)
	if cr == nil {
		cr = exec.NewRealRunner()
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}
	taskName := strings.TrimSpace(opts.Name)
	if taskName == "" {
		return fail(errors.New(errors.EUsage, "use 'agency task start <name>'"))
	}
	mode, headless, err := validateStartMode(startModeOptions{
		Mode:          opts.Mode,
		Prompt:        opts.Prompt,
		PromptFile:    opts.PromptFile,
		Detached:      opts.Detached,
		IsInteractive: opts.IsInteractive,
	}, string(store.RunnerModeHeadless), "task start")
	if err != nil {
		return fail(err)
	}

	prompt := ""
	if headless {
		prompt, err = resolveBoundedPromptInput(
			opts.Prompt,
			opts.PromptFile,
			daemon.MaxPromptSize,
			"headless task start requires a prompt (use --prompt or --prompt-file)",
			"headless task start prompt cannot be empty",
		)
		if err != nil {
			return fail(err)
		}
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return fail(err)
	}
	repoRoot, baseRoot, err := resolveWorktreeCreateRoots(ctx, cr, ns, cwd, strings.TrimSpace(opts.RepoRef))
	if err != nil {
		return fail(err)
	}
	if opts.AgencyConfigPath != "" && !filepath.IsAbs(opts.AgencyConfigPath) {
		opts.AgencyConfigPath = filepath.Join(cwd, opts.AgencyConfigPath)
	}

	baseBranch := strings.TrimSpace(opts.BaseBranch)
	if baseBranch == "" {
		currentBranch, ok, err := git.GetCurrentBranch(ctx, cr, baseRoot, nil)
		if err != nil {
			return fail(errors.Wrap(errors.EBaseBranchNotFound, "failed to determine the current branch; pass --base explicitly", err))
		}
		if !ok {
			return fail(errors.NewWithDetails(errors.EBaseBranchNotFound, "failed to determine the current branch; pass --base explicitly", map[string]string{"repo_root": baseRoot}))
		}
		baseBranch = currentBranch
	}
	hasCommits, err := git.HasCommits(ctx, cr, baseRoot, nil)
	if err != nil {
		return fail(err)
	}
	if !hasCommits {
		return fail(errors.New(errors.EEmptyRepo, "repository has no commits; create an initial commit first"))
	}
	clean, err := git.IsCleanExcludingAgency(ctx, cr, baseRoot, nil)
	if err != nil {
		return fail(err)
	}
	if !clean {
		return fail(errors.New(errors.EBaseDirty, "the checkout used to resolve --base is dirty; commit or stash changes first"))
	}
	branchExists, err := git.BranchExists(ctx, cr, baseRoot, baseBranch, nil)
	if err != nil {
		return fail(err)
	}
	if !branchExists {
		return fail(errors.NewWithDetails(errors.EBaseBranchNotFound, "local base branch '"+baseBranch+"' was not found", map[string]string{"branch": baseBranch}))
	}

	runner, runnerArgs, err := resolveStartRunnerAndArgs(ctx, fsys, cwd, ns, repoRoot, "", startRunnerConfigOpts{
		Runner:           opts.Runner,
		RunnerArgs:       opts.RunnerArgs,
		Model:            opts.Model,
		Effort:           opts.Effort,
		PermissionMode:   opts.PermissionMode,
		AgencyConfigPath: opts.AgencyConfigPath,
		Headless:         headless,
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.TaskStart(ctx, daemon.TaskStartRequest{
		RepoRoot:           repoRoot,
		Name:               taskName,
		BaseBranch:         baseBranch,
		Mode:               mode,
		Runner:             runner,
		Prompt:             prompt,
		InvocationName:     strings.TrimSpace(opts.InvocationName),
		RunnerArgs:         runnerArgs,
		ExecutionProfile:   strings.TrimSpace(opts.ExecutionProfile),
		AgencyConfigPath:   opts.AgencyConfigPath,
		NoIncludeUntracked: opts.NoIncludeUntracked,
	})
	if err != nil {
		return fail(err)
	}

	if opts.JSON {
		return writeCommandJSON(stdout, taskStartJSON(resp))
	}

	_, _ = fmt.Fprintf(stdout, "Started task %s\n", resp.TaskName)
	_, _ = fmt.Fprintf(stdout, "  task:       %s\n", resp.TaskID)
	_, _ = fmt.Fprintf(stdout, "  worktree:   %s (%s)\n", resp.WorktreeName, resp.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:     %s\n", resp.Branch)
	_, _ = fmt.Fprintf(stdout, "  invocation: %s\n", resp.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  runner:     %s\n", resp.Runner)
	_, _ = fmt.Fprintf(stdout, "  mode:       %s\n", resp.Mode)
	_, _ = fmt.Fprintf(stdout, "  profile:    %s\n", resp.ExecutionProfile)
	_, _ = fmt.Fprintf(stdout, "  checkout_root: %s\n", resp.CheckoutRoot)
	if resp.Mode == store.RunnerModeHeaded {
		_, _ = fmt.Fprintf(stdout, "  tmux:       %s\n", resp.TmuxSession)
		if !opts.Detached {
			attachHeadedSession(ctx, headedAttachOpts{
				AttachFn:    opts.TmuxAttachFn,
				Stdout:      stdout,
				Stderr:      stderr,
				SessionName: resp.TmuxSession,
				Invocation:  resp.InvocationID,
				RepoID:      resp.RepoID,
				Banner:      true,
				LaterHint:   true,
			})
		} else {
			_, _ = fmt.Fprintf(stdout, "\nSession started in detached mode.\n")
		}
	} else if resp.PID != 0 {
		_, _ = fmt.Fprintf(stdout, "  pid:        %d\n", resp.PID)
	}
	_, _ = fmt.Fprintf(stdout, "\nUse 'agency task %s' to view status.\n", shortRef(resp.TaskID))
	if resp.InvocationID != "" {
		_, _ = fmt.Fprintf(stdout, "Use 'agency agent %s' to inspect the invocation.\n", shortRef(resp.InvocationID))
	}
	return nil
}

func TaskShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskShowOpts, stdout, stderr io.Writer) error {
	ns, repoID, err := resolveTaskCommandRepo(ctx, cr, fsys, cwd, opts.RepoRef)
	if err != nil {
		if opts.JSON {
			return writeCommandJSONError(stdout, err)
		}
		return err
	}
	result, err := ns.client.GetTask(ctx, opts.TaskRef, repoID)
	if err != nil {
		if opts.JSON {
			return writeCommandJSONError(stdout, err)
		}
		return err
	}
	if opts.JSON {
		return writeCommandJSON(stdout, result.Data)
	}
	printTaskDTO(stdout, result.Data)
	return nil
}

func TaskLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskLSOpts, stdout, stderr io.Writer) error {
	if fsys == nil {
		fsys = fs.NewRealFS()
	}
	ns, repoID, err := resolveTaskCommandRepo(ctx, cr, fsys, cwd, opts.RepoRef)
	if err != nil {
		if !opts.AllRepos {
			if opts.JSON {
				return writeCommandJSONError(stdout, err)
			}
			return err
		}
		ns, err = setupDaemonNav(ctx, fsys, "")
		if err != nil {
			if opts.JSON {
				return writeCommandJSONError(stdout, err)
			}
			return err
		}
	}
	var tasks []daemon.TaskDTO
	if opts.AllRepos {
		repos, err := ns.client.ListRepos(ctx)
		if err != nil {
			if opts.JSON {
				return writeCommandJSONError(stdout, err)
			}
			return err
		}
		for _, repo := range repos.Data.Repos {
			result, err := ns.client.ListTasks(ctx, repo.RepoID, opts.All)
			if err != nil {
				if opts.JSON {
					return writeCommandJSONError(stdout, err)
				}
				return err
			}
			tasks = append(tasks, result.Data.Tasks...)
		}
	} else {
		result, err := ns.client.ListTasks(ctx, repoID, opts.All)
		if err != nil {
			if opts.JSON {
				return writeCommandJSONError(stdout, err)
			}
			return err
		}
		tasks = result.Data.Tasks
	}
	if opts.JSON {
		return writeCommandJSON(stdout, daemon.ListTasksData{Tasks: tasks})
	}
	if len(tasks) == 0 {
		_, _ = fmt.Fprintln(stdout, "No tasks.")
		return nil
	}
	for _, task := range tasks {
		_, _ = fmt.Fprintf(stdout, "%s  %-10s  %-8s  %s\n", shortRef(task.TaskID), task.State, task.Mode, task.Name)
	}
	return nil
}

func TaskArchive(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskArchiveOpts, stdout, stderr io.Writer) error {
	ns, repoID, err := resolveTaskCommandRepo(ctx, cr, fsys, cwd, opts.RepoRef)
	if err != nil {
		if opts.JSON {
			return writeCommandJSONError(stdout, err)
		}
		return err
	}
	resp, err := ns.client.ArchiveTask(ctx, opts.TaskRef, repoID)
	if err != nil {
		if opts.JSON {
			return writeCommandJSONError(stdout, err)
		}
		return err
	}
	if opts.JSON {
		return writeCommandJSON(stdout, taskStartJSON(resp))
	}
	_, _ = fmt.Fprintf(stdout, "Archived task %s\n", resp.TaskID)
	return nil
}

func TaskRetry(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskRetryOpts, stdout, stderr io.Writer) error {
	fail := commandFail(stdout, opts.JSON)
	if fsys == nil {
		fsys = fs.NewRealFS()
	}
	mode, headless, err := validateStartMode(startModeOptions{
		Mode:          opts.Mode,
		Prompt:        opts.Prompt,
		PromptFile:    opts.PromptFile,
		Detached:      opts.Detached,
		IsInteractive: opts.IsInteractive,
	}, string(store.RunnerModeHeadless), "task retry")
	if err != nil {
		return fail(err)
	}
	prompt := ""
	if headless {
		prompt, err = resolveBoundedPromptInput(opts.Prompt, opts.PromptFile, daemon.MaxPromptSize, "headless task retry requires a prompt (use --prompt or --prompt-file)", "headless task retry prompt cannot be empty")
		if err != nil {
			return fail(err)
		}
	}
	ns, repo, err := resolveTaskCommandRepoData(ctx, cr, fsys, cwd, opts.RepoRef)
	if err != nil {
		return fail(err)
	}
	task, err := ns.client.GetTask(ctx, opts.TaskRef, repo.RepoID)
	if err != nil {
		return fail(err)
	}
	if opts.AgencyConfigPath != "" && !filepath.IsAbs(opts.AgencyConfigPath) {
		opts.AgencyConfigPath = filepath.Join(cwd, opts.AgencyConfigPath)
	}
	runnerInput := strings.TrimSpace(opts.Runner)
	if runnerInput == "" {
		runnerInput = task.Data.Runner
	}
	runner, runnerArgs, err := resolveStartRunnerAndArgs(ctx, fsys, cwd, ns, repo.PreferredRoot, repo.RepoID, startRunnerConfigOpts{
		Runner:           runnerInput,
		RunnerArgs:       opts.RunnerArgs,
		Model:            opts.Model,
		Effort:           opts.Effort,
		PermissionMode:   opts.PermissionMode,
		AgencyConfigPath: opts.AgencyConfigPath,
		Headless:         headless,
	})
	if err != nil {
		return fail(err)
	}
	resp, err := ns.client.RetryTask(ctx, opts.TaskRef, repo.RepoID, daemon.TaskRetryRequest{
		Mode:               mode,
		Runner:             runner,
		Prompt:             prompt,
		InvocationName:     opts.InvocationName,
		RunnerArgs:         runnerArgs,
		ExecutionProfile:   strings.TrimSpace(opts.ExecutionProfile),
		AgencyConfigPath:   opts.AgencyConfigPath,
		NoIncludeUntracked: opts.NoIncludeUntracked,
	})
	if err != nil {
		return fail(err)
	}
	if opts.JSON {
		return writeCommandJSON(stdout, taskStartJSON(resp))
	}
	_, _ = fmt.Fprintf(stdout, "Retried task %s\n", resp.TaskID)
	_, _ = fmt.Fprintf(stdout, "  invocation: %s\n", resp.InvocationID)
	_, _ = fmt.Fprintf(stdout, "  mode:       %s\n", resp.Mode)
	_, _ = fmt.Fprintf(stdout, "  profile:    %s\n", resp.ExecutionProfile)
	_, _ = fmt.Fprintf(stdout, "  checkout_root: %s\n", resp.CheckoutRoot)
	if resp.Mode == store.RunnerModeHeaded && !opts.Detached {
		attachHeadedSession(ctx, headedAttachOpts{
			AttachFn:    opts.TmuxAttachFn,
			Stdout:      stdout,
			Stderr:      stderr,
			SessionName: resp.TmuxSession,
		})
	}
	return nil
}

func TaskWatch(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts TaskWatchOpts, stdout, stderr io.Writer) error {
	ns, repoID, err := resolveTaskCommandRepo(ctx, cr, fsys, cwd, opts.RepoRef)
	if err != nil {
		return err
	}
	result, err := ns.client.GetTask(ctx, opts.TaskRef, repoID)
	if err != nil {
		return err
	}
	if result.Data.PrimaryInvocationID == "" {
		printTaskDTO(stdout, result.Data)
		return nil
	}
	return launchWatchWorkspace(ctx, cr, fsys, cwd, stdout, stderr, watchLaunchOptions{
		initialPage:   watch.InitialPageHistory,
		invocationID:  result.Data.PrimaryInvocationID,
		repoID:        result.Data.RepoID,
		input:         opts.Input,
		output:        opts.Output,
		isInteractive: opts.IsInteractive,
	})
}

func resolveTaskCommandRepo(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd, repoRef string) (*daemonNavSetup, string, error) {
	ns, repo, err := resolveTaskCommandRepoData(ctx, cr, fsys, cwd, repoRef)
	if err != nil {
		return nil, "", err
	}
	return ns, repo.RepoID, nil
}

func resolveTaskCommandRepoData(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd, repoRef string) (*daemonNavSetup, daemon.RepoDTO, error) {
	if cr == nil {
		cr = exec.NewRealRunner()
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return nil, daemon.RepoDTO{}, err
	}
	repo, _, err := resolveAgentStartRepo(ctx, cr, ns, cwd, repoRef)
	if err != nil {
		return nil, daemon.RepoDTO{}, err
	}
	return ns, repo, nil
}

func printTaskDTO(w io.Writer, task daemon.TaskDTO) {
	_, _ = fmt.Fprintf(w, "Task %s\n", task.Name)
	_, _ = fmt.Fprintf(w, "  task:       %s\n", task.TaskID)
	_, _ = fmt.Fprintf(w, "  state:      %s\n", task.State)
	_, _ = fmt.Fprintf(w, "  repo:       %s\n", task.RepoID)
	_, _ = fmt.Fprintf(w, "  profile:    %s\n", task.ExecutionProfile)
	_, _ = fmt.Fprintf(w, "  checkout_root: %s\n", task.CheckoutRoot)
	if task.WorktreeID != "" {
		_, _ = fmt.Fprintf(w, "  worktree:   %s (%s)\n", task.WorktreeName, task.WorktreeID)
		_, _ = fmt.Fprintf(w, "  branch:     %s\n", task.Branch)
	}
	if task.PrimaryInvocationID != "" {
		_, _ = fmt.Fprintf(w, "  invocation: %s\n", task.PrimaryInvocationID)
		_, _ = fmt.Fprintf(w, "  runner:     %s\n", task.Runner)
		_, _ = fmt.Fprintf(w, "  mode:       %s\n", task.Mode)
	}
	if task.State == store.TaskStateFailed {
		_, _ = fmt.Fprintf(w, "  failed:     %s\n", task.FailedPhase)
		_, _ = fmt.Fprintf(w, "  error:      %s\n", task.Error)
	}
}

func taskStartJSON(resp *daemon.TaskStartResponse) any {
	return struct {
		commandJSONBase
		Duplicate        bool             `json:"duplicate,omitempty"`
		TaskID           string           `json:"task_id,omitempty"`
		TaskName         string           `json:"task_name,omitempty"`
		State            store.TaskState  `json:"state,omitempty"`
		RepoID           string           `json:"repo_id,omitempty"`
		RepoName         string           `json:"repo_name,omitempty"`
		WorktreeID       string           `json:"worktree_id,omitempty"`
		WorktreeName     string           `json:"worktree_name,omitempty"`
		WorktreePath     string           `json:"worktree_path,omitempty"`
		Branch           string           `json:"branch,omitempty"`
		ExecutionProfile string           `json:"execution_profile,omitempty"`
		CheckoutRoot     string           `json:"checkout_root,omitempty"`
		CustomEnvKeys    []string         `json:"custom_env_keys,omitempty"`
		InvocationID     string           `json:"invocation_id,omitempty"`
		SandboxPath      string           `json:"sandbox_path,omitempty"`
		Mode             store.RunnerMode `json:"mode,omitempty"`
		Runner           string           `json:"runner,omitempty"`
		PID              int              `json:"pid,omitempty"`
		PGID             int              `json:"pgid,omitempty"`
		TmuxSession      string           `json:"tmux_session,omitempty"`
		DaemonInstanceID string           `json:"daemon_instance_id,omitempty"`
		LogPaths         *daemon.LogPaths `json:"log_paths,omitempty"`
	}{
		commandJSONBase:  newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, resp.ClientRequestID, resp.RequestID),
		Duplicate:        resp.Duplicate,
		TaskID:           resp.TaskID,
		TaskName:         resp.TaskName,
		State:            resp.State,
		RepoID:           resp.RepoID,
		RepoName:         resp.RepoName,
		WorktreeID:       resp.WorktreeID,
		WorktreeName:     resp.WorktreeName,
		WorktreePath:     resp.WorktreePath,
		Branch:           resp.Branch,
		ExecutionProfile: resp.ExecutionProfile,
		CheckoutRoot:     resp.CheckoutRoot,
		CustomEnvKeys:    slices.Clone(resp.CustomEnvKeys),
		InvocationID:     resp.InvocationID,
		SandboxPath:      resp.SandboxPath,
		Mode:             resp.Mode,
		Runner:           resp.Runner,
		PID:              resp.PID,
		PGID:             resp.PGID,
		TmuxSession:      resp.TmuxSession,
		DaemonInstanceID: resp.DaemonInstanceID,
		LogPaths:         resp.LogPaths,
	}
}

func shortRef(ref string) string {
	if len(ref) <= 8 {
		return ref
	}
	return ref[:8]
}
