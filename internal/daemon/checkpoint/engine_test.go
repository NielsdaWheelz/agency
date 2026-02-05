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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "index"), []byte("fake-index"), 0o644))

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
		require.NoError(t, err)
	}
	defer func() { _ = f.Close() }()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev Event
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &ev), "failed to parse event")
		events = append(events, ev)
	}
	return events
}

// ---------------------------------------------------------------------------
// Existing tests (retained from original file)
// ---------------------------------------------------------------------------

func TestMatchesDenylist(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchesDenylist(tt.filename)
			assert.Equal(t, tt.want, got, "matchesDenylist(%q)", tt.filename)
		})
	}
}

func TestCheckpointsFile_NextID(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &CheckpointsFile{
				SchemaVersion: SchemaVersion,
				Checkpoints:   tt.checkpoints,
			}
			got := f.NextID()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckpointsFile_FindByID(t *testing.T) {
	t.Parallel()
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
				require.NotNil(t, got, "FindByID(%d) returned nil, want checkpoint", tt.id)
				assert.Equal(t, tt.wantCommit, got.SnapshotCommit, "FindByID(%d).SnapshotCommit", tt.id)
			} else {
				assert.Nil(t, got, "FindByID(%d)", tt.id)
			}
		})
	}
}

func TestEngine_shouldIgnorePath(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			t.Parallel()
			got := e.shouldIgnorePath(tt.path)
			assert.Equal(t, tt.want, got, "shouldIgnorePath(%q)", tt.path)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	assert.True(t, cfg.IncludeUntracked, "DefaultConfig().IncludeUntracked should be true")
	assert.Equal(t, time.Duration(3e9), cfg.DebounceInterval, "DefaultConfig().DebounceInterval")
	assert.Equal(t, time.Duration(10e9), cfg.RateLimit, "DefaultConfig().RateLimit")
	assert.Equal(t, time.Duration(30e9), cfg.PollInterval, "DefaultConfig().PollInterval")
}

func TestNewCheckpointsFile(t *testing.T) {
	t.Parallel()
	f := NewCheckpointsFile()

	assert.Equal(t, SchemaVersion, f.SchemaVersion)
	assert.Len(t, f.Checkpoints, 0)
}

func TestDenylistPatterns(t *testing.T) {
	t.Parallel()
	expected := []string{
		".env",
		".env.*",
		"*.key",
		"*.pem",
		"credentials.json",
		"secrets.json",
	}

	assert.Equal(t, expected, DenylistPatterns)
}

// ---------------------------------------------------------------------------
// Phase 1: New unit tests
// ---------------------------------------------------------------------------

// 1.1 TestEngine_isDirty
func TestEngine_isDirty(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDirty, dirty)
		})
	}
}

// 1.2 TestEngine_checkDenylist
func TestEngine_checkDenylist(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// 1.3 TestEngine_computeDiffstat
func TestEngine_computeDiffstat(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			assert.Equal(t, tt.want, got)
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
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, checkpointsDir := newTestEngine(t, sr, cfg)

	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	err := e.createCheckpointInternal(context.Background())
	require.NoError(t, err, "createCheckpointInternal()")

	// Verify checkpoints.json
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err, "loadCheckpoints()")
	require.Len(t, cpFile.Checkpoints, 1)
	cp := cpFile.Checkpoints[0]
	assert.Equal(t, 1, cp.ID)
	assert.Equal(t, "ffffffffffffffff", cp.SnapshotCommit)
	assert.Equal(t, "aabbccdd00112233", cp.SandboxHeadSHA)
	assert.True(t, cp.IncludesUntracked, "expected IncludesUntracked=true")
	assert.Equal(t, "+2 -3 in 1 files", cp.Diffstat)

	// Verify events.jsonl
	events := readEvents(t, e.eventsPath)
	require.Len(t, events, 1)
	assert.Equal(t, EventKindCheckpointCreated, events[0].Kind)

	// Verify git commands were called in order
	calls := sr.callKeys()
	expectedCalls := []string{
		fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		fmt.Sprintf("git -C %s ls-files -o --exclude-standard", e.sandboxPath),
		fmt.Sprintf("git -C %s rev-parse HEAD", e.sandboxPath),
		fmt.Sprintf("git -C %s rev-parse --git-dir", e.sandboxPath),
	}
	for i, expected := range expectedCalls {
		require.Greater(t, len(calls), i, "expected call %d: %s, but only got %d calls", i, expected, len(calls))
		assert.Equal(t, expected, calls[i], "call[%d]", i)
	}

	// Verify checkpoints.json file exists on disk
	cpPath := filepath.Join(checkpointsDir, "checkpoints.json")
	assert.FileExists(t, cpPath, "checkpoints.json not written to disk")
}

