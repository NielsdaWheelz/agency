package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/NielsdaWheelz/agency/internal/tmux"
	"github.com/NielsdaWheelz/agency/internal/watch"
)

func TestAgentHistory_InteractiveAttachRunsAfterWatchExit(t *testing.T) {
	env := setupAgentNavEnv(t, "history-attach", store.RunnerModeHeaded)
	sessionName := tmux.SessionName(env.InvocationID)
	env.Tmux.Sessions[sessionName] = testutil.FakeTmuxSession{Name: sessionName}

	shimDir := t.TempDir()
	recordFile := filepath.Join(shimDir, "record.txt")
	shimPath := filepath.Join(shimDir, "tmux")
	script := fmt.Sprintf("#!/bin/sh\npwd > '%s'\necho \"$@\" >> '%s'\n", recordFile, recordFile)
	require.NoError(t, os.WriteFile(shimPath, []byte(script), 0o755))

	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	cr := testutil.NewFakeCommandRunner()

	var stdout, stderr bytes.Buffer
	err := AgentHistory(context.Background(), cr, fs.NewRealFS(), "", AgentHistoryOpts{
		InvocationRef:   env.InvocationID,
		RepoRef:         env.RepoID,
		Limit:           50,
		DataDirOverride: env.DataDir,
		IsInteractive:   func() bool { return true },
		RunWatch: func(_ context.Context, _ *daemonclient.Client, _ watch.RunOptions) (watch.RunResult, error) {
			return watch.RunResult{
				AttachInvocationID: env.InvocationID,
				AttachRepoID:       env.RepoID,
			}, nil
		},
	}, &stdout, &stderr)
	require.NoError(t, err)

	_, args := readShimRecord(t, recordFile)
	assert.Equal(t, "attach-session -t "+sessionName, args)
}
