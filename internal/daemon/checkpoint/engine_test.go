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

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
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

type failingAppender struct {
	err error
}

func (f failingAppender) Append(string, string, string, map[string]any, invocationevents.AppendOptions) (invocationevents.AppendResult, error) {
	if f.err != nil {
		return invocationevents.AppendResult{}, f.err
	}
	return invocationevents.AppendResult{Seq: 1}, nil
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
		gitIgnoredDirs: map[string]bool{
			"/sandbox/tree/node_modules": true,
			"/sandbox/tree/.venv":        true,
		},
	}

	tests := []struct {
		path string
		want bool
	}{
		// Always-skip dirs
		{"/sandbox/tree/.git/index", true},
		{"/sandbox/tree/.git", true},
		{"/sandbox/tree/.agency/state/runner_status.json", true},
		{"/sandbox/tree/.agency", true},
		// Gitignored dirs
		{"/sandbox/tree/node_modules/express/index.js", true},
		{"/sandbox/tree/node_modules", true},
		{"/sandbox/tree/.venv/lib/python3/site.py", true},
		// Non-ignored paths
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

func TestEngine_isSkippedDir(t *testing.T) {
	t.Parallel()
	e := &Engine{
		sandboxPath: "/sandbox/tree",
		gitIgnoredDirs: map[string]bool{
			"/sandbox/tree/node_modules": true,
			"/sandbox/tree/build":        true,
		},
	}

	tests := []struct {
		path string
		want bool
	}{
		// Always-skip by base name
		{"/sandbox/tree/.git", true},
		{"/sandbox/tree/.agency", true},
		{"/sandbox/tree/sub/.git", true},
		{"/sandbox/tree/sub/.agency", true},
		// Gitignored by absolute path
		{"/sandbox/tree/node_modules", true},
		{"/sandbox/tree/build", true},
		// Not skipped
		{"/sandbox/tree/src", false},
		{"/sandbox/tree/src/components", false},
		{"/sandbox/tree", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := e.isSkippedDir(tt.path)
			assert.Equal(t, tt.want, got, "isSkippedDir(%q)", tt.path)
		})
	}
}