// 1.5 TestEngine_createCheckpointInternal_DenylistDegradation
func TestEngine_createCheckpointInternal_DenylistDegradation(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "createCheckpointInternal()")

	// Verify checkpoint has IncludesUntracked=false
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)
	assert.False(t, cpFile.Checkpoints[0].IncludesUntracked, "expected IncludesUntracked=false after denylist degradation")

	// Verify git add -u was used (not git add -A)
	calls := sr.callKeys()
	foundAddU := false
	for _, c := range calls {
		if strings.Contains(c, "add -u") {
			foundAddU = true
		}
		assert.NotContains(t, c, "add -A", "git add -A should not be called after denylist degradation")
	}
	assert.True(t, foundAddU, "expected git add -u to be called")

	// Verify events: should have denylist_triggered + checkpoint_created
	events := readEvents(t, e.eventsPath)
	require.GreaterOrEqual(t, len(events), 2)
	foundDenylist := false
	for _, ev := range events {
		if ev.Kind == EventKindCheckpointDenylistTriggered {
			foundDenylist = true
		}
	}
	assert.True(t, foundDenylist, "expected denylist_triggered event")
}

// 1.6 TestEngine_createCheckpointInternal_NotDirty
func TestEngine_createCheckpointInternal_NotDirty(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	e, _ := newTestEngine(t, sr, cfg)

	// isDirty: clean
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", e.sandboxPath),
		exec.CmdResult{Stdout: ""})

	err := e.createCheckpointInternal(context.Background())
	require.NoError(t, err, "createCheckpointInternal()")

	// Verify no checkpoints created
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 0)

	// Verify no events
	events := readEvents(t, e.eventsPath)
	assert.Len(t, events, 0)

	// Verify only status was called (no further git commands)
	calls := sr.getCalls()
	assert.Len(t, calls, 1, "expected 1 git call (status)")
}

// 1.7 TestEngine_CreateCheckpoint_RateLimited
func TestEngine_CreateCheckpoint_RateLimited(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "index"), []byte("fake"), 0o644))

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
	require.NoError(t, e.CreateCheckpoint(ctx), "T=0 checkpoint failed")

	// T=5s: Rate limited, should be a no-op
	clock.Advance(5 * time.Second)
	// Stub status in case it's called (it shouldn't be due to rate limit)
	sr.stub(fmt.Sprintf("git -C %s status --porcelain", sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n"})
	require.NoError(t, e.CreateCheckpoint(ctx), "T=5s checkpoint failed")

	// T=11s: Past rate limit, should succeed
	clock.Advance(6 * time.Second)
	stubForID(2)
	require.NoError(t, e.CreateCheckpoint(ctx), "T=11s checkpoint failed")

	// Verify exactly 2 checkpoints were created
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 2)
}

// 1.8 TestEngine_loadSaveCheckpoints_Roundtrip
func TestEngine_loadSaveCheckpoints_Roundtrip(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	e, _ := newTestEngine(t, sr, cfg)

	// Load on non-existent file should return empty
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 0)

	// Save 3 checkpoints
	cpFile.Checkpoints = []Checkpoint{
		{ID: 1, SnapshotRef: "refs/agency/snapshots/test/1", SnapshotCommit: "aaa", SandboxHeadSHA: "base1", CreatedAt: "2026-01-15T12:00:00Z", IncludesUntracked: true, Diffstat: "+1 -0 in 1 files"},
		{ID: 2, SnapshotRef: "refs/agency/snapshots/test/2", SnapshotCommit: "bbb", SandboxHeadSHA: "base2", CreatedAt: "2026-01-15T12:01:00Z", IncludesUntracked: false, Diffstat: "+2 -1 in 2 files"},
		{ID: 3, SnapshotRef: "refs/agency/snapshots/test/3", SnapshotCommit: "ccc", SandboxHeadSHA: "base3", CreatedAt: "2026-01-15T12:02:00Z", IncludesUntracked: true, Diffstat: "+5 -3 in 3 files"},
	}
	require.NoError(t, e.saveCheckpoints(cpFile))

	// Load back and verify
	loaded, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, loaded.Checkpoints, 3)
	assert.Equal(t, SchemaVersion, loaded.SchemaVersion)
	for i, cp := range loaded.Checkpoints {
		assert.Equal(t, cpFile.Checkpoints[i].ID, cp.ID, "checkpoint[%d].ID", i)
		assert.Equal(t, cpFile.Checkpoints[i].SnapshotCommit, cp.SnapshotCommit, "checkpoint[%d].SnapshotCommit", i)
	}
}

