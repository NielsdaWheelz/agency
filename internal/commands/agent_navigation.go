package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// AgentPathOpts holds options for the agent path command.
type AgentPathOpts struct {
	InvocationRef string
	RepoFlag      string
}

// AgentPath outputs the daemon-resolved sandbox path for an invocation.
func AgentPath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentPathOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent path", nil)
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        opts.InvocationRef,
		},
	}, deps)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, result.ResolvedPath)
	return nil
}

// AgentOpenOpts holds options for the agent open command.
type AgentOpenOpts struct {
	InvocationRef   string
	RepoFlag        string
	Editor          string // override for tests; empty uses config/env/default
	DataDirOverride string
}

// AgentOpen opens the sandbox directory in the configured editor.
func AgentOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent open", nil)
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        opts.InvocationRef,
		},
	}, deps)
	if err != nil {
		return err
	}

	sandboxPath := result.ResolvedPath

	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"sandbox_path":  sandboxPath,
				"hint":          "sandbox was removed after landing or discarding",
			},
		)
	}

	editor := opts.Editor
	if editor == "" {
		userCfg, _, _ := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
		editor = userCfg.Defaults.Editor
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "code"
	}

	runResult, runErr := runAttachedInDir(ctx, editor, []string{sandboxPath}, sandboxPath)
	if runErr != nil {
		return errors.Wrap(errors.EEditorNotConfigured, "failed to open editor", runErr)
	}
	if runResult.ExitCode != 0 {
		return errors.WithExitCode(
			errors.New(errors.EInternal, fmt.Sprintf("editor exited with code %d", runResult.ExitCode)),
			runResult.ExitCode,
		)
	}

	return nil
}

// AgentShellOpts holds options for the agent shell command.
type AgentShellOpts struct {
	InvocationRef string
	RepoFlag      string
}

// AgentShell opens a shell with cwd set to the daemon-resolved sandbox path.
func AgentShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShellOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent shell", nil)
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        opts.InvocationRef,
		},
	}, deps)
	if err != nil {
		return err
	}

	sandboxPath := result.ResolvedPath

	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"sandbox_path":  sandboxPath,
				"hint":          "sandbox was removed after landing or discarding",
			},
		)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	runResult, runErr := runAttachedInDir(ctx, shell, []string{"-l"}, sandboxPath)
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

// AgentEnterOpts holds options for the agent enter command.
type AgentEnterOpts struct {
	InvocationRef string
	RepoFlag      string

	IsInteractive   func() bool
	TmuxClient      tmux.Client
	TmuxAttachFn    func(sessionName string) error
	DataDirOverride string
}

// AgentEnter attaches to a running headed invocation via daemon-first resolution.
// Headed-only: headless invocations return E_INVOCATION_INVALID_MODE.
func AgentEnter(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentEnterOpts, stdout, stderr io.Writer) error {
	isInteractive := opts.IsInteractive
	if isInteractive == nil {
		isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) }
	}
	if !isInteractive() {
		return errors.NewWithDetails(
			errors.ENotInteractive,
			"this command requires an interactive terminal",
			map[string]string{
				"hint": "run this command in an interactive terminal, or use a non-interactive alternative",
			},
		)
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	deps := ns.buildNavDeps(cr, cwd, opts.RepoFlag, "agent enter", isInteractive)
	result, err := ResolveNavigation(ctx, NavigationIntent{
		Selection: NavigationSelection{
			TargetKind: TargetInvocation,
			Ref:        opts.InvocationRef,
		},
		RequiresTTY: true,
	}, deps)
	if err != nil {
		return err
	}

	invocationResult, err := ns.client.GetInvocation(ctx, result.ResolvedID, result.ResolvedRepoID)
	if err != nil {
		return err
	}
	if invocationResult.Data.Mode != "headed" {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"invocation is headless; enter is only supported for headed invocations",
			map[string]string{
				"invocation_id": result.ResolvedID,
				"mode":          invocationResult.Data.Mode,
				"hint":          "use 'agency agent logs' to view headless invocation output",
			},
		)
	}

	sessionName := tmux.SessionName(result.ResolvedID)

	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	exists, checkErr := tmuxClient.HasSession(ctx, sessionName)
	if checkErr != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not check tmux session status: %v\n", checkErr)
	}
	if !exists {
		return errors.NewWithDetails(
			errors.ESessionEnded,
			"tmux session not found",
			map[string]string{
				"session_name":  sessionName,
				"invocation_id": result.ResolvedID,
				"hint":          "session ended; use 'agency agent logs' or 'agency agent open' to view",
			},
		)
	}

	attachFn := opts.TmuxAttachFn
	if attachFn == nil {
		attachFn = realTmuxAttach
	}
	return attachFn(sessionName)
}

// Shared navigation kernel setup for agent path/open/shell/enter.
func (ns *daemonNavSetup) buildNavDeps(cr exec.CommandRunner, cwd, repoFlag, cmdName string, isInteractive func() bool) NavigationDeps {
	return NavigationDeps{
		ResolveRepo: func(ctx context.Context) (*RepoContextResult, error) {
			return ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
				RepoFlag:      repoFlag,
				AllowAllRepos: false,
				CmdName:       cmdName,
			})
		},
		GetInvocation: func(ctx context.Context, ref, repoID string) (*NavigationResult, error) {
			result, err := ns.client.GetInvocation(ctx, ref, repoID)
			if err != nil {
				return nil, err
			}
			return &NavigationResult{
				TargetKind:     TargetInvocation,
				ResolvedRepoID: result.Data.RepoID,
				ResolvedID:     result.Data.InvocationID,
				ResolvedPath:   result.Data.SandboxPath,
			}, nil
		},
		IsInteractive: isInteractive,
	}
}
