package checkpoint

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

// stubRunner implements exec.CommandRunner for testing.
type stubRunner struct {
	mu        sync.Mutex
	responses []stubbedResponse
	calls     []stubCall
}

type stubbedResponse struct {
	key    string // "git -C /path status --porcelain" etc.
	result exec.CmdResult
	err    error // non-nil for exec-level error
}

type stubCall struct {
	name string
	args []string
	dir  string
	env  map[string]string
}

func newStubRunner() *stubRunner {
	return &stubRunner{}
}

// stub registers a response for a command key.
// key is the name + args joined by space (e.g., "git -C /path status --porcelain").
func (s *stubRunner) stub(key string, result exec.CmdResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, stubbedResponse{key: key, result: result})
}

// stubErr registers an exec-level error for a command key.
func (s *stubRunner) stubErr(key string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, stubbedResponse{key: key, err: err})
}

func (s *stubRunner) Run(_ context.Context, name string, args []string, opts exec.RunOpts) (exec.CmdResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, stubCall{
		name: name,
		args: args,
		dir:  opts.Dir,
		env:  opts.Env,
	})

	key := name + " " + strings.Join(args, " ")

	// Find first matching response (consumes it)
	for i, resp := range s.responses {
		if resp.key == key {
			s.responses = append(s.responses[:i], s.responses[i+1:]...)
			if resp.err != nil {
				return exec.CmdResult{}, resp.err
			}
			return resp.result, nil
		}
	}

	// Default: success with empty output
	return exec.CmdResult{ExitCode: 0}, nil
}

func (s *stubRunner) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

// getCalls returns a copy of recorded calls.
func (s *stubRunner) getCalls() []stubCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]stubCall, len(s.calls))
	copy(cp, s.calls)
	return cp
}

// callKeys returns all call keys for assertion.
func (s *stubRunner) callKeys() []string {
	calls := s.getCalls()
	keys := make([]string, len(calls))
	for i, c := range calls {
		keys[i] = c.name + " " + strings.Join(c.args, " ")
	}
	return keys
}

// controllable clock for testing.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(t time.Time) *testClock {
	return &testClock{now: t}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newTestEngine creates a test Engine with temp directories and the given stubRunner.
func newTestEngine(t *testing.T, sr *stubRunner, config Config) (*Engine, string) {
	t.Helper()
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	// Create a .git/index file in sandbox so temp index copy works
	gitDir := filepath.Join(sandboxPath, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("fake-index"), 0o644); err != nil {
		t.Fatal(err)
	}

	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	e := NewEngine(
		"test-inv-001",
		"test-repo",
		sandboxPath,
		sandboxPath, // repoRoot = sandboxPath for tests
		checkpointsDir,
		eventsPath,
		config,
		sr,
		fs.NewRealFS(),
		clock.Now,
	)
	return e, checkpointsDir
}

