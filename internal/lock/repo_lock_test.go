package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Parallel()
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	unlock, err := l.Lock("test-repo-id", "push")
	require.NoError(t, err, "Lock() failed")
	t.Cleanup(func() {
		assert.NoError(t, unlock(), "unlock failed")
	})

	// Verify lock file exists
	lockPath := filepath.Join(dataDir, "repos", "test-repo-id", ".lock")
	data, err := os.ReadFile(lockPath)
	require.NoError(t, err, "failed to read lock file")

	var info LockInfo
	err = json.Unmarshal(data, &info)
	require.NoError(t, err, "failed to parse lock file")

	assert.Equal(t, os.Getpid(), info.PID)
	assert.True(t, info.CreatedAt.Equal(now))
	assert.Equal(t, "push", info.Cmd)

	// Verify permissions are 0600
	stat, err := os.Stat(lockPath)
	require.NoError(t, err, "failed to stat lock file")
	assert.Equal(t, os.FileMode(0600), stat.Mode().Perm())
}

func TestRepoLock_ErrLockedOnContention(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "Lock A failed")
	t.Cleanup(func() {
		assert.NoError(t, unlockA(), "unlockA failed")
	})

	// Attempt lock with locker B (same repo)
	_, err = l.Lock("test-repo", "cmd-b")
	require.Error(t, err, "Lock B should have failed")

	errLocked, ok := err.(*ErrLocked)
	require.True(t, ok, "expected *ErrLocked, got %T", err)
	assert.Equal(t, "test-repo", errLocked.RepoID)
	require.NotNil(t, errLocked.Info)
	assert.Equal(t, "cmd-a", errLocked.Info.Cmd)
}

func TestRepoLock_StaleByDeadPIDSteals(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	// Write a lock file manually with a "dead" PID
	repoDir := filepath.Join(dataDir, "repos", "stale-repo")
	err := os.MkdirAll(repoDir, 0755)
	require.NoError(t, err, "failed to create repo dir")
	lockPath := filepath.Join(repoDir, ".lock")
	info := LockInfo{
		PID:       999999, // unlikely to be alive
		CreatedAt: now,    // recent timestamp
		Cmd:       "old-cmd",
	}
	data, _ := json.Marshal(info)
	err = os.WriteFile(lockPath, data, 0600)
	require.NoError(t, err, "failed to write lock file")

	// Locker uses IsPIDAlive stub returning false
	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(false), // PID is dead
	}

	// Lock acquisition should succeed
	unlock, err := l.Lock("stale-repo", "new-cmd")
	require.NoError(t, err, "Lock() failed (should steal stale lock)")
	t.Cleanup(func() {
		assert.NoError(t, unlock(), "unlock failed")
	})

	// Verify new lock was written
	data, err = os.ReadFile(lockPath)
	require.NoError(t, err, "failed to read lock file")
	var newInfo LockInfo
	err = json.Unmarshal(data, &newInfo)
	require.NoError(t, err, "failed to parse lock file")
	assert.Equal(t, "new-cmd", newInfo.Cmd)
}

func TestRepoLock_PIDOnlyStaleness_AgeDoesNotSteal(t *testing.T) {
	t.Parallel()
	// Per S5 spec: stale detection is pid-only. Age alone never steals the lock.
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	staleAfter := 2 * time.Hour

	// Write a lock file with old timestamp but "alive" pid
	repoDir := filepath.Join(dataDir, "repos", "old-repo")
	err := os.MkdirAll(repoDir, 0755)
	require.NoError(t, err, "failed to create repo dir")
	lockPath := filepath.Join(repoDir, ".lock")
	oldTime := now.Add(-(staleAfter + time.Second)) // 1 second past stale threshold
	info := LockInfo{
		PID:       12345,
		CreatedAt: oldTime,
		Cmd:       "old-cmd",
	}
	data, _ := json.Marshal(info)
	err = os.WriteFile(lockPath, data, 0600)
	require.NoError(t, err, "failed to write lock file")

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: staleAfter,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true), // PID is alive, so lock should NOT be stolen
	}

	// Lock acquisition should FAIL because pid-only staleness means age doesn't matter
	_, err = l.Lock("old-repo", "new-cmd")
	require.Error(t, err, "Lock() should have failed (pid is alive, age alone should not steal lock)")

	errLocked, ok := err.(*ErrLocked)
	require.True(t, ok, "expected *ErrLocked, got %T: %v", err, err)
	require.NotNil(t, errLocked.Info)
	assert.Equal(t, "old-cmd", errLocked.Info.Cmd)
}

