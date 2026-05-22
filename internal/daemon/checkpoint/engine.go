package checkpoint

import (
	"context"
	"fmt"
	"maps"
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
	env := maps.Clone(base)
	if env == nil {
		env = make(map[string]string, len(extra))
	}
	maps.Copy(env, extra)
	return env
}

// parseGitIgnoredDirs parses the output of `git ls-files --others --ignored
// --exclude-standard --directory` into a set of absolute directory paths.
func parseGitIgnoredDirs(sandboxPath, gitOutput string) map[string]bool {
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

	hasTriggerCh := e.triggerCh != nil
	_ = e.setupInitialWatches()
	if !hasTriggerCh {
		e.tryCheckpointIfDirty(ctx)
	}

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
