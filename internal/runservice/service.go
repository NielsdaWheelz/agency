// Package runservice provides the concrete implementation of pipeline.RunService.
// It wires together all the real step implementations (repo gates, config loading,
// worktree creation, etc.) for the run pipeline.
package runservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/ids"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/pipeline"
	"github.com/NielsdaWheelz/agency/internal/repo"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/worktree"
)

// Service is the production implementation of pipeline.RunService.
type Service struct {
	cr      exec.CommandRunner
	fsys    fs.FS
	nowFunc func() time.Time

	// WorkingDirOverride, if set, is used by CheckRepoSafe instead of os.Getwd().
	// This keeps command flows free of process-global cwd mutation.
	WorkingDirOverride string

	// DataDirOverride, if set, is used instead of resolving AGENCY_DATA_DIR from env.
	// This enables tests to use t.TempDir() without t.Setenv.
	DataDirOverride string

	// ConfigDirOverride, if set, is used instead of resolving AGENCY_CONFIG_DIR from env.
	// This enables tests to use t.TempDir() without t.Setenv.
	ConfigDirOverride string
}

type osEnv struct{}

func (osEnv) Get(key string) string {
	return os.Getenv(key)
}

// New creates a new Service with production dependencies.
func New() *Service {
	return &Service{
		cr:      exec.NewRealRunner(),
		fsys:    fs.NewRealFS(),
		nowFunc: time.Now,
	}
}

// NewWithDeps creates a new Service with injected dependencies for testing.
func NewWithDeps(cr exec.CommandRunner, fsys fs.FS) *Service {
	return &Service{
		cr:      cr,
		fsys:    fsys,
		nowFunc: time.Now,
	}
}

// SetNowFunc overrides the time source for testing.
func (s *Service) SetNowFunc(fn func() time.Time) {
	s.nowFunc = fn
}

// SetWorkingDir sets an explicit working directory for repo checks.
func (s *Service) SetWorkingDir(path string) {
	s.WorkingDirOverride = path
}

// CheckRepoSafe verifies repo safety (clean working tree, parent branch exists, etc.).
func (s *Service) CheckRepoSafe(ctx context.Context, st *pipeline.PipelineState) error {
	if st.Parent == "" {
		return errors.New(errors.EParentBranchNotFound, "parent branch must be provided")
	}

	cwd := s.WorkingDirOverride
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get current directory", err)
		}
	}

	result, err := repo.CheckRepoSafe(ctx, s.cr, s.fsys, cwd, repo.CheckRepoSafeOpts{
		ParentBranch:    st.Parent,
		DataDirOverride: s.DataDirOverride,
	})
	if err != nil {
		return err
	}

	// Populate pipeline state
	st.RepoRoot = result.RepoRoot
	st.RepoID = result.RepoID
	st.RepoKey = result.RepoKey
	st.OriginURL = result.OriginURL
	st.DataDir = result.DataDir

	// Check name uniqueness among active runs
	return s.checkNameUnique(st)
}

// checkNameUnique verifies the run name is not already used by an active run.
func (s *Service) checkNameUnique(st *pipeline.PipelineState) error {
	// Scan runs for this repo
	records, err := store.ScanRunsForRepo(st.DataDir, st.RepoID)
	if err != nil {
		// Best-effort: if scan fails, continue (new repo with no runs yet is fine)
		return nil
	}

	// Convert to RunRef for the uniqueness check
	refs := make([]ids.RunRef, len(records))
	for i, r := range records {
		refs[i] = ids.RunRef{
			RepoID: r.RepoID,
			RunID:  r.RunID,
			Name:   r.Name,
			Broken: r.Broken,
		}
	}

	// isArchived checks if a run is archived (has Archive field set)
	isArchived := func(ref ids.RunRef) bool {
		// Find the record to check Archive status
		for _, r := range records {
			if r.RunID == ref.RunID && r.RepoID == ref.RepoID {
				return r.Meta != nil && r.Meta.Archive != nil
			}
		}
		return false
	}

	return ids.CheckNameUnique(st.Name, refs, isArchived)
}