// readEvents reads all events from the events.jsonl file.
func readEvents(t *testing.T, eventsPath string) []Event {
	t.Helper()
	f, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("failed to parse event: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

// ---------------------------------------------------------------------------
// Existing tests (retained from original file)
// ---------------------------------------------------------------------------

func TestMatchesDenylist(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		// .env exact matches
		{"env exact", ".env", true},

		// .env.* prefix matches
		{"env local", ".env.local", true},
		{"env production", ".env.production", true},

		// *.key suffix matches
		{"private key", "private.key", true},
		{"server key", "server.key", true},

		// *.pem suffix matches
		{"cert pem", "cert.pem", true},
		{"private pem", "private.pem", true},

		// credentials.json and secrets.json
		{"credentials.json", "credentials.json", true},
		{"secrets.json", "secrets.json", true},

		// Non-matching files
		{"regular file", "main.go", false},
		{"readme", "README.md", false},
		{"env prefix", "env.local", false}, // doesn't start with .
		{"key in name", "mykey.txt", false},
		{"pem in name", "mypem.txt", false},
		{"credentials yaml", "credentials.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesDenylist(tt.filename)
			if got != tt.want {
				t.Errorf("matchesDenylist(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestCheckpointsFile_NextID(t *testing.T) {
	tests := []struct {
		name        string
		checkpoints []Checkpoint
		want        int
	}{
		{
			name:        "empty file",
			checkpoints: []Checkpoint{},
			want:        1,
		},
		{
			name: "one checkpoint",
			checkpoints: []Checkpoint{
				{ID: 1},
			},
			want: 2,
		},
		{
			name: "multiple checkpoints",
			checkpoints: []Checkpoint{
				{ID: 1},
				{ID: 2},
				{ID: 5},
			},
			want: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &CheckpointsFile{
				SchemaVersion: SchemaVersion,
				Checkpoints:   tt.checkpoints,
			}
			got := f.NextID()
			if got != tt.want {
				t.Errorf("NextID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckpointsFile_FindByID(t *testing.T) {
	f := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotCommit: "abc123"},
			{ID: 2, SnapshotCommit: "def456"},
			{ID: 5, SnapshotCommit: "ghi789"},
		},
	}

	tests := []struct {
		id          int
		wantCommit  string
		shouldExist bool
	}{
		{1, "abc123", true},
		{2, "def456", true},
		{5, "ghi789", true},
		{3, "", false},
		{0, "", false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := f.FindByID(tt.id)
			if tt.shouldExist {
				if got == nil {
					t.Errorf("FindByID(%d) returned nil, want checkpoint", tt.id)
				} else if got.SnapshotCommit != tt.wantCommit {
					t.Errorf("FindByID(%d).SnapshotCommit = %q, want %q", tt.id, got.SnapshotCommit, tt.wantCommit)
				}
			} else {
				if got != nil {
					t.Errorf("FindByID(%d) = %v, want nil", tt.id, got)
				}
			}
		})
	}
}

func TestEngine_shouldIgnorePath(t *testing.T) {
	e := &Engine{
		sandboxPath: "/sandbox/tree",
	}

	tests := []struct {
		path string
		want bool
	}{
		{"/sandbox/tree/.git/index", true},
		{"/sandbox/tree/.git", true},
		{"/sandbox/tree/.agency/state/runner_status.json", true},
		{"/sandbox/tree/.agency", true},
		{"/sandbox/tree/src/main.go", false},
		{"/sandbox/tree/README.md", false},
		{"/sandbox/tree/subdir/.env", false}, // .env file itself is not ignored (denylist handles it)
	}

	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			got := e.shouldIgnorePath(tt.path)
			if got != tt.want {
				t.Errorf("shouldIgnorePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.IncludeUntracked {
		t.Error("DefaultConfig().IncludeUntracked should be true")
	}
	if cfg.DebounceInterval != 3e9 {
		t.Errorf("DefaultConfig().DebounceInterval = %v, want 3s", cfg.DebounceInterval)
	}
	if cfg.RateLimit != 10e9 {
		t.Errorf("DefaultConfig().RateLimit = %v, want 10s", cfg.RateLimit)
	}
	if cfg.PollInterval != 30e9 {
		t.Errorf("DefaultConfig().PollInterval = %v, want 30s", cfg.PollInterval)
	}
}

func TestNewCheckpointsFile(t *testing.T) {
	f := NewCheckpointsFile()

	if f.SchemaVersion != SchemaVersion {
		t.Errorf("NewCheckpointsFile().SchemaVersion = %q, want %q", f.SchemaVersion, SchemaVersion)
	}
	if len(f.Checkpoints) != 0 {
		t.Errorf("NewCheckpointsFile().Checkpoints has %d elements, want 0", len(f.Checkpoints))
	}
}

func TestDenylistPatterns(t *testing.T) {
	expected := []string{
		".env",
		".env.*",
		"*.key",
		"*.pem",
		"credentials.json",
		"secrets.json",
	}

	if len(DenylistPatterns) != len(expected) {
		t.Errorf("DenylistPatterns has %d entries, want %d", len(DenylistPatterns), len(expected))
	}

	for i, p := range expected {
		if DenylistPatterns[i] != p {
			t.Errorf("DenylistPatterns[%d] = %q, want %q", i, DenylistPatterns[i], p)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 1: New unit tests
// ---------------------------------------------------------------------------

// 1.1 TestEngine_isDirty
func TestEngine_isDirty(t *testing.T) {
	tests := []struct {
		name             string
		stdout           string
		exitCode         int
		execErr          bool
		includeUntracked bool
		wantDirty        bool
		wantErr          bool
	}{
		{
			name:      "clean workspace",
			stdout:    "",
			wantDirty: false,
		},
		{
			name:      "modified tracked file",
			stdout:    " M main.go\n",
			wantDirty: true,
		},
		{
			name:             "only untracked, IncludeUntracked=true",
			stdout:           "?? new-file.txt\n",
			includeUntracked: true,
			wantDirty:        true,
		},
		{
			name:             "only untracked, IncludeUntracked=false",
			stdout:           "?? new-file.txt\n",
			includeUntracked: false,
			wantDirty:        false,
		},
		{
			name:             "mixed tracked+untracked, IncludeUntracked=false",
			stdout:           " M main.go\n?? new-file.txt\n",
			includeUntracked: false,
			wantDirty:        true,
		},
		{
			name:     "git status fails exit code 1",
			exitCode: 1,
			wantErr:  true,
		},
		{
			name:    "git status exec error",
			execErr: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := newStubRunner()
			cfg := DefaultConfig()
			cfg.IncludeUntracked = tt.includeUntracked
			e, _ := newTestEngine(t, sr, cfg)

			key := fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath)
			if tt.execErr {
				sr.stubErr(key, fmt.Errorf("exec failed"))
			} else {
				sr.stub(key, exec.CmdResult{
					Stdout:   tt.stdout,
					ExitCode: tt.exitCode,
					Stderr:   "some error",
				})
			}

			dirty, err := e.isDirty(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("isDirty() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if dirty != tt.wantDirty {
				t.Errorf("isDirty() = %v, want %v", dirty, tt.wantDirty)
			}
		})
	}
}

// 1.2 TestEngine_checkDenylist
func TestEngine_checkDenylist(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		exitCode int
		execErr  bool
		want     []string
		wantErr  bool
	}{
		{
			name:   "no files",
			stdout: "",
			want:   nil,
		},
		{
			name:   "regular files only",
			stdout: "src/main.go\nREADME.md\n",
			want:   nil,
		},
		{
			name:   ".env found",
			stdout: ".env\n",
			want:   []string{".env"},
		},
		{
			name:   ".env.production found",
			stdout: ".env.production\n",
			want:   []string{".env.production"},
		},
		{
			name:   "*.key found",
			stdout: "certs/private.key\n",
			want:   []string{"certs/private.key"},
		},
		{
			name:   "*.pem found",
			stdout: "cert.pem\n",
			want:   []string{"cert.pem"},
		},
		{
			name:   "credentials.json",
			stdout: "credentials.json\n",
			want:   []string{"credentials.json"},
		},
		{
			name:   "multiple denied files",
			stdout: ".env\ncerts/private.key\nsecrets.json\nsrc/main.go\n",
			want:   []string{".env", "certs/private.key", "secrets.json"},
		},
		{
			name:     "git ls-files fails",
			exitCode: 1,
			wantErr:  true,
		},
		{
			name:    "exec error",
			execErr: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := newStubRunner()
			cfg := DefaultConfig()
			cfg.IncludeUntracked = true
			e, _ := newTestEngine(t, sr, cfg)

			key := fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath)
			if tt.execErr {
				sr.stubErr(key, fmt.Errorf("exec failed"))
			} else {
				sr.stub(key, exec.CmdResult{
					Stdout:   tt.stdout,
					ExitCode: tt.exitCode,
					Stderr:   "error",
				})
			}

			got, err := e.checkDenylist(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("checkDenylist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("checkDenylist() returned %d files, want %d: got %v", len(got), len(tt.want), got)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("checkDenylist()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// 1.3 TestEngine_computeDiffstat
func TestEngine_computeDiffstat(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		exitCode int
		execErr  bool
		want     string
	}{
		{
			name:   "single file change",
			stdout: " main.go | 5 ++---\n 1 file changed, 2 insertions(+), 3 deletions(-)\n",
			want:   "+2 -3 in 1 files",
		},
		{
			name:   "multiple files",
			stdout: " a.go | 30 ++++\n b.go | 12 ----\n c.go | 15 ++++---\n 3 files changed, 42 insertions(+), 15 deletions(-)\n",
			want:   "+42 -15 in 3 files",
		},
		{
			name:   "insertions only",
			stdout: " new.go | 10 ++++++++++\n 1 file changed, 10 insertions(+)\n",
			want:   "+10 -0 in 1 files",
		},
		{
			name:   "deletions only",
			stdout: " old.go | 5 -----\n 1 file changed, 5 deletions(-)\n",
			want:   "+0 -5 in 1 files",
		},
		{
			name:   "empty output",
			stdout: "",
			want:   "",
		},
		{
			name:     "command fails",
			exitCode: 1,
			want:     "",
		},
		{
			name:    "exec error",
			execErr: true,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := newStubRunner()
			cfg := DefaultConfig()
			e, _ := newTestEngine(t, sr, cfg)

			key := fmt.Sprintf("git -C %s diff --stat --stat-width=80 abc123..def456", e.sandboxPath)
			if tt.execErr {
				sr.stubErr(key, fmt.Errorf("exec failed"))
			} else {
				sr.stub(key, exec.CmdResult{
					Stdout:   tt.stdout,
					ExitCode: tt.exitCode,
				})
			}

			got := e.computeDiffstat(context.Background(), "abc123", "def456")
			if got != tt.want {
				t.Errorf("computeDiffstat() = %q, want %q", got, tt.want)
			}
		})
	}
}

// stubFullCheckpointSequence stubs all git commands needed for a successful checkpoint creation.
func stubFullCheckpointSequence(sr *stubRunner, sandboxPath string, includeUntracked bool) {
	// isDirty: status --porcelain
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n"})

	// checkDenylist: ls-files (only if includeUntracked)
	if includeUntracked {
		sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", sandboxPath),
			exec.CmdResult{Stdout: ""})
	}

	// rev-parse HEAD
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath),
		exec.CmdResult{Stdout: "aabbccdd00112233\n"})

	// rev-parse --git-dir
	sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", sandboxPath),
		exec.CmdResult{Stdout: ".git\n"})

	// git add
	if includeUntracked {
		sr.stub(fmt.Sprintf("git -C %s add -A -- . :(exclude).agency :(exclude).git", sandboxPath),
			exec.CmdResult{})
	} else {
		sr.stub(fmt.Sprintf("git -C %s add -u", sandboxPath),
			exec.CmdResult{})
	}

	// write-tree
	sr.stub(fmt.Sprintf("git -C %s write-tree", sandboxPath),
		exec.CmdResult{Stdout: "eeeeeeeeeeee\n"})

	// commit-tree
	sr.stub(fmt.Sprintf("git -C %s commit-tree eeeeeeeeeeee -p HEAD -m agency snapshot test-inv-001 1", sandboxPath),
		exec.CmdResult{Stdout: "ffffffffffffffff\n"})

	// update-ref (uses repoRoot = sandboxPath)
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/snapshots/test-inv-001/1 ffffffffffffffff", sandboxPath),
		exec.CmdResult{})

	// diff --stat
	sr.stub(fmt.Sprintf("git -C %s diff --stat --stat-width=80 aabbccdd00112233..ffffffffffffffff", sandboxPath),
		exec.CmdResult{Stdout: " main.go | 5 ++---\n 1 file changed, 2 insertions(+), 3 deletions(-)\n"})
}

// 1.4 TestEngine_createCheckpointInternal_Success
func TestEngine_createCheckpointInternal_Success(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, checkpointsDir := newTestEngine(t, sr, cfg)

	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	err := e.createCheckpointInternal(context.Background())
	if err != nil {
		t.Fatalf("createCheckpointInternal() returned error: %v", err)
	}

	// Verify checkpoints.json
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatalf("loadCheckpoints() error: %v", err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(cpFile.Checkpoints))
	}
	cp := cpFile.Checkpoints[0]
	if cp.ID != 1 {
		t.Errorf("checkpoint ID = %d, want 1", cp.ID)
	}
	if cp.SnapshotCommit != "ffffffffffffffff" {
		t.Errorf("snapshot_commit = %q, want %q", cp.SnapshotCommit, "ffffffffffffffff")
	}
	if cp.SandboxHeadSHA != "aabbccdd00112233" {
		t.Errorf("sandbox_head_sha = %q, want %q", cp.SandboxHeadSHA, "aabbccdd00112233")
	}
	if !cp.IncludesUntracked {
		t.Error("expected IncludesUntracked=true")
	}
	if cp.Diffstat != "+2 -3 in 1 files" {
		t.Errorf("diffstat = %q, want %q", cp.Diffstat, "+2 -3 in 1 files")
	}

	// Verify events.jsonl
	events := readEvents(t, e.eventsPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventKindCheckpointCreated {
		t.Errorf("event kind = %q, want %q", events[0].Kind, EventKindCheckpointCreated)
	}

	// Verify git commands were called in order
	calls := sr.callKeys()
	expectedCalls := []string{
		fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
		fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
		fmt.Sprintf("git -C %s rev-parse --git-dir", e.sandboxPath),
	}
	for i, expected := range expectedCalls {
		if i >= len(calls) {
			t.Errorf("expected call %d: %s, but only got %d calls", i, expected, len(calls))
			continue
		}
		if calls[i] != expected {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], expected)
		}
	}

	// Verify checkpoints.json file exists on disk
	cpPath := filepath.Join(checkpointsDir, "checkpoints.json")
	if _, err := os.Stat(cpPath); os.IsNotExist(err) {
		t.Error("checkpoints.json not written to disk")
	}
}

// 1.5 TestEngine_createCheckpointInternal_DenylistDegradation
func TestEngine_createCheckpointInternal_DenylistDegradation(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	// isDirty
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n?? .env\n"})

	// checkDenylist: .env found
	sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
		exec.CmdResult{Stdout: ".env\n"})

	// rev-parse HEAD
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
		exec.CmdResult{Stdout: "aabbccdd00112233\n"})

	// rev-parse --git-dir
	sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", e.sandboxPath),
		exec.CmdResult{Stdout: ".git\n"})

	// After denylist, should use git add -u (tracked only)
	sr.stub(fmt.Sprintf("git -C %s add -u", e.sandboxPath),
		exec.CmdResult{})

	// write-tree
	sr.stub(fmt.Sprintf("git -C %s write-tree", e.sandboxPath),
		exec.CmdResult{Stdout: "eeeeeeeeeeee\n"})

	// commit-tree
	sr.stub(fmt.Sprintf("git -C %s commit-tree eeeeeeeeeeee -p HEAD -m agency snapshot test-inv-001 1", e.sandboxPath),
		exec.CmdResult{Stdout: "ffffffffffffffff\n"})

	// update-ref
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/snapshots/test-inv-001/1 ffffffffffffffff", e.sandboxPath),
		exec.CmdResult{})

	// diff --stat
	sr.stub(fmt.Sprintf("git -C %s diff --stat --stat-width=80 aabbccdd00112233..ffffffffffffffff", e.sandboxPath),
		exec.CmdResult{Stdout: " main.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n"})

	err := e.createCheckpointInternal(context.Background())
	if err != nil {
		t.Fatalf("createCheckpointInternal() returned error: %v", err)
	}

	// Verify checkpoint has IncludesUntracked=false
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(cpFile.Checkpoints))
	}
	if cpFile.Checkpoints[0].IncludesUntracked {
		t.Error("expected IncludesUntracked=false after denylist degradation")
	}

	// Verify git add -u was used (not git add -A)
	calls := sr.callKeys()
	foundAddU := false
	for _, c := range calls {
		if strings.Contains(c, "add -u") {
			foundAddU = true
		}
		if strings.Contains(c, "add -A") {
			t.Error("git add -A should not be called after denylist degradation")
		}
	}
	if !foundAddU {
		t.Error("expected git add -u to be called")
	}

	// Verify events: should have denylist_triggered + checkpoint_created
	events := readEvents(t, e.eventsPath)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	foundDenylist := false
	for _, ev := range events {
		if ev.Kind == EventKindCheckpointDenylistTriggered {
			foundDenylist = true
		}
	}
	if !foundDenylist {
		t.Error("expected denylist_triggered event")
	}
}

