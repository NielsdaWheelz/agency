package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// stubNow returns a function that returns a fixed time for deterministic tests.
func stubNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// stubPIDAlive returns a function that returns a fixed value for pid checks.
func stubPIDAlive(alive bool) func(int) bool {
	return func(int) bool { return alive }
}

func TestRepoLock_WritesLockFile(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	unlock, err := l.Lock("test-repo-id", "push")
	if err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}
	defer unlock()

	// Verify lock file exists
	lockPath := filepath.Join(dataDir, "repos", "test-repo-id", ".lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("failed to parse lock file: %v", err)
	}

	if info.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", info.PID, os.Getpid())
	}
	if !info.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", info.CreatedAt, now)
	}
	if info.Cmd != "push" {
		t.Errorf("Cmd = %q, want %q", info.Cmd, "push")
	}

	// Verify permissions are 0600
	stat, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("failed to stat lock file: %v", err)
	}
	if stat.Mode().Perm() != 0600 {
		t.Errorf("lock file permissions = %o, want 0600", stat.Mode().Perm())
	}
}

func TestRepoLock_ErrLockedOnContention(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	// Acquire lock with locker A
	unlockA, err := l.Lock("test-repo", "cmd-a")
	if err != nil {
		t.Fatalf("Lock A failed: %v", err)
	}
	defer unlockA()

	// Attempt lock with locker B (same repo)
	_, err = l.Lock("test-repo", "cmd-b")
	if err == nil {
		t.Fatal("Lock B should have failed")
	}

	errLocked, ok := err.(*ErrLocked)
	if !ok {
		t.Fatalf("expected *ErrLocked, got %T", err)
	}
	if errLocked.RepoID != "test-repo" {
		t.Errorf("RepoID = %q, want %q", errLocked.RepoID, "test-repo")
	}
	if errLocked.Info == nil {
		t.Error("Info should not be nil")
	}
	if errLocked.Info != nil && errLocked.Info.Cmd != "cmd-a" {
		t.Errorf("Info.Cmd = %q, want %q", errLocked.Info.Cmd, "cmd-a")
	}
}

func TestRepoLock_StaleByDeadPIDSteals(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	// Write a lock file manually with a "dead" PID
	repoDir := filepath.Join(dataDir, "repos", "stale-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	lockPath := filepath.Join(repoDir, ".lock")
	info := LockInfo{
		PID:       999999, // unlikely to be alive
		CreatedAt: now,    // recent timestamp
		Cmd:       "old-cmd",
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(lockPath, data, 0600); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	// Locker uses IsPIDAlive stub returning false
	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(false), // PID is dead
	}

	// Lock acquisition should succeed
	unlock, err := l.Lock("stale-repo", "new-cmd")
	if err != nil {
		t.Fatalf("Lock() failed (should steal stale lock): %v", err)
	}
	defer unlock()

	// Verify new lock was written
	data, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	var newInfo LockInfo
	if err := json.Unmarshal(data, &newInfo); err != nil {
		t.Fatalf("failed to parse lock file: %v", err)
	}
	if newInfo.Cmd != "new-cmd" {
		t.Errorf("Cmd = %q, want %q", newInfo.Cmd, "new-cmd")
	}
}

func TestRepoLock_StaleByAgeSteals(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	staleAfter := 2 * time.Hour

	// Write a lock file with old timestamp but "alive" pid
	repoDir := filepath.Join(dataDir, "repos", "old-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	lockPath := filepath.Join(repoDir, ".lock")
	oldTime := now.Add(-(staleAfter + time.Second)) // 1 second past stale threshold
	info := LockInfo{
		PID:       12345,
		CreatedAt: oldTime,
		Cmd:       "old-cmd",
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(lockPath, data, 0600); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: staleAfter,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true), // PID is alive but lock is old
	}

	// Lock acquisition should succeed
	unlock, err := l.Lock("old-repo", "new-cmd")
	if err != nil {
		t.Fatalf("Lock() failed (should steal stale-by-age lock): %v", err)
	}
	defer unlock()
}

func TestRepoLock_UnreadableLockFile_MtimeFallback(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	staleAfter := 2 * time.Hour

	repoDir := filepath.Join(dataDir, "repos", "garbage-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	lockPath := filepath.Join(repoDir, ".lock")

	t.Run("recent garbage file is treated as locked", func(t *testing.T) {
		// Write garbage bytes
		if err := os.WriteFile(lockPath, []byte("garbage data"), 0600); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}
		// Touch with recent mtime
		recentTime := now.Add(-time.Minute)
		if err := os.Chtimes(lockPath, recentTime, recentTime); err != nil {
			t.Fatalf("failed to set mtime: %v", err)
		}

		l := RepoLock{
			DataDir:    dataDir,
			StaleAfter: staleAfter,
			Now:        stubNow(now),
			IsPIDAlive: stubPIDAlive(true),
		}

		_, err := l.Lock("garbage-repo", "cmd")
		if err == nil {
			t.Fatal("Lock() should have failed")
		}
		if _, ok := err.(*ErrLocked); !ok {
			t.Fatalf("expected *ErrLocked, got %T: %v", err, err)
		}
	})

	t.Run("old garbage file is treated as stale", func(t *testing.T) {
		// Write garbage bytes
		if err := os.WriteFile(lockPath, []byte("garbage data"), 0600); err != nil {
			t.Fatalf("failed to write lock file: %v", err)
		}
		// Touch with old mtime
		oldTime := now.Add(-(staleAfter + time.Second))
		if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set mtime: %v", err)
		}

		l := RepoLock{
			DataDir:    dataDir,
			StaleAfter: staleAfter,
			Now:        stubNow(now),
			IsPIDAlive: stubPIDAlive(true),
		}

		unlock, err := l.Lock("garbage-repo", "new-cmd")
		if err != nil {
			t.Fatalf("Lock() failed (should steal stale garbage lock): %v", err)
		}
		defer unlock()
	})
}