func TestParseGitIgnoredDirs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		sandboxPath string
		gitOutput   string
		want        map[string]bool
	}{
		{
			name:        "typical gitignored dirs",
			sandboxPath: "/sandbox/tree",
			gitOutput:   "node_modules/\nvendor/\n",
			want: map[string]bool{
				"/sandbox/tree/node_modules": true,
				"/sandbox/tree/vendor":       true,
			},
		},
		{
			name:        "empty output",
			sandboxPath: "/sandbox/tree",
			gitOutput:   "",
			want:        map[string]bool{},
		},
		{
			name:        "mixed with blank lines",
			sandboxPath: "/sandbox/tree",
			gitOutput:   "build/\n\n.venv/\n",
			want: map[string]bool{
				"/sandbox/tree/build": true,
				"/sandbox/tree/.venv": true,
			},
		},
		{
			name:        "files without trailing slash are skipped",
			sandboxPath: "/sandbox/tree",
			gitOutput:   "node_modules/\nsome-file.txt\nvendor/\n",
			want: map[string]bool{
				"/sandbox/tree/node_modules": true,
				"/sandbox/tree/vendor":       true,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseGitIgnoredDirs(tt.sandboxPath, tt.gitOutput)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadGitIgnoredDirs(t *testing.T) {
	t.Parallel()

	t.Run("reads gitignore and finds directories", func(t *testing.T) {
		t.Parallel()
		sandbox := t.TempDir()

		// Create .gitignore
		gitignoreContent := "node_modules/\nbuild/\n# comment\n*.log\n.venv/\n"
		require.NoError(t, os.WriteFile(filepath.Join(sandbox, ".gitignore"), []byte(gitignoreContent), 0o644))

		// Create matching directories
		require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "node_modules", "express"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "build"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(sandbox, ".venv", "lib"), 0o755))
		// Create non-matching directory
		require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "src"), 0o755))

		got := ReadGitIgnoredDirs(sandbox)
		assert.True(t, got[filepath.Join(sandbox, "node_modules")], "node_modules should be found")
		assert.True(t, got[filepath.Join(sandbox, "build")], "build should be found")
		assert.True(t, got[filepath.Join(sandbox, ".venv")], ".venv should be found")
		assert.False(t, got[filepath.Join(sandbox, "src")], "src should not be in result")
	})

	t.Run("no gitignore file returns empty", func(t *testing.T) {
		t.Parallel()
		sandbox := t.TempDir()
		got := ReadGitIgnoredDirs(sandbox)
		assert.Empty(t, got)
	})

	t.Run("skips glob patterns and negation", func(t *testing.T) {
		t.Parallel()
		sandbox := t.TempDir()
		gitignoreContent := "*.log\n!important.log\nnode_modules/\n"
		require.NoError(t, os.WriteFile(filepath.Join(sandbox, ".gitignore"), []byte(gitignoreContent), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "node_modules"), 0o755))

		got := ReadGitIgnoredDirs(sandbox)
		assert.Len(t, got, 1)
		assert.True(t, got[filepath.Join(sandbox, "node_modules")])
	})

	t.Run("dir pattern without trailing slash matches existing dir", func(t *testing.T) {
		t.Parallel()
		sandbox := t.TempDir()
		gitignoreContent := "dist\n"
		require.NoError(t, os.WriteFile(filepath.Join(sandbox, ".gitignore"), []byte(gitignoreContent), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "dist"), 0o755))

		got := ReadGitIgnoredDirs(sandbox)
		assert.True(t, got[filepath.Join(sandbox, "dist")], "dist should be found even without trailing /")
	})
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
	// Stub rev-parse HEAD
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath),
		exec.CmdResult{Stdout: "head-before-apply\n"})
	// Stub backup ref creation
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/restore-backups/test-inv/20260115T120000.000000000Z-cp3 head-before-apply", sandboxPath),
		exec.CmdResult{})
	// Stub reset
	sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
		exec.CmdResult{})
	// Stub clean
	sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath),
		exec.CmdResult{})
	// Stub exact tree restore
	sr.stub(fmt.Sprintf("git -C %s read-tree --reset -u ccc333", sandboxPath),
		exec.CmdResult{})

	cp, err := applier.Apply(context.Background(), 3)
	require.NoError(t, err, "Apply()")

	assert.Equal(t, 3, cp.ID)
	assert.Equal(t, "ccc333", cp.SnapshotCommit)

	// Verify git commands called in correct order
	calls := sr.callKeys()
	expected := []string{
		fmt.Sprintf("git -C %s cat-file -t ccc333", sandboxPath),
		fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath),
		fmt.Sprintf("git -C %s update-ref refs/agency/restore-backups/test-inv/20260115T120000.000000000Z-cp3 head-before-apply", sandboxPath),
		fmt.Sprintf("git -C %s reset --hard", sandboxPath),
		fmt.Sprintf("git -C %s clean -fd", sandboxPath),
		fmt.Sprintf("git -C %s read-tree --reset -u ccc333", sandboxPath),
	}
	require.Len(t, calls, len(expected))
	for i := range expected {
		assert.Equal(t, expected[i], calls[i], "call[%d]", i)
	}

	// Verify checkpoint_applied event
	events := readEvents(t, eventsPath)
	require.Len(t, events, 2)
	assert.Equal(t, EventKindCheckpointApplyStarted, events[0].Kind)
	assert.Equal(t, EventKindCheckpointApplied, events[1].Kind)
}