// LoadAgencyConfig loads and validates agency.json, populates runner/setup info.
func (s *Service) LoadAgencyConfig(ctx context.Context, st *pipeline.PipelineState) error {
	if st.Parent == "" {
		return errors.New(errors.EParentBranchNotFound, "parent branch must be provided")
	}

	// Load and validate config for S1 requirements
	cfg, err := config.LoadAndValidateForS1(s.fsys, st.RepoRoot)
	if err != nil {
		return err
	}

	var configDir string
	if s.ConfigDirOverride != "" {
		configDir = s.ConfigDirOverride
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(errors.EInternal, "failed to get home directory", err)
		}
		dirs := paths.ResolveDirs(osEnv{}, homeDir)
		configDir = dirs.ConfigDir
	}

	userCfg, _, err := config.LoadUserConfig(s.fsys, configDir)
	if err != nil {
		return err
	}

	// Determine runner name to use
	runnerName := st.Runner
	if runnerName == "" {
		runnerName = userCfg.Defaults.Runner
	}

	// Resolve runner command using shared helper
	resolvedRunnerCmd, err := config.ResolveRunnerCmd(s.cr, s.fsys, configDir, userCfg, runnerName)
	if err != nil {
		return err
	}

	// Populate state
	st.Runner = runnerName // Store the resolved runner name (may differ from CLI input)
	st.ResolvedRunnerCmd = resolvedRunnerCmd
	st.SetupScript = cfg.Scripts.Setup.Path
	st.SetupTimeout = cfg.Scripts.Setup.Timeout
	st.ParentBranch = st.Parent

	return nil
}

// CreateWorktree creates the git worktree and .agency/ directories.
func (s *Service) CreateWorktree(ctx context.Context, st *pipeline.PipelineState) error {
	result, err := worktree.Create(ctx, s.cr, s.fsys, worktree.CreateOpts{
		RunID:        st.RunID,
		Name:         st.Name,
		RepoRoot:     st.RepoRoot,
		RepoID:       st.RepoID,
		ParentBranch: st.ParentBranch,
		DataDir:      st.DataDir,
	})
	if err != nil {
		return err
	}

	// Populate state
	st.Branch = result.Branch
	st.WorktreePath = result.WorktreePath

	// Convert worktree warnings to pipeline warnings
	for _, w := range result.Warnings {
		st.Warnings = append(st.Warnings, pipeline.Warning{
			Code:    w.Code,
			Message: w.Message,
		})
	}

	return nil
}

// WriteMeta writes the initial meta.json for the run.
// Creates the run directory with exclusive semantics, creates the logs subdirectory,
// and writes meta.json atomically with required fields.
func (s *Service) WriteMeta(ctx context.Context, st *pipeline.PipelineState) error {
	// Validate worktree exists (should have been created by CreateWorktree)
	info, err := s.fsys.Stat(st.WorktreePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.NewWithDetails(
				errors.EInternal,
				"worktree_path does not exist (WriteMeta called before CreateWorktree?)",
				map[string]string{
					"step":          "WriteMeta",
					"worktree_path": st.WorktreePath,
				},
			)
		}
		return errors.WrapWithDetails(
			errors.EInternal,
			"failed to stat worktree_path",
			err,
			map[string]string{
				"step":          "WriteMeta",
				"worktree_path": st.WorktreePath,
			},
		)
	}
	if !info.IsDir() {
		return errors.NewWithDetails(
			errors.EInternal,
			"worktree_path is not a directory",
			map[string]string{
				"step":          "WriteMeta",
				"worktree_path": st.WorktreePath,
			},
		)
	}

	// Create a store for the run operations
	st2 := store.NewStore(s.fsys, st.DataDir, s.nowFunc)

	// Create run directory (exclusive semantics) + logs subdirectory
	_, err = st2.EnsureRunDir(st.RepoID, st.RunID)
	if err != nil {
		return err
	}

	// Create initial meta (runner name was resolved in LoadAgencyConfig)
	meta := store.NewRunMeta(
		st.RunID,
		st.RepoID,
		st.Name,
		st.Runner,
		st.ResolvedRunnerCmd,
		st.ParentBranch,
		st.Branch,
		st.WorktreePath,
		s.nowFunc(),
	)

	// Write meta.json atomically
	if err := st2.WriteInitialMeta(st.RepoID, st.RunID, meta); err != nil {
		return err
	}

	return nil
}