func TestRepoLock_UnreadableLockFile_MtimeFallback(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)
	staleAfter := 2 * time.Hour

	repoDir := filepath.Join(dataDir, "repos", "garbage-repo")
	err := os.MkdirAll(repoDir, 0755)
	require.NoError(t, err, "failed to create repo dir")
	lockPath := filepath.Join(repoDir, ".lock")

	t.Run("recent garbage file is treated as locked", func(t *testing.T) {
		// Write garbage bytes
		err := os.WriteFile(lockPath, []byte("garbage data"), 0600)
		require.NoError(t, err, "failed to write lock file")
		// Touch with recent mtime
		recentTime := now.Add(-time.Minute)
		err = os.Chtimes(lockPath, recentTime, recentTime)
		require.NoError(t, err, "failed to set mtime")

		l := RepoLock{
			DataDir:    dataDir,
			StaleAfter: staleAfter,
			Now:        stubNow(now),
			IsPIDAlive: stubPIDAlive(true),
		}

		_, err = l.Lock("garbage-repo", "cmd")
		require.Error(t, err, "Lock() should have failed")
		_, ok := err.(*ErrLocked)
		require.True(t, ok, "expected *ErrLocked, got %T: %v", err, err)
	})

	t.Run("old garbage file is treated as stale", func(t *testing.T) {
		// Write garbage bytes
		err := os.WriteFile(lockPath, []byte("garbage data"), 0600)
		require.NoError(t, err, "failed to write lock file")
		// Touch with old mtime
		oldTime := now.Add(-(staleAfter + time.Second))
		err = os.Chtimes(lockPath, oldTime, oldTime)
		require.NoError(t, err, "failed to set mtime")

		l := RepoLock{
			DataDir:    dataDir,
			StaleAfter: staleAfter,
			Now:        stubNow(now),
			IsPIDAlive: stubPIDAlive(true),
		}

		unlock, err := l.Lock("garbage-repo", "new-cmd")
		require.NoError(t, err, "Lock() failed (should steal stale garbage lock)")
		t.Cleanup(func() {
			assert.NoError(t, unlock(), "unlock failed")
		})
	})
}

func TestRepoLock_UnlockIdempotent(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	now := time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

	l := RepoLock{
		DataDir:    dataDir,
		StaleAfter: 2 * time.Hour,
		Now:        stubNow(now),
		IsPIDAlive: stubPIDAlive(true),
	}

	unlock, err := l.Lock("idempotent-repo", "cmd")
	require.NoError(t, err, "Lock() failed")

	// First unlock
	err = unlock()
	require.NoError(t, err, "first unlock failed")

	// Second unlock (should be idempotent)
	err = unlock()
	require.NoError(t, err, "second unlock failed")

	// Verify lock file is gone
	lockPath := filepath.Join(dataDir, "repos", "idempotent-repo", ".lock")
	_, err = os.Stat(lockPath)
	assert.True(t, os.IsNotExist(err), "lock file should not exist after unlock")
}

func TestRepoLock_ParentDirCreation(t *testing.T) {
	t.Parallel()
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
	_, err := os.Stat(repoDir)
	require.True(t, os.IsNotExist(err), "repo dir should not exist yet")

	unlock, err := l.Lock("new-repo", "cmd")
	require.NoError(t, err, "Lock() failed")
	t.Cleanup(func() {
		assert.NoError(t, unlock(), "unlock failed")
	})

	// Verify directory was created
	_, err = os.Stat(repoDir)
	assert.NoError(t, err, "repo dir should exist after lock")
}

func TestRepoLock_ConcurrencySanity(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "Lock A failed")
	t.Cleanup(func() {
		assert.NoError(t, unlockA(), "unlockA failed")
	})

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
		require.Error(t, err, "Lock B should have failed")
		_, ok := err.(*ErrLocked)
		assert.True(t, ok, "expected *ErrLocked, got %T: %v", err, err)
	case <-time.After(time.Second):
		assert.Fail(t, "Lock B should have returned quickly, not blocked")
	}

	wg.Wait()
}

func TestRepoLock_DifferentReposIndependent(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "Lock repo-a failed")
	t.Cleanup(func() {
		assert.NoError(t, unlockA(), "unlockA failed")
	})

	// Lock repo B should succeed (different repo)
	unlockB, err := l.Lock("repo-b", "cmd")
	require.NoError(t, err, "Lock repo-b failed")
	t.Cleanup(func() {
		assert.NoError(t, unlockB(), "unlockB failed")
	})
}

func TestNewRepoLock_DefaultValues(t *testing.T) {
	t.Parallel()
	l := NewRepoLock("/some/data/dir")

	assert.Equal(t, "/some/data/dir", l.DataDir)
	assert.Equal(t, 2*time.Hour, l.StaleAfter)
	assert.NotNil(t, l.Now)
	assert.NotNil(t, l.IsPIDAlive)
}

func TestErrLocked_Error(t *testing.T) {
	t.Parallel()
	t.Run("with info", func(t *testing.T) {
		t.Parallel()
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
		assert.NotEmpty(t, msg)
		// Should contain key information
		assert.True(t, containsAll(msg, "test-repo", "12345", "/data/repos/test-repo/.lock"),
			"error message missing expected info: %s", msg)
	})

	t.Run("without info", func(t *testing.T) {
		t.Parallel()
		err := &ErrLocked{
			RepoID: "test-repo",
			Info:   nil,
			Path:   "/data/repos/test-repo/.lock",
		}
		msg := err.Error()
		assert.NotEmpty(t, msg)
		assert.True(t, containsAll(msg, "test-repo", "/data/repos/test-repo/.lock"),
			"error message missing expected info: %s", msg)
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
