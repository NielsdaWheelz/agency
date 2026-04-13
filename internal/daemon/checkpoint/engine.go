package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/NielsdaWheelz/agency/internal/daemon/invocationevents"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Engine manages checkpoint creation for a single sandbox.
type Engine struct {
	// Configuration
	invocationID   string
	repoID         string
	sandboxPath    string
	repoRoot       string
	checkpointsDir string // Directory containing checkpoints.json (sandbox dir, not tree)
	eventsPath     string // Path to invocation events.jsonl
	config         Config

	// Dependencies
	runner      exec.CommandRunner
	fsys        fs.FS
	clock       func() time.Time
	eventWriter invocationevents.Appender

	// State
	mu             sync.Mutex
	lastCheckpoint time.Time // last drift/poll checkpoint time (semantic triggers ignore this)
	watcher        *fsnotify.Watcher
	watchedDirs    map[string]bool

	// gitIgnoredDirs is a pre-computed set of absolute directory paths that are
	// gitignored. These directories (and their subtrees) are skipped during
	// fsnotify watch setup to avoid FD exhaustion on large ignored trees like
	// node_modules.
	gitIgnoredDirs map[string]bool

	// Semantic trigger channel (optional). When set, the engine creates
	// checkpoints in response to TriggerEvents (tool completions, commits)
	// in addition to the fsnotify drift safety net.
	triggerCh <-chan TriggerEvent

	// Shutdown coordination
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
		invocationevents.NewWriter(clock),
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
	eventWriter invocationevents.Appender,
) *Engine {
	if eventWriter == nil {
		eventWriter = invocationevents.NewWriter(clock)
	}
	engine := &Engine{
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
	return engine
}

// ParseGitIgnoredDirs parses the output of `git ls-files --others --ignored
// --exclude-standard --directory` into a set of absolute directory paths.
// Lines with a trailing `/` are directories; lines without are files (skipped).
func ParseGitIgnoredDirs(sandboxPath, gitOutput string) map[string]bool {
	result := make(map[string]bool)
	for _, line := range strings.Split(gitOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// git ls-files --directory appends / to directory entries
		if !strings.HasSuffix(line, "/") {
			continue
		}
		dir := strings.TrimSuffix(line, "/")
		result[filepath.Join(sandboxPath, dir)] = true
	}
	return result
}

// ReadGitIgnoredDirs reads the .gitignore file in sandboxPath and returns
// a set of absolute directory paths that match existing gitignored directories.
// This is a fast in-process alternative to running `git ls-files` as a subprocess.
// It handles simple directory patterns (e.g., "node_modules/", "build/", ".venv/")
// from the root .gitignore only. Returns an empty map on any error.
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

		// We only care about directory patterns (trailing /) or bare names
		// that could be directories
		dirName := strings.TrimSuffix(line, "/")
		if dirName == "" || strings.Contains(dirName, "*") || strings.Contains(dirName, "?") {
			continue // skip glob patterns
		}

		// Check if this is actually a directory in the sandbox
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
// tool completion trigger. Unlike CreateCheckpoint, this is NOT rate-limited —
// each tool completion is semantically distinct and warrants its own checkpoint.
// If trigger is nil, it creates a plain checkpoint.
func (e *Engine) CreateSemanticCheckpoint(ctx context.Context, trigger *TriggerEvent) error {
	return e.createCheckpointWithMetadata(ctx, trigger)
}

// Run starts the checkpoint engine. It blocks until Stop is called.
func (e *Engine) Run(ctx context.Context) error {
	// Initialize fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	e.watcher = watcher
	defer func() { _ = watcher.Close() }()

	// Add initial watches for all directories in sandbox tree.
	_ = e.setupInitialWatches()

	// Start main loop
	hasTriggerCh := e.triggerCh != nil

	// When semantic triggers are active, use DriftInterval for fsnotify debounce
	// instead of the shorter DebounceInterval. This makes fsnotify a safety net
	// rather than the primary trigger mechanism.
	debounceInterval := e.config.DebounceInterval
	if hasTriggerCh && e.config.DriftInterval > 0 {
		debounceInterval = e.config.DriftInterval
	}

	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		select {
		case <-debounceTimer.C:
		default:
		}
	}
	debouncePending := false

	pollTicker := time.NewTicker(e.config.PollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, do final checkpoint
			e.doFinalCheckpoint(ctx)
			return ctx.Err()

		case <-e.done:
			// Stop called, do final checkpoint
			e.doFinalCheckpoint(context.Background())
			return nil

		case trigger, ok := <-e.triggerChanOrNil():
			if !ok {
				// Trigger channel closed; continue with fsnotify/polling only
				e.triggerCh = nil
				continue
			}
			// Semantic trigger — create checkpoint immediately (no rate limit)
			if err := e.CreateSemanticCheckpoint(ctx, &trigger); err != nil {
				_ = e.emitCheckpointFailed(err.Error())
			}

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// Ignore events from .git/ and .agency/ paths
			if e.shouldIgnorePath(event.Name) {
				continue
			}

			// Handle new directory creation - add watcher
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = e.addWatchRecursive(event.Name)
				}
			}

			// Reset debounce timer (drift safety net)
			if !debouncePending {
				debouncePending = true
				debounceTimer.Reset(debounceInterval)
			} else {
				// Extend debounce period
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(debounceInterval)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			_ = err

		case <-debounceTimer.C:
			debouncePending = false
			if hasTriggerCh {
				// With semantic triggers active, fsnotify fires as drift detection
				e.tryDriftCheckpoint(ctx)
			} else {
				e.tryCheckpoint(ctx)
			}

		case <-pollTicker.C:
			e.tryCheckpointIfDirty(ctx)
		}
	}
}

