package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenInvocationLogFiles_CreatesPrivatePermissions(t *testing.T) {
	t.Parallel()

	st := store.NewStore(fs.NewRealFS(), t.TempDir(), time.Now)
	srv := &Server{Store: st}

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
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}