func TestApplier_Apply_EmitsStartedAndAppliedEvents(t *testing.T) {
	t.Parallel()
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotRef: "refs/agency/snapshots/inv/1", SnapshotCommit: "aaa111"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	sr.stub(fmt.Sprintf("git -C %s cat-file -t aaa111", sandboxPath), exec.CmdResult{Stdout: "commit\n"})
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath), exec.CmdResult{Stdout: "head-before-apply\n"})
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/restore-backups/test-inv/20260115T120000.000000000Z-cp1 head-before-apply", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s read-tree --reset -u aaa111", sandboxPath), exec.CmdResult{})

	_, err := applier.Apply(context.Background(), 1)
	require.NoError(t, err)

	events := readEvents(t, eventsPath)
	require.Len(t, events, 2, "apply should emit a start marker and a success marker")
	assert.Equal(t, EventKindCheckpointApplyStarted, events[0].Kind)
	assert.Equal(t, EventKindCheckpointApplied, events[1].Kind)
}

func TestApplier_ApplyWithOptions_RewindHeadUsesSandboxHeadSHA(t *testing.T) {
	t.Parallel()
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotRef: "refs/agency/snapshots/inv/1", SnapshotCommit: "aaa111", SandboxHeadSHA: "parent0001"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	sr.stub(fmt.Sprintf("git -C %s cat-file -t aaa111", sandboxPath), exec.CmdResult{Stdout: "commit\n"})
	sr.stub(fmt.Sprintf("git -C %s cat-file -t parent0001", sandboxPath), exec.CmdResult{Stdout: "commit\n"})
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath), exec.CmdResult{Stdout: "head-before-apply\n"})
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/restore-backups/test-inv/20260115T120000.000000000Z-cp1 head-before-apply", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s reset --hard parent0001", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s read-tree --reset -u aaa111", sandboxPath), exec.CmdResult{})

	_, err := applier.ApplyWithOptions(context.Background(), 1, ApplyOptions{RewindHeadToSnapshotBase: true})
	require.NoError(t, err)

	calls := sr.callKeys()
	assert.Contains(t, calls, fmt.Sprintf("git -C %s reset --hard parent0001", sandboxPath))
	assert.NotContains(t, calls, fmt.Sprintf("git -C %s rev-parse aaa111^", sandboxPath))
}

func TestApplier_ApplyWithOptions_RewindHeadRequiresSandboxHeadSHA(t *testing.T) {
	t.Parallel()
	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 1, SnapshotRef: "refs/agency/snapshots/inv/1", SnapshotCommit: "aaa111"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	sr.stub(fmt.Sprintf("git -C %s cat-file -t aaa111", sandboxPath), exec.CmdResult{Stdout: "commit\n"})

	_, err := applier.ApplyWithOptions(context.Background(), 1, ApplyOptions{RewindHeadToSnapshotBase: true})
	require.Error(t, err)
	assert.Equal(t, errors.ECheckpointFailed, errors.GetCode(err))
	assert.Contains(t, err.Error(), "sandbox_head_sha")
	assert.NotContains(t, sr.callKeys(), fmt.Sprintf("git -C %s rev-parse", sandboxPath))
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
		failAt string // reset, clean, or read-tree
	}{
		{name: "reset fails", failAt: "reset"},
		{name: "clean fails", failAt: "clean"},
		{name: "read-tree fails", failAt: "read-tree"},
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
			sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath),
				exec.CmdResult{Stdout: "head-before-apply\n"})
			sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/restore-backups/test-inv/20260115T120000.000000000Z-cp1 head-before-apply", sandboxPath),
				exec.CmdResult{})

			switch tt.failAt {
			case "reset":
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "reset failed"})
			case "clean":
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{})
				sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "clean failed"})
			case "read-tree":
				sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath),
					exec.CmdResult{})
				sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath),
					exec.CmdResult{})
				sr.stub(fmt.Sprintf("git -C %s read-tree --reset -u aaa111", sandboxPath),
					exec.CmdResult{ExitCode: 1, Stderr: "read-tree failed"})
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
		require.NoError(t, e.emitCheckpointFailed(err.Error()))

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
		{
			name: "invalid schema version",
			setup: func(sr *stubRunner, sandboxPath, checkpointsDir string) {
				cpFile := &CheckpointsFile{
					SchemaVersion: "9.9",
					Checkpoints:   []Checkpoint{{ID: 1, SnapshotCommit: "aaa111"}},
				}
				cpData, _ := json.MarshalIndent(cpFile, "", "  ")
				_ = os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644)
			},
			cpID:     1,
			wantCode: errors.ECheckpointFailed,
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