// 1.9 TestEngine_pruneCheckpoints
func TestEngine_pruneCheckpoints(t *testing.T) {
	t.Parallel()
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
	assert.Len(t, cpFile.Checkpoints, MaxCheckpoints)

	// Oldest should now be ID=6
	assert.Equal(t, 6, cpFile.Checkpoints[0].ID)

	// Verify 5 update-ref -d calls
	calls := sr.getCalls()
	deleteCount := 0
	for _, c := range calls {
		key := c.name + " " + strings.Join(c.args, " ")
		if strings.Contains(key, "update-ref -d") {
			deleteCount++
		}
	}
	assert.Equal(t, 5, deleteCount, "expected 5 update-ref -d calls")
}

// 1.10 TestEngine_createCheckpointInternal_GitFailures
func TestEngine_createCheckpointInternal_GitFailures(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErrText)

			// Verify no checkpoint was saved
			cpFile, loadErr := e.loadCheckpoints()
			require.NoError(t, loadErr)
			assert.Len(t, cpFile.Checkpoints, 0, "expected 0 checkpoints after failure")
		})
	}
}

// 1.11 TestApplier_Apply_Success
func TestApplier_Apply_Success(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

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
	require.NoError(t, err, "Apply()")

	assert.Equal(t, 3, cp.ID)
	assert.Equal(t, "ccc333", cp.SnapshotCommit)

	// Verify git commands called in correct order
	calls := sr.callKeys()
	expected := []string{
		fmt.Sprintf("git -C %s cat-file -t ccc333", sandboxPath),
		fmt.Sprintf("git -C %s reset --hard", sandboxPath),
		fmt.Sprintf("git -C %s clean -fd", sandboxPath),
		fmt.Sprintf("git -C %s checkout ccc333 -- .", sandboxPath),
	}
	require.Len(t, calls, len(expected))
	for i := range expected {
		assert.Equal(t, expected[i], calls[i], "call[%d]", i)
	}

	// Verify checkpoint_applied event
	events := readEvents(t, eventsPath)
	require.Len(t, events, 1)
	assert.Equal(t, EventKindCheckpointApplied, events[0].Kind)
}

// 1.12 TestApplier_Apply_NotFound
func TestApplier_Apply_NotFound(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	_, err := applier.Apply(context.Background(), 5)
	require.Error(t, err)
	assert.Equal(t, errors.ECheckpointNotFound, errors.GetCode(err))
}

// 1.13 TestApplier_Apply_SnapshotMissing
func TestApplier_Apply_SnapshotMissing(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	// cat-file fails
	sr.stub(fmt.Sprintf("git -C %s cat-file -t deadbeef", sandboxPath),
		exec.CmdResult{ExitCode: 128, Stderr: "fatal: not a valid object"})

	_, err := applier.Apply(context.Background(), 1)
	require.Error(t, err)
	assert.Equal(t, errors.ECheckpointNotFound, errors.GetCode(err))
}

// 1.14 TestApplier_Apply_GitFailures
func TestApplier_Apply_GitFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		failAt string // reset, clean, or checkout
	}{
		{name: "reset fails", failAt: "reset"},
		{name: "clean fails", failAt: "clean"},
		{name: "checkout fails", failAt: "checkout"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

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
			require.Error(t, err)
			assert.Equal(t, errors.ERollbackFailed, errors.GetCode(err))
		})
	}
}

