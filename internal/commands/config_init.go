// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// ConfigInitOpts holds options for the config init command.
type ConfigInitOpts struct {
	Force bool

	// ConfigDirOverride, if set, is used instead of resolving from environment.
	ConfigDirOverride string
}

// ConfigInit implements the `agency config init` command.
// It writes exactly one file: the user config under AGENCY_CONFIG_DIR.
func ConfigInit(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, opts ConfigInitOpts, stdout, stderr io.Writer) error {
	_ = ctx
	_ = stderr

	runnerCandidates := []struct {
		id         string
		executable string
	}{
		{id: runners.RunnerClaudeCode, executable: "claude"},
		{id: runners.RunnerCodex, executable: "codex"},
		{id: runners.RunnerCursor, executable: "cursor"},
		{id: runners.RunnerAmp, executable: "amp"},
		{id: runners.RunnerOpenCode, executable: "opencode"},
		{id: runners.RunnerDroid, executable: "droid"},
	}
	editorCandidates := []string{"code", "cursor", "zed", "nvim", "vim", "vi", "nano"}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}

	dirs := paths.ResolveDirs(os.Getenv, homeDir)
	if opts.ConfigDirOverride != "" {
		dirs.ConfigDir = opts.ConfigDirOverride
	}

	userConfigPath := config.UserConfigPath(dirs.ConfigDir)
	userConfigExists := false
	if _, err := fsys.Stat(userConfigPath); err == nil {
		userConfigExists = true
	} else if !os.IsNotExist(err) {
		return errors.Wrap(errors.EInvalidUserConfig, "failed to check user config", err)
	}

	if userConfigExists && !opts.Force {
		return errors.New(errors.EUsage, "user config already exists; use --force to overwrite")
	}

	detectedRunners := make(map[string]string)
	runnerIDs := make([]string, 0, len(runnerCandidates))
	for _, candidate := range runnerCandidates {
		if _, err := cr.LookPath(candidate.executable); err == nil {
			detectedRunners[candidate.id] = candidate.executable
			runnerIDs = append(runnerIDs, candidate.id)
		}
	}
	if len(runnerIDs) == 0 {
		executables := make([]string, 0, len(runnerCandidates))
		for _, candidate := range runnerCandidates {
			executables = append(executables, candidate.executable)
		}
		return errors.New(
			errors.ERunnerNotFound,
			"no supported runner executable found on PATH; install one of: "+strings.Join(executables, ", "),
		)
	}

	defaultEditor := ""
	for _, candidate := range editorCandidates {
		if _, err := cr.LookPath(candidate); err == nil {
			defaultEditor = candidate
			break
		}
	}
	if defaultEditor == "" {
		return errors.New(
			errors.EEditorNotConfigured,
			"no supported editor executable found on PATH; install one of: "+strings.Join(editorCandidates, ", "),
		)
	}

	cfg := config.UserConfig{
		Version: config.AgencyConfigVersion,
		Defaults: config.UserDefaults{
			Runner:           runnerIDs[0],
			Editor:           defaultEditor,
			BaseBranch:       "main",
			ExecutionProfile: "personal",
		},
		Runners: detectedRunners,
		ExecutionProfiles: map[string]config.ExecutionProfile{
			"personal": {Env: map[string]string{}},
		},
	}
	cfg, err = config.ValidateUserConfig(cfg)
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to validate scaffolded user config", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to serialize user config", err)
	}
	data = append(data, '\n')

	if err := fsys.MkdirAll(dirs.ConfigDir, 0o755); err != nil {
		return errors.Wrap(errors.EInvalidUserConfig, "failed to create user config directory", err)
	}
	if err := fs.WriteFileAtomic(fsys, userConfigPath, data, 0o644); err != nil {
		return errors.Wrap(errors.EInvalidUserConfig, "failed to write user config", err)
	}

	userConfigState := "created"
	if userConfigExists {
		userConfigState = "overwritten"
	}

	_, _ = fmt.Fprintf(stdout, "user_config_path: %s\n", userConfigPath)
	_, _ = fmt.Fprintf(stdout, "user_config: %s\n", userConfigState)
	_, _ = fmt.Fprintf(stdout, "defaults_runner: %s\n", runnerIDs[0])
	_, _ = fmt.Fprintf(stdout, "defaults_editor: %s\n", defaultEditor)
	_, _ = fmt.Fprintf(stdout, "defaults_base_branch: %s\n", cfg.Defaults.BaseBranch)
	_, _ = fmt.Fprintf(stdout, "defaults_execution_profile: %s\n", cfg.Defaults.ExecutionProfile)
	_, _ = fmt.Fprintf(stdout, "runners: %s\n", strings.Join(runnerIDs, ", "))
	_, _ = fmt.Fprintf(stdout, "execution_profiles: personal\n")

	return nil
}