// 1.6 TestEngine_createCheckpointInternal_NotDirty
func TestEngine_createCheckpointInternal_NotDirty(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	e, _ := newTestEngine(t, sr, cfg)

	// isDirty: clean
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		exec.CmdResult{Stdout: ""})

	err := e.createCheckpointInternal(context.Background())
	if err != nil {
		t.Fatalf("createCheckpointInternal() returned error: %v", err)
	}

	// Verify no checkpoints created
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 0 {
		t.Errorf("expected 0 checkpoints, got %d", len(cpFile.Checkpoints))
	}

	// Verify no events
	events := readEvents(t, e.eventsPath)
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// Verify only status was called (no further git commands)
	calls := sr.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 git call (status), got %d: %v", len(calls), sr.callKeys())
	}
}

// 1.7 TestEngine_CreateCheckpoint_RateLimited
func TestEngine_CreateCheckpoint_RateLimited(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	cfg.RateLimit = 10 * time.Second

	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))

	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	// Create .git/index
	gitDir := filepath.Join(sandboxPath, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEngine(
		"test-inv-001", "test-repo",
		sandboxPath, sandboxPath,
		checkpointsDir, eventsPath,
		cfg, sr, fs.NewRealFS(), clock.Now,
	)

	// Helper to stub a full checkpoint sequence with unique commit and tree IDs
	stubForID := func(id int) {
		commitSHA := fmt.Sprintf("commit%d", id)
		treeSHA := fmt.Sprintf("tree%04d", id)
		sr.stub(fmt.Sprintf("git -C %s status --porcelain", sandboxPath),
			exec.CmdResult{Stdout: " M main.go\n"})
		sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", sandboxPath),
			exec.CmdResult{Stdout: ""})
		sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath),
			exec.CmdResult{Stdout: "head1234\n"})
		sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", sandboxPath),
			exec.CmdResult{Stdout: ".git\n"})
		sr.stub(fmt.Sprintf("git -C %s add -A -- . :(exclude).agency :(exclude).git", sandboxPath),
			exec.CmdResult{})
		sr.stub(fmt.Sprintf("git -C %s write-tree", sandboxPath),
			exec.CmdResult{Stdout: treeSHA + "\n"})
		sr.stub(fmt.Sprintf("git -C %s commit-tree %s -p HEAD -m agency snapshot test-inv-001 %d", sandboxPath, treeSHA, id),
			exec.CmdResult{Stdout: commitSHA + "\n"})
		sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/snapshots/test-inv-001/%d %s", sandboxPath, id, commitSHA),
			exec.CmdResult{})
		sr.stub(fmt.Sprintf("git -C %s diff --stat --stat-width=80 head1234..%s", sandboxPath, commitSHA),
			exec.CmdResult{Stdout: ""})
	}

	ctx := context.Background()

	// T=0: First checkpoint should succeed
	stubForID(1)
	if err := e.CreateCheckpoint(ctx); err != nil {
		t.Fatalf("T=0 checkpoint failed: %v", err)
	}

	// T=5s: Rate limited, should be a no-op
	clock.Advance(5 * time.Second)
	// Stub status in case it's called (it shouldn't be due to rate limit)
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n"})
	if err := e.CreateCheckpoint(ctx); err != nil {
		t.Fatalf("T=5s checkpoint failed: %v", err)
	}

	// T=11s: Past rate limit, should succeed
	clock.Advance(6 * time.Second)
	stubForID(2)
	if err := e.CreateCheckpoint(ctx); err != nil {
		t.Fatalf("T=11s checkpoint failed: %v", err)
	}

	// Verify exactly 2 checkpoints were created
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(cpFile.Checkpoints))
	}
}

