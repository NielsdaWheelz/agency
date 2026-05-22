package scaffold

import (
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

type stubScript struct {
	RelPath string // relative path from repo root (e.g., "scripts/agency_setup.sh")
	Content string
}

const setupStub = `#!/usr/bin/env bash
set -euo pipefail
# agency stub: replace with repo-specific setup steps (deps/env/etc)
exit 0
`

const verifyStub = `#!/usr/bin/env bash
set -euo pipefail
# agency stub: replace with repo-specific verification (tests/lint/etc)
echo "replace scripts/agency_verify.sh"
exit 1
`

const archiveStub = `#!/usr/bin/env bash
set -euo pipefail
# agency stub: replace with repo-specific archive steps (cleanup/etc)
exit 0
`

// CreateStubs creates stub scripts under repoRoot if they don't exist.
// Never overwrites existing scripts. Sets mode 0755 on created scripts.
func CreateStubs(fsys fs.FS, repoRoot string) ([]string, error) {
	var created []string
	stubs := []stubScript{
		{RelPath: "scripts/agency_setup.sh", Content: setupStub},
		{RelPath: "scripts/agency_verify.sh", Content: verifyStub},
		{RelPath: "scripts/agency_archive.sh", Content: archiveStub},
	}

	// Ensure scripts/ directory exists
	scriptsDir := filepath.Join(repoRoot, "scripts")
	if err := fsys.MkdirAll(scriptsDir, 0755); err != nil {
		return nil, err
	}

	for _, stub := range stubs {
		absPath := filepath.Join(repoRoot, stub.RelPath)

		// Check if file already exists
		_, err := fsys.Stat(absPath)
		if err == nil {
			continue
		}
		if !os.IsNotExist(err) {
			// Unexpected error
			return created, err
		}

		// File doesn't exist, create it
		if err := fsys.WriteFile(absPath, []byte(stub.Content), 0644); err != nil {
			return created, err
		}

		// Set executable bit
		if err := fsys.Chmod(absPath, 0755); err != nil {
			return created, err
		}

		created = append(created, stub.RelPath)
	}

	return created, nil
}
