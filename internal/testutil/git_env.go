package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// UnsetGitEnv clears git environment variables that can override repo paths.
func UnsetGitEnv() error {
	envVars := []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_COMMON_DIR",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	}
	for _, name := range envVars {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("unset %s: %w", name, err)
		}
	}
	return nil
}

// HermeticGitEnv configures environment variables so that git commands
// run during the test are fully isolated from the host's git configuration.
// It blocks system and global config, and provides a deterministic committer
// identity so tests never depend on the developer's ~/.gitconfig.
// All environment variables are automatically restored when the test finishes.
func HermeticGitEnv(t *testing.T) {
	t.Helper()

	// Block system-level (/etc/gitconfig) and global (~/.gitconfig) config.
	// Use an empty temp file instead of /dev/null so that tools like
	// `gh auth setup-git` that write to global config don't fail.
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))

	// Provide a deterministic committer/author identity.
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@test.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@test.com")
}
