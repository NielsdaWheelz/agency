// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
)

// DoctorReport holds all the data for doctor output.
type DoctorReport struct {
	// Repo and directories
	RepoRoot        string
	AgencyDataDir   string
	AgencyConfigDir string
	UserConfigPath  string
	AgencyCacheDir  string

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
	DefaultsParentBranch string
	DefaultsRunner       string
	DefaultsEditor       string
	RunnerCmd            string
	ScriptSetup          string
	ScriptVerify         string
	ScriptArchive        string
}

// osEnv implements paths.Env using os.Getenv.
type osEnv struct{}

func (osEnv) Get(key string) string {
	return os.Getenv(key)
}

// DoctorOpts holds options for the doctor command.
type DoctorOpts struct {
	// RepoPath is the optional --repo flag to target a specific repo.
	RepoPath string
}

// Doctor implements the `agency doctor` command.
// Validates repo, tools, config, scripts, and persists repo identity on success.
func Doctor(ctx context.Context, cr agencyexec.CommandRunner, fsys fs.FS, cwd string, opts DoctorOpts, stdout, stderr io.Writer) error {
	// 1. Discover repo root (use --repo if provided, otherwise CWD)
	targetPath := cwd
	if opts.RepoPath != "" {
		targetPath = opts.RepoPath
	}
	repoRoot, err := git.GetRepoRoot(ctx, cr, targetPath)
	if err != nil {
		if opts.RepoPath != "" {
			return errors.NewWithDetails(
				errors.EInvalidRepoPath,
				fmt.Sprintf("--repo path is not inside a git repository: %s", opts.RepoPath),
				map[string]string{"path": opts.RepoPath},
			)
		}
		return err
	}

	// 2. Resolve directories
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// 3. Load and validate user config
	userCfg, found, err := config.LoadUserConfig(fsys, dirs.ConfigDir)
	if err != nil {
		return err
	}
	if !found {
		return errors.New(errors.EInvalidUserConfig, "user config not found: "+config.UserConfigPath(dirs.ConfigDir))
	}

	// 4. Load and validate agency.json
	cfg, err := config.LoadAndValidate(fsys, repoRoot.Path)
	if err != nil {
		return err
	}

	// 5. Get origin info
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)

	// 6. Derive repo identity
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

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

	// 9. Resolve runner/editor commands
	resolvedRunnerCmd, err := config.ResolveRunnerCmd(cr, fsys, dirs.ConfigDir, userCfg, userCfg.Defaults.Runner)
	if err != nil {
		return err
	}
	if _, err := config.ResolveEditorCmd(cr, fsys, dirs.ConfigDir, userCfg, userCfg.Defaults.Editor); err != nil {
		return err
	}

	// 10. Check scripts exist and are executable
	scriptSetup, err := checkScript(fsys, cfg.Scripts.Setup, repoRoot.Path, "setup")
	if err != nil {
		return err
	}
	scriptVerify, err := checkScript(fsys, cfg.Scripts.Verify, repoRoot.Path, "verify")
	if err != nil {
		return err
	}
	scriptArchive, err := checkScript(fsys, cfg.Scripts.Archive, repoRoot.Path, "archive")
	if err != nil {
		return err
	}

	currentBranch, err := currentBranch(ctx, cr, repoRoot.Path)
	if err != nil {
		return err
	}

	// Build report
	report := DoctorReport{
		RepoRoot:             repoRoot.Path,
		AgencyDataDir:        dirs.DataDir,
		AgencyConfigDir:      dirs.ConfigDir,
		UserConfigPath:       config.UserConfigPath(dirs.ConfigDir),
		AgencyCacheDir:       dirs.CacheDir,
		RepoKey:              repoIdentity.RepoKey,
		RepoID:               repoIdentity.RepoID,
		OriginPresent:        originInfo.Present,
		OriginURL:            originInfo.URL,
		OriginHost:           originInfo.Host,
		GitHubFlowAvailable:  repoIdentity.GitHubFlowAvailable,
		GitVersion:           gitVersion,
		TmuxVersion:          tmuxVersion,
		GhVersion:            ghVersion,
		GhAuthenticated:      true,
		DefaultsParentBranch: currentBranch,
		DefaultsRunner:       userCfg.Defaults.Runner,
		DefaultsEditor:       userCfg.Defaults.Editor,
		RunnerCmd:            resolvedRunnerCmd,
		ScriptSetup:          scriptSetup,
		ScriptVerify:         scriptVerify,
		ScriptArchive:        scriptArchive,
	}

	// 10. Persist repo index and repo record (only on success)
	if err := persistOnSuccess(fsys, dirs.DataDir, repoRoot.Path, repoIdentity, originInfo, cfg); err != nil {
		return err
	}

	// 11. Write output
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

// persistOnSuccess writes repo_index.json and repo.json atomically.
func persistOnSuccess(fsys fs.FS, dataDir, repoRoot string, repoIdentity identity.RepoIdentity, originInfo git.OriginInfo, cfg config.AgencyConfig) error {
	st := store.NewStore(fsys, dataDir, time.Now)

	// Load existing repo index (or empty if missing)
	idx, err := st.LoadRepoIndex()
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to load repo_index.json", err)
	}

	// Upsert entry
	idx = st.UpsertRepoIndexEntry(idx, repoIdentity.RepoKey, repoIdentity.RepoID, repoRoot)

	// Load existing repo record (if any)
	existingRec, exists, err := st.LoadRepoRecord(repoIdentity.RepoID)
	if err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to load repo.json", err)
	}

	var existingPtr *store.RepoRecord
	if exists {
		existingPtr = &existingRec
	}

	// Build repo record
	agencyJSONPath := filepath.Join(repoRoot, "agency.json")
	rec := st.UpsertRepoRecord(existingPtr, store.BuildRepoRecordInput{
		RepoKey:          repoIdentity.RepoKey,
		RepoID:           repoIdentity.RepoID,
		RepoRootLastSeen: repoRoot,
		AgencyJSONPath:   agencyJSONPath,
		OriginPresent:    originInfo.Present,
		OriginURL:        originInfo.URL,
		OriginHost:       originInfo.Host,
		Capabilities: store.Capabilities{
			GitHubOrigin: repoIdentity.GitHubFlowAvailable,
			OriginHost:   originInfo.Host,
			GhAuthed:     true,
		},
	})

	// Save repo record first (so repo dir exists for repo_index to reference)
	if err := st.SaveRepoRecord(rec); err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to write repo.json", err)
	}

	// Save repo index
	if err := st.SaveRepoIndex(idx); err != nil {
		return errors.Wrap(errors.EPersistFailed, "failed to write repo_index.json", err)
	}

	return nil
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
	_, _ = fmt.Fprintf(w, "defaults_parent_branch: %s\n", r.DefaultsParentBranch)
	_, _ = fmt.Fprintf(w, "defaults_runner: %s\n", r.DefaultsRunner)
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