// RunSetup executes the setup script with timeout.
// Runs the configured setup script via `sh -lc <setup_script>` in the worktree.
// Captures stdout/stderr to logs/setup.log (truncated on each attempt).
// Updates meta.json with setup evidence (flags.setup_failed, setup.* fields).
// Optionally parses .agency/out/setup.json for structured output.
func (s *Service) RunSetup(ctx context.Context, st *pipeline.PipelineState) error {
	// Build paths
	st2 := store.NewStore(s.fsys, st.DataDir, s.nowFunc)
	logsDir := st2.RunLogsDir(st.RepoID, st.RunID)
	logPath := filepath.Join(logsDir, "setup.log")

	// Ensure logs directory exists (should exist from WriteMeta, but be safe)
	if err := s.fsys.MkdirAll(logsDir, 0o700); err != nil {
		return errors.WrapWithDetails(
			errors.EInternal,
			"failed to ensure logs directory exists",
			err,
			map[string]string{"logs_dir": logsDir},
		)
	}

	// Build environment variables
	env := buildSetupEnv(st, logsDir)

	// Execute setup script
	result := executeSetupScript(ctx, st.SetupScript, st.WorktreePath, env, logPath, st.SetupTimeout)

	// Parse optional setup.json if it exists
	setupJSONPath := filepath.Join(st.WorktreePath, ".agency", "out", "setup.json")
	structuredOutput := parseSetupJSON(s.fsys, setupJSONPath)

	// Determine if setup failed
	setupFailed := result.Failed
	if !setupFailed && structuredOutput != nil && structuredOutput.Ok != nil && !*structuredOutput.Ok {
		// setup.json says ok=false, override success
		setupFailed = true
	}

	// Build setup metadata
	setupMeta := &store.RunMetaSetup{
		Command:    "sh -lc " + st.SetupScript,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
		TimedOut:   result.TimedOut,
		LogPath:    logPath,
	}

	// Add structured output fields if present
	if structuredOutput != nil {
		setupMeta.OutputOk = structuredOutput.Ok
		setupMeta.OutputSummary = structuredOutput.Summary
	}

	// Update meta.json atomically (read-modify-write)
	err := st2.UpdateMeta(st.RepoID, st.RunID, func(meta *store.RunMeta) {
		meta.Setup = setupMeta
		if setupFailed {
			if meta.Flags == nil {
				meta.Flags = &store.RunMetaFlags{}
			}
			meta.Flags.SetupFailed = true
		}
	})
	if err != nil {
		return err
	}

	// Return error if setup failed
	if result.TimedOut {
		return errors.NewWithDetails(
			errors.EScriptTimeout,
			"setup script timed out after "+st.SetupTimeout.String(),
			map[string]string{
				"command":  "sh -lc " + st.SetupScript,
				"log_path": logPath,
			},
		)
	}
	if setupFailed {
		msg := "setup script failed"
		if structuredOutput != nil && structuredOutput.Ok != nil && !*structuredOutput.Ok {
			msg = "setup script reported failure via setup.json"
			if structuredOutput.Summary != "" {
				msg += ": " + structuredOutput.Summary
			}
		}
		return errors.NewWithDetails(
			errors.EScriptFailed,
			msg,
			map[string]string{
				"command":   "sh -lc " + st.SetupScript,
				"exit_code": fmt.Sprintf("%d", result.ExitCode),
				"log_path":  logPath,
			},
		)
	}

	return nil
}

// setupResult holds the result of setup script execution.
type setupResult struct {
	ExitCode   int
	DurationMs int64
	TimedOut   bool
	Failed     bool
}