func TestEngine_EventSeqMonotonicAcrossEngineRestart(t *testing.T) {
	t.Parallel()

	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true

	e1, _ := newTestEngine(t, sr, cfg)
	require.NoError(t, e1.emitCheckpointCreated(1, true, "head-1"))
	require.NoError(t, e1.emitCheckpointFailed("first-engine-failure"))

	events := readEvents(t, e1.eventsPath)
	require.Len(t, events, 2)
	assert.Equal(t, uint64(1), events[0].Seq)
	assert.Equal(t, uint64(2), events[1].Seq)

	clock2 := newTestClock(time.Date(2026, 1, 15, 12, 5, 0, 0, time.UTC))
	e2 := NewEngine(
		"test-inv-001",
		"test-repo",
		e1.sandboxPath,
		e1.repoRoot,
		e1.checkpointsDir,
		e1.eventsPath,
		cfg,
		sr,
		fs.NewRealFS(),
		clock2.Now,
	)

	require.NoError(t, e2.emitCheckpointCreated(2, true, "head-2"))

	events = readEvents(t, e1.eventsPath)
	require.Len(t, events, 3)
	assert.Equal(t, uint64(1), events[0].Seq)
	assert.Equal(t, uint64(2), events[1].Seq)
	assert.Equal(t, uint64(3), events[2].Seq, "sequence must continue across engine restart")
}

func TestApplier_Apply_EmitsMonotonicSeqAfterExistingEvents(t *testing.T) {
	t.Parallel()

	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	cpFile := &CheckpointsFile{
		SchemaVersion: SchemaVersion,
		Checkpoints: []Checkpoint{
			{ID: 3, SnapshotRef: "refs/agency/snapshots/inv/3", SnapshotCommit: "ccc333"},
		},
	}
	cpData, _ := json.MarshalIndent(cpFile, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(checkpointsDir, "checkpoints.json"), cpData, 0o644))

	seedEvents := []Event{
		NewEvent("test-inv", 7, EventKindCheckpointCreated, CheckpointCreatedData(1, true, "head-a"), time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)),
		NewEvent("test-inv", 8, EventKindCheckpointFailed, CheckpointFailedData("seed-failure"), time.Date(2026, 1, 15, 11, 1, 0, 0, time.UTC)),
	}
	f, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	for _, ev := range seedEvents {
		line, mErr := json.Marshal(ev)
		require.NoError(t, mErr)
		_, _ = f.Write(append(line, '\n'))
	}
	require.NoError(t, f.Close())

	sr := newStubRunner()
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	applier := NewApplier("test-inv", sandboxPath, checkpointsDir, eventsPath, sr, fs.NewRealFS(), clock.Now)

	sr.stub(fmt.Sprintf("git -C %s cat-file -t ccc333", sandboxPath), exec.CmdResult{Stdout: "commit\n"})
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath), exec.CmdResult{Stdout: "head-before-apply\n"})
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/restore-backups/test-inv/20260115T120000.000000000Z-cp3 head-before-apply", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s reset --hard", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s clean -fd", sandboxPath), exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s read-tree --reset -u ccc333", sandboxPath), exec.CmdResult{})

	_, err = applier.Apply(context.Background(), 3)
	require.NoError(t, err)

	events := readEvents(t, eventsPath)
	require.Len(t, events, 4)
	assert.Equal(t, uint64(7), events[0].Seq)
	assert.Equal(t, uint64(8), events[1].Seq)
	assert.Equal(t, uint64(9), events[2].Seq, "checkpoint_apply_started must continue the existing sequence")
	assert.Equal(t, EventKindCheckpointApplyStarted, events[2].Kind)
	assert.Equal(t, uint64(10), events[3].Seq, "checkpoint_applied must continue the existing sequence")
	assert.Equal(t, EventKindCheckpointApplied, events[3].Kind)
}