// Stop signals the engine to stop and perform a final checkpoint.
func (e *Engine) Stop() {
	e.doneOnce.Do(func() {
		close(e.done)
	})
}

// isSkippedDir returns true if the given directory path should be skipped
// during fsnotify watch setup. A directory is skipped if:
//   - Its base name is ".git" or ".agency" (always skipped)
//   - Its absolute path is in the pre-computed gitIgnoredDirs set
func (e *Engine) isSkippedDir(path string) bool {
	base := filepath.Base(path)
	if base == ".git" || base == ".agency" {
		return true
	}
	if len(e.gitIgnoredDirs) > 0 {
		return e.gitIgnoredDirs[path]
	}
	return false
}

// setupInitialWatches walks the sandbox tree and adds watches for all directories.
func (e *Engine) setupInitialWatches() error {
	return e.addWatchRecursive(e.sandboxPath)
}

// addWatchRecursive adds watches for a directory and all its subdirectories.
func (e *Engine) addWatchRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if e.isSkippedDir(path) {
				return filepath.SkipDir
			}

			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}

			e.mu.Lock()
			if !e.watchedDirs[path] {
				if err := e.watcher.Add(path); err == nil {
					e.watchedDirs[path] = true
				}
			}
			e.mu.Unlock()
		}
		return nil
	})
}

// shouldIgnorePath returns true if events from this path should be ignored.
func (e *Engine) shouldIgnorePath(path string) bool {
	rel, err := filepath.Rel(e.sandboxPath, path)
	if err != nil {
		return false
	}

	// Check each path component for always-skip dirs
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		if p == ".git" || p == ".agency" {
			return true
		}
	}

	// Walk ancestor paths against gitIgnoredDirs
	if len(e.gitIgnoredDirs) > 0 {
		current := e.sandboxPath
		for _, p := range parts {
			current = filepath.Join(current, p)
			if e.gitIgnoredDirs[current] {
				return true
			}
		}
	}

	return false
}

// triggerChanOrNil returns the trigger channel, or a nil channel if triggers
// are not configured. A nil channel in a select never fires.
func (e *Engine) triggerChanOrNil() <-chan TriggerEvent {
	return e.triggerCh
}

