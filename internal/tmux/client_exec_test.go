package tmux

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

// fakeRunner is a test double for exec.CommandRunner.
type fakeRunner struct {
	calls     []fakeCall
	responses []fakeResponse
	callIndex int
}

type fakeCall struct {
	Name string
	Args []string
	Opts exec.RunOpts
}

type fakeResponse struct {
	Result exec.CmdResult
	Err    error
}

func newFakeRunner(responses ...fakeResponse) *fakeRunner {
	return &fakeRunner{responses: responses}
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	f.calls = append(f.calls, fakeCall{Name: name, Args: args, Opts: opts})

	if f.callIndex < len(f.responses) {
		resp := f.responses[f.callIndex]
		f.callIndex++
		return resp.Result, resp.Err
	}
	return exec.CmdResult{}, nil
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

func TestExecClient_HasSession(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		responses []fakeResponse
		wantExist bool
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "session exists (exit 0)",
			session: "agency_abc123",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantExist: true,
			wantErr:   false,
			wantArgs:  []string{"has-session", "-t", "agency_abc123"},
		},
		{
			name:    "session does not exist (exit 1)",
			session: "agency_abc123",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "can't find session"}},
			},
			wantExist: false,
			wantErr:   false,
			wantArgs:  []string{"has-session", "-t", "agency_abc123"},
		},
		{
			name:    "unexpected exit code 2",
			session: "agency_abc123",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 2, Stderr: "some error"}},
			},
			wantExist: false,
			wantErr:   true,
			wantArgs:  []string{"has-session", "-t", "agency_abc123"},
		},
		{
			name:    "unexpected exit code 127 (not found)",
			session: "agency_abc123",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 127, Stderr: "command not found"}},
			},
			wantExist: false,
			wantErr:   true,
			wantArgs:  []string{"has-session", "-t", "agency_abc123"},
		},
		{
			name:    "execution error (binary not found)",
			session: "agency_abc123",
			responses: []fakeResponse{
				{Err: errors.New("exec: tmux not found")},
			},
			wantExist: false,
			wantErr:   true,
			wantArgs:  []string{"has-session", "-t", "agency_abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			exists, err := client.HasSession(context.Background(), tt.session)

			if exists != tt.wantExist {
				t.Errorf("HasSession() exists = %v, want %v", exists, tt.wantExist)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("HasSession() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(runner.calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(runner.calls))
			}

			call := runner.calls[0]
			if call.Name != "tmux" {
				t.Errorf("call.Name = %q, want %q", call.Name, "tmux")
			}
			if !slicesEqual(call.Args, tt.wantArgs) {
				t.Errorf("call.Args = %v, want %v", call.Args, tt.wantArgs)
			}
		})
	}
}

func TestExecClient_NewSession(t *testing.T) {
	tests := []struct {
		name           string
		sessionName    string
		cwd            string
		argv           []string
		responses      []fakeResponse
		wantErr        bool
		wantArgsPrefix []string // args before "--"
		wantArgsTail   []string // args after "--"
	}{
		{
			name:        "single command",
			sessionName: "agency_123",
			cwd:         "/tmp/wt",
			argv:        []string{"claude"},
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:        false,
			wantArgsPrefix: []string{"new-session", "-d", "-s", "agency_123", "-c", "/tmp/wt", "--"},
			wantArgsTail:   []string{"claude"},
		},
		{
			name:        "command with args",
			sessionName: "agency_456",
			cwd:         "/home/user/project",
			argv:        []string{"claude", "--foo", "bar"},
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:        false,
			wantArgsPrefix: []string{"new-session", "-d", "-s", "agency_456", "-c", "/home/user/project", "--"},
			wantArgsTail:   []string{"claude", "--foo", "bar"},
		},
		{
			name:        "empty argv",
			sessionName: "agency_789",
			cwd:         "/tmp",
			argv:        []string{},
			responses:   []fakeResponse{},
			wantErr:     true,
		},
		{
			name:        "non-zero exit",
			sessionName: "agency_abc",
			cwd:         "/tmp",
			argv:        []string{"claude"},
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "session exists"}},
			},
			wantErr:        true,
			wantArgsPrefix: []string{"new-session", "-d", "-s", "agency_abc", "-c", "/tmp", "--"},
			wantArgsTail:   []string{"claude"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.NewSession(context.Background(), tt.sessionName, tt.cwd, tt.argv)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewSession() error = %v, wantErr %v", err, tt.wantErr)
			}

			// For empty argv, no command should be run
			if len(tt.argv) == 0 {
				if len(runner.calls) != 0 {
					t.Errorf("expected 0 calls for empty argv, got %d", len(runner.calls))
				}
				return
			}

			if len(runner.calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(runner.calls))
			}

			call := runner.calls[0]
			if call.Name != "tmux" {
				t.Errorf("call.Name = %q, want %q", call.Name, "tmux")
			}

			// Verify args structure: prefix + "--" + tail
			expectedArgs := append(tt.wantArgsPrefix, tt.wantArgsTail...)
			if !slicesEqual(call.Args, expectedArgs) {
				t.Errorf("call.Args = %v, want %v", call.Args, expectedArgs)
			}

			// Verify "--" separator is present
			found := false
			for _, arg := range call.Args {
				if arg == "--" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("call.Args missing '--' separator")
			}
		})
	}
}