func TestEngine_createCheckpointInternal_EventAppendFailure(t *testing.T) {
	t.Parallel()

	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)
	stubFullCheckpointSequence(sr, e.sandboxPath, true)
	e.eventWriter = failingAppender{
		err: fmt.Errorf("append failed"),
	}

	err := e.createCheckpointInternal(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "checkpoint_created")
}

// ---------------------------------------------------------------------------
// Semantic trigger tests (RED phase — new checkpoint trigger system)
// ---------------------------------------------------------------------------

func TestIsMutatingTool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"Edit", true},
		{"Write", true},
		{"Bash", true},
		{"NotebookEdit", true},
		{"MultiEdit", true},
		{"Read", false},
		{"Glob", false},
		{"Grep", false},
		{"WebSearch", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsMutatingTool(tt.name))
		})
	}
}

func TestValidSchemaVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v    string
		want bool
	}{
		{"1.0", true},
		{"1.1", true},
		{"2.0", false},
		{"", false},
		{"0.9", false},
	}
	for _, tt := range tests {
		t.Run(tt.v, func(t *testing.T) {
			assert.Equal(t, tt.want, ValidSchemaVersion(tt.v))
		})
	}
}

func TestDefaultConfig_DriftInterval(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	assert.Equal(t, 60*time.Second, cfg.DriftInterval, "DefaultConfig().DriftInterval")
}

func TestEngine_CreateSemanticCheckpoint_SetsMetadata(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	trigger := &TriggerEvent{
		Kind:     TriggerToolEnd,
		ToolName: "Edit",
		Seq:      42,
	}

	err := e.CreateSemanticCheckpoint(context.Background(), trigger)
	require.NoError(t, err)

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)

	cp := cpFile.Checkpoints[0]
	assert.Equal(t, TriggerToolEnd, cp.Trigger, "checkpoint should record trigger type")
	assert.Equal(t, "Edit", cp.ToolName, "checkpoint should record tool name")
	assert.Equal(t, uint64(42), cp.StreamSeq, "checkpoint should record stream seq")
	assert.Contains(t, cp.Description, "Edit", "description should mention the tool")
}

func TestEngine_CreateSemanticCheckpoint_NotRateLimited(t *testing.T) {
	t.Parallel()

	// Semantic checkpoints should NOT be rate limited — each tool completion is distinct.
	clock := newTestClock(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))

	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	cfg.RateLimit = 10 * time.Second

	sandboxPath := t.TempDir()
	checkpointsDir := t.TempDir()
	eventsDir := t.TempDir()
	eventsPath := filepath.Join(eventsDir, "events.jsonl")

	gitDir := filepath.Join(sandboxPath, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "index"), []byte("fake"), 0o644))

	e := NewEngine(
		"test-inv-sem", "test-repo",
		sandboxPath, sandboxPath,
		checkpointsDir, eventsPath,
		cfg, sr, fs.NewRealFS(), clock.Now,
	)

	ctx := context.Background()

	// Create first semantic checkpoint at T=0
	stubForSemanticCheckpoint(sr, sandboxPath, 1)
	trigger1 := &TriggerEvent{Kind: TriggerToolEnd, ToolName: "Edit", Seq: 1}
	require.NoError(t, e.CreateSemanticCheckpoint(ctx, trigger1))

	// Advance only 2 seconds — within rate limit window for drift checkpoints.
	clock.Advance(2 * time.Second)

	// Create second semantic checkpoint — should succeed (not rate limited).
	stubForSemanticCheckpoint(sr, sandboxPath, 2)
	trigger2 := &TriggerEvent{Kind: TriggerToolEnd, ToolName: "Write", Seq: 2}
	require.NoError(t, e.CreateSemanticCheckpoint(ctx, trigger2))

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 2, "both semantic checkpoints should be created despite rate limit window")
	assert.Equal(t, "Edit", cpFile.Checkpoints[0].ToolName)
	assert.Equal(t, "Write", cpFile.Checkpoints[1].ToolName)
}

