package tmux

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	t.Parallel()
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
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			exists, err := client.HasSession(context.Background(), tt.session)

			assert.Equal(t, tt.wantExist, exists)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, runner.calls, 1)

			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)
			assert.Equal(t, tt.wantArgs, call.Args)
		})
	}
}

func TestExecClient_NewSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		sessionName    string
		cwd            string
		argv           []string
		env            map[string]string
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
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.NewSession(context.Background(), tt.sessionName, tt.cwd, tt.argv, tt.env)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// For empty argv, no command should be run
			if len(tt.argv) == 0 {
				assert.Empty(t, runner.calls, "expected 0 calls for empty argv")
				return
			}

			require.Len(t, runner.calls, 1)

			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)

			// Verify args structure: prefix + "--" + tail
			expectedArgs := append(tt.wantArgsPrefix, tt.wantArgsTail...)
			assert.Equal(t, expectedArgs, call.Args)

			assert.Contains(t, call.Args, "--", "call.Args missing '--' separator")
		})
	}
}

func TestExecClient_NewSession_EnvUsesProcessEnvironmentNotArgv(t *testing.T) {
	t.Parallel()

	secretEnv := map[string]string{
		"ZED": "last-secret",
		"ABC": "first-secret",
		"MID": "middle-secret",
	}
	runner := newFakeRunner(
		fakeResponse{Result: exec.CmdResult{ExitCode: 0, Stdout: "DISPLAY SSH_AUTH_SOCK\n"}},
		fakeResponse{Result: exec.CmdResult{ExitCode: 0}},
		fakeResponse{Result: exec.CmdResult{ExitCode: 0}},
		fakeResponse{Result: exec.CmdResult{ExitCode: 0}},
	)
	client := NewExecClient(runner)

	err := client.NewSession(context.Background(), "agency_env", "/tmp/wt", []string{"claude", "--resume"}, secretEnv)

	require.NoError(t, err)
	require.Len(t, runner.calls, 4)
	assert.Equal(t, []string{"show-option", "-gqv", "update-environment"}, runner.calls[0].Args)
	assert.Equal(t, []string{"set-option", "-gq", "update-environment", "DISPLAY SSH_AUTH_SOCK ABC MID ZED"}, runner.calls[1].Args)
	assert.Equal(t, []string{"new-session", "-d", "-s", "agency_env", "-c", "/tmp/wt", "--", "claude", "--resume"}, runner.calls[2].Args)
	assert.Equal(t, secretEnv, runner.calls[2].Opts.Env)
	assert.Equal(t, []string{"set-option", "-gq", "update-environment", "DISPLAY SSH_AUTH_SOCK"}, runner.calls[3].Args)

	for _, call := range runner.calls {
		for _, arg := range call.Args {
			assert.NotContains(t, arg, "first-secret")
			assert.NotContains(t, arg, "middle-secret")
			assert.NotContains(t, arg, "last-secret")
		}
	}
}

func TestAttachSessionCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		session    string
		insideTmux bool
		wantSubcmd string
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "outside tmux",
			session:    "agency_abc123",
			insideTmux: false,
			wantSubcmd: "attach-session",
			wantArgs:   []string{"attach-session", "-t", "agency_abc123"},
		},
		{
			name:       "inside tmux",
			session:    "agency_abc123",
			insideTmux: true,
			wantSubcmd: "switch-client",
			wantArgs:   []string{"switch-client", "-t", "agency_abc123"},
		},
		{
			name:    "missing session name",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSubcmd, gotArgs, err := attachSessionCommand(tt.session, tt.insideTmux)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSubcmd, gotSubcmd)
			assert.Equal(t, tt.wantArgs, gotArgs)
		})
	}
}

func TestExecClient_KillSession(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.KillSession(context.Background(), tt.session)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, runner.calls, 1)

			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)
			assert.Equal(t, tt.wantArgs, call.Args)
		})
	}
}

func TestExecClient_InterruptSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		session   string
		responses []fakeResponse
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "interrupt success",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantErr:  false,
			wantArgs: []string{"send-keys", "-t", "agency_abc", "C-c"},
		},
		{
			name:    "interrupt failure",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "no session"}},
			},
			wantErr:  true,
			wantArgs: []string{"send-keys", "-t", "agency_abc", "C-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.InterruptSession(context.Background(), tt.session)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, runner.calls, 1)

			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)
			assert.Equal(t, tt.wantArgs, call.Args)
		})
	}
}

func TestExecClient_CaptureScrollback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		target    string
		responses []fakeResponse
		want      string
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:   "capture success",
			target: "agency_abc123:0.0",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0, Stdout: "line 1\nline 2\n"}},
			},
			want:     "line 1\nline 2\n",
			wantErr:  false,
			wantArgs: []string{"capture-pane", "-p", "-S", "-", "-t", "agency_abc123:0.0"},
		},
		{
			name:   "capture failure",
			target: "agency_abc123:0.0",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "can't find pane"}},
			},
			want:     "",
			wantErr:  true,
			wantArgs: []string{"capture-pane", "-p", "-S", "-", "-t", "agency_abc123:0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			got, err := client.CaptureScrollback(context.Background(), tt.target)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)

			require.Len(t, runner.calls, 1)
			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)
			assert.Equal(t, tt.wantArgs, call.Args)
		})
	}
}