// tryDriftCheckpoint creates a drift checkpoint when fsnotify detects changes
// but no semantic trigger has fired recently. Uses the normal rate limit.
func (e *Engine) tryDriftCheckpoint(ctx context.Context) {
	e.mu.Lock()
	timeSinceLast := e.clock().Sub(e.lastCheckpoint)
	if timeSinceLast < e.config.RateLimit {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	trigger := &TriggerEvent{Kind: TriggerDrift}
	if err := e.createCheckpointWithMetadata(ctx, trigger); err != nil {
		_ = e.emitCheckpointFailed(err.Error())
	}
}

// tryCheckpoint attempts to create a checkpoint, respecting rate limiting.
func (e *Engine) tryCheckpoint(ctx context.Context) {
	e.mu.Lock()
	timeSinceLast := e.clock().Sub(e.lastCheckpoint)
	if timeSinceLast < e.config.RateLimit {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	if err := e.CreateCheckpoint(ctx); err != nil {
		_ = e.emitCheckpointFailed(err.Error())
	}
}

// tryCheckpointIfDirty checks if the sandbox is dirty and creates a checkpoint if so.
func (e *Engine) tryCheckpointIfDirty(ctx context.Context) {
	e.mu.Lock()
	timeSinceLast := e.clock().Sub(e.lastCheckpoint)
	if timeSinceLast < e.config.RateLimit {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	dirty, err := e.isDirty(ctx)
	if err != nil || !dirty {
		return
	}

	if err := e.CreateCheckpoint(ctx); err != nil {
		_ = e.emitCheckpointFailed(err.Error())
	}
}

// isDirty checks if the sandbox has uncommitted changes.
func (e *Engine) isDirty(ctx context.Context) (bool, error) {
	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"status", "--porcelain",
	}, exec.RunOpts{})
	if err != nil {
		return false, err
	}
	if result.ExitCode != 0 {
		return false, fmt.Errorf("git status failed: %s", result.Stderr)
	}

	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return false, nil
	}

	// If not including untracked, filter out ?? lines
	if !e.config.IncludeUntracked {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if !strings.HasPrefix(line, "?? ") {
				return true, nil
			}
		}
		return false, nil
	}

	return true, nil
}

// doFinalCheckpoint performs a final checkpoint on shutdown.
func (e *Engine) doFinalCheckpoint(ctx context.Context) {
	dirty, err := e.isDirty(ctx)
	if err != nil || !dirty {
		return
	}

	// Force create regardless of rate limit
	if err := e.createCheckpointInternal(ctx); err != nil {
		_ = e.emitCheckpointFailed(err.Error())
	}
}

// CreateCheckpoint creates a new checkpoint for the sandbox.
// This is the main public entry point.
func (e *Engine) CreateCheckpoint(ctx context.Context) error {
	e.mu.Lock()
	timeSinceLast := e.clock().Sub(e.lastCheckpoint)
	if timeSinceLast < e.config.RateLimit {
		e.mu.Unlock()
		return nil // Rate limited, not an error
	}
	e.mu.Unlock()

	return e.createCheckpointInternal(ctx)
}

// createCheckpointInternal is the actual checkpoint creation logic (no semantic metadata).
func (e *Engine) createCheckpointInternal(ctx context.Context) error {
	return e.createCheckpointWithMetadata(ctx, nil)
}