func TestEngine_CreateSemanticCheckpoint_SkipsIfTreeUnchanged(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	// First checkpoint
	stubFullCheckpointSequence(sr, e.sandboxPath, true)
	trigger1 := &TriggerEvent{Kind: TriggerToolEnd, ToolName: "Edit", Seq: 1}
	require.NoError(t, e.CreateSemanticCheckpoint(context.Background(), trigger1))

	// Second checkpoint with same tree SHA (should be skipped)
	stubFullCheckpointSequence(sr, e.sandboxPath, true)
	trigger2 := &TriggerEvent{Kind: TriggerToolEnd, ToolName: "Edit", Seq: 2}
	require.NoError(t, e.CreateSemanticCheckpoint(context.Background(), trigger2))

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	assert.Len(t, cpFile.Checkpoints, 1, "duplicate tree should be skipped")
}

func TestEngine_CreateSemanticCheckpoint_NilTriggerFallsBack(t *testing.T) {
	t.Parallel()
	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)

	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	// nil trigger should create a checkpoint without semantic metadata
	err := e.CreateSemanticCheckpoint(context.Background(), nil)
	require.NoError(t, err)

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)
	assert.Empty(t, cpFile.Checkpoints[0].Trigger, "nil trigger should leave trigger empty")
	assert.Empty(t, cpFile.Checkpoints[0].ToolName)
}

func TestEngine_RunWithTriggerChannel(t *testing.T) {
	// Verify the engine processes TriggerEvents from a channel.
	t.Parallel()

	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	cfg.PollInterval = 100 * time.Millisecond // fast poll for test
	e, _ := newTestEngine(t, sr, cfg)

	triggerCh := make(chan TriggerEvent, 10)
	e.SetTriggerChannel(triggerCh)

	// Stub for one checkpoint
	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx)
	}()

	// Send a semantic trigger
	triggerCh <- TriggerEvent{
		Kind:     TriggerToolEnd,
		ToolName: "Edit",
		Seq:      10,
	}

	// Allow engine to process; then stop
	time.Sleep(100 * time.Millisecond)
	cancel()

	err := <-done
	assert.ErrorIs(t, err, context.Canceled)

	// Verify a checkpoint was created with semantic metadata
	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(cpFile.Checkpoints), 1, "should have created at least one checkpoint from trigger")

	// Find the semantic checkpoint
	found := false
	for _, cp := range cpFile.Checkpoints {
		if cp.Trigger == TriggerToolEnd {
			found = true
			assert.Equal(t, "Edit", cp.ToolName)
			assert.Equal(t, uint64(10), cp.StreamSeq)
		}
	}
	assert.True(t, found, "should have found a semantic checkpoint")
}

func TestEngine_computeChangedPaths_TruncatesAndCountsUnique(t *testing.T) {
	t.Parallel()

	sr := newStubRunner()
	e, _ := newTestEngine(t, sr, DefaultConfig())

	base := "base123"
	commit := "commit456"
	sr.stub(
		fmt.Sprintf("git -C %s diff --name-status --find-renames %s..%s", e.sandboxPath, base, commit),
		exec.CmdResult{
			Stdout: strings.Join([]string{
				"A\tone.txt",
				"M\ttwo.txt",
				"R100\told-three.txt\tthree.txt",
				"M\ttwo.txt", // duplicate should be de-duped
				"D\tfour.txt",
				"M\tfive.txt",
			}, "\n") + "\n",
		},
	)

	paths, count, truncated := e.computeChangedPaths(context.Background(), base, commit, 3)
	assert.Equal(t, []string{"one.txt", "two.txt", "three.txt"}, paths)
	assert.Equal(t, 5, count)
	assert.True(t, truncated)
}

