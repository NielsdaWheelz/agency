package commands

import (
	"context"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// daemonNavSetup bundles the resolved dirs and a connected daemon client.
// It is the shared prelude for every CLI command that talks to the daemon.
type daemonNavSetup struct {
	dirs   paths.Dirs
	client *daemonclient.Client
}

func resolveCommandDirs(dataDirOverride, configDirOverride string) (paths.Dirs, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return paths.Dirs{}, errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(os.Getenv, homeDir)
	if dataDirOverride != "" {
		dirs.DataDir = dataDirOverride
	}
	if configDirOverride != "" {
		dirs.ConfigDir = configDirOverride
	}
	return dirs, nil
}

func ensureDaemonClient(ctx context.Context, fsys fs.FS, dataDirOverride string) (*daemonclient.Client, error) {
	dirs, err := resolveCommandDirs(dataDirOverride, "")
	if err != nil {
		return nil, err
	}
	return ensureDaemonClientFromDirs(ctx, fsys, dirs)
}

func ensureDaemonClientFromDirs(ctx context.Context, fsys fs.FS, dirs paths.Dirs) (*daemonclient.Client, error) {
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	client, err := daemonclient.EnsureDaemonRunning(ctx, st.DaemonSocketPath(), st.DaemonLogPath())
	if err != nil {
		return nil, err
	}
	if err := client.CheckAPIVersion(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func setupDaemonNav(ctx context.Context, fsys fs.FS, dataDirOverride string) (*daemonNavSetup, error) {
	dirs, err := resolveCommandDirs(dataDirOverride, "")
	if err != nil {
		return nil, err
	}
	client, err := ensureDaemonClientFromDirs(ctx, fsys, dirs)
	if err != nil {
		return nil, err
	}
	return &daemonNavSetup{dirs: dirs, client: client}, nil
}

// setupDaemonNavAndRepo bundles daemon nav setup with repo-context resolution,
// the universal two-step prelude for every CLI command that talks to the daemon
// about a specific repo. Pass "" for dataDirOverride to use the default.
func setupDaemonNavAndRepo(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd, dataDirOverride string, repoOpts ResolveRepoContextOpts) (*daemonNavSetup, *RepoContextResult, error) {
	ns, err := setupDaemonNav(ctx, fsys, dataDirOverride)
	if err != nil {
		return nil, nil, err
	}
	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, repoOpts)
	if err != nil {
		return nil, nil, err
	}
	return ns, repoCtx, nil
}