// createCheckpointWithMetadata is the core checkpoint creation logic.
// If trigger is non-nil, semantic metadata (Trigger, ToolName, StreamSeq, Description)
// is attached to the checkpoint.
func (e *Engine) createCheckpointWithMetadata(ctx context.Context, trigger *TriggerEvent) error {
	// 1. Check if sandbox is dirty
	dirty, err := e.isDirty(ctx)
	if err != nil {
		return fmt.Errorf("failed to check dirty state: %w", err)
	}
	if !dirty {
		return nil // Nothing to checkpoint
	}

	// 2. Check denylist for untracked files
	includeUntracked := e.config.IncludeUntracked
	var deniedFiles []string
	if includeUntracked {
		deniedFiles, err = e.checkDenylist(ctx)
		if err != nil {
			return fmt.Errorf("failed to check denylist: %w", err)
		}
		if len(deniedFiles) > 0 {
			// Degrade to tracked-only
			includeUntracked = false
			if err := e.emitDenylistTriggered(deniedFiles); err != nil {
				return fmt.Errorf("failed to append checkpoint_denylist_triggered event: %w", err)
			}
		}
	}

	// 3. Get current HEAD
	headResult, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"rev-parse", "HEAD",
	}, exec.RunOpts{})
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	if headResult.ExitCode != 0 {
		return fmt.Errorf("git rev-parse HEAD failed: %s", headResult.Stderr)
	}
	sandboxHeadSHA := strings.TrimSpace(headResult.Stdout)

	// 4. Create temp index
	tempIndex, err := os.CreateTemp("", "agency-checkpoint-index-*")
	if err != nil {
		return fmt.Errorf("failed to create temp index: %w", err)
	}
	tempIndexPath := tempIndex.Name()
	_ = tempIndex.Close()
	defer func() { _ = os.Remove(tempIndexPath) }()

	// Copy current index to temp
	gitDir, err := e.getGitDir(ctx)
	if err != nil {
		return fmt.Errorf("failed to get git dir: %w", err)
	}
	indexPath := filepath.Join(gitDir, "index")
	if data, err := os.ReadFile(indexPath); err == nil {
		if err := os.WriteFile(tempIndexPath, data, 0o600); err != nil {
			return fmt.Errorf("failed to copy index: %w", err)
		}
	}

	// 5. Stage changes into temp index.
	// Two-step approach: add everything, then remove private dirs from the
	// index. This avoids pathspec-vs-gitignore conflicts where git add -A
	// with :(exclude) still fails when the excluded path is also gitignored.
	env := map[string]string{"GIT_INDEX_FILE": tempIndexPath}

	var addArgs []string
	if includeUntracked {
		addArgs = []string{"-C", e.sandboxPath, "add", "-A"}
	} else {
		addArgs = []string{"-C", e.sandboxPath, "add", "-u"}
	}

	addResult, err := e.runner.Run(ctx, "git", addArgs, exec.RunOpts{Env: env})
	if err != nil {
		return fmt.Errorf("failed to run git add: %w", err)
	}
	if addResult.ExitCode != 0 {
		if strings.Contains(addResult.Stderr, "index.lock") {
			return fmt.Errorf("index lock detected: %s", addResult.Stderr)
		}
		return fmt.Errorf("git add failed: %s", addResult.Stderr)
	}

	// Remove private directories from the temp index. --ignore-unmatch
	// ensures this is a no-op when the paths weren't staged.
	for _, exclude := range []string{".agency", ".git"} {
		rmResult, rmErr := e.runner.Run(ctx, "git", []string{
			"-C", e.sandboxPath,
			"rm", "-r", "--cached", "--ignore-unmatch", exclude,
		}, exec.RunOpts{Env: env})
		if rmErr != nil {
			return fmt.Errorf("failed to run git rm --cached %s: %w", exclude, rmErr)
		}
		if rmResult.ExitCode != 0 {
			return fmt.Errorf("git rm --cached %s failed: %s", exclude, rmResult.Stderr)
		}
	}

	// 6. Write tree from temp index
	writeTreeResult, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"write-tree",
	}, exec.RunOpts{Env: env})
	if err != nil {
		return fmt.Errorf("failed to run git write-tree: %w", err)
	}
	if writeTreeResult.ExitCode != 0 {
		return fmt.Errorf("git write-tree failed: %s", writeTreeResult.Stderr)
	}
	treeHash := strings.TrimSpace(writeTreeResult.Stdout)

	// 7. Load checkpoints file and get next ID
	cpFile, err := e.loadCheckpoints()
	if err != nil {
		return fmt.Errorf("failed to load checkpoints: %w", err)
	}
	// Skip if tree is identical to last checkpoint (no actual content change).
	if n := len(cpFile.Checkpoints); n > 0 {
		if lastTree := cpFile.Checkpoints[n-1].TreeSHA; lastTree == treeHash && treeHash != "" {
			return nil
		}
	}

	checkpointID := cpFile.NextID()
	snapshotRef := fmt.Sprintf("%s%s/%d", RefPrefix, e.invocationID, checkpointID)

	// 8. Create commit
	commitMessage := fmt.Sprintf("agency snapshot %s %d", e.invocationID, checkpointID)
	commitTreeResult, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"commit-tree", treeHash,
		"-p", "HEAD",
		"-m", commitMessage,
	}, exec.RunOpts{})
	if err != nil {
		return fmt.Errorf("failed to run git commit-tree: %w", err)
	}
	if commitTreeResult.ExitCode != 0 {
		return fmt.Errorf("git commit-tree failed: %s", commitTreeResult.Stderr)
	}
	snapshotCommit := strings.TrimSpace(commitTreeResult.Stdout)

	// 9. Create ref (run from repo root to ensure ref is accessible)
	updateRefResult, err := e.runner.Run(ctx, "git", []string{
		"-C", e.repoRoot,
		"update-ref", snapshotRef, snapshotCommit,
	}, exec.RunOpts{})
	if err != nil {
		return fmt.Errorf("failed to run git update-ref: %w", err)
	}
	if updateRefResult.ExitCode != 0 {
		return fmt.Errorf("git update-ref failed: %s", updateRefResult.Stderr)
	}

	// 10. Compute human-readable delta metadata using a deterministic base.
	// First checkpoint is relative to sandbox HEAD; subsequent checkpoints are
	// relative to the previous snapshot to provide interval-accurate context.
	diffBaseCommit := sandboxHeadSHA
	if n := len(cpFile.Checkpoints); n > 0 {
		if prev := strings.TrimSpace(cpFile.Checkpoints[n-1].SnapshotCommit); prev != "" {
			diffBaseCommit = prev
		}
	}
	diffstat := e.computeDiffstat(ctx, diffBaseCommit, snapshotCommit)
	changedPaths, changedPathCount, changedPathTruncated := e.computeChangedPaths(
		ctx,
		diffBaseCommit,
		snapshotCommit,
		maxChangedPathsPreview,
	)

	// 11. Add checkpoint to file with optional semantic metadata
	now := e.clock()
	cp := Checkpoint{
		ID:                   checkpointID,
		SnapshotRef:          snapshotRef,
		SnapshotCommit:       snapshotCommit,
		SandboxHeadSHA:       sandboxHeadSHA,
		CreatedAt:            now.UTC().Format(time.RFC3339),
		IncludesUntracked:    includeUntracked,
		Diffstat:             diffstat,
		TreeSHA:              treeHash,
		ChangedPaths:         changedPaths,
		ChangedPathCount:     changedPathCount,
		ChangedPathTruncated: changedPathTruncated,
	}
	if trigger != nil {
		cp.Trigger = trigger.Kind
		cp.ToolName = trigger.ToolName
		cp.StreamSeq = trigger.Seq
		cp.Description = describeTrigger(trigger)
	}
	cpFile.Checkpoints = append(cpFile.Checkpoints, cp)

	// 12. Prune old checkpoints if needed
	if len(cpFile.Checkpoints) > MaxCheckpoints {
		e.pruneCheckpoints(ctx, cpFile)
	}

	// 13. Save checkpoints file
	if err := e.saveCheckpoints(cpFile); err != nil {
		return fmt.Errorf("failed to save checkpoints: %w", err)
	}

	// 14. Update state
	e.mu.Lock()
	e.lastCheckpoint = now
	e.mu.Unlock()

	// 15. Emit success event
	if err := e.emitCheckpointCreated(checkpointID, includeUntracked, sandboxHeadSHA); err != nil {
		return fmt.Errorf("failed to append checkpoint_created event: %w", err)
	}

	return nil
}

