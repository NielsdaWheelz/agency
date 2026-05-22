package testutil

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// FakeTmuxSession tracks a session created via NewSession.
type FakeTmuxSession struct {
	Name    string
	CWD     string
	Argv    []string
	Env     map[string]string
	Clients []tmux.AttachedClient
}

// FakeTmuxNewSessionCall records the arguments of a NewSession call.
type FakeTmuxNewSessionCall struct {
	Name string
	CWD  string
	Argv []string
	Env  map[string]string
}

// FakeTmuxInterruptCall records the arguments of an interrupt call.
type FakeTmuxInterruptCall struct {
	Name string
}

// FakeTmuxPipePaneCall records the arguments of a PipePane call.
type FakeTmuxPipePaneCall struct {
	Target  string
	LogPath string
}

// FakeTmuxClient is a thread-safe test double for daemon tmux session management.
// All methods acquire mu for thread safety so it can be used from
// multiple goroutines (e.g. daemon HTTP handler goroutines).
type FakeTmuxClient struct {
	Mu sync.Mutex

	Sessions map[string]FakeTmuxSession

	// Error injection — set these before calling the method under test.
	NewSessionErr  error
	HasSessionErr  error
	KillSessionErr error
	InterruptErr   error
	CaptureErr     error
	CaptureOutput  string
	PipePaneErr    error
	ListClientsErr error

	// HasSessionFunc, when non-nil, overrides the default HasSession logic.
	// Useful for tests that need sequential/conditional results (e.g. race-condition tests).
	// Called with the lock held — callers must not re-acquire Mu.
	HasSessionFunc func(name string) (bool, error)

	// ListAttachedClientsFunc, when non-nil, overrides the default ListAttachedClients logic.
	// Called with the lock held — callers must not re-acquire Mu.
	ListAttachedClientsFunc func(name string) ([]tmux.AttachedClient, error)

	// Call tracking — read these after the method under test returns.
	NewSessionCalls  []FakeTmuxNewSessionCall
	InterruptCalls   []FakeTmuxInterruptCall
	KillSessionCalls []string
	HasSessionCalls  []string
	CaptureCalls     []string
	PipePaneCalls    []FakeTmuxPipePaneCall
	ListClientsCalls []string
}

// NewFakeTmuxClient creates a ready-to-use FakeTmuxClient.
func NewFakeTmuxClient() *FakeTmuxClient {
	return &FakeTmuxClient{
		Sessions: make(map[string]FakeTmuxSession),
	}
}

// HasSession reports whether a fake tmux session exists.
func (f *FakeTmuxClient) HasSession(_ context.Context, name string) (bool, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.HasSessionCalls = append(f.HasSessionCalls, name)
	if f.HasSessionFunc != nil {
		return f.HasSessionFunc(name)
	}
	if f.HasSessionErr != nil {
		return false, f.HasSessionErr
	}
	_, ok := f.Sessions[name]
	return ok, nil
}

// NewSession records a fake detached tmux session.
func (f *FakeTmuxClient) NewSession(_ context.Context, name, cwd string, argv []string, env map[string]string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	envMap := maps.Clone(env)
	f.NewSessionCalls = append(f.NewSessionCalls, FakeTmuxNewSessionCall{Name: name, CWD: cwd, Argv: argv, Env: envMap})
	if f.NewSessionErr != nil {
		return f.NewSessionErr
	}
	f.Sessions[name] = FakeTmuxSession{Name: name, CWD: cwd, Argv: argv, Env: envMap}
	return nil
}

// KillSession removes a fake tmux session.
func (f *FakeTmuxClient) KillSession(_ context.Context, name string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.KillSessionCalls = append(f.KillSessionCalls, name)
	if f.KillSessionErr != nil {
		return f.KillSessionErr
	}
	delete(f.Sessions, name)
	return nil
}

// InterruptSession records a fake tmux interrupt.
func (f *FakeTmuxClient) InterruptSession(_ context.Context, name string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.InterruptCalls = append(f.InterruptCalls, FakeTmuxInterruptCall{Name: name})
	if f.InterruptErr != nil {
		return f.InterruptErr
	}
	return nil
}

// CaptureScrollback returns the configured fake scrollback.
func (f *FakeTmuxClient) CaptureScrollback(_ context.Context, target string) (string, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.CaptureCalls = append(f.CaptureCalls, target)
	if f.CaptureErr != nil {
		return "", f.CaptureErr
	}
	return f.CaptureOutput, nil
}

// PipePane records fake pane piping.
func (f *FakeTmuxClient) PipePane(_ context.Context, target, logPath string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.PipePaneCalls = append(f.PipePaneCalls, FakeTmuxPipePaneCall{Target: target, LogPath: logPath})
	if f.PipePaneErr != nil {
		return f.PipePaneErr
	}
	return nil
}

// ListAttachedClients returns clients attached to a fake session.
func (f *FakeTmuxClient) ListAttachedClients(_ context.Context, name string) ([]tmux.AttachedClient, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.ListClientsCalls = append(f.ListClientsCalls, name)
	if f.ListAttachedClientsFunc != nil {
		return f.ListAttachedClientsFunc(name)
	}
	if f.ListClientsErr != nil {
		return nil, f.ListClientsErr
	}
	session, ok := f.Sessions[name]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", name)
	}
	clients := make([]tmux.AttachedClient, len(session.Clients))
	copy(clients, session.Clients)
	return clients, nil
}

// FakeTmuxCommandRunner routes tmux commands to a FakeTmuxClient and delegates
// all other commands to Base.
type FakeTmuxCommandRunner struct {
	Base exec.CommandRunner
	Tmux fakeTmuxSessionClient
}