func TestExecClient_PipePane(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		target    string
		logPath   string
		responses []fakeResponse
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "pipe success",
			target:  "agency_abc123:0.0",
			logPath: "/tmp/agent's log.txt",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0}},
			},
			wantArgs: []string{"pipe-pane", "-o", "-t", "agency_abc123:0.0", "cat >> '/tmp/agent'\"'\"'s log.txt'"},
		},
		{
			name:    "pipe failure",
			target:  "agency_abc123:0.0",
			logPath: "/tmp/agent.log",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "can't pipe pane"}},
			},
			wantErr:  true,
			wantArgs: []string{"pipe-pane", "-o", "-t", "agency_abc123:0.0", "cat >> '/tmp/agent.log'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			err := client.PipePane(context.Background(), tt.target, tt.logPath)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, runner.calls, 1)
			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)
			assert.Equal(t, tt.wantArgs, call.Args)
		})
	}
}

func TestExecClient_ListAttachedClients(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		session   string
		responses []fakeResponse
		want      []AttachedClient
		wantErr   bool
		wantArgs  []string
	}{
		{
			name:    "list success",
			session: "agency_abc",
			responses: []fakeResponse{
				{
					Result: exec.CmdResult{
						ExitCode: 0,
						Stdout: strings.Join([]string{
							"client-1\t/dev/ttys001\t101\t0",
							"client-2\t/dev/ttys002\t202\t1",
						}, "\n"),
					},
				},
			},
			want: []AttachedClient{
				{Name: "client-1", TTY: "/dev/ttys001", PID: 101, ReadOnly: false},
				{Name: "client-2", TTY: "/dev/ttys002", PID: 202, ReadOnly: true},
			},
			wantArgs: []string{
				"list-clients",
				"-t", "agency_abc",
				"-F", "#{client_name}\t#{client_tty}\t#{client_pid}\t#{client_readonly}",
			},
		},
		{
			name:    "no attached clients",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0, Stdout: ""}},
			},
			want: []AttachedClient{},
			wantArgs: []string{
				"list-clients",
				"-t", "agency_abc",
				"-F", "#{client_name}\t#{client_tty}\t#{client_pid}\t#{client_readonly}",
			},
		},
		{
			name:    "missing session",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 1, Stderr: "can't find session: agency_abc"}},
			},
			wantErr: true,
			wantArgs: []string{
				"list-clients",
				"-t", "agency_abc",
				"-F", "#{client_name}\t#{client_tty}\t#{client_pid}\t#{client_readonly}",
			},
		},
		{
			name:    "malformed pid",
			session: "agency_abc",
			responses: []fakeResponse{
				{Result: exec.CmdResult{ExitCode: 0, Stdout: "client-1\t/dev/ttys001\tnot-a-pid\t0"}},
			},
			wantErr: true,
			wantArgs: []string{
				"list-clients",
				"-t", "agency_abc",
				"-F", "#{client_name}\t#{client_tty}\t#{client_pid}\t#{client_readonly}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := newFakeRunner(tt.responses...)
			client := NewExecClient(runner)

			got, err := client.ListAttachedClients(context.Background(), tt.session)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)

			require.Len(t, runner.calls, 1)
			call := runner.calls[0]
			assert.Equal(t, "tmux", call.Name)
			assert.Equal(t, tt.wantArgs, call.Args)
		})
	}
}

func TestExecClient_ErrorFormatting(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			runner := newFakeRunner(fakeResponse{
				Result: exec.CmdResult{ExitCode: tt.exitCode, Stderr: tt.stderr},
			})
			client := NewExecClient(runner)

			_, err := client.HasSession(context.Background(), "test")

			require.Error(t, err)

			errStr := err.Error()
			for _, want := range tt.wantContains {
				assert.Contains(t, errStr, want)
			}
		})
	}
}

func TestExecClient_ErrorStderrCapping(t *testing.T) {
	t.Parallel()
	// Create a long stderr string
	longStderr := strings.Repeat("x", 5000)

	runner := newFakeRunner(fakeResponse{
		Result: exec.CmdResult{ExitCode: 2, Stderr: longStderr},
	})
	client := NewExecClient(runner)

	_, err := client.HasSession(context.Background(), "test")

	require.Error(t, err)

	errStr := err.Error()

	// Error should be capped and include "..."
	assert.Contains(t, errStr, "...")

	// Error should not be longer than reasonable (4kb + overhead)
	assert.True(t, len(errStr) <= 5000, "error message too long: %d chars", len(errStr))
}
