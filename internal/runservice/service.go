// Package runservice provides the concrete implementation of pipeline.RunService.
// It wires together all the real step implementations (repo gates, config loading,
// worktree creation, etc.) for the run pipeline.
package runservice

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/core"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
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

// CheckRepoSafe verifies repo safety (clean working tree, parent branch exists, etc.).
func (s *Service) CheckRepoSafe(ctx context.Context, st *pipeline.PipelineState) error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get current directory", err)
	}

	// Determine parent branch: use from opts if provided, otherwise will be resolved from config later
	parentBranch := st.Parent
	if parentBranch == "" {
		// Temporarily use a placeholder; will be validated after config is loaded
		// For now, we need to pass something to CheckRepoSafe
		// The actual parent branch will be validated in a real run when we have config
		//
		// Actually, looking at the pipeline order, CheckRepoSafe runs BEFORE LoadAgencyConfig.
		// This means we need to either:
		// 1. Load config first (but that changes order)
		// 2. Do parent branch check in LoadAgencyConfig (after config loads)
		// 3. Skip parent branch check if not provided, do it later
		//
		// Looking at the existing repo.CheckRepoSafe, it validates parent branch.
		// The pipeline spec says CheckRepoSafe first, then LoadAgencyConfig.
		// This means if --parent is not provided on CLI, we can't check it here.
		//
		// For now, let's handle this by:
		// - If Parent is provided, validate it
		// - If not provided, skip parent branch validation here (do it after config loads)
		parentBranch = "__deferred__" // sentinel value
	}

	// Only run full CheckRepoSafe with parent validation if parent is provided
	if parentBranch != "__deferred__" {
		result, err := repo.CheckRepoSafe(ctx, s.cr, s.fsys, cwd, repo.CheckRepoSafeOpts{
			ParentBranch: parentBranch,
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
		return nil
	}

	// Parent not provided - do basic repo checks without parent validation
	// The parent branch check will be done after config loads
	result, err := checkRepoSafeWithoutParent(ctx, s.cr, s.fsys, cwd)
	if err != nil {
		return err
	}

	st.RepoRoot = result.RepoRoot
	st.RepoID = result.RepoID
	st.RepoKey = result.RepoKey
	st.OriginURL = result.OriginURL
	st.DataDir = result.DataDir
	return nil
}

// checkRepoSafeWithoutParent performs repo safety checks without parent branch validation.
// Parent branch will be validated later after config is loaded.
func checkRepoSafeWithoutParent(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string) (*repo.RepoContext, error) {
	// This is a simplified version that doesn't check parent branch.
	// We'll use a dummy branch name that we know exists (HEAD) just to satisfy the API,
	// but actually the gates.go will check parent branch existence.
	//
	// Actually, looking more closely, the gates.go does a separate branch existence check.
	// Let me reconsider the design...
	//
	// The cleanest approach: have the pipeline do the parent branch check after
	// LoadAgencyConfig if it wasn't already checked. Let's just pass a sentinel
	// and document that we need to check parent branch later.
	//
	// For now, let's call the existing CheckRepoSafe but first check what the
	// current branch is, and use that if parent is not specified.
	// This defers the actual parent branch check.

	// We need to load repo info even without parent branch validation.
	// Let's inline the repo checks without the parent branch check.
	return checkRepoContextOnly(ctx, cr, fsys, cwd)
}

// checkRepoContextOnly resolves repo context without running all gates.
// This is used when parent branch will be validated later.
func checkRepoContextOnly(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string) (*repo.RepoContext, error) {
	// Import the packages we need and run the checks inline
	// Since this is getting complex, let me just call the actual CheckRepoSafe
	// with a branch we know exists - the current HEAD.

	// Get current branch
	result, err := cr.Run(ctx, "git", []string{"branch", "--show-current"}, exec.RunOpts{Dir: cwd})
	if err != nil {
		return nil, errors.Wrap(errors.ENoRepo, "failed to get current branch", err)
	}

	currentBranch := result.Stdout
	if len(currentBranch) > 0 && currentBranch[len(currentBranch)-1] == '\n' {
		currentBranch = currentBranch[:len(currentBranch)-1]
	}

	// Fallback to a common default if no branch (detached HEAD, etc.)
	if currentBranch == "" {
		currentBranch = "main"
	}

	// Now call the full CheckRepoSafe with the current branch
	// This validates everything except the *actual* parent branch the user wants
	return repo.CheckRepoSafe(ctx, cr, fsys, cwd, repo.CheckRepoSafeOpts{
		ParentBranch: currentBranch,
	})
}

// LoadAgencyConfig loads and validates agency.json, populates runner/setup info.
func (s *Service) LoadAgencyConfig(ctx context.Context, st *pipeline.PipelineState) error {
	// Load and validate config for S1 requirements
	cfg, err := config.LoadAndValidateForS1(s.fsys, st.RepoRoot)
	if err != nil {
		return err
	}

	// Determine runner name to use
	runnerName := st.Runner
	if runnerName == "" {
		runnerName = cfg.Defaults.Runner
	}

	// If a non-default runner is requested, we need to resolve it
	// ValidateForS1 already resolved the default runner, but if user specified
	// a different one, we need to check if it's configured
	resolvedRunnerCmd := cfg.ResolvedRunnerCmd
	if runnerName != cfg.Defaults.Runner {
		// Check if the requested runner is configured
		if cfg.Runners != nil {
			if cmd, ok := cfg.Runners[runnerName]; ok {
				resolvedRunnerCmd = cmd
			} else if runnerName == "claude" || runnerName == "codex" {
				// Standard runners fallback to PATH
				resolvedRunnerCmd = runnerName
			} else {
				return errors.New(errors.ERunnerNotConfigured,
					"runner \""+runnerName+"\" not configured; set runners."+runnerName+" or choose claude/codex")
			}
		} else if runnerName == "claude" || runnerName == "codex" {
			resolvedRunnerCmd = runnerName
		} else {
			return errors.New(errors.ERunnerNotConfigured,
				"runner \""+runnerName+"\" not configured; set runners."+runnerName+" or choose claude/codex")
		}
	}

	// Resolve parent branch
	parentBranch := st.Parent
	if parentBranch == "" {
		parentBranch = cfg.Defaults.ParentBranch
	}

	// If parent branch wasn't checked in CheckRepoSafe (was deferred), validate it now
	if st.Parent == "" {
		// Need to validate the resolved parent branch exists
		exists, err := branchExists(ctx, s.cr, st.RepoRoot, parentBranch)
		if err != nil {
			return err
		}
		if !exists {
			return errors.NewWithDetails(
				errors.EParentBranchNotFound,
				"local branch '"+parentBranch+"' not found; checkout or fetch parent locally",
				map[string]string{"branch": parentBranch},
			)
		}
	}

	// Populate state
	st.Runner = runnerName // Store the resolved runner name (may differ from CLI input)
	st.ResolvedRunnerCmd = resolvedRunnerCmd
	st.SetupScript = cfg.Scripts.Setup
	st.ParentBranch = parentBranch

	return nil
}

// branchExists checks if a local branch exists.
func branchExists(ctx context.Context, cr exec.CommandRunner, repoRoot, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	result, err := cr.Run(ctx, "git", []string{"show-ref", "--verify", ref}, exec.RunOpts{Dir: repoRoot})
	if err != nil {
		return false, errors.Wrap(errors.EInternal, "failed to check branch existence", err)
	}
	return result.ExitCode == 0, nil
}

// CreateWorktree creates the git worktree and .agency/ directories.
func (s *Service) CreateWorktree(ctx context.Context, st *pipeline.PipelineState) error {
	result, err := worktree.Create(ctx, s.cr, s.fsys, worktree.CreateOpts{
		RunID:        st.RunID,
		Title:        st.Title,
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

	// If title was empty, use the resolved title for later use
	if st.Title == "" {
		st.Title = result.ResolvedTitle
	}

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
		st.Title,
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

// SetupTimeout is the timeout for the setup script (10 minutes per spec).
const SetupTimeout = 10 * time.Minute

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
	result := executeSetupScript(ctx, st.SetupScript, st.WorktreePath, env, logPath, SetupTimeout)

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
			"setup script timed out after "+SetupTimeout.String(),
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

	// Write header to log
	fmt.Fprintf(logFile, "# agency setup log\n")
	fmt.Fprintf(logFile, "# timestamp: %s\n", start.UTC().Format(time.RFC3339))
	fmt.Fprintf(logFile, "# command: sh -lc %s\n", script)
	fmt.Fprintf(logFile, "# cwd: %s\n", workDir)
	fmt.Fprintf(logFile, "# ---\n\n")

	// Apply timeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build command: sh -lc <script>
	cmd := osexec.CommandContext(ctx, "sh", "-lc", script)
	cmd.Dir = workDir

	// Set stdout/stderr to log file
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Open /dev/null for stdin
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		logFile.Close()
		return setupResult{ExitCode: -1, Failed: true}
	}
	cmd.Stdin = devnull
	defer devnull.Close()

	// Build environment: inherit + overlay AGENCY_* vars
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = setEnvVar(cmd.Env, k, v)
	}

	// Run command
	runErr := cmd.Run()
	duration := time.Since(start)
	durationMs := duration.Milliseconds()

	// Close log file
	logFile.Close()

	result := setupResult{
		DurationMs: durationMs,
	}

	if runErr != nil {
		// Check for timeout
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -1
			result.TimedOut = true
			result.Failed = true
			return result
		}

		// Check for exit error
		var exitErr *osexec.ExitError
		if stderrors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.Failed = true
			return result
		}

		// Other error (failed to start)
		result.ExitCode = -1
		result.Failed = true
		return result
	}

	result.ExitCode = 0
	result.Failed = false
	return result
}

// setEnvVar sets or replaces an environment variable in the env slice.
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if len(e) > len(prefix) && e[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// buildSetupEnv builds the environment variables for the setup script.
func buildSetupEnv(st *pipeline.PipelineState, logsDir string) map[string]string {
	dotAgencyDir := filepath.Join(st.WorktreePath, ".agency")
	outputDir := filepath.Join(dotAgencyDir, "out")

	env := map[string]string{
		"AGENCY_RUN_ID":         st.RunID,
		"AGENCY_TITLE":          st.Title,
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
		// Best effort: try to kill the session
		s.cr.Run(ctx, "tmux", []string{"kill-session", "-t", sessionName}, exec.RunOpts{})
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