// 1.8 TestEngine_loadSaveCheckpoints_Roundtrip
func TestEngine_loadSaveCheckpoints_Roundtrip(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	e, _ := newTestEngine(t, sr, cfg)

	// Load on non-existent file should return empty
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 0 {
		t.Errorf("expected empty checkpoints, got %d", len(cpFile.Checkpoints))
	}

	// Save 3 checkpoints
	cpFile.Checkpoints = []Checkpoint{
		{ID: 1, SnapshotRef: "refs/agency/snapshots/test/1", SnapshotCommit: "aaa", SandboxHeadSHA: "base1", CreatedAt: "2026-01-15T12:00:00Z", IncludesUntracked: true, Diffstat: "+1 -0 in 1 files"},
		{ID: 2, SnapshotRef: "refs/agency/snapshots/test/2", SnapshotCommit: "bbb", SandboxHeadSHA: "base2", CreatedAt: "2026-01-15T12:01:00Z", IncludesUntracked: false, Diffstat: "+2 -1 in 2 files"},
		{ID: 3, SnapshotRef: "refs/agency/snapshots/test/3", SnapshotCommit: "ccc", SandboxHeadSHA: "base3", CreatedAt: "2026-01-15T12:02:00Z", IncludesUntracked: true, Diffstat: "+5 -3 in 3 files"},
	}
	if err := e.saveCheckpoints(cpFile); err != nil {
		t.Fatal(err)
	}

	// Load back and verify
	loaded, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Checkpoints) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(loaded.Checkpoints))
	}
	if loaded.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %q, want %q", loaded.SchemaVersion, SchemaVersion)
	}
	for i, cp := range loaded.Checkpoints {
		if cp.ID != cpFile.Checkpoints[i].ID {
			t.Errorf("checkpoint[%d].ID = %d, want %d", i, cp.ID, cpFile.Checkpoints[i].ID)
		}
		if cp.SnapshotCommit != cpFile.Checkpoints[i].SnapshotCommit {
			t.Errorf("checkpoint[%d].SnapshotCommit = %q, want %q", i, cp.SnapshotCommit, cpFile.Checkpoints[i].SnapshotCommit)
		}
	}
}