func TestExecClient_Attach(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		responses []fakeResponse
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "attach success",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:  false,
			wantArgs: []string{"attach", "-t", "agency_abc"},
		},
		{
			name:    "attach failure",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "no session"}},
			},
			wantErr:  true,
			wantArgs: []string{"attach", "-t", "agency_abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.Attach(context.Background(), tt.session)

			if (err != nil) != tt.wantErr {
				t.Errorf("Attach() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(runner.calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(runner.calls))
			}

			call := runner.calls[0]
			if call.Name != "tmux" {
				t.Errorf("call.Name = %q, want %q", call.Name, "tmux")
			}
			if !slicesEqual(call.Args, tt.wantArgs) {
				t.Errorf("call.Args = %v, want %v", call.Args, tt.wantArgs)
			}
		})
	}
}

func TestExecClient_KillSession(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		responses []fakeResponse
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "kill success",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:  false,
			wantArgs: []string{"kill-session", "-t", "agency_abc"},
		},
		{
			name:    "kill failure (no session)",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "can't find session"}},
			},
			wantErr:  true,
			wantArgs: []string{"kill-session", "-t", "agency_abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.KillSession(context.Background(), tt.session)

			if (err != nil) != tt.wantErr {
				t.Errorf("KillSession() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(runner.calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(runner.calls))
			}

			call := runner.calls[0]
			if call.Name != "tmux" {
				t.Errorf("call.Name = %q, want %q", call.Name, "tmux")
			}
			if !slicesEqual(call.Args, tt.wantArgs) {
				t.Errorf("call.Args = %v, want %v", call.Args, tt.wantArgs)
			}
		})
	}
}

func TestExecClient_SendKeys(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		keys      []Key
		responses []fakeResponse
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "send C-c",
			session: "agency_abc",
			keys:    []Key{KeyCtrlC},
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:  false,
			wantArgs: []string{"send-keys", "-t", "agency_abc", "C-c"},
		},
		{
			name:    "send multiple keys",
			session: "agency_abc",
			keys:    []Key{KeyCtrlC, Key("Enter")},
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:  false,
			wantArgs: []string{"send-keys", "-t", "agency_abc", "C-c", "Enter"},
		},
		{
			name:      "empty keys",
			session:   "agency_abc",
			keys:      []Key{},
			responses: []fakeResponse{},
			wantErr:   true,
		},
		{
			name:    "send failure",
			session: "agency_abc",
			keys:    []Key{KeyCtrlC},
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "no session"}},
			},
			wantErr:  true,
			wantArgs: []string{"send-keys", "-t", "agency_abc", "C-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.SendKeys(context.Background(), tt.session, tt.keys)

			if (err != nil) != tt.wantErr {
				t.Errorf("SendKeys() error = %v, wantErr %v", err, tt.wantErr)
			}

			// For empty keys, no command should be run
			if len(tt.keys) == 0 {
				if len(runner.calls) != 0 {
					t.Errorf("expected 0 calls for empty keys, got %d", len(runner.calls))
				}
				return
			}

			if len(runner.calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(runner.calls))
			}

			call := runner.calls[0]
			if call.Name != "tmux" {
				t.Errorf("call.Name = %q, want %q", call.Name, "tmux")
			}
			if !slicesEqual(call.Args, tt.wantArgs) {
				t.Errorf("call.Args = %v, want %v", call.Args, tt.wantArgs)
			}
		})
	}
}

func TestExecClient_ErrorFormatting(t *testing.T) {
	tests := []struct {
		name         string
		stderr       string
		exitCode     int
		wantContains []string
	}{
		{
			name:         "error with stderr",
			stderr:       "some tmux error",
			exitCode:     2, // exit code 1 is "not found", 2+ is error
			wantContains: []string{"has-session", "exit=2", "some tmux error"},
		},
		{
			name:         "error without stderr",
			stderr:       "",
			exitCode:     2,
			wantContains: []string{"has-session", "exit=2"},
		},
		{
			name:         "error with whitespace-only stderr",
			stderr:       "  \n\t  ",
			exitCode:     3,
			wantContains: []string{"has-session", "exit=3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner(fakeResponse{
				Result: exec.CmdResult{ExitCode: tt.exitCode, Stderr: tt.stderr},
			})
			client := NewExecClient(runner)

			_, err := client.HasSession(context.Background(), "test")

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			errStr := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(errStr, want) {
					t.Errorf("error %q should contain %q", errStr, want)
				}
			}
		})
	}
}

func TestExecClient_ErrorStderrCapping(t *testing.T) {
	// Create a long stderr string
	longStderr := strings.Repeat("x", 5000)

	runner := newFakeRunner(fakeResponse{
		Result: exec.CmdResult{ExitCode: 2, Stderr: longStderr},
	})
	client := NewExecClient(runner)

	_, err := client.HasSession(context.Background(), "test")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errStr := err.Error()

	// Error should be capped and include "..."
	if !strings.Contains(errStr, "...") {
		t.Errorf("long stderr should be capped with '...', got: %q", errStr)
	}

	// Error should not be longer than reasonable (4kb + overhead)
	if len(errStr) > 5000 {
		t.Errorf("error message too long: %d chars", len(errStr))
	}
}
