package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"golang.org/x/term"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
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

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent path",
	})
	if err != nil {
		return err
	}

	invocation, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "invocation")
	}

	_, _ = fmt.Fprintln(stdout, invocation.Data.SandboxPath)
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

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent open",
	})
	if err != nil {
		return err
	}

	invocation, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "invocation")
	}
	sandboxPath := invocation.Data.SandboxPath

	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": invocation.Data.InvocationID,
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

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent shell",
	})
	if err != nil {
		return err
	}

	invocation, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "invocation")
	}
	sandboxPath := invocation.Data.SandboxPath

	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": invocation.Data.InvocationID,
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

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent enter",
	})
	if err != nil {
		return err
	}

	invocation, err := ns.client.GetInvocation(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "invocation")
	}
	if invocation.Data.Mode != "headed" {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"invocation is headless; enter is only supported for headed invocations",
			map[string]string{
				"invocation_id": invocation.Data.InvocationID,
				"mode":          invocation.Data.Mode,
				"hint":          "use 'agency agent logs' to view headless invocation output",
			},
		)
	}

	sessionName := tmux.SessionName(invocation.Data.InvocationID)

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
				"invocation_id": invocation.Data.InvocationID,
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

func translateNavigationError(err error, targetKind string) error {
	dre, isDaemonErr := daemonclient.AsDaemonReadError(err)

	code := errors.GetCode(err)
	if code == errors.EWorktreeIDAmbiguous || code == errors.EInvocationIDAmbiguous {
		details := map[string]string{
			"target_kind": targetKind,
		}

		if isDaemonErr {
			candidates := dre.Candidates()
			if len(candidates) > 0 {
				candidatesJSON, _ := json.Marshal(candidates)
				details["candidates"] = string(candidatesJSON)
				details["candidate_count"] = strconv.Itoa(len(candidates))
			}
			if dre.Hint != "" {
				details["hint"] = dre.Hint
			}
		}

		ae, _ := errors.AsAgencyError(err)
		msg := "ambiguous target"
		if ae != nil {
			msg = ae.Msg
		}

		return errors.NewWithDetails(errors.EAmbiguous, msg, details)
	}

	if isDaemonErr && dre.Hint != "" {
		return errors.NewWithDetails(code, dre.AgencyErr.Msg, map[string]string{"hint": dre.Hint})
	}

	return err
}

// realTmuxAttach performs a real interactive tmux attach with stdin/stdout/stderr connected.
// This is the only way to get proper interactive terminal behavior.
func realTmuxAttach(sessionName string) error {
	result, err := exec.RunAttached(context.Background(), "tmux", []string{"attach", "-t", sessionName}, exec.AttachedRunOpts{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tmux attach exited with code %d", result.ExitCode)
	}
	return nil
}

// isTerminal returns true if the given file descriptor is a terminal.
func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}