// 1.9 TestEngine_pruneCheckpoints
func TestEngine_pruneCheckpoints(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	e, _ := newTestEngine(t, sr, cfg)

	// Create CheckpointsFile with 205 entries
	cpFile := NewCheckpointsFile()
	for i := 1; i <= 205; i++ {
		ref := fmt.Sprintf("refs/agency/snapshots/test-inv-001/%d", i)
		cpFile.Checkpoints = append(cpFile.Checkpoints, Checkpoint{
			ID:          i,
			SnapshotRef: ref,
		})
		// Stub the update-ref -d call for each pruned ref
		if i <= 5 { // oldest 5 will be pruned
			sr.stub(fmt.Sprintf("git -C %s update-ref -d %s", e.sandboxPath, ref),
				exec.CmdResult{})
		}
	}

	e.pruneCheckpoints(context.Background(), cpFile)

	// Should have 200 remaining
	if len(cpFile.Checkpoints) != MaxCheckpoints {
		t.Errorf("expected %d checkpoints after prune, got %d", MaxCheckpoints, len(cpFile.Checkpoints))
	}

	// Oldest should now be ID=6
	if cpFile.Checkpoints[0].ID != 6 {
		t.Errorf("oldest checkpoint ID = %d, want 6", cpFile.Checkpoints[0].ID)
	}

	// Verify 5 update-ref -d calls
	calls := sr.getCalls()
	deleteCount := 0
	for _, c := range calls {
		key := c.name + " " + strings.Join(c.args, " ")
		if strings.Contains(key, "update-ref -d") {
			deleteCount++
		}
	}
	if deleteCount != 5 {
		t.Errorf("expected 5 update-ref -d calls, got %d", deleteCount)
	}
}

