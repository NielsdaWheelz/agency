package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/fs"
)

func TestWriteIntegrationWorktreeVerifyRecord(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	st := NewStore(fs.NewRealFS(), tmpDir, time.Now)

	repoID := "repo-1"
	worktreeID := "wt-1"
	_, err := st.EnsureIntegrationWorktreeDir(repoID, worktreeID)
	require.NoError(t, err)

	exitCode := 0
	record := VerifyRecord{
		SchemaVersion: VerifyRecordSchemaVersion,
		RepoID:        repoID,
		RunID:         worktreeID,
		ScriptPath:    "scripts/verify.sh",
		TimeoutMS:     int64((30 * time.Minute) / time.Millisecond),
		ExitCode:      &exitCode,
		OK:            true,
		LogPath:       "/tmp/verify.log",
		Summary:       "verify passed",
	}

	require.NoError(t, st.WriteIntegrationWorktreeVerifyRecord(repoID, worktreeID, record))

	recordPath := st.integrationWorktreeVerifyRecordPath(repoID, worktreeID)
	info, err := os.Stat(recordPath)
	require.NoError(t, err)
	if got := info.Mode().Perm(); got != os.FileMode(0o600) {
		t.Fatalf("verify record mode = %v, want %v", got, os.FileMode(0o600))
	}

	var got VerifyRecord
	data, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	if got.SchemaVersion != VerifyRecordSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", got.SchemaVersion, VerifyRecordSchemaVersion)
	}
	if got.RepoID != repoID {
		t.Fatalf("repo_id = %q, want %q", got.RepoID, repoID)
	}
	if got.RunID != worktreeID {
		t.Fatalf("run_id = %q, want %q", got.RunID, worktreeID)
	}
	if !got.OK {
		t.Fatalf("ok = false, want true")
	}
	require.NotNil(t, got.ExitCode)
	if *got.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", *got.ExitCode)
	}
}