// executeSetupScript runs the setup script and captures output to the log file.
func executeSetupScript(ctx context.Context, script, workDir string, env map[string]string, logPath string, timeout time.Duration) setupResult {
	start := time.Now()

	// Create/truncate log file
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return setupResult{ExitCode: -1, Failed: true}
	}

	// Write header to log (best-effort diagnostic output)
	_, _ = fmt.Fprintf(logFile, "# agency setup log\n")
	_, _ = fmt.Fprintf(logFile, "# timestamp: %s\n", start.UTC().Format(time.RFC3339))
	_, _ = fmt.Fprintf(logFile, "# command: sh -lc %s\n", script)
	_, _ = fmt.Fprintf(logFile, "# cwd: %s\n", workDir)
	_, _ = fmt.Fprintf(logFile, "# ---\n\n")

	// Apply timeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Run command with explicit stdio targets.
	cmdResult, runErr := exec.RunAttached(ctx, "sh", []string{"-lc", script}, exec.AttachedRunOpts{
		Dir:    workDir,
		Env:    env,
		Stdout: logFile,
		Stderr: logFile,
	})
	duration := time.Since(start)
	durationMs := duration.Milliseconds()

	// Close log file (best-effort; command result takes priority)
	_ = logFile.Close()

	result := setupResult{
		DurationMs: durationMs,
	}

	if runErr != nil {
		// Failed to start/cancel path.
		result.ExitCode = -1
		result.Failed = true
		return result
	}

	if cmdResult.ExitCode == exec.ExitTimeout {
		result.ExitCode = -1
		result.TimedOut = true
		result.Failed = true
		return result
	}
	if cmdResult.ExitCode != 0 {
		result.ExitCode = cmdResult.ExitCode
		result.Failed = true
		return result
	}

	result.ExitCode = 0
	result.Failed = false
	return result
}

// buildSetupEnv builds the environment variables for the setup script.
func buildSetupEnv(st *pipeline.PipelineState, logsDir string) map[string]string {
	dotAgencyDir := filepath.Join(st.WorktreePath, ".agency")
	outputDir := filepath.Join(dotAgencyDir, "out")

	env := map[string]string{
		"AGENCY_RUN_ID":         st.RunID,
		"AGENCY_NAME":           st.Name,
		"AGENCY_REPO_ROOT":      st.RepoRoot,
		"AGENCY_WORKSPACE_ROOT": st.WorktreePath,
		"AGENCY_BRANCH":         st.Branch,
		"AGENCY_PARENT_BRANCH":  st.ParentBranch,
		"AGENCY_ORIGIN_NAME":    "origin",
		"AGENCY_ORIGIN_URL":     st.OriginURL,
		"AGENCY_RUNNER":         st.Runner,
		"AGENCY_PR_URL":         "", // empty in S1 (no PR yet)
		"AGENCY_PR_NUMBER":      "", // empty in S1 (no PR yet)
		"AGENCY_DOTAGENCY_DIR":  dotAgencyDir,
		"AGENCY_OUTPUT_DIR":     outputDir,
		"AGENCY_LOG_DIR":        logsDir,
		"AGENCY_NONINTERACTIVE": "1",
		"CI":                    "1",
	}
	return env
}

// structuredSetupOutput represents the optional .agency/out/setup.json output.
type structuredSetupOutput struct {
	Ok      *bool
	Summary string
}

