package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/NielsdaWheelz/agency/internal/daemon/eventlog"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Engine manages checkpoint creation for a single sandbox.
type Engine struct {
	invocationID   string
	repoID         string
	sandboxPath    string
	repoRoot       string
	checkpointsDir string
	eventsPath     string
	config         Config

	runner      exec.CommandRunner
	fsys        fs.FS
	clock       func() time.Time
	eventWriter eventlog.Appender

	mu             sync.Mutex
	lastCheckpoint time.Time
	watcher        *fsnotify.Watcher
	watchedDirs    map[string]bool
	gitIgnoredDirs map[string]bool
	triggerCh      <-chan TriggerEvent

	done     chan struct{}
	doneOnce sync.Once
}

const maxChangedPathsPreview = 20

// NewEngine creates a new checkpoint engine for the given sandbox.
func NewEngine(
	invocationID, repoID, sandboxPath, repoRoot, checkpointsDir, eventsPath string,
	config Config,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
) *Engine {
	return NewEngineWithWriter(
		invocationID,
		repoID,
		sandboxPath,
		repoRoot,
		checkpointsDir,
		eventsPath,
		config,
		runner,
		fsys,
		clock,
		eventlog.NewWriter("invocation_id", clock),
	)
}

// NewEngineWithWriter creates a checkpoint engine using a shared invocation
// event writer.
func NewEngineWithWriter(
	invocationID, repoID, sandboxPath, repoRoot, checkpointsDir, eventsPath string,
	config Config,
	runner exec.CommandRunner,
	fsys fs.FS,
	clock func() time.Time,
	eventWriter eventlog.Appender,
) *Engine {
	if eventWriter == nil {
		eventWriter = eventlog.NewWriter("invocation_id", clock)
	}
	config.Env = gitEnv(config.Env, nil)
	return &Engine{
		invocationID:   invocationID,
		repoID:         repoID,
		sandboxPath:    sandboxPath,
		repoRoot:       repoRoot,
		checkpointsDir: checkpointsDir,
		eventsPath:     eventsPath,
		config:         config,
		runner:         runner,
		fsys:           fsys,
		clock:          clock,
		eventWriter:    eventWriter,
		watchedDirs:    make(map[string]bool),
		done:           make(chan struct{}),
	}
}

func gitEnv(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	env := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// ParseGitIgnoredDirs parses the output of `git ls-files --others --ignored
// --exclude-standard --directory` into a set of absolute directory paths.
func ParseGitIgnoredDirs(sandboxPath, gitOutput string) map[string]bool {
	result := make(map[string]bool)
	for _, line := range strings.Split(gitOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, "/") {
			continue
		}
		dir := strings.TrimSuffix(line, "/")
		result[filepath.Join(sandboxPath, dir)] = true
	}
	return result
}

// ReadGitIgnoredDirs reads the root .gitignore and returns existing gitignored
// directories that should be skipped during watch setup.
func ReadGitIgnoredDirs(sandboxPath string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(sandboxPath, ".gitignore"))
	if err != nil {
		return map[string]bool{}
	}

	result := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		dirName := strings.TrimSuffix(line, "/")
		if dirName == "" || strings.Contains(dirName, "*") || strings.Contains(dirName, "?") {
			continue
		}

		absPath := filepath.Join(sandboxPath, dirName)
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			continue
		}
		result[absPath] = true
	}
	return result
}

// SetTriggerChannel sets the semantic trigger channel for tool-completion-based
// checkpoints. Must be called before Run.
func (e *Engine) SetTriggerChannel(ch <-chan TriggerEvent) {
	e.triggerCh = ch
}

// SetGitIgnoredDirs sets the pre-computed gitignored directory set.
// Must be called before Run.
func (e *Engine) SetGitIgnoredDirs(dirs map[string]bool) {
	if len(dirs) > 0 {
		e.gitIgnoredDirs = dirs
	}
}

// CreateSemanticCheckpoint creates a checkpoint with semantic metadata from a
// tool completion trigger. Unlike CreateCheckpoint, this is not rate-limited.
func (e *Engine) CreateSemanticCheckpoint(ctx context.Context, trigger *TriggerEvent) error {
	return e.createCheckpointWithMetadata(ctx, trigger)
}

// Run starts the checkpoint engine. It blocks until Stop is called.
func (e *Engine) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	e.watcher = watcher
	defer func() { _ = watcher.Close() }()

	_ = e.setupInitialWatches()

	hasTriggerCh := e.triggerCh != nil
	debounceInterval := e.config.DebounceInterval
	if hasTriggerCh && e.config.DriftInterval > 0 {
		debounceInterval = e.config.DriftInterval
	}
	return e.runWithWatcher(ctx, watcher, debounceInterval, hasTriggerCh)
}

// Stop signals the engine to stop and perform a final checkpoint.
func (e *Engine) Stop() {
	e.doneOnce.Do(func() {
		close(e.done)
	})
}
