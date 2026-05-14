package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// AgentPathOpts holds options for the agent path command.
type AgentPathOpts struct {
	InvocationRef string
	RepoRef       string
}

// AgentPath outputs the daemon-resolved sandbox path for an invocation.
func AgentPath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentPathOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
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
	RepoRef         string
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
		RepoRef:       opts.RepoRef,
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

	editorCmd, err := resolveEditorCmdWithOptionalOverride(cr, fsys, ns.dirs.ConfigDir, opts.Editor)
	if err != nil {
		return err
	}

	runResult, runErr := runAttachedInDir(ctx, editorCmd, []string{sandboxPath}, sandboxPath)
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
	RepoRef       string
}

// AgentShell opens a shell with cwd set to the daemon-resolved sandbox path.
func AgentShell(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShellOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
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

// AgentAttachOpts holds options for the agent attach command.
type AgentAttachOpts struct {
	InvocationRef string
	RepoRef       string

	IsInteractive   func() bool
	TmuxAttachFn    func(sessionName string) error
	DataDirOverride string
}

// AgentClientsOpts holds options for the agent clients command.
type AgentClientsOpts struct {
	InvocationRef   string
	RepoRef         string
	DataDirOverride string
}

// AgentAttach attaches to a live headed invocation via daemon-authored session facts.
func AgentAttach(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentAttachOpts, stdout, stderr io.Writer) error {
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
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent attach",
	})
	if err != nil {
		return err
	}

	session, err := ns.client.GetInvocationSession(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "invocation")
	}
	if session.Data.SessionStatus != "live" {
		invocationID := strings.TrimSpace(session.Data.InvocationID)
		if invocationID == "" {
			invocationID = strings.TrimSpace(opts.InvocationRef)
		}
		return errors.NewWithDetails(
			errors.ESessionEnded,
			"tmux session not found",
			map[string]string{
				"session_name":  strings.TrimSpace(session.Data.TmuxSession),
				"invocation_id": invocationID,
				"hint":          "session ended; use history, transcript, logs, or recreate to inspect the invocation",
			},
		)
	}
	if strings.TrimSpace(session.Data.TmuxSession) == "" {
		return errors.New(errors.EInternal, "daemon session read did not include a tmux session name")
	}

	attachFn := opts.TmuxAttachFn
	if attachFn == nil {
		attachFn = realTmuxAttach
	}
	return attachFn(session.Data.TmuxSession)
}

// AgentClients prints the currently connected tmux clients for a headed invocation session.
func AgentClients(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentClientsOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "agent clients",
	})
	if err != nil {
		return err
	}

	session, err := ns.client.GetInvocationSession(ctx, opts.InvocationRef, repoCtx.RepoID)
	if err != nil {
		return translateNavigationError(err, "invocation")
	}
	if session.Data.SessionStatus != "live" {
		invocationID := strings.TrimSpace(session.Data.InvocationID)
		if invocationID == "" {
			invocationID = strings.TrimSpace(opts.InvocationRef)
		}
		return errors.NewWithDetails(
			errors.ESessionEnded,
			"tmux session not found",
			map[string]string{
				"session_name":  strings.TrimSpace(session.Data.TmuxSession),
				"invocation_id": invocationID,
				"hint":          "session ended; use 'agency agent <invocation-ref> recreate' to start a new headed session in the same sandbox",
			},
		)
	}
	_, _ = fmt.Fprintf(stdout, "invocation: %s\n", session.Data.InvocationID)
	_, _ = fmt.Fprintf(stdout, "repo: %s\n", session.Data.RepoID)
	_, _ = fmt.Fprintf(stdout, "tmux session: %s\n", session.Data.TmuxSession)
	_, _ = fmt.Fprintf(stdout, "connected clients: %d\n", session.Data.ClientCount)
	if len(session.Data.ConnectedClients) == 0 {
		_, _ = fmt.Fprintln(stdout, "(no connected clients)")
		return nil
	}
	for _, client := range session.Data.ConnectedClients {
		line := strings.TrimSpace(client.TTY)
		if line == "" {
			line = "<unknown tty>"
		}
		if client.Name != "" {
			line += " (" + client.Name + ")"
		}
		if client.PID > 0 {
			line += " pid=" + strconv.Itoa(client.PID)
		}
		if client.ReadOnly {
			line += " read-only"
		} else {
			line += " read-write"
		}
		_, _ = fmt.Fprintln(stdout, line)
	}
	return nil
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

// realTmuxAttach performs the canonical tmux client handoff with stdin/stdout/stderr connected.
func realTmuxAttach(sessionName string) error {
	args := []string{"attach-session", "-t", sessionName}
	if os.Getenv("TMUX") != "" {
		args = []string{"switch-client", "-t", sessionName}
	}
	result, err := exec.RunAttached(context.Background(), "tmux", args, exec.AttachedRunOpts{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tmux command exited with code %d", result.ExitCode)
	}
	return nil
}

// isTerminal returns true if the given file descriptor is a terminal.
func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}
