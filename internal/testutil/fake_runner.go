package testutil

import (
	"context"
	"strings"
	"sync"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

// FakeResponse holds the canned response for a command.
type FakeResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// FakeCommandRunner is a thread-safe, map-based test double for exec.CommandRunner.
// Commands are matched by key "name arg1 arg2 ...".
type FakeCommandRunner struct {
	mu        sync.Mutex
	Responses map[string]FakeResponse // key: "name arg1 arg2"
	Calls     []string
	CallEnvs  []map[string]string
}

// NewFakeCommandRunner creates a ready-to-use FakeCommandRunner.
func NewFakeCommandRunner() *FakeCommandRunner {
	return &FakeCommandRunner{
		Responses: make(map[string]FakeResponse),
	}
}

// Run implements exec.CommandRunner.
func (f *FakeCommandRunner) Run(_ context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}
	var env map[string]string
	if opts.Env != nil {
		env = make(map[string]string, len(opts.Env))
		for k, v := range opts.Env {
			env[k] = v
		}
	}

	f.mu.Lock()
	f.Calls = append(f.Calls, key)
	f.CallEnvs = append(f.CallEnvs, env)
	resp, ok := f.Responses[key]
	f.mu.Unlock()

	if ok {
		if resp.Err != nil {
			return exec.CmdResult{}, resp.Err
		}
		return exec.CmdResult{
			Stdout:   resp.Stdout,
			Stderr:   resp.Stderr,
			ExitCode: resp.ExitCode,
		}, nil
	}

	// Default: empty success.
	return exec.CmdResult{ExitCode: 0}, nil
}

// LookPath implements exec.CommandRunner.
func (f *FakeCommandRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}
