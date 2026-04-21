package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// FakeTmuxSession tracks a session created via NewSession.
type FakeTmuxSession struct {
	Name    string
	CWD     string
	Argv    []string
	Clients []tmux.AttachedClient
}

// FakeTmuxNewSessionCall records the arguments of a NewSession call.
type FakeTmuxNewSessionCall struct {
	Name string
	CWD  string
	Argv []string
}

// FakeTmuxSendKeysCall records the arguments of a SendKeys call.
type FakeTmuxSendKeysCall struct {
	Name string
	Keys []tmux.Key
}

// FakeTmuxPipePaneCall records the arguments of a PipePane call.
type FakeTmuxPipePaneCall struct {
	Target  string
	LogPath string
}

// FakeTmuxClient is a thread-safe test double for tmux.Client.
// All methods acquire mu for thread safety so it can be used from
// multiple goroutines (e.g. daemon HTTP handler goroutines).
type FakeTmuxClient struct {
	Mu sync.Mutex

	Sessions map[string]FakeTmuxSession

	// Error injection — set these before calling the method under test.
	NewSessionErr  error
	HasSessionErr  error
	KillSessionErr error
	SendKeysErr    error
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
	SendKeysCalls    []FakeTmuxSendKeysCall
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

// HasSession implements tmux.Client.
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

// NewSession implements tmux.Client.
func (f *FakeTmuxClient) NewSession(_ context.Context, name, cwd string, argv []string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.NewSessionCalls = append(f.NewSessionCalls, FakeTmuxNewSessionCall{Name: name, CWD: cwd, Argv: argv})
	if f.NewSessionErr != nil {
		return f.NewSessionErr
	}
	f.Sessions[name] = FakeTmuxSession{Name: name, CWD: cwd, Argv: argv}
	return nil
}

// KillSession implements tmux.Client.
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

// SendKeys implements tmux.Client.
func (f *FakeTmuxClient) SendKeys(_ context.Context, name string, keys []tmux.Key) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.SendKeysCalls = append(f.SendKeysCalls, FakeTmuxSendKeysCall{Name: name, Keys: keys})
	if f.SendKeysErr != nil {
		return f.SendKeysErr
	}
	return nil
}

// CaptureScrollback implements tmux.Client.
func (f *FakeTmuxClient) CaptureScrollback(_ context.Context, target string) (string, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.CaptureCalls = append(f.CaptureCalls, target)
	if f.CaptureErr != nil {
		return "", f.CaptureErr
	}
	return f.CaptureOutput, nil
}

// PipePane implements tmux.Client.
func (f *FakeTmuxClient) PipePane(_ context.Context, target, logPath string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.PipePaneCalls = append(f.PipePaneCalls, FakeTmuxPipePaneCall{Target: target, LogPath: logPath})
	if f.PipePaneErr != nil {
		return f.PipePaneErr
	}
	return nil
}

// ListAttachedClients implements tmux.Client.
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
