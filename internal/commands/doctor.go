// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// DoctorReport holds all the data for doctor output.
type DoctorReport struct {
	// Repo and directories
	RepoRoot         string
	AgencyDataDir    string
	AgencyConfigDir  string
	UserConfigPath   string
	AgencyJSONPath   string
	AgencyJSONSource string
	AgencyCacheDir   string

	// Identity/origin
	RepoKey             string
	RepoID              string
	OriginPresent       bool
	OriginURL           string
	OriginHost          string
	GitHubFlowAvailable bool

	// Tooling
	GitVersion      string
	TmuxVersion     string
	GhVersion       string
	GhAuthenticated bool

	// Config resolution
	DefaultsBaseBranch         string
	DefaultsRunner             string
	DefaultsRunnerModel        string
	DefaultsRunnerModelSource  string
	DefaultsRunnerEffort       string
	DefaultsRunnerEffortSource string
	DefaultsEditor             string
	RunnerCmd                  string
	ScriptSetup                string
	ScriptVerify               string
	ScriptArchive              string
}

// osEnv implements paths.Env using os.Getenv.
type osEnv struct{}

func (osEnv) Get(key string) string {
	return os.Getenv(key)
}

// DoctorOpts holds options for the doctor command.
type DoctorOpts struct {
	// Path is the optional --path flag to target a specific repo checkout.
	Path string

	// DataDirOverride, if set, is used instead of resolving from environment.
	DataDirOverride string

	// ConfigDirOverride, if set, is used instead of resolving from environment.
	ConfigDirOverride string

	// AgencyConfigPath, if set, is the exact agency config file to load.
	AgencyConfigPath string
}

// Doctor implements the `agency doctor` command.
// Validates repo, tools, config, and scripts without mutating on-disk state.
func Doctor(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts DoctorOpts, stdout, stderr io.Writer) error {
	targetPath := cwd
	if opts.Path != "" {
		targetPath = opts.Path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	if opts.DataDirOverride != "" {
		dirs.DataDir = opts.DataDirOverride
	}
	if opts.ConfigDirOverride != "" {
		dirs.ConfigDir = opts.ConfigDirOverride
	}
	if repoID, ok := agencyManagedTreeRepoID(targetPath, dirs.DataDir); ok {
		ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
		if err != nil {
			return err
		}
		repo, err := ns.client.GetRepo(ctx, repoID)
		if err != nil {
			return err
		}
		if repo.Data.PreferredRoot == "" || !repo.Data.PreferredRootAccessible {
			return errors.NewWithDetails(
				errors.ERepoRootInaccessible,
				"repo preferred_root is not accessible",
				map[string]string{
					"repo": repoID,
					"hint": "re-register this repo from an accessible checkout, then re-run doctor",
				},
			)
		}
		targetPath = repo.Data.PreferredRoot
	}

	repoRoot, err := git.GetRepoRoot(ctx, cr, targetPath)
	if err != nil {
		if opts.Path != "" {
			return errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--path is not inside a git repository: %s", opts.Path),
				map[string]string{"path": opts.Path},
			)
		}
		return err
	}

	userCfg, err := config.LoadUserConfig(fsys, dirs.ConfigDir)
	if err != nil {
		return err
	}

	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)

	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	agencyConfigPath := opts.AgencyConfigPath
	if agencyConfigPath != "" && !filepath.IsAbs(agencyConfigPath) {
		agencyConfigPath = filepath.Join(cwd, agencyConfigPath)
	}
	resolvedAgencyConfig, err := config.ResolveAgencyConfig(fsys, repoRoot.Path, dirs.ConfigDir, repoIdentity.RepoID, agencyConfigPath)
	if err != nil {
		return err
	}
	cfg := resolvedAgencyConfig.Config

	// 7. Check tools
	gitVersion, err := checkGit(ctx, cr)
	if err != nil {
		return err
	}

	tmuxVersion, err := checkTmux(ctx, cr)
	if err != nil {
		return err
	}

	ghVersion, err := checkGh(ctx, cr)
	if err != nil {
		return err
	}

	// 8. Check gh auth status
	if err := checkGhAuth(ctx, cr); err != nil {
		return err
	}

	resolvedRunnerCmd, err := config.ResolveRunnerCmd(cr, fsys, dirs.ConfigDir, userCfg, userCfg.Defaults.Runner)
	if err != nil {
		return err
	}
	if _, err := config.ResolveEditorCmd(cr, fsys, dirs.ConfigDir, userCfg, userCfg.Defaults.Editor); err != nil {
		return err
	}
	canonicalRunner, err := runners.Canonicalize(userCfg.Defaults.Runner)
	if err != nil {
		return err
	}

	defaultsRunnerModel := ""
	defaultsRunnerModelSource := "none"
	defaultsRunnerEffort := ""
	defaultsRunnerEffortSource := "none"
	if runnerDefaults, ok := userCfg.RunnerDefaults[canonicalRunner]; ok {
		if runnerDefaults.Model != "" {
			defaultsRunnerModel = runnerDefaults.Model
			defaultsRunnerModelSource = "user"
		}
		if runnerDefaults.Effort != "" {
			defaultsRunnerEffort = runnerDefaults.Effort
			defaultsRunnerEffortSource = "user"
		}
	}
	if runnerDefaults, ok := cfg.RunnerDefaults[canonicalRunner]; ok {
		if runnerDefaults.Model != "" {
			defaultsRunnerModel = runnerDefaults.Model
			defaultsRunnerModelSource = resolvedAgencyConfig.Source
		}
		if runnerDefaults.Effort != "" {
			defaultsRunnerEffort = runnerDefaults.Effort
			defaultsRunnerEffortSource = resolvedAgencyConfig.Source
		}
	}

	// 10. Check scripts exist and are executable
	scriptSetup, err := checkScript(fsys, cfg.Scripts.Setup.Path, repoRoot.Path, "setup")
	if err != nil {
		return err
	}
	scriptVerify, err := checkScript(fsys, cfg.Scripts.Verify.Path, repoRoot.Path, "verify")
	if err != nil {
		return err
	}
	scriptArchive, err := checkScript(fsys, cfg.Scripts.Archive.Path, repoRoot.Path, "archive")
	if err != nil {
		return err
	}

	currentBranch, err := currentBranch(ctx, cr, repoRoot.Path)
	if err != nil {
		return err
	}

	report := DoctorReport{
		RepoRoot:                   repoRoot.Path,
		AgencyDataDir:              dirs.DataDir,
		AgencyConfigDir:            dirs.ConfigDir,
		UserConfigPath:             config.UserConfigPath(dirs.ConfigDir),
		AgencyJSONPath:             resolvedAgencyConfig.Path,
		AgencyJSONSource:           resolvedAgencyConfig.Source,
		AgencyCacheDir:             dirs.CacheDir,
		RepoKey:                    repoIdentity.RepoKey,
		RepoID:                     repoIdentity.RepoID,
		OriginPresent:              originInfo.Present,
		OriginURL:                  originInfo.URL,
		OriginHost:                 originInfo.Host,
		GitHubFlowAvailable:        repoIdentity.GitHubFlowAvailable,
		GitVersion:                 gitVersion,
		TmuxVersion:                tmuxVersion,
		GhVersion:                  ghVersion,
		GhAuthenticated:            true,
		DefaultsBaseBranch:         currentBranch,
		DefaultsRunner:             userCfg.Defaults.Runner,
		DefaultsRunnerModel:        defaultsRunnerModel,
		DefaultsRunnerModelSource:  defaultsRunnerModelSource,
		DefaultsRunnerEffort:       defaultsRunnerEffort,
		DefaultsRunnerEffortSource: defaultsRunnerEffortSource,
		DefaultsEditor:             userCfg.Defaults.Editor,
		RunnerCmd:                  resolvedRunnerCmd,
		ScriptSetup:                scriptSetup,
		ScriptVerify:               scriptVerify,
		ScriptArchive:              scriptArchive,
	}

	writeDoctorOutput(stdout, report)

	return nil
}

