package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/require"
)

func TestOpenInvocationLogFiles_CreatesPrivatePermissions(t *testing.T) {
	t.Parallel()

	st := store.NewStore(fs.NewRealFS(), t.TempDir(), time.Now)
	srv := &Server{store: st}

	const repoID = "repo-1"
	const invocationID = "inv-1"
	_, err := st.EnsureInvocationDir(repoID, invocationID)
	require.NoError(t, err)

	files, err := srv.openInvocationLogFiles(repoID, invocationID)
	require.NoError(t, err)
	t.Cleanup(func() { files.Close() })

	for _, path := range []string{files.RawPath, files.StderrPath, files.StreamPath} {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		if got := info.Mode().Perm(); got != os.FileMode(0o600) {
			t.Fatalf("log file mode for %s = %v, want %v", path, got, os.FileMode(0o600))
		}
	}

	dirInfo, err := os.Stat(st.InvocationLogsDir(repoID, invocationID))
	require.NoError(t, err)
	if got := dirInfo.Mode().Perm(); got != os.FileMode(0o700) {
		t.Fatalf("log dir mode = %v, want %v", got, os.FileMode(0o700))
	}
}