// getGitDir returns the .git directory path for the sandbox.
func (e *Engine) getGitDir(ctx context.Context) (string, error) {
	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"rev-parse", "--git-dir",
	}, exec.RunOpts{})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse --git-dir failed: %s", result.Stderr)
	}

	gitDir := strings.TrimSpace(result.Stdout)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(e.sandboxPath, gitDir)
	}
	return gitDir, nil
}

// checkDenylist returns a list of untracked files that match denylist patterns.
func (e *Engine) checkDenylist(ctx context.Context) ([]string, error) {
	// Get list of untracked non-ignored files
	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"ls-files", "-o", "--exclude-standard",
	}, exec.RunOpts{})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("git ls-files failed: %s", result.Stderr)
	}

	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return nil, nil
	}

	var denied []string
	for _, file := range strings.Split(output, "\n") {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		base := filepath.Base(file)
		if matchesDenylist(base) {
			denied = append(denied, file)
		}
	}

	return denied, nil
}

// matchesDenylist checks if a filename matches any denylist pattern.
func matchesDenylist(basename string) bool {
	for _, pattern := range DenylistPatterns {
		// Handle exact match
		if pattern == basename {
			return true
		}

		// Handle glob patterns
		matched, err := filepath.Match(pattern, basename)
		if err == nil && matched {
			return true
		}

		// Handle .env.* pattern (prefix match)
		if pattern == ".env.*" && strings.HasPrefix(basename, ".env.") {
			return true
		}
	}
	return false
}