// checkGit verifies git is installed and returns its version.
func checkGit(ctx context.Context, cr agencyexec.CommandRunner) (string, error) {
	result, err := cr.Run(ctx, "git", []string{"--version"}, agencyexec.RunOpts{})
	if err != nil {
		return "", errors.New(errors.EGitNotInstalled, "git is not installed or not on PATH")
	}
	if result.ExitCode != 0 {
		return "", errors.New(errors.EGitNotInstalled, "git --version failed")
	}
	return strings.TrimSpace(result.Stdout), nil
}

// checkTmux verifies tmux is installed and returns its version.
func checkTmux(ctx context.Context, cr agencyexec.CommandRunner) (string, error) {
	result, err := cr.Run(ctx, "tmux", []string{"-V"}, agencyexec.RunOpts{})
	if err != nil {
		return "", errors.New(errors.ETmuxNotInstalled, "tmux is not installed or not on PATH")
	}
	if result.ExitCode != 0 {
		return "", errors.New(errors.ETmuxNotInstalled, "tmux -V failed")
	}
	return strings.TrimSpace(result.Stdout), nil
}

// checkGh verifies gh is installed and returns its version.
func checkGh(ctx context.Context, cr agencyexec.CommandRunner) (string, error) {
	result, err := cr.Run(ctx, "gh", []string{"--version"}, agencyexec.RunOpts{})
	if err != nil {
		return "", errors.New(errors.EGhNotInstalled, "gh is not installed or not on PATH; install from https://cli.github.com/")
	}
	if result.ExitCode != 0 {
		return "", errors.New(errors.EGhNotInstalled, "gh --version failed")
	}
	// gh --version outputs multiple lines; take first line
	lines := strings.Split(result.Stdout, "\n")
	version := strings.TrimSpace(lines[0])
	return version, nil
}