// 1.15 TestEngine_EventEmission
func TestEngine_EventEmission(t *testing.T) {
	t.Parallel()
	t.Run("success event", func(t *testing.T) {
		sr := newStubRunner()
		cfg := DefaultConfig()
		cfg.IncludeUntracked = true
		e, _ := newTestEngine(t, sr, cfg)

		stubFullCheckpointSequence(sr, e.sandboxPath, true)

		require.NoError(t, e.createCheckpointInternal(context.Background()))

		events := readEvents(t, e.eventsPath)
		require.Len(t, events, 1)

		ev := events[0]
		assert.Equal(t, "1.0", ev.SchemaVersion)
		assert.Equal(t, uint64(1), ev.Seq)
		assert.Equal(t, "test-inv-001", ev.InvocationID)
		assert.Equal(t, EventKindCheckpointCreated, ev.Kind)
		assert.NotEmpty(t, ev.Timestamp, "timestamp should not be empty")
		require.NotNil(t, ev.Data, "data should not be nil")
		// Check data fields
		cpID, ok := ev.Data["checkpoint_id"]
		assert.True(t, ok, "data should contain checkpoint_id")
		assert.Equal(t, float64(1), cpID)
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
		require.Error(t, err)

		// Manually emit failure event (as the caller would)
		e.emitCheckpointFailed(err.Error())

		events := readEvents(t, e.eventsPath)
		foundFailed := false
		for _, ev := range events {
			if ev.Kind == EventKindCheckpointFailed {
				foundFailed = true
				reason, ok := ev.Data["reason"]
				assert.True(t, ok, "checkpoint_failed event should have reason key")
				assert.NotEmpty(t, reason, "checkpoint_failed event reason should not be empty")
			}
		}
		assert.True(t, foundFailed, "expected checkpoint_failed event")
	})
}

// ---------------------------------------------------------------------------
// Phase 2: Duplicate detection & typed error tests
// ---------------------------------------------------------------------------

// 2.1 TestEngine_createCheckpointInternal_SkipsDuplicate
func TestEngine_createCheckpointInternal_SkipsDuplicate(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	// First checkpoint: full sequence
	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	require.NoError(t, e.createCheckpointInternal(context.Background()), "first checkpoint failed")

	// Verify 1 checkpoint with TreeSHA populated
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)
	assert.Equal(t, "eeeeeeeeeeee", cpFile.Checkpoints[0].TreeSHA)

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

	require.NoError(t, e.createCheckpointInternal(context.Background()), "second checkpoint call failed")

	// Still only 1 checkpoint
	cpFile, err = e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 1, "expected 1 checkpoint (duplicate skipped)")

	// Verify commit-tree was NOT called for the second attempt
	calls := sr.callKeys()
	commitTreeCount := 0
	for _, c := range calls {
		if strings.Contains(c, "commit-tree") {
			commitTreeCount++
		}
	}
	assert.Equal(t, 1, commitTreeCount, "expected 1 commit-tree call (not 2)")
}

// 2.2 TestEngine_createCheckpointInternal_NewTreeCreatesCheckpoint
func TestEngine_createCheckpointInternal_NewTreeCreatesCheckpoint(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	// First checkpoint
	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	require.NoError(t, e.createCheckpointInternal(context.Background()), "first checkpoint failed")

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

	require.NoError(t, e.createCheckpointInternal(context.Background()), "second checkpoint failed")

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 2)
	assert.NotEqual(t, cpFile.Checkpoints[0].TreeSHA, cpFile.Checkpoints[1].TreeSHA, "expected distinct TreeSHA values")
	assert.Equal(t, "dddddddddddd", cpFile.Checkpoints[1].TreeSHA)
}

// 2.3 TestApplier_Apply_ReturnsTypedErrors
func TestApplier_Apply_ReturnsTypedErrors(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sandboxPath := t.TempDir()
			checkpointsDir := t.TempDir()
			eventsDir := t.TempDir()
			eventsPath := filepath.Join(eventsDir, "events.jsonl")

			sr := newStubRunner()
			clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
			tt.setup(sr, sandboxPath, checkpointsDir)

			applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)
			_, err := applier.Apply(context.Background(), tt.cpID)
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, errors.GetCode(err))
		})
	}
}