// computeDiffstat returns a human-readable diffstat.
func (e *Engine) computeDiffstat(ctx context.Context, base, commit string) string {
	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"diff", "--stat", "--stat-width=80", base + ".." + commit,
	}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		return ""
	}

	// Parse the summary line (e.g., "3 files changed, 42 insertions(+), 15 deletions(-)")
	trimmed := strings.TrimSpace(result.Stdout)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")

	// Get last line which is the summary
	summary := lines[len(lines)-1]

	// Extract numbers using regex
	filesRe := regexp.MustCompile(`(\d+) files? changed`)
	insertionsRe := regexp.MustCompile(`(\d+) insertions?\(\+\)`)
	deletionsRe := regexp.MustCompile(`(\d+) deletions?\(-\)`)

	files := "0"
	insertions := "0"
	deletions := "0"

	if m := filesRe.FindStringSubmatch(summary); len(m) > 1 {
		files = m[1]
	}
	if m := insertionsRe.FindStringSubmatch(summary); len(m) > 1 {
		insertions = m[1]
	}
	if m := deletionsRe.FindStringSubmatch(summary); len(m) > 1 {
		deletions = m[1]
	}

	return fmt.Sprintf("+%s -%s in %s files", insertions, deletions, files)
}

func (e *Engine) computeChangedPaths(ctx context.Context, base, commit string, maxPaths int) ([]string, int, bool) {
	if strings.TrimSpace(base) == "" || strings.TrimSpace(commit) == "" || maxPaths <= 0 {
		return nil, 0, false
	}

	result, err := e.runner.Run(ctx, "git", []string{
		"-C", e.sandboxPath,
		"diff", "--name-status", "--find-renames", base + ".." + commit,
	}, exec.RunOpts{})
	if err != nil || result.ExitCode != 0 {
		return nil, 0, false
	}

	trimmed := strings.TrimSpace(result.Stdout)
	if trimmed == "" {
		return nil, 0, false
	}

	lines := strings.Split(trimmed, "\n")
	seen := make(map[string]struct{}, len(lines))
	paths := make([]string, 0, maxPaths)
	total := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := ""
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if len(fields) >= 3 {
				path = strings.TrimSpace(fields[2])
			}
		} else {
			path = strings.TrimSpace(fields[1])
		}
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		total++
		if len(paths) < maxPaths {
			paths = append(paths, path)
		}
	}

	if total == 0 {
		return nil, 0, false
	}
	return paths, total, total > len(paths)
}

