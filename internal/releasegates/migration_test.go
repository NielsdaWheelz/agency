package releasegates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNoLegacyS1GatesPackageReferencesRemain scans Go source for any import or
// reference to the legacy internal/s1gates package. This test must pass before
// PR-05 is considered complete.
func TestNoLegacyS1GatesPackageReferencesRemain(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)

	var violations []string

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "vendor" || base == "node_modules" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, _ := filepath.Rel(repoRoot, path)

		// Skip this test file itself and archival doc references
		if strings.Contains(relPath, "migration_test.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)

		if strings.Contains(content, "internal/s1gates") {
			violations = append(violations, relPath)
		}

		return nil
	})

	assert.NoError(t, err)
	assert.Empty(t, violations,
		"legacy internal/s1gates references found in Go source files: %v\n"+
			"All imports and code references must use internal/releasegates instead.", violations)
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}
