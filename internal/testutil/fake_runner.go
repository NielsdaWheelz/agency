package testutil

import (
	"context"
	"fmt"
	"maps"
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

// fakePrefixResponse holds a canned response for commands with dynamic suffixes.
type fakePrefixResponse struct {
	Prefix   string
	Response FakeResponse
}

// FakeCommandRunner is a thread-safe, map-based test double for exec.CommandRunner.
// Commands are matched by key "name arg1 arg2 ...".
type FakeCommandRunner struct {
	mu              sync.Mutex
	Responses       map[string]FakeResponse // key: "name arg1 arg2"
	prefixResponses []fakePrefixResponse
	Calls           []string
	CallEnvs        []map[string]string
}

// NewFakeCommandRunner creates a ready-to-use FakeCommandRunner.
func NewFakeCommandRunner() *FakeCommandRunner {
	return &FakeCommandRunner{
		Responses: make(map[string]FakeResponse),
	}
}

// RespondToPrefix registers a response for commands with dynamic suffixes.
func (f *FakeCommandRunner) RespondToPrefix(prefix string, resp FakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefixResponses = append(f.prefixResponses, fakePrefixResponse{
		Prefix:   prefix,
		Response: resp,
	})
}

// Run implements exec.CommandRunner.
func (f *FakeCommandRunner) Run(_ context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}
	var env map[string]string
	if opts.Env != nil {
		env = maps.Clone(opts.Env)
	}

	f.mu.Lock()
	f.Calls = append(f.Calls, key)
	f.CallEnvs = append(f.CallEnvs, env)
	resp, ok := f.Responses[key]
	if !ok {
		for _, prefixResp := range f.prefixResponses {
			if strings.HasPrefix(key, prefixResp.Prefix) {
				resp = prefixResp.Response
				ok = true
				break
			}
		}
	}
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

	return exec.CmdResult{}, fmt.Errorf("unexpected command: %s", key)
}

// LookPath implements exec.CommandRunner.
func (f *FakeCommandRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}