func TestRepoLock_UnlockIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	unlock, err := l.Lock("idempotent-repo", "cmd")
	if err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}

	// First unlock
	if err := unlock(); err != nil {
		t.Fatalf("first unlock failed: %v", err)
	}

	// Second unlock (should be idempotent)
	if err := unlock(); err != nil {
		t.Fatalf("second unlock failed: %v", err)
	}

	// Verify lock file is gone
	lockPath := filepath.Join(dataDir, "repos", "idempotent-repo", ".lock")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file should not exist after unlock")
	}
}

func TestRepoLock_ParentDirCreation(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	// Ensure repos/<repo_id>/ does not exist
	repoDir := filepath.Join(dataDir, "repos", "new-repo")
	if _, err := os.Stat(repoDir); !os.IsNotExist(err) {
		t.Fatalf("repo dir should not exist yet")
	}

	unlock, err := l.Lock("new-repo", "cmd")
	if err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}
	defer unlock()

	// Verify directory was created
	if _, err := os.Stat(repoDir); err != nil {
		t.Errorf("repo dir should exist after lock: %v", err)
	}
}

func TestRepoLock_ConcurrencySanity(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	// Acquire lock with locker A
	unlockA, err := l.Lock("concurrent-repo", "cmd-a")
	if err != nil {
		t.Fatalf("Lock A failed: %v", err)
	}
	defer unlockA()

	// In goroutine, attempt lock B and expect ErrLocked quickly
	var wg sync.WaitGroup
	wg.Add(1)

	errChan := make(chan error, 1)
	go func() {
		defer wg.Done()
		_, err := l.Lock("concurrent-repo", "cmd-b")
		errChan <- err
	}()

	// Wait with timeout
	select {
	case err := <-errChan:
		if err == nil {
			t.Error("Lock B should have failed")
		} else if _, ok := err.(*ErrLocked); !ok {
			t.Errorf("expected *ErrLocked, got %T: %v", err, err)
		}
	case <-time.After(time.Second):
		t.Error("Lock B should have returned quickly, not blocked")
	}

	wg.Wait()
}

func TestRepoLock_DifferentReposIndependent(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	// Lock repo A
	unlockA, err := l.Lock("repo-a", "cmd")
	if err != nil {
		t.Fatalf("Lock repo-a failed: %v", err)
	}
	defer unlockA()

	// Lock repo B should succeed (different repo)
	unlockB, err := l.Lock("repo-b", "cmd")
	if err != nil {
		t.Fatalf("Lock repo-b failed: %v", err)
	}
	defer unlockB()
}

func TestNewRepoLock_DefaultValues(t *testing.T) {
	l := NewRepoLock("/some/data/dir")

	if l.DataDir != "/some/data/dir" {
		t.Errorf("DataDir = %q, want %q", l.DataDir, "/some/data/dir")
	}
	if l.StaleAfter != 2*time.Hour {
		t.Errorf("StaleAfter = %v, want %v", l.StaleAfter, 2*time.Hour)
	}
	if l.Now == nil {
		t.Error("Now should not be nil")
	}
	if l.IsPIDAlive == nil {
		t.Error("IsPIDAlive should not be nil")
	}
}

func TestErrLocked_Error(t *testing.T) {
	t.Run("with info", func(t *testing.T) {
		err := &ErrLocked{
			RepoID: "test-repo",
			Info: &LockInfo{
				PID:       12345,
				CreatedAt: time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC),
				Cmd:       "push",
			},
			Path: "/data/repos/test-repo/.lock",
		}
		msg := err.Error()
		if msg == "" {
			t.Error("error message should not be empty")
		}
		// Should contain key information
		if !containsAll(msg, "test-repo", "12345", "/data/repos/test-repo/.lock") {
			t.Errorf("error message missing expected info: %s", msg)
		}
	})

	t.Run("without info", func(t *testing.T) {
		err := &ErrLocked{
			RepoID: "test-repo",
			Info:   nil,
			Path:   "/data/repos/test-repo/.lock",
		}
		msg := err.Error()
		if msg == "" {
			t.Error("error message should not be empty")
		}
		if !containsAll(msg, "test-repo", "/data/repos/test-repo/.lock") {
			t.Errorf("error message missing expected info: %s", msg)
		}
	})
}

// containsAll returns true if s contains all substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