// 1.10 TestEngine_createCheckpointInternal_GitFailures
func TestEngine_createCheckpointInternal_GitFailures(t *testing.T) {
	tests := []struct {
		name        string
		failAt      string // which command should fail
		wantErrText string
	}{
		{
			name:        "write-tree fails",
			failAt:      "write-tree",
			wantErrText: "write-tree",
		},
		{
			name:        "commit-tree fails",
			failAt:      "commit-tree",
			wantErrText: "commit-tree",
		},
		{
			name:        "update-ref fails",
			failAt:      "update-ref",
			wantErrText: "update-ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := newStubRunner()
			cfg := DefaultConfig()
			cfg.IncludeUntracked = true
			e, _ := newTestEngine(t, sr, cfg)

			// isDirty
			sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
				exec.CmdResult{Stdout: " M main.go\n"})
			// checkDenylist
			sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
				exec.CmdResult{Stdout: ""})
			// rev-parse HEAD
			sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
				exec.CmdResult{Stdout: "head1234\n"})
			// rev-parse --git-dir
			sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", e.sandboxPath),
				exec.CmdResult{Stdout: ".git\n"})
			// git add
			sr.stub(fmt.Sprintf("git -C %s add -A -- . :(exclude).agency :(exclude).git", e.sandboxPath),
				exec.CmdResult{})

			switch tt.failAt {
			case "write-tree":
				sr.stub(fmt.Sprintf("git -C %s write-tree", e.sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "write-tree failed"})
			case "commit-tree":
				sr.stub(fmt.Sprintf("git -C %s write-tree", e.sandboxPath),
					exec.CmdResult{Stdout: "tree1234\n"})
				sr.stub(fmt.Sprintf("git -C %s commit-tree tree1234 -p HEAD -m agency snapshot test-inv-001 1", e.sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "commit-tree failed"})
			case "update-ref":
				sr.stub(fmt.Sprintf("git -C %s write-tree", e.sandboxPath),
					exec.CmdResult{Stdout: "tree1234\n"})
				sr.stub(fmt.Sprintf("git -C %s commit-tree tree1234 -p HEAD -m agency snapshot test-inv-001 1", e.sandboxPath),
					exec.CmdResult{Stdout: "commitsha\n"})
				sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/snapshots/test-inv-001/1 commitsha", e.sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "update-ref failed"})
			}

			err := e.createCheckpointInternal(context.Background())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErrText)
			}

			// Verify no checkpoint was saved
			cpFile, loadErr := e.loadCheckpoints()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(cpFile.Checkpoints) != 0 {
				t.Errorf("expected 0 checkpoints after failure, got %d", len(cpFile.Checkpoints))
			}
		})
	}
}

// 1.11 TestApplier_Apply_Success
func TestApplier_Apply_Success(t *testing.T) {
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	// Write checkpoints.json
	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotRef: "refs/agency/snapshots/inv/1", SnapshotCommit: "aaa111"},
			{ID: 2, SnapshotRef: "refs/agency/snapshots/inv/2", SnapshotCommit: "bbb222"},
			{ID: 3, SnapshotRef: "refs/agency/snapshots/inv/3", SnapshotCommit: "ccc333"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	if err := os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644); err != nil {
		t.Fatal(err)
	}

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))

	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	// Stub cat-file
	sr.stub(fmt.Sprintf("git -C %s cat-file -t ccc333", sandboxPath),
		exec.CmdResult{Stdout: "commit\n"})
	// Stub reset
	sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
		exec.CmdResult{})
	// Stub clean
	sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath),
		exec.CmdResult{})
	// Stub checkout
	sr.stub(fmt.Sprintf("git -C %s checkout ccc333 -- .", sandboxPath),
		exec.CmdResult{})

	cp, err := applier.Apply(context.Background(), 3)
	if err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}

	if cp.ID != 3 {
		t.Errorf("applied checkpoint ID = %d, want 3", cp.ID)
	}
	if cp.SnapshotCommit != "ccc333" {
		t.Errorf("snapshot_commit = %q, want %q", cp.SnapshotCommit, "ccc333")
	}

	// Verify git commands called in correct order
	calls := sr.callKeys()
	expected := []string{
		fmt.Sprintf("git -C %s cat-file -t ccc333", sandboxPath),
		fmt.Sprintf("git -C %s reset --hard", sandboxPath),
		fmt.Sprintf("git -C %s clean -fd", sandboxPath),
		fmt.Sprintf("git -C %s checkout ccc333 -- .", sandboxPath),
	}
	if len(calls) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(calls), calls)
	}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], expected[i])
		}
	}

	// Verify checkpoint_applied event
	events := readEvents(t, eventsPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventKindCheckpointApplied {
		t.Errorf("event kind = %q, want %q", events[0].Kind, EventKindCheckpointApplied)
	}
}

// 1.12 TestApplier_Apply_NotFound
func TestApplier_Apply_NotFound(t *testing.T) {
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	// Write checkpoints.json with IDs 1,2
	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotCommit: "aaa111"},
			{ID: 2, SnapshotCommit: "bbb222"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	if err := os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644); err != nil {
		t.Fatal(err)
	}

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	_, err := applier.Apply(context.Background(), 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.GetCode(err) != errors.ECheckpointNotFound {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.ECheckpointNotFound)
	}
}

// 1.13 TestApplier_Apply_SnapshotMissing
func TestApplier_Apply_SnapshotMissing(t *testing.T) {
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotCommit: "deadbeef"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	if err := os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644); err != nil {
		t.Fatal(err)
	}

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	// cat-file fails
	sr.stub(fmt.Sprintf("git -C %s cat-file -t deadbeef", sandboxPath),
		exec.CmdResult{ExitCode: 128, Stderr: "fatal: not a valid object"})

	_, err := applier.Apply(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.GetCode(err) != errors.ECheckpointNotFound {
		t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.ECheckpointNotFound)
	}
}