// parseSetupJSON attempts to parse .agency/out/setup.json if it exists.
// Returns nil if the file doesn't exist or is invalid JSON.
func parseSetupJSON(fsys fs.FS, path string) *structuredSetupOutput {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return nil // file doesn't exist or can't be read
	}

	var raw struct {
		SchemaVersion string `json:"schema_version"`
		Ok            *bool  `json:"ok"`
		Summary       string `json:"summary"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil // invalid JSON, ignore
	}

	return &structuredSetupOutput{
		Ok:      raw.Ok,
		Summary: raw.Summary,
	}
}

// TmuxSessionPrefix is the prefix for all agency tmux session names.
// Note: Using underscore instead of colon because tmux interprets colons
// as session:window.pane syntax separators and converts them to underscores.
const TmuxSessionPrefix = "agency_"

// StartTmux creates the tmux session with the runner command.
// Only runs if setup succeeded (flags.setup_failed is absent/false).
// Creates a detached tmux session `agency:<run_id>` running the runner.
// Updates meta.json with tmux_session_name on success or flags.tmux_failed on failure.
func (s *Service) StartTmux(ctx context.Context, st *pipeline.PipelineState) error {
	// Check if setup failed - should not start tmux if so
	st2 := store.NewStore(s.fsys, st.DataDir, s.nowFunc)
	meta, err := st2.ReadMeta(st.RepoID, st.RunID)
	if err != nil {
		return err
	}

	if meta.Flags != nil && meta.Flags.SetupFailed {
		return errors.NewWithDetails(
			errors.ETmuxFailed,
			"cannot start tmux: setup failed",
			map[string]string{
				"run_id": st.RunID,
			},
		)
	}

	// Build the tmux session name
	sessionName := TmuxSessionPrefix + st.RunID

	// Check if session already exists (collision detection)
	hasSessionResult, err := s.cr.Run(ctx, "tmux", []string{"has-session", "-t", sessionName}, exec.RunOpts{})
	if err != nil {
		// tmux command failed to run (not installed, etc.)
		return errors.Wrap(errors.ETmuxNotInstalled, "failed to check tmux session", err)
	}
	if hasSessionResult.ExitCode == 0 {
		// Session already exists - collision
		return errors.NewWithDetails(
			errors.ETmuxSessionExists,
			"tmux session '"+sessionName+"' already exists",
			map[string]string{
				"session": sessionName,
				"run_id":  st.RunID,
			},
		)
	}

	// Build the pane command
	paneCmd := core.BuildRunnerShellScript(st.WorktreePath, st.ResolvedRunnerCmd)

	// Create the tmux session detached
	// Use: tmux new-session -d -s <session> -- sh -lc '<pane_cmd>'
	newSessionResult, err := s.cr.Run(ctx, "tmux", []string{
		"new-session",
		"-d",
		"-s", sessionName,
		"--",
		"sh", "-lc", paneCmd,
	}, exec.RunOpts{})
	if err != nil {
		// tmux command failed to run
		s.setTmuxFailedFlag(st.DataDir, st.RepoID, st.RunID)
		return errors.Wrap(errors.ETmuxFailed, "failed to create tmux session", err)
	}
	if newSessionResult.ExitCode != 0 {
		// tmux command returned non-zero
		s.setTmuxFailedFlag(st.DataDir, st.RepoID, st.RunID)
		return errors.NewWithDetails(
			errors.ETmuxFailed,
			"tmux new-session failed: "+newSessionResult.Stderr,
			map[string]string{
				"session":   sessionName,
				"exit_code": fmt.Sprintf("%d", newSessionResult.ExitCode),
				"stderr":    newSessionResult.Stderr,
			},
		)
	}

	// Update meta.json with tmux_session_name
	err = st2.UpdateMeta(st.RepoID, st.RunID, func(m *store.RunMeta) {
		m.TmuxSessionName = sessionName
	})
	if err != nil {
		// Meta write failed, but tmux session was created
		// Best-effort: try to kill the session; returning meta write error
		_, _ = s.cr.Run(ctx, "tmux", []string{"kill-session", "-t", sessionName}, exec.RunOpts{})
		return err
	}

	return nil
}

// setTmuxFailedFlag updates meta.json to set flags.tmux_failed=true.
// Called when tmux session creation fails.
func (s *Service) setTmuxFailedFlag(dataDir, repoID, runID string) {
	st2 := store.NewStore(s.fsys, dataDir, s.nowFunc)
	_ = st2.UpdateMeta(repoID, runID, func(m *store.RunMeta) {
		if m.Flags == nil {
			m.Flags = &store.RunMetaFlags{}
		}
		m.Flags.TmuxFailed = true
	})
}
