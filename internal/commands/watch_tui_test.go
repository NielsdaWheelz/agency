package commands

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func TestWatch_NotInteractive_ReturnsENotInteractive(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Watch(context.Background(), exec.NewRealRunner(), fs.NewRealFS(), "", WatchOpts{
		IsInteractive: func() bool { return false },
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Equal(t, errors.ENotInteractive, errors.GetCode(err))

	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Details["hint"], "interactive terminal")
}

func TestWatchActionDelegates_UseCanonicalCommandOptions(t *testing.T) {
	t.Parallel()

	type enterCall struct {
		cwd  string
		opts AgentEnterOpts
	}
	type openCall struct {
		cwd  string
		opts AgentOpenOpts
	}
	type prSyncCall struct {
		cwd  string
		opts WorktreePRSyncOpts
	}

	var gotEnter enterCall
	var gotOpen openCall
	var gotPRSync prSyncCall

	delegates := newWatchActionDelegates(exec.NewRealRunner(), fs.NewRealFS(), "/tmp/repo", "/tmp/agency-data", io.Discard, io.Discard)
	delegates.enterFn = func(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentEnterOpts, stdout, stderr io.Writer) error {
		gotEnter = enterCall{cwd: cwd, opts: opts}
		return nil
	}
	delegates.openFn = func(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentOpenOpts, stdout, stderr io.Writer) error {
		gotOpen = openCall{cwd: cwd, opts: opts}
		return nil
	}
	delegates.prSyncFn = func(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePRSyncOpts, stdout, stderr io.Writer) error {
		gotPRSync = prSyncCall{cwd: cwd, opts: opts}
		return nil
	}

	require.NoError(t, delegates.Enter(context.Background(), "inv-1", "repo-1"))
	require.NoError(t, delegates.Open(context.Background(), "inv-2", "repo-2"))
	require.NoError(t, delegates.PRSync(context.Background(), "wt-3", "repo-3"))

	assert.Equal(t, "/tmp/repo", gotEnter.cwd)
	assert.Equal(t, AgentEnterOpts{
		InvocationRef:   "inv-1",
		RepoFlag:        "repo-1",
		DataDirOverride: "/tmp/agency-data",
	}, gotEnter.opts)

	assert.Equal(t, "/tmp/repo", gotOpen.cwd)
	assert.Equal(t, AgentOpenOpts{
		InvocationRef:   "inv-2",
		RepoFlag:        "repo-2",
		DataDirOverride: "/tmp/agency-data",
	}, gotOpen.opts)

	assert.Equal(t, "/tmp/repo", gotPRSync.cwd)
	assert.Equal(t, WorktreePRSyncOpts{
		WorktreeRef:     "wt-3",
		RepoFlag:        "repo-3",
		DataDirOverride: "/tmp/agency-data",
	}, gotPRSync.opts)
}