// 1.14 TestApplier_Apply_GitFailures
func TestApplier_Apply_GitFailures(t *testing.T) {
	tests := []struct {
		name   string
		failAt string // reset, clean, or checkout
	}{
		{name: "reset fails", failAt: "reset"},
		{name: "clean fails", failAt: "clean"},
		{name: "checkout fails", failAt: "checkout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandboxPath := t.TempDir()
			checkpointsDir := t.TempDir()
			eventsDir := t.TempDir()
			eventsPath := filepath.Join(eventsDir, "events.jsonl")

			cpFile := &CheckpointsFile{
				SchemaVersion: SchemaVersion,
				Checkpoints: []Checkpoint{
					{ID: 1, SnapshotCommit: "aaa111"},
				},
			}
			cpData, _ := json.MarshalIndent(cpFile, "", "  ")
			if err := os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644); err != nil {
				t.Fatal(err)
			}

			sr := newStubRunner()
			clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
			applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

			// cat-file succeeds
			sr.stub(fmt.Sprintf("git -C %s cat-file -t aaa111", sandboxPath),
				exec.CmdResult{Stdout: "commit\n"})

			switch tt.failAt {
			case "reset":
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "reset failed"})
			case "clean":
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{})
				sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "clean failed"})
			case "checkout":
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{})
				sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath),
					exec.CmdResult{})
				sr.stub(fmt.Sprintf("git -C %s checkout aaa111 -- .", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "checkout failed"})
			}

			_, err := applier.Apply(context.Background(), 1)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if errors.GetCode(err) != errors.ERollbackFailed {
				t.Errorf("error code = %q, want %q", errors.GetCode(err), errors.ERollbackFailed)
			}
		})
	}
}

// 1.15 TestEngine_EventEmission
func TestEngine_EventEmission(t *testing.T) {
	t.Run("success event", func(t *testing.T) {
		sr := newStubRunner()
		cfg := DefaultConfig()
		cfg.IncludeUntracked = true
		e, _ := newTestEngine(t, sr, cfg)

		stubFullCheckpointSequence(sr, e.sandboxPath, true)

		if err := e.createCheckpointInternal(context.Background()); err != nil {
			t.Fatal(err)
		}

		events := readEvents(t, e.eventsPath)
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		ev := events[0]
		if ev.SchemaVersion != "1.0" {
			t.Errorf("schema_version = %q, want %q", ev.SchemaVersion, "1.0")
		}
		if ev.Seq != 1 {
			t.Errorf("seq = %d, want 1", ev.Seq)
		}
		if ev.InvocationID != "test-inv-001" {
			t.Errorf("invocation_id = %q, want %q", ev.InvocationID, "test-inv-001")
		}
		if ev.Kind != EventKindCheckpointCreated {
			t.Errorf("kind = %q, want %q", ev.Kind, EventKindCheckpointCreated)
		}
		if ev.Timestamp == "" {
			t.Error("timestamp should not be empty")
		}
		if ev.Data == nil {
			t.Error("data should not be nil")
		}
		// Check data fields
		if cpID, ok := ev.Data["checkpoint_id"]; !ok || cpID != float64(1) {
			t.Errorf("data.checkpoint_id = %v, want 1", cpID)
		}
	})

	t.Run("failure event", func(t *testing.T) {
		sr := newStubRunner()
		cfg := DefaultConfig()
		cfg.IncludeUntracked = true
		e, _ := newTestEngine(t, sr, cfg)

		// isDirty: dirty
		sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
			exec.CmdResult{Stdout: " M main.go\n"})
		// denylist: clean
		sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
			exec.CmdResult{Stdout: ""})
		// rev-parse HEAD fails
		sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
			exec.CmdResult{ExitCode: 128, Stderr: "fatal: not a git repo"})

		err := e.createCheckpointInternal(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}

		// Manually emit failure event (as the caller would)
		e.emitCheckpointFailed(err.Error())

		events := readEvents(t, e.eventsPath)
		foundFailed := false
		for _, ev := range events {
			if ev.Kind == EventKindCheckpointFailed {
				foundFailed = true
				if reason, ok := ev.Data["reason"]; !ok || reason == "" {
					t.Errorf("checkpoint_failed event should have reason, got %v", ev.Data)
				}
			}
		}
		if !foundFailed {
			t.Error("expected checkpoint_failed event")
		}
	})
}

// ---------------------------------------------------------------------------
// Phase 2: Duplicate detection & typed error tests
// ---------------------------------------------------------------------------

// 2.1 TestEngine_createCheckpointInternal_SkipsDuplicate
func TestEngine_createCheckpointInternal_SkipsDuplicate(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	// First checkpoint: full sequence
	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	if err := e.createCheckpointInternal(context.Background()); err != nil {
		t.Fatalf("first checkpoint failed: %v", err)
	}

	// Verify 1 checkpoint with TreeSHA populated
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(cpFile.Checkpoints))
	}
	if cpFile.Checkpoints[0].TreeSHA != "eeeeeeeeeeee" {
		t.Errorf("TreeSHA = %q, want %q", cpFile.Checkpoints[0].TreeSHA, "eeeeeeeeeeee")
	}

	// Second checkpoint: same tree hash → should be skipped
	// Stub just the commands up to write-tree (duplicate detection happens before commit-tree)
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n"})
	sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
		exec.CmdResult{Stdout: ""})
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
		exec.CmdResult{Stdout: "aabbccdd00112233\n"})
	sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", e.sandboxPath),
		exec.CmdResult{Stdout: ".git\n"})
	sr.stub(fmt.Sprintf("git -C %s add -A -- . :(exclude).agency :(exclude).git", e.sandboxPath),
		exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s write-tree", e.sandboxPath),
		exec.CmdResult{Stdout: "eeeeeeeeeeee\n"}) // Same tree!

	if err := e.createCheckpointInternal(context.Background()); err != nil {
		t.Fatalf("second checkpoint call failed: %v", err)
	}

	// Still only 1 checkpoint
	cpFile, err = e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint (duplicate skipped), got %d", len(cpFile.Checkpoints))
	}

	// Verify commit-tree was NOT called for the second attempt
	calls := sr.callKeys()
	commitTreeCount := 0
	for _, c := range calls {
		if strings.Contains(c, "commit-tree") {
			commitTreeCount++
		}
	}
	if commitTreeCount != 1 {
		t.Errorf("expected 1 commit-tree call (not 2), got %d", commitTreeCount)
	}
}

