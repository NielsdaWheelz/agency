package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
)

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

func (e *Engine) setupInitialWatches() error {
	return e.addWatchRecursive(e.sandboxPath)
}

func (e *Engine) addWatchRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
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
		return nil
	})
}

func (e *Engine) shouldIgnorePath(path string) bool {
	rel, err := filepath.Rel(e.sandboxPath, path)
	if err != nil {
		return false
	}

	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		if p == ".git" || p == ".agency" {
			return true
		}
	}

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

func (e *Engine) triggerChanOrNil() <-chan TriggerEvent {
	return e.triggerCh
}

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

func (e *Engine) doFinalCheckpoint(ctx context.Context) {
	dirty, err := e.isDirty(ctx)
	if err != nil || !dirty {
		return
	}
	if err := e.createCheckpointInternal(ctx); err != nil {
		_ = e.emitCheckpointFailed(err.Error())
	}
}

func (e *Engine) CreateCheckpoint(ctx context.Context) error {
	e.mu.Lock()
	timeSinceLast := e.clock().Sub(e.lastCheckpoint)
	if timeSinceLast < e.config.RateLimit {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	return e.createCheckpointInternal(ctx)
}

func (e *Engine) createCheckpointInternal(ctx context.Context) error {
	return e.createCheckpointWithMetadata(ctx, nil)
}

func (e *Engine) createCheckpointWithMetadata(ctx context.Context, trigger *TriggerEvent) error {
	return e.createCheckpointFlow(ctx, trigger)
}
