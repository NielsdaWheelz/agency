package testutil

import (
	"fmt"
	"os"
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