// 2.2 TestEngine_createCheckpointInternal_NewTreeCreatesCheckpoint
func TestEngine_createCheckpointInternal_NewTreeCreatesCheckpoint(t *testing.T) {
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	// First checkpoint
	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	if err := e.createCheckpointInternal(context.Background()); err != nil {
		t.Fatalf("first checkpoint failed: %v", err)
	}

	// Second checkpoint: different tree hash → should create new checkpoint
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n"})
	sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
		exec.CmdResult{Stdout: ""})
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
		exec.CmdResult{Stdout: "aabbccdd00112233\n"})
	sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", e.sandboxPath),
		exec.CmdResult{Stdout: ".git\n"})
	sr.stub(fmt.Sprintf("git -C %s add -A -- . :(exclude).agency :(exclude).git", e.sandboxPath),
		exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s write-tree", e.sandboxPath),
		exec.CmdResult{Stdout: "dddddddddddd\n"}) // Different tree!
	sr.stub(fmt.Sprintf("git -C %s commit-tree dddddddddddd -p HEAD -m agency snapshot test-inv-001 2", e.sandboxPath),
		exec.CmdResult{Stdout: "newcommitsha\n"})
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/snapshots/test-inv-001/2 newcommitsha", e.sandboxPath),
		exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s diff --stat --stat-width=80 aabbccdd00112233..newcommitsha", e.sandboxPath),
		exec.CmdResult{Stdout: ""})

	if err := e.createCheckpointInternal(context.Background()); err != nil {
		t.Fatalf("second checkpoint failed: %v", err)
	}

	cpFile, err := e.loadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cpFile.Checkpoints) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(cpFile.Checkpoints))
	}
	if cpFile.Checkpoints[0].TreeSHA == cpFile.Checkpoints[1].TreeSHA {
		t.Error("expected distinct TreeSHA values")
	}
	if cpFile.Checkpoints[1].TreeSHA != "dddddddddddd" {
		t.Errorf("second TreeSHA = %q, want %q", cpFile.Checkpoints[1].TreeSHA, "dddddddddddd")
	}
}

// 2.3 TestApplier_Apply_ReturnsTypedErrors
func TestApplier_Apply_ReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(sr *stubRunner, sandboxPath, checkpointsDir string)
		cpID     int
		wantCode errors.Code
	}{
		{
			name: "checkpoint not found",
			setup: func(sr *stubRunner, sandboxPath, checkpointsDir string) {
				// checkpoints.json with IDs 1,2 only
				cpFile := &CheckpointsFile{
					SchemaVersion: SchemaVersion,
					Checkpoints: []Checkpoint{
						{ID: 1, SnapshotCommit: "aaa"},
						{ID: 2, SnapshotCommit: "bbb"},
					},
				}
				cpData, _ := json.MarshalIndent(cpFile, "", "  ")
				_ = os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644)
			},
			cpID:     99,
			wantCode: errors.ECheckpointNotFound,
		},
		{
			name: "snapshot missing",
			setup: func(sr *stubRunner, sandboxPath, checkpointsDir string) {
				cpFile := &CheckpointsFile{
					SchemaVersion: SchemaVersion,
					Checkpoints:   []Checkpoint{{ID: 1, SnapshotCommit: "deadbeef"}},
				}
				cpData, _ := json.MarshalIndent(cpFile, "", "  ")
				_ = os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644)
				sr.stub(fmt.Sprintf("git -C %s cat-file -t deadbeef", sandboxPath),
					exec.CmdResult{ExitCode: 128, Stderr: "not a valid object"})
			},
			cpID:     1,
			wantCode: errors.ECheckpointNotFound,
		},
		{
			name: "git reset fails",
			setup: func(sr *stubRunner, sandboxPath, checkpointsDir string) {
				cpFile := &CheckpointsFile{
					SchemaVersion: SchemaVersion,
					Checkpoints:   []Checkpoint{{ID: 1, SnapshotCommit: "aaa111"}},
				}
				cpData, _ := json.MarshalIndent(cpFile, "", "  ")
				_ = os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644)
				sr.stub(fmt.Sprintf("git -C %s cat-file -t aaa111", sandboxPath),
					exec.CmdResult{Stdout: "commit\n"})
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "reset failed"})
			},
			cpID:     1,
			wantCode: errors.ERollbackFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandboxPath := t.TempDir()
			checkpointsDir := t.TempDir()
			eventsDir := t.TempDir()
			eventsPath := filepath.Join(eventsDir, "events.jsonl")

			sr := newStubRunner()
			clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
			tt.setup(sr, sandboxPath, checkpointsDir)

			applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)
			_, err := applier.Apply(context.Background(), tt.cpID)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if errors.GetCode(err) != tt.wantCode {
				t.Errorf("error code = %q, want %q (err: %v)", errors.GetCode(err), tt.wantCode, err)
			}
		})
	}
}
