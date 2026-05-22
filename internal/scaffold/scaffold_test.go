package scaffold

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgencyJSONTemplateIsValidDefaultConfig(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	configDir := t.TempDir()
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	require.NoError(t, os.WriteFile(agencyJSONPath, []byte(AgencyJSONTemplate), 0o644))

	resolved, err := config.ResolveAgencyConfig(fs.NewRealFS(), repoRoot, configDir, "repo-1", "")
	require.NoError(t, err)

	assert.Equal(t, "repo", resolved.Source)
	assert.Equal(t, agencyJSONPath, resolved.Path)
	assert.Equal(t, config.AgencyConfigVersion, resolved.Config.Version)
	assert.Equal(t, config.CheckoutRootSibling, resolved.Config.Execution.CheckoutRoot)
	assert.Equal(t, filepath.Join(repoRoot, "scripts/agency_setup.sh"), resolved.Config.Scripts.Setup.Path)
	assert.Equal(t, filepath.Join(repoRoot, "scripts/agency_verify.sh"), resolved.Config.Scripts.Verify.Path)
	assert.Equal(t, filepath.Join(repoRoot, "scripts/agency_archive.sh"), resolved.Config.Scripts.Archive.Path)
	assert.Equal(t, config.DefaultSetupTimeout, resolved.Config.Scripts.Setup.Timeout)
	assert.Equal(t, config.DefaultVerifyTimeout, resolved.Config.Scripts.Verify.Timeout)
	assert.Equal(t, config.DefaultArchiveTimeout, resolved.Config.Scripts.Archive.Timeout)
}

func TestCreateStubsCreatesExecutableDefaults(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	created, err := CreateStubs(fs.NewRealFS(), repoRoot)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{
		"scripts/agency_setup.sh",
		"scripts/agency_verify.sh",
		"scripts/agency_archive.sh",
	}, created)

	for _, relPath := range created {
		path := filepath.Join(repoRoot, relPath)
		gotContent, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(gotContent), "set -euo pipefail")

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&0o100, "%s should be executable by owner", relPath)
	}
}

func TestCreateStubsLeavesExistingScripts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	custom := map[string]string{
		"scripts/agency_setup.sh":   "#!/usr/bin/env bash\necho custom setup\n",
		"scripts/agency_verify.sh":  "#!/usr/bin/env bash\necho custom verify\n",
		"scripts/agency_archive.sh": "#!/usr/bin/env bash\necho custom archive\n",
	}
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "scripts"), 0o755))
	for relPath, content := range custom {
		require.NoError(t, os.WriteFile(filepath.Join(repoRoot, relPath), []byte(content), 0o755))
	}

	created, err := CreateStubs(fs.NewRealFS(), repoRoot)
	require.NoError(t, err)

	assert.Empty(t, created)

	for relPath, wantContent := range custom {
		gotContent, err := os.ReadFile(filepath.Join(repoRoot, relPath))
		require.NoError(t, err)
		assert.Equal(t, wantContent, string(gotContent))
	}
}
