package mergeflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeEnvDeterministic_OverlayWinsAndSorted(t *testing.T) {
	t.Parallel()
	base := []string{"B=base", "A=base", "INVALID", "C=base"}
	overlay1 := map[string]string{"B": "overlay1", "D": "overlay1"}
	overlay2 := map[string]string{"B": "overlay2", "A": "overlay2"}

	got := MergeEnvDeterministic(base, overlay1, overlay2)
	assert.Equal(t, []string{
		"A=overlay2",
		"B=overlay2",
		"C=base",
		"D=overlay1",
	}, got)
}

func TestWriteMergeLog_WritesPrivateFileAndDir(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "logs", "merge.log")

	err := WriteMergeLog(
		fs.NewRealFS(),
		logPath,
		"gh pr merge 1 -R owner/repo --squash",
		exec.CmdResult{ExitCode: 0, Stdout: "merged\n", Stderr: ""},
		nil,
	)
	require.NoError(t, err)

	dirInfo, err := os.Stat(filepath.Dir(logPath))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	fileInfo, err := os.Stat(logPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "gh pr merge 1 -R owner/repo --squash")
	assert.Contains(t, string(content), "Exit code: 0")
	assert.Contains(t, string(content), "=== stdout ===")
}

func TestWriteMergeLog_IncludesExecutionError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "logs", "merge.log")

	err := WriteMergeLog(
		fs.NewRealFS(),
		logPath,
		"gh pr merge 2 -R owner/repo --merge",
		exec.CmdResult{ExitCode: exec.ExitStartFail, Stdout: "", Stderr: ""},
		errors.New("exec failed"),
	)
	require.NoError(t, err)

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "=== execution_error ===")
	assert.Contains(t, string(content), "exec failed")
}