type fakeTmuxSessionClient interface {
	HasSession(ctx context.Context, name string) (bool, error)
	NewSession(ctx context.Context, name, cwd string, argv []string, env map[string]string) error
	KillSession(ctx context.Context, name string) error
	InterruptSession(ctx context.Context, name string) error
	CaptureScrollback(ctx context.Context, target string) (string, error)
	PipePane(ctx context.Context, target, logPath string) error
	ListAttachedClients(ctx context.Context, name string) ([]tmux.AttachedClient, error)
}

// NewFakeTmuxCommandRunner creates a command runner that exercises the
// production exec-backed tmux client without requiring a real tmux server.
func NewFakeTmuxCommandRunner(base exec.CommandRunner, tmuxClient fakeTmuxSessionClient) *FakeTmuxCommandRunner {
	return &FakeTmuxCommandRunner{Base: base, Tmux: tmuxClient}
}

// Run implements exec.CommandRunner.
func (r *FakeTmuxCommandRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if name != "tmux" {
		if r.Base == nil {
			return exec.CmdResult{}, fmt.Errorf("unexpected command: %s %s", name, strings.Join(args, " "))
		}
		return r.Base.Run(ctx, name, args, opts)
	}
	if r.Tmux == nil {
		return exec.CmdResult{}, fmt.Errorf("unexpected tmux command: %s", strings.Join(args, " "))
	}
	return r.runTmux(ctx, args, opts)
}

// LookPath implements exec.CommandRunner.
func (r *FakeTmuxCommandRunner) LookPath(file string) (string, error) {
	if r.Base != nil {
		return r.Base.LookPath(file)
	}
	return "/usr/bin/" + file, nil
}

func (r *FakeTmuxCommandRunner) runTmux(ctx context.Context, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	if len(args) == 0 {
		return exec.CmdResult{}, fmt.Errorf("unexpected tmux command")
	}

	switch args[0] {
	case "show-option":
		if slices.Equal(args, []string{"show-option", "-gqv", "update-environment"}) {
			return exec.CmdResult{ExitCode: 1}, nil
		}
	case "set-option":
		if len(args) >= 4 && args[1] == "-gq" && args[2] == "update-environment" {
			return exec.CmdResult{ExitCode: 0}, nil
		}
	case "has-session":
		if len(args) == 3 && args[1] == "-t" {
			ok, err := r.Tmux.HasSession(ctx, args[2])
			if err != nil {
				return exec.CmdResult{}, err
			}
			if ok {
				return exec.CmdResult{ExitCode: 0}, nil
			}
			return exec.CmdResult{ExitCode: 1}, nil
		}
	case "new-session":
		name, cwd, argv, ok := parseTmuxNewSession(args)
		if ok {
			if err := r.Tmux.NewSession(ctx, name, cwd, argv, opts.Env); err != nil {
				return tmuxCommandFailure(err), nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		}
	case "kill-session":
		if len(args) == 3 && args[1] == "-t" {
			if err := r.Tmux.KillSession(ctx, args[2]); err != nil {
				return tmuxCommandFailure(err), nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		}
	case "send-keys":
		if len(args) == 4 && args[1] == "-t" && args[3] == "C-c" {
			if err := r.Tmux.InterruptSession(ctx, args[2]); err != nil {
				return tmuxCommandFailure(err), nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		}
	case "capture-pane":
		if len(args) == 6 && args[1] == "-p" && args[2] == "-S" && args[3] == "-" && args[4] == "-t" {
			out, err := r.Tmux.CaptureScrollback(ctx, args[5])
			if err != nil {
				return tmuxCommandFailure(err), nil
			}
			return exec.CmdResult{Stdout: out, ExitCode: 0}, nil
		}
	case "pipe-pane":
		if len(args) == 5 && args[1] == "-o" && args[2] == "-t" {
			logPath := strings.TrimPrefix(args[4], "cat >> ")
			logPath = strings.Trim(logPath, "'")
			if err := r.Tmux.PipePane(ctx, args[3], logPath); err != nil {
				return tmuxCommandFailure(err), nil
			}
			return exec.CmdResult{ExitCode: 0}, nil
		}
	case "list-clients":
		if len(args) == 5 && args[1] == "-t" && args[3] == "-F" {
			clients, err := r.Tmux.ListAttachedClients(ctx, args[2])
			if err != nil {
				return tmuxCommandFailure(err), nil
			}
			return exec.CmdResult{Stdout: formatFakeTmuxClients(clients), ExitCode: 0}, nil
		}
	}

	return exec.CmdResult{}, fmt.Errorf("unexpected tmux command: %s", strings.Join(args, " "))
}

func parseTmuxNewSession(args []string) (string, string, []string, bool) {
	if len(args) < 8 || args[0] != "new-session" || args[1] != "-d" || args[2] != "-s" || args[4] != "-c" {
		return "", "", nil, false
	}
	separator := -1
	for i := 6; i < len(args); i++ {
		if args[i] == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator == len(args)-1 {
		return "", "", nil, false
	}
	return args[3], args[5], slices.Clone(args[separator+1:]), true
}

func tmuxCommandFailure(err error) exec.CmdResult {
	return exec.CmdResult{Stderr: err.Error(), ExitCode: 1}
}

func formatFakeTmuxClients(clients []tmux.AttachedClient) string {
	var b strings.Builder
	for _, client := range clients {
		readOnly := "0"
		if client.ReadOnly {
			readOnly = "1"
		}
		fmt.Fprintf(&b, "%s\t%s\t%d\t%s\n", client.Name, client.TTY, client.PID, readOnly)
	}
	return b.String()
}
