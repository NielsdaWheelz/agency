package mergeflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/stretchr/testify/require"
)

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
	if got := dirInfo.Mode().Perm(); got != os.FileMode(0o700) {
		t.Fatalf("log dir mode = %v, want %v", got, os.FileMode(0o700))
	}

	fileInfo, err := os.Stat(logPath)
	require.NoError(t, err)
	if got := fileInfo.Mode().Perm(); got != os.FileMode(0o600) {
		t.Fatalf("log file mode = %v, want %v", got, os.FileMode(0o600))
	}

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	contentText := string(content)
	for _, want := range []string{
		"gh pr merge 1 -R owner/repo --squash",
		"Exit code: 0",
		"=== stdout ===",
	} {
		if !strings.Contains(contentText, want) {
			t.Fatalf("merge log missing %q:\n%s", want, contentText)
		}
	}
}

func TestWriteMergeLog_IncludesExecutionError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "logs", "merge.log")

	err := WriteMergeLog(
		fs.NewRealFS(),
		logPath,
		"gh pr merge 2 -R owner/repo --merge",
		exec.CmdResult{ExitCode: -1, Stdout: "", Stderr: ""},
		errors.New("exec failed"),
	)
	require.NoError(t, err)

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	contentText := string(content)
	for _, want := range []string{"=== execution_error ===", "exec failed"} {
		if !strings.Contains(contentText, want) {
			t.Fatalf("merge log missing %q:\n%s", want, contentText)
		}
	}
}
