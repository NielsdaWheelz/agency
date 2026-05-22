package daemon

import (
	"context"

	"github.com/NielsdaWheelz/agency/internal/daemon/checkpoint"
)

func (s *Server) configureCheckpointIgnoredDirs(ctx context.Context, repoID, invocationID string, engine *checkpoint.Engine, sandboxPath string, env map[string]string) {
	if engine == nil {
		return
	}
	dirs, err := checkpoint.DiscoverGitIgnoredDirs(ctx, s.runner, sandboxPath, env)
	if err != nil {
		s.recordInvocationWarning(repoID, invocationID, "checkpoint_ignored_dir_discovery_failed", err.Error(), map[string]any{
			"sandbox_path": sandboxPath,
		})
		return
	}
	engine.SetGitIgnoredDirs(dirs)
}
