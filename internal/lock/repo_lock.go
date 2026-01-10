// Package lock provides repo-level locking for agency mutating commands.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockInfo contains the metadata stored in a lock file.
type LockInfo struct {
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
	Cmd       string    `json:"cmd,omitempty"`
}

// ErrLocked indicates a non-stale lock is held by someone else.
type ErrLocked struct {
	RepoID string
	Info   *LockInfo // nil if lock file is unreadable
	Path   string
}

func (e *ErrLocked) Error() string {
	if e.Info != nil {
		return fmt.Sprintf("repo %s is locked by pid %d since %s (lock file: %s)",
			e.RepoID, e.Info.PID, e.Info.CreatedAt.Format(time.RFC3339), e.Path)
	}
	return fmt.Sprintf("repo %s is locked (lock file: %s)", e.RepoID, e.Path)
}

// RepoLock provides repo-level locking for mutating commands.
type RepoLock struct {
	DataDir    string
	StaleAfter time.Duration
	Now        func() time.Time
	IsPIDAlive func(pid int) bool
}

// NewRepoLock returns a RepoLock with v1 defaults:
// - StaleAfter: 2h
// - Now: time.Now
// - IsPIDAlive: platform impl (best-effort)
func NewRepoLock(dataDir string) RepoLock {
	return RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        time.Now,
		IsPIDAlive: isPIDAlive,
	}
}

// lockPath returns the path to the lock file for a repo.
func (l RepoLock) lockPath(repoID string) string {
	return filepath.Join(l.DataDir, "repos", repoID, ".lock")
}

// Lock acquires the repo lock and returns an unlock function.
// - cmd is stored in the lock file for debugging (may be empty).
// - if already locked and not stale: returns *ErrLocked.
func (l RepoLock) Lock(repoID string, cmd string) (unlock func() error, err error) {
	lockPath := l.lockPath(repoID)
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Ensure parent directory exists
		dir := filepath.Dir(lockPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create lock directory: %w", err)
		}

		// Try to create lock file with O_EXCL for atomic acquisition
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			// Successfully created lock file - write info and return
			info := LockInfo{
				PID:       os.Getpid(),
				CreatedAt: l.Now(),
				Cmd:       cmd,
			}
			data, _ := json.Marshal(info)
			if _, writeErr := f.Write(data); writeErr != nil {
				f.Close()
				os.Remove(lockPath)
				return nil, fmt.Errorf("failed to write lock file: %w", writeErr)
			}
			if closeErr := f.Close(); closeErr != nil {
				os.Remove(lockPath)
				return nil, fmt.Errorf("failed to close lock file: %w", closeErr)
			}

			// Return unlock function
			return func() error {
				err := os.Remove(lockPath)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			}, nil
		}

		// Lock file exists - check if it's stale
		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to create lock file: %w", err)
		}

		// Try to read existing lock info
		info, readErr := l.readLockInfo(lockPath)
		if readErr != nil {
			// Lock file exists but is unreadable - check mtime for staleness
			stat, statErr := os.Stat(lockPath)
			if statErr != nil {
				return nil, &ErrLocked{RepoID: repoID, Path: lockPath}
			}
			age := l.Now().Sub(stat.ModTime())
			if age <= l.StaleAfter {
				// Lock is not stale by age, treat as locked (conservative)
				return nil, &ErrLocked{RepoID: repoID, Path: lockPath}
			}
			// Stale by age - remove and retry
			if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return nil, &ErrLocked{RepoID: repoID, Path: lockPath}
			}
			continue
		}

		// Check if lock is stale
		if l.isStale(info) {
			// Remove stale lock and retry
			if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return nil, &ErrLocked{RepoID: repoID, Info: info, Path: lockPath}
			}
			continue
		}

		// Lock is held by an active process
		return nil, &ErrLocked{RepoID: repoID, Info: info, Path: lockPath}
	}

	// Exhausted retries - return locked error
	return nil, &ErrLocked{RepoID: repoID, Path: lockPath}
}

// readLockInfo reads and parses the lock file.
func (l RepoLock) readLockInfo(path string) (*LockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// isStale returns true if the lock should be considered stale.
func (l RepoLock) isStale(info *LockInfo) bool {
	// Stale if pid is not alive
	if !l.IsPIDAlive(info.PID) {
		return true
	}
	// Stale if created_at is older than stale_after
	if l.Now().Sub(info.CreatedAt) > l.StaleAfter {
		return true
	}
	return false
}

// isPIDAlive checks if a process with the given pid is alive.
// Uses the Unix signal 0 trick: sending signal 0 to a process succeeds
// if the process exists and we have permission to signal it.
func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 doesn't send anything but checks if process exists
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means process exists but we don't have permission - treat as alive
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	// ESRCH means no such process
	return false
}