func TestEngine_CreateSemanticCheckpoint_PersistsChangedPathCountAndTruncation(t *testing.T) {
	t.Parallel()

	sr := newStubRunner()
	cfg := DefaultConfig()
	cfg.IncludeUntracked = true
	e, _ := newTestEngine(t, sr, cfg)
	stubFullCheckpointSequence(sr, e.sandboxPath, true)

	var changedPathLines strings.Builder
	for i := 1; i <= 25; i++ {
		_, err := fmt.Fprintf(&changedPathLines, "M\tfile-%02d.txt\n", i)
		require.NoError(t, err)
	}
	sr.stub(
		fmt.Sprintf("git -C %s diff --name-status --find-renames aabbccdd00112233..ffffffffffffffff", e.sandboxPath),
		exec.CmdResult{Stdout: changedPathLines.String()},
	)

	trigger := &TriggerEvent{Kind: TriggerToolEnd, ToolName: "Edit", Seq: 42}
	require.NoError(t, e.CreateSemanticCheckpoint(context.Background(), trigger))

	cpFile, err := e.loadCheckpoints()
	require.NoError(t, err)
	require.Len(t, cpFile.Checkpoints, 1)

	cp := cpFile.Checkpoints[0]
	assert.Equal(t, 25, cp.ChangedPathCount)
	assert.Len(t, cp.ChangedPaths, maxChangedPathsPreview)
	assert.True(t, cp.ChangedPathTruncated)
	assert.Equal(t, "file-01.txt", cp.ChangedPaths[0])
	assert.Equal(t, "file-20.txt", cp.ChangedPaths[maxChangedPathsPreview-1])
}

func TestCheckpoint_SchemaVersion_Writes1_1(t *testing.T) {
	t.Parallel()
	f := NewCheckpointsFile()
	assert.Equal(t, "1.1", f.SchemaVersion, "new checkpoints files should use schema 1.1")
}

func TestCheckpoint_SchemaVersion_RejectsUnknown(t *testing.T) {
	t.Parallel()

	unknown := `{"schema_version":"2.0","checkpoints":[]}`
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "checkpoints.json")
	require.NoError(t, os.WriteFile(cpPath, []byte(unknown), 0o644))

	_, err := LoadCheckpointsFile(fs.NewRealFS(), dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}

// stubForSemanticCheckpoint stubs all git commands for checkpoint #id with unique tree/commit SHAs.
func stubForSemanticCheckpoint(sr *stubRunner, sandboxPath string, id int) {
	commitSHA := fmt.Sprintf("semantic_commit_%d", id)
	treeSHA := fmt.Sprintf("semantic_tree_%04d", id)

	sr.stub(fmt.Sprintf("git -C %s status --porcelain", sandboxPath),
		exec.CmdResult{Stdout: " M main.go\n"})
	sr.stub(fmt.Sprintf("git -C %s ls-files -o --exclude-standard", sandboxPath),
		exec.CmdResult{Stdout: ""})
	sr.stub(fmt.Sprintf("git -C %s rev-parse HEAD", sandboxPath),
		exec.CmdResult{Stdout: "head_sem\n"})
	sr.stub(fmt.Sprintf("git -C %s rev-parse --git-dir", sandboxPath),
		exec.CmdResult{Stdout: ".git\n"})
	sr.stub(fmt.Sprintf("git -C %s add -A -- . :(exclude).agency :(exclude).git", sandboxPath),
		exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s write-tree", sandboxPath),
		exec.CmdResult{Stdout: treeSHA + "\n"})
	sr.stub(fmt.Sprintf("git -C %s commit-tree %s -p HEAD -m agency snapshot test-inv-sem %d", sandboxPath, treeSHA, id),
		exec.CmdResult{Stdout: commitSHA + "\n"})
	sr.stub(fmt.Sprintf("git -C %s update-ref refs/agency/snapshots/test-inv-sem/%d %s", sandboxPath, id, commitSHA),
		exec.CmdResult{})
	sr.stub(fmt.Sprintf("git -C %s diff --stat --stat-width=80 head_sem..%s", sandboxPath, commitSHA),
		exec.CmdResult{Stdout: " main.go | 1 +\n 1 file changed, 1 insertion(+)\n"})
}