// checkGhAuth verifies gh is authenticated.
func checkGhAuth(ctx context.Context, cr agencyexec.CommandRunner) error {
	result, err := cr.Run(ctx, "gh", []string{"auth", "status"}, agencyexec.RunOpts{})
	if err != nil {
		return errors.New(errors.EGhNotAuthenticated, "gh auth check failed; run 'gh auth login'")
	}
	if result.ExitCode != 0 {
		return errors.New(errors.EGhNotAuthenticated, "gh is not authenticated; run 'gh auth login'")
	}
	return nil
}

// checkRunnerExists verifies the runner command exists on PATH or as a path.
// checkScript verifies a script exists and is executable.
// Returns the resolved absolute path.
func checkScript(fsys fs.FS, scriptPath, repoRoot, scriptName string) (string, error) {
	// Resolve path
	absPath := scriptPath
	if !filepath.IsAbs(scriptPath) {
		absPath = filepath.Join(repoRoot, scriptPath)
	}

	info, err := fsys.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New(errors.EScriptNotFound, "script not found: "+scriptPath)
		}
		return "", errors.Wrap(errors.EScriptNotFound, "failed to check script "+scriptName, err)
	}

	// Follow symlink if needed and check executable
	// For symlinks, Stat already follows them, so mode check is on the target
	if info.Mode().Perm()&0111 == 0 {
		return "", errors.New(errors.EScriptNotExecutable, "script is not executable: "+scriptPath+"; run 'chmod +x "+scriptPath+"'")
	}

	return absPath, nil
}

func currentBranch(ctx context.Context, cr agencyexec.CommandRunner, repoRoot string) (string, error) {
	result, err := cr.Run(ctx, "git", []string{"branch", "--show-current"}, agencyexec.RunOpts{Dir: repoRoot})
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to get current branch", err)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// writeDoctorOutput writes the stable key: value output.
// All writes use explicit error ignoring since this is informational output
// where write failures cannot be meaningfully handled.
func writeDoctorOutput(w io.Writer, r DoctorReport) {
	// Repo + dirs
	_, _ = fmt.Fprintf(w, "repo_root: %s\n", r.RepoRoot)
	_, _ = fmt.Fprintf(w, "agency_data_dir: %s\n", r.AgencyDataDir)
	_, _ = fmt.Fprintf(w, "agency_config_dir: %s\n", r.AgencyConfigDir)
	_, _ = fmt.Fprintf(w, "user_config_path: %s\n", r.UserConfigPath)
	_, _ = fmt.Fprintf(w, "agency_json_path: %s\n", r.AgencyJSONPath)
	_, _ = fmt.Fprintf(w, "agency_json_source: %s\n", r.AgencyJSONSource)
	_, _ = fmt.Fprintf(w, "agency_cache_dir: %s\n", r.AgencyCacheDir)

	// Identity/origin
	_, _ = fmt.Fprintf(w, "repo_key: %s\n", r.RepoKey)
	_, _ = fmt.Fprintf(w, "repo_id: %s\n", r.RepoID)
	_, _ = fmt.Fprintf(w, "origin_present: %s\n", boolStr(r.OriginPresent))
	_, _ = fmt.Fprintf(w, "origin_url: %s\n", r.OriginURL)
	_, _ = fmt.Fprintf(w, "origin_host: %s\n", r.OriginHost)
	_, _ = fmt.Fprintf(w, "github_flow_available: %s\n", boolStr(r.GitHubFlowAvailable))

	// Tooling
	_, _ = fmt.Fprintf(w, "git_version: %s\n", r.GitVersion)
	_, _ = fmt.Fprintf(w, "tmux_version: %s\n", r.TmuxVersion)
	_, _ = fmt.Fprintf(w, "gh_version: %s\n", r.GhVersion)
	_, _ = fmt.Fprintf(w, "gh_authenticated: %s\n", boolStr(r.GhAuthenticated))

	// Config resolution
	_, _ = fmt.Fprintf(w, "defaults_base_branch: %s\n", r.DefaultsBaseBranch)
	_, _ = fmt.Fprintf(w, "defaults_runner: %s\n", r.DefaultsRunner)
	_, _ = fmt.Fprintf(w, "defaults_runner_model: %s\n", r.DefaultsRunnerModel)
	_, _ = fmt.Fprintf(w, "defaults_runner_model_source: %s\n", r.DefaultsRunnerModelSource)
	_, _ = fmt.Fprintf(w, "defaults_runner_effort: %s\n", r.DefaultsRunnerEffort)
	_, _ = fmt.Fprintf(w, "defaults_runner_effort_source: %s\n", r.DefaultsRunnerEffortSource)
	_, _ = fmt.Fprintf(w, "defaults_editor: %s\n", r.DefaultsEditor)
	_, _ = fmt.Fprintf(w, "runner_cmd: %s\n", r.RunnerCmd)
	_, _ = fmt.Fprintf(w, "script_setup: %s\n", r.ScriptSetup)
	_, _ = fmt.Fprintf(w, "script_verify: %s\n", r.ScriptVerify)
	_, _ = fmt.Fprintf(w, "script_archive: %s\n", r.ScriptArchive)

	// Final
	_, _ = fmt.Fprintln(w, "status: ok")
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
