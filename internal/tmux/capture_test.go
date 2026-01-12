package tmux

import (
	"testing"
)

// MockExecutor is a test double for Executor.
type MockExecutor struct {
	// Calls records all calls made to Run.
	Calls []MockCall

	// Response is the response to return for each call.
	// If nil, returns empty strings and exit code 0.
	Responses []MockResponse
	callIndex int
}

// MockCall records a single call to Run.
type MockCall struct {
	Name string
	Args []string
}

// MockResponse is the response for a single Run call.
type MockResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// NewMockExecutor creates a new MockExecutor.
func NewMockExecutor(responses ...MockResponse) *MockExecutor {
	return &MockExecutor{
		Responses: responses,
	}
}

// Run implements Executor.
func (m *MockExecutor) Run(name string, args ...string) (stdout string, stderr string, exitCode int, err error) {
	m.Calls = append(m.Calls, MockCall{Name: name, Args: args})

	if m.callIndex < len(m.Responses) {
		resp := m.Responses[m.callIndex]
		m.callIndex++
		return resp.Stdout, resp.Stderr, resp.ExitCode, resp.Err
	}

	return "", "", 0, nil
}

func TestHasSession(t *testing.T) {
	tests := []struct {
		name        string
		session     string
		responses   []MockResponse
		want        bool
		wantCall    MockCall
	}{
		{
			name:    "session exists",
			session: "agency_abc123",
			responses: []MockResponse{
				{ExitCode: 0},
			},
			want: true,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"has-session", "-t", "agency_abc123"},
			},
		},
		{
			name:    "session does not exist",
			session: "agency_abc123",
			responses: []MockResponse{
				{ExitCode: 1, Stderr: "can't find session agency_abc123"},
			},
			want: false,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"has-session", "-t", "agency_abc123"},
			},
		},
		{
			name:    "tmux not available",
			session: "agency_abc123",
			responses: []MockResponse{
				{ExitCode: -1, Err: &mockError{msg: "executable file not found"}},
			},
			want: false,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"has-session", "-t", "agency_abc123"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewMockExecutor(tt.responses...)
			got := HasSession(exec, tt.session)

			if got != tt.want {
				t.Errorf("HasSession() = %v, want %v", got, tt.want)
			}

			if len(exec.Calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(exec.Calls))
			}

			call := exec.Calls[0]
			if call.Name != tt.wantCall.Name {
				t.Errorf("call.Name = %q, want %q", call.Name, tt.wantCall.Name)
			}
			if !slicesEqual(call.Args, tt.wantCall.Args) {
				t.Errorf("call.Args = %v, want %v", call.Args, tt.wantCall.Args)
			}
		})
	}
}

func TestCaptureScrollback(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		responses []MockResponse
		want      string
		wantErr   bool
		wantCall  MockCall
	}{
		{
			name:   "capture success",
			target: "agency_abc123:0.0",
			responses: []MockResponse{
				{Stdout: "$ echo hello\nhello\n$\n", ExitCode: 0},
			},
			want:    "$ echo hello\nhello\n$\n",
			wantErr: false,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"capture-pane", "-p", "-S", "-", "-t", "agency_abc123:0.0"},
			},
		},
		{
			name:   "capture failure - session not found",
			target: "agency_abc123:0.0",
			responses: []MockResponse{
				{Stderr: "can't find pane", ExitCode: 1},
			},
			want:    "",
			wantErr: true,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"capture-pane", "-p", "-S", "-", "-t", "agency_abc123:0.0"},
			},
		},
		{
			name:   "tmux not available",
			target: "agency_abc123:0.0",
			responses: []MockResponse{
				{Err: &mockError{msg: "executable file not found"}},
			},
			want:    "",
			wantErr: true,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"capture-pane", "-p", "-S", "-", "-t", "agency_abc123:0.0"},
			},
		},
		{
			name:   "capture with ANSI codes",
			target: "agency_abc123:0.0",
			responses: []MockResponse{
				{Stdout: "\x1b[32m$ ls\x1b[0m\nfile.txt\n", ExitCode: 0},
			},
			want:    "\x1b[32m$ ls\x1b[0m\nfile.txt\n",
			wantErr: false,
			wantCall: MockCall{
				Name: "tmux",
				Args: []string{"capture-pane", "-p", "-S", "-", "-t", "agency_abc123:0.0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewMockExecutor(tt.responses...)
			got, err := CaptureScrollback(exec, tt.target)

			if (err != nil) != tt.wantErr {
				t.Errorf("CaptureScrollback() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("CaptureScrollback() = %q, want %q", got, tt.want)
			}

			if len(exec.Calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(exec.Calls))
			}

			call := exec.Calls[0]
			if call.Name != tt.wantCall.Name {
				t.Errorf("call.Name = %q, want %q", call.Name, tt.wantCall.Name)
			}
			if !slicesEqual(call.Args, tt.wantCall.Args) {
				t.Errorf("call.Args = %v, want %v", call.Args, tt.wantCall.Args)
			}
		})
	}
}

func TestSessionTarget(t *testing.T) {
	tests := []struct {
		runID string
		want  string
	}{
		{"abc123", "agency_abc123:0.0"},
		{"20260110-a3f2", "agency_20260110-a3f2:0.0"},
		{"test", "agency_test:0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.runID, func(t *testing.T) {
			got := SessionTarget(tt.runID)
			if got != tt.want {
				t.Errorf("SessionTarget(%q) = %q, want %q", tt.runID, got, tt.want)
			}
		})
	}
}

func TestSessionName(t *testing.T) {
	tests := []struct {
		runID string
		want  string
	}{
		{"abc123", "agency_abc123"},
		{"20260110-a3f2", "agency_20260110-a3f2"},
		{"test", "agency_test"},
	}

	for _, tt := range tests {
		t.Run(tt.runID, func(t *testing.T) {
			got := SessionName(tt.runID)
			if got != tt.want {
				t.Errorf("SessionName(%q) = %q, want %q", tt.runID, got, tt.want)
			}
		})
	}
}

// mockError implements error for testing.
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// slicesEqual compares two string slices for equality.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
