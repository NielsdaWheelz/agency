package commands

import (
	"context"
	"encoding/json"
	stderrors "errors"
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
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// AgentPathOpts holds options for the agent path command.
type AgentPathOpts struct {
	InvocationRef string
	RepoRef       string
}

// AgentPath outputs the daemon-resolved sandbox path for an invocation.
func AgentPath(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentPathOpts, stdout, stderr io.Writer) error {
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, "", ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent path",
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
	DataDirOverride string
}

// AgentOpen opens the sandbox directory in the configured editor.
func AgentOpen(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
	ns, sandboxPath, err := resolvePresentAgentSandbox(ctx, cr, fsys, cwd, opts.DataDirOverride, "agent open", opts.InvocationRef, opts.RepoRef)
	if err != nil {
		return err
	}

	editorCmd, err := resolveEditorCmdWithOptionalOverride(cr, fsys, ns.dirs.ConfigDir, "")
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
	_, sandboxPath, err := resolvePresentAgentSandbox(ctx, cr, fsys, cwd, "", "agent shell", opts.InvocationRef, opts.RepoRef)
	if err != nil {
		return err
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

func resolvePresentAgentSandbox(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd, dataDirOverride, cmdName, invocationRef, repoRef string) (*daemonNavSetup, string, error) {
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, dataDirOverride, ResolveRepoContextOpts{
		RepoRef: repoRef,
		CmdName: cmdName,
	})
	if err != nil {
		return nil, "", err
	}

	invocation, err := ns.client.GetInvocation(ctx, invocationRef, repoCtx.RepoID)
	if err != nil {
		return nil, "", translateNavigationError(err, "invocation")
	}

	sandboxPath := invocation.Data.SandboxPath
	if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
		return nil, "", errors.NewWithDetails(
			errors.ESandboxMissing,
			"sandbox no longer exists",
			map[string]string{
				"invocation_id": invocation.Data.InvocationID,
				"sandbox_path":  sandboxPath,
				"hint":          "sandbox was removed after landing or discarding",
			},
		)
	}

	return ns, sandboxPath, nil
}

// AgentAttachOpts holds options for the agent attach command.
type AgentAttachOpts struct {
	InvocationRef string
	RepoRef       string

	IsInteractive   func() bool
	TmuxAttachFn    func(context.Context, string) error
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

	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, opts.DataDirOverride, ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent attach",
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
	return attachFn(ctx, session.Data.TmuxSession)
}

// AgentClients prints the currently connected tmux clients for a headed invocation session.
func AgentClients(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentClientsOpts, stdout, stderr io.Writer) error {
	ns, repoCtx, err := setupDaemonNavAndRepo(ctx, cr, fsys, cwd, opts.DataDirOverride, ResolveRepoContextOpts{
		RepoRef: opts.RepoRef,
		CmdName: "agent clients",
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
	var dre *daemonclient.DaemonReadError
	isDaemonErr := stderrors.As(err, &dre)
	ae, _ := errors.AsAgencyError(err)
	hint := ""
	if ae != nil {
		hint = ae.Details["hint"]
	}

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
			if hint != "" {
				details["hint"] = hint
			}
		}

		msg := "ambiguous target"
		if ae != nil {
			msg = ae.Msg
		}

		return errors.NewWithDetails(errors.EAmbiguous, msg, details)
	}

	if isDaemonErr && hint != "" {
		msg := err.Error()
		if ae != nil {
			msg = ae.Msg
		}
		return errors.NewWithDetails(code, msg, map[string]string{"hint": hint})
	}

	return err
}

// realTmuxAttach performs the canonical tmux client handoff with stdin/stdout/stderr connected.
func realTmuxAttach(ctx context.Context, sessionName string) error {
	client := tmux.NewExecClient(exec.NewRealRunner())
	return client.AttachSession(ctx, sessionName, tmux.AttachOpts{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		InsideTmux: os.Getenv("TMUX") != "",
	})
}

type headedAttachOpts struct {
	AttachFn    func(context.Context, string) error
	Stdout      io.Writer
	Stderr      io.Writer
	SessionName string
	Invocation  string
	RepoID      string
	Banner      bool
	LaterHint   bool
}

func attachHeadedSession(ctx context.Context, opts headedAttachOpts) {
	if opts.Banner {
		_, _ = fmt.Fprintln(opts.Stdout, "\nAttaching to tmux session... (detach with Ctrl+b, d)")
	}
	attachFn := opts.AttachFn
	if attachFn == nil {
		attachFn = realTmuxAttach
	}
	if err := attachFn(ctx, opts.SessionName); err != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "warning: could not attach to tmux session: %v\n", err)
		if opts.LaterHint {
			_, _ = fmt.Fprintf(opts.Stderr, "Use 'agency agent %s attach --repo %s' to attach later.\n", opts.Invocation, opts.RepoID)
		}
	}
}

// isTerminal returns true if the given file descriptor is a terminal.
func isTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}