// pruneCheckpoints removes the oldest checkpoints to stay under MaxCheckpoints.
func (e *Engine) pruneCheckpoints(ctx context.Context, cpFile *CheckpointsFile) {
	excess := len(cpFile.Checkpoints) - MaxCheckpoints
	if excess <= 0 {
		return
	}

	// Delete refs for oldest checkpoints
	for i := 0; i < excess; i++ {
		cp := cpFile.Checkpoints[i]
		// Delete ref
		_, _ = e.runner.Run(ctx, "git", []string{
			"-C", e.repoRoot,
			"update-ref", "-d", cp.SnapshotRef,
		}, exec.RunOpts{})
	}

	// Remove from file
	cpFile.Checkpoints = cpFile.Checkpoints[excess:]
}

// loadCheckpoints reads checkpoints.json or creates a new one.
func (e *Engine) loadCheckpoints() (*CheckpointsFile, error) {
	cpPath := filepath.Join(e.checkpointsDir, "checkpoints.json")
	data, err := e.fsys.ReadFile(cpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewCheckpointsFile(), nil
		}
		return nil, err
	}

	var cpFile CheckpointsFile
	if err := json.Unmarshal(data, &cpFile); err != nil {
		return nil, err
	}

	if !ValidSchemaVersion(cpFile.SchemaVersion) {
		return nil, fmt.Errorf("unknown checkpoints.json schema_version %q", cpFile.SchemaVersion)
	}

	return &cpFile, nil
}

// saveCheckpoints writes checkpoints.json atomically.
func (e *Engine) saveCheckpoints(cpFile *CheckpointsFile) error {
	cpPath := filepath.Join(e.checkpointsDir, "checkpoints.json")
	return fs.WriteJSONAtomic(cpPath, cpFile, 0o644)
}

// describeTrigger generates a human-readable description from a trigger event.
func describeTrigger(trigger *TriggerEvent) string {
	if trigger == nil {
		return ""
	}
	switch trigger.Kind {
	case TriggerToolEnd:
		if trigger.ToolName != "" {
			return "After " + trigger.ToolName
		}
		return "After tool completion"
	case TriggerDrift:
		return "Drift checkpoint"
	case TriggerShutdown:
		return "Final checkpoint"
	case TriggerPoll:
		return "Poll checkpoint"
	case TriggerManual:
		return "Manual checkpoint"
	default:
		return "Checkpoint"
	}
}

// emitCheckpointCreated emits a checkpoint_created event.
func (e *Engine) emitCheckpointCreated(checkpointID int, includesUntracked bool, sandboxHeadSHA string) error {
	_, err := e.eventWriter.Append(
		e.eventsPath,
		e.invocationID,
		string(EventKindCheckpointCreated),
		CheckpointCreatedData(checkpointID, includesUntracked, sandboxHeadSHA),
		invocationevents.AppendOptions{},
	)
	return err
}

// emitCheckpointFailed emits a checkpoint_failed event.
func (e *Engine) emitCheckpointFailed(reason string) error {
	_, err := e.eventWriter.Append(
		e.eventsPath,
		e.invocationID,
		string(EventKindCheckpointFailed),
		CheckpointFailedData(reason),
		invocationevents.AppendOptions{},
	)
	return err
}

// emitDenylistTriggered emits a checkpoint_denylist_triggered event.
func (e *Engine) emitDenylistTriggered(files []string) error {
	_, err := e.eventWriter.Append(
		e.eventsPath,
		e.invocationID,
		string(EventKindCheckpointDenylistTriggered),
		CheckpointDenylistTriggeredData(files),
		invocationevents.AppendOptions{},
	)
	return err
}
