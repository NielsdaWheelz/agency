// Package commands implements agency CLI commands.
// This file implements agent commands (Slice 8 PR-02/03).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/identity"
	"github.com/NielsdaWheelz/agency/internal/integrationworktree"
	"github.com/NielsdaWheelz/agency/internal/invocation"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/tmux"
)

// AgentStartOpts holds options for the agent start command.
type AgentStartOpts struct {
	// WorktreeRef is the integration worktree reference (name, id, or prefix).
	WorktreeRef string

	// Runner is the runner type (claude, codex).
	Runner string

	// Headless indicates whether to run in headless mode.
	Headless bool

	// InvocationName is an optional human-readable label.
	InvocationName string

	// Detached starts but does not attach (headed mode only).
	Detached bool

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStart starts a new agent invocation.
// For headed mode (default): creates sandbox, launches tmux session, optionally attaches.
// For headless mode: creates sandbox (runner execution added in PR-04).
func AgentStart(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStartOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve integration worktree
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	wtSvc := integrationworktree.NewService(st, cr, fsys, time.Now)

	wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
	if err != nil {
		return err
	}

	if wtRecord.Broken || wtRecord.Meta == nil {
		return errors.NewWithDetails(
			errors.EWorktreeBroken,
			"integration worktree exists but meta.json is unreadable or invalid",
			map[string]string{
				"worktree_id":  wtRecord.WorktreeID,
				"worktree_dir": wtRecord.WorktreeDir,
			},
		)
	}

	// Verify the worktree is in present state
	if wtRecord.Meta.State != store.WorktreeStatePresent {
		return errors.NewWithDetails(
			errors.EWorktreeNotFound,
			"integration worktree is archived",
			map[string]string{
				"worktree_id": wtRecord.WorktreeID,
				"state":       string(wtRecord.Meta.State),
				"hint":        "use a present (non-archived) integration worktree",
			},
		)
	}

	// Determine runner mode
	mode := store.RunnerModeHeaded
	if opts.Headless {
		mode = store.RunnerModeHeadless
	}

	// Validate runner
	runner := opts.Runner
	if runner == "" {
		runner = "claude"
	}
	if runner != "claude" && runner != "codex" {
		return errors.NewWithDetails(
			errors.EUsage,
			"invalid runner: "+runner,
			map[string]string{
				"runner": runner,
				"valid":  "claude, codex",
			},
		)
	}

	// Create invocation service and create invocation
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	result, err := invSvc.Create(ctx, invocation.CreateOpts{
		IntegrationWorktreeID:   wtRecord.WorktreeID,
		IntegrationWorktreeMeta: wtRecord.Meta,
		RepoRoot:                repoRoot.Path,
		RepoID:                  repoIdentity.RepoID,
		Runner:                  runner,
		Mode:                    mode,
		InvocationName:          opts.InvocationName,
	})
	if err != nil {
		return err
	}

	// For headed mode, launch tmux session
	if mode == store.RunnerModeHeaded {
		// Resolve runner command
		userCfg, _, _ := config.LoadUserConfig(fsys, dirs.ConfigDir)
		runnerCmd, err := config.ResolveRunnerCmd(cr, fsys, dirs.ConfigDir, userCfg, runner)
		if err != nil {
			// Mark invocation as failed
			_ = st.UpdateInvocationMeta(repoIdentity.RepoID, result.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "start_failed"
				meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			})
			return err
		}

		// Get tmux client
		tmuxClient := opts.TmuxClient
		if tmuxClient == nil {
			tmuxClient = tmux.NewExecClient(cr)
		}

		sessionName := tmux.SessionName(result.InvocationID)

		// Preflight: check if session already exists (guards against leaked sessions)
		exists, err := tmuxClient.HasSession(ctx, sessionName)
		if err != nil {
			// Non-fatal error checking, proceed anyway
			_, _ = fmt.Fprintf(stderr, "warning: could not check for existing tmux session: %v\n", err)
		} else if exists {
			// Mark invocation as failed
			_ = st.UpdateInvocationMeta(repoIdentity.RepoID, result.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "start_failed"
				meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			})
			return errors.NewWithDetails(
				errors.ETmuxSessionExists,
				"tmux session already exists",
				map[string]string{
					"session_name":  sessionName,
					"invocation_id": result.InvocationID,
					"hint":          "a tmux session with this name already exists; kill it with 'tmux kill-session -t " + sessionName + "' or use a different invocation",
				},
			)
		}

		// Create tmux session with CWD = sandbox tree, argv = [runnerCmd]
		// No shell wrapping - pass runner command directly to tmux
		err = tmuxClient.NewSession(ctx, sessionName, result.SandboxPath, []string{runnerCmd})
		if err != nil {
			// Mark invocation as failed
			_ = st.UpdateInvocationMeta(repoIdentity.RepoID, result.InvocationID, func(meta *store.InvocationMeta) {
				meta.Status = store.InvocationStatusFailed
				meta.ExitReason = "start_failed"
				meta.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			})
			return errors.WrapWithDetails(
				errors.EInvocationStartFailed,
				"failed to create tmux session",
				err,
				map[string]string{
					"session_name":  sessionName,
					"sandbox_path":  result.SandboxPath,
					"runner_cmd":    runnerCmd,
					"invocation_id": result.InvocationID,
				},
			)
		}

		// Update invocation meta: status = "running", tmux_session set
		err = st.UpdateInvocationMeta(repoIdentity.RepoID, result.InvocationID, func(meta *store.InvocationMeta) {
			meta.Status = store.InvocationStatusRunning
			meta.TmuxSession = sessionName
		})
		if err != nil {
			// Best-effort: try to kill the session we just created
			_ = tmuxClient.KillSession(ctx, sessionName)
			return err
		}

		// Output result
		_, _ = fmt.Fprintf(stdout, "Started agent invocation\n")
		_, _ = fmt.Fprintf(stdout, "  invocation_id:  %s\n", result.InvocationID)
		if opts.InvocationName != "" {
			_, _ = fmt.Fprintf(stdout, "  name:           %s\n", opts.InvocationName)
		}
		_, _ = fmt.Fprintf(stdout, "  runner:         %s\n", runner)
		_, _ = fmt.Fprintf(stdout, "  mode:           %s\n", mode)
		_, _ = fmt.Fprintf(stdout, "  worktree:       %s (%s)\n", wtRecord.Meta.Name, wtRecord.WorktreeID)
		_, _ = fmt.Fprintf(stdout, "  sandbox_path:   %s\n", result.SandboxPath)
		_, _ = fmt.Fprintf(stdout, "  tmux_session:   %s\n", sessionName)

		// If not detached, attach to the session
		if !opts.Detached {
			_, _ = fmt.Fprintf(stdout, "\nAttaching to tmux session... (detach with Ctrl+b, d)\n")
			if err := tmuxClient.Attach(ctx, sessionName); err != nil {
				// Attach failed but session exists - not a fatal error
				_, _ = fmt.Fprintf(stderr, "warning: could not attach to tmux session: %v\n", err)
				_, _ = fmt.Fprintf(stderr, "Use 'agency agent attach %s' to attach later.\n", result.InvocationID[:8])
			}
		} else {
			_, _ = fmt.Fprintf(stdout, "\nSession started in detached mode.\n")
			_, _ = fmt.Fprintf(stdout, "Use 'agency agent attach %s' to attach.\n", result.InvocationID[:8])
		}

		return nil
	}

	// Headless mode - sandbox created, runner execution in PR-04
	_, _ = fmt.Fprintf(stdout, "Created agent invocation\n")
	_, _ = fmt.Fprintf(stdout, "  invocation_id: %s\n", result.InvocationID)
	if opts.InvocationName != "" {
		_, _ = fmt.Fprintf(stdout, "  name:          %s\n", opts.InvocationName)
	}
	_, _ = fmt.Fprintf(stdout, "  runner:        %s\n", runner)
	_, _ = fmt.Fprintf(stdout, "  mode:          %s\n", mode)
	_, _ = fmt.Fprintf(stdout, "  worktree:      %s (%s)\n", wtRecord.Meta.Name, wtRecord.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "  sandbox_path:  %s\n", result.SandboxPath)
	_, _ = fmt.Fprintf(stdout, "  sandbox_branch: %s\n", result.SandboxBranch)
	_, _ = fmt.Fprintf(stdout, "  base_commit:   %s\n", result.BaseCommit[:12])

	_, _ = fmt.Fprintf(stderr, "\nNote: Headless runner execution not yet implemented (PR-04).\n")
	_, _ = fmt.Fprintf(stderr, "Use 'agency agent show %s' to view invocation details.\n", result.InvocationID[:8])

	return nil
}

// AgentLSOpts holds options for the agent ls command.
type AgentLSOpts struct {
	// WorktreeRef filters by integration worktree (optional).
	WorktreeRef string

	// All includes finished (landed/discarded) invocations.
	All bool

	// JSON outputs as JSON.
	JSON bool
}

// AgentLS lists agent invocations.
func AgentLS(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentLSOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve worktree filter if provided
	var worktreeFilter string
	if opts.WorktreeRef != "" {
		st := store.NewStore(fsys, dirs.DataDir, time.Now)
		wtSvc := integrationworktree.NewService(st, cr, fsys, time.Now)
		wtRecord, err := wtSvc.Resolve(repoIdentity.RepoID, opts.WorktreeRef, false)
		if err != nil {
			return err
		}
		worktreeFilter = wtRecord.WorktreeID
	}

	// Scan invocations
	var records []store.InvocationRecord
	if worktreeFilter != "" {
		records, err = store.ScanInvocationsForWorktree(dirs.DataDir, repoIdentity.RepoID, worktreeFilter)
	} else {
		records, err = store.ScanInvocationsForRepo(dirs.DataDir, repoIdentity.RepoID)
	}
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to scan invocations", err)
	}

	// Filter by status unless --all
	var filtered []store.InvocationRecord
	for _, r := range records {
		if r.Broken {
			if opts.All {
				filtered = append(filtered, r)
			}
			continue
		}
		if r.Meta != nil {
			landed := r.Meta.LandingStatus == store.LandingStatusLanded
			discarded := r.Meta.LandingStatus == store.LandingStatusDiscarded
			if (landed || discarded) && !opts.All {
				continue
			}
		}
		filtered = append(filtered, r)
	}

	// Output
	if opts.JSON {
		return writeAgentLSJSON(stdout, filtered)
	}

	return writeAgentLSHuman(stdout, filtered)
}

func writeAgentLSJSON(w io.Writer, records []store.InvocationRecord) error {
	type jsonRecord struct {
		InvocationID          string `json:"invocation_id"`
		InvocationName        string `json:"invocation_name,omitempty"`
		IntegrationWorktreeID string `json:"integration_worktree_id,omitempty"`
		Runner                string `json:"runner,omitempty"`
		Mode                  string `json:"mode,omitempty"`
		Status                string `json:"status,omitempty"`
		LandingStatus         string `json:"landing_status,omitempty"`
		SandboxPath           string `json:"sandbox_path,omitempty"`
		StartedAt             string `json:"started_at,omitempty"`
		SandboxExists         bool   `json:"sandbox_exists"`
		Broken                bool   `json:"broken,omitempty"`
	}

	out := make([]jsonRecord, len(records))
	for i, r := range records {
		jr := jsonRecord{
			InvocationID:  r.InvocationID,
			SandboxExists: r.SandboxExists,
			Broken:        r.Broken,
		}
		if r.Meta != nil {
			jr.InvocationName = r.Meta.InvocationName
			jr.IntegrationWorktreeID = r.Meta.IntegrationWorktreeID
			jr.Runner = r.Meta.Runner
			jr.Mode = string(r.Meta.Mode)
			jr.Status = string(r.Meta.Status)
			jr.LandingStatus = string(r.Meta.LandingStatus)
			jr.SandboxPath = r.Meta.SandboxPath
			jr.StartedAt = r.Meta.StartedAt
		}
		out[i] = jr
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeAgentLSHuman(w io.Writer, records []store.InvocationRecord) error {
	if len(records) == 0 {
		_, _ = fmt.Fprintln(w, "No agent invocations found.")
		return nil
	}

	for _, r := range records {
		if r.Broken {
			_, _ = fmt.Fprintf(w, "%s  [broken]\n", r.InvocationID)
			continue
		}

		name := ""
		if r.Meta.InvocationName != "" {
			name = " (" + r.Meta.InvocationName + ")"
		}

		sandboxStatus := ""
		if !r.SandboxExists {
			sandboxStatus = " [no sandbox]"
		}

		landingStatus := ""
		switch r.Meta.LandingStatus {
		case store.LandingStatusLanded:
			landingStatus = " [landed]"
		case store.LandingStatusDiscarded:
			landingStatus = " [discarded]"
		}

		_, _ = fmt.Fprintf(w, "%s  %s  %s  %s%s%s%s\n",
			r.InvocationID,
			r.Meta.Runner,
			r.Meta.Mode,
			r.Meta.Status,
			name,
			sandboxStatus,
			landingStatus,
		)
	}

	return nil
}

// AgentShowOpts holds options for the agent show command.
type AgentShowOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// JSON outputs as JSON.
	JSON bool
}

// AgentShow shows details of an agent invocation.
func AgentShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentShowOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // show can view any invocation
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
				"hint":           "inspect or remove the directory manually",
			},
		)
	}

	// Output
	if opts.JSON {
		return writeAgentShowJSON(stdout, record)
	}

	return writeAgentShowHuman(stdout, record)
}

func writeAgentShowJSON(w io.Writer, r *store.InvocationRecord) error {
	type jsonRecord struct {
		InvocationID          string `json:"invocation_id"`
		InvocationName        string `json:"invocation_name,omitempty"`
		IntegrationWorktreeID string `json:"integration_worktree_id"`
		SandboxPath           string `json:"sandbox_path"`
		SandboxBranch         string `json:"sandbox_branch"`
		BaseCommit            string `json:"base_commit"`
		Runner                string `json:"runner"`
		Mode                  string `json:"mode"`
		Status                string `json:"status"`
		LandingStatus         string `json:"landing_status,omitempty"`
		StartedAt             string `json:"started_at"`
		FinishedAt            string `json:"finished_at,omitempty"`
		ExitCode              *int   `json:"exit_code,omitempty"`
		SandboxExists         bool   `json:"sandbox_exists"`
		InvocationDir         string `json:"invocation_dir"`
	}

	out := jsonRecord{
		InvocationID:          r.InvocationID,
		InvocationName:        r.Meta.InvocationName,
		IntegrationWorktreeID: r.Meta.IntegrationWorktreeID,
		SandboxPath:           r.Meta.SandboxPath,
		SandboxBranch:         r.Meta.SandboxBranch,
		BaseCommit:            r.Meta.BaseCommit,
		Runner:                r.Meta.Runner,
		Mode:                  string(r.Meta.Mode),
		Status:                string(r.Meta.Status),
		LandingStatus:         string(r.Meta.LandingStatus),
		StartedAt:             r.Meta.StartedAt,
		FinishedAt:            r.Meta.FinishedAt,
		ExitCode:              r.Meta.ExitCode,
		SandboxExists:         r.SandboxExists,
		InvocationDir:         r.InvocationDir,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeAgentShowHuman(w io.Writer, r *store.InvocationRecord) error {
	_, _ = fmt.Fprintf(w, "invocation_id:          %s\n", r.InvocationID)
	if r.Meta.InvocationName != "" {
		_, _ = fmt.Fprintf(w, "name:                   %s\n", r.Meta.InvocationName)
	}
	_, _ = fmt.Fprintf(w, "integration_worktree:   %s\n", r.Meta.IntegrationWorktreeID)
	_, _ = fmt.Fprintf(w, "runner:                 %s\n", r.Meta.Runner)
	_, _ = fmt.Fprintf(w, "mode:                   %s\n", r.Meta.Mode)
	_, _ = fmt.Fprintf(w, "status:                 %s\n", r.Meta.Status)
	if r.Meta.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:         %s\n", r.Meta.LandingStatus)
	}
	_, _ = fmt.Fprintf(w, "started_at:             %s\n", r.Meta.StartedAt)
	if r.Meta.FinishedAt != "" {
		_, _ = fmt.Fprintf(w, "finished_at:            %s\n", r.Meta.FinishedAt)
	}
	if r.Meta.TmuxSession != "" {
		_, _ = fmt.Fprintf(w, "tmux_session:           %s\n", r.Meta.TmuxSession)
	}
	_, _ = fmt.Fprintf(w, "base_commit:            %s\n", r.Meta.BaseCommit)
	_, _ = fmt.Fprintf(w, "sandbox_branch:         %s\n", r.Meta.SandboxBranch)
	_, _ = fmt.Fprintf(w, "sandbox_path:           %s\n", r.Meta.SandboxPath)
	_, _ = fmt.Fprintf(w, "sandbox_exists:         %v\n", r.SandboxExists)
	return nil
}

// AgentAttachOpts holds options for the agent attach command.
type AgentAttachOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentAttach attaches to a running headed invocation's tmux session.
// This is only supported for headed invocations.
func AgentAttach(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentAttachOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // allow attaching to see final state
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	// Verify this is a headed invocation
	if record.Meta.Mode != store.RunnerModeHeaded {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"invocation is headless; attach is only supported for headed invocations",
			map[string]string{
				"invocation_id": record.InvocationID,
				"mode":          string(record.Meta.Mode),
				"hint":          "use 'agency agent logs' to view headless invocation output",
			},
		)
	}

	// Get tmux client
	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	// Get session name
	sessionName := record.Meta.TmuxSession
	if sessionName == "" {
		// Fall back to computed name if not in meta (shouldn't happen for properly started invocations)
		sessionName = tmux.SessionName(record.InvocationID)
	}

	// Check if session exists
	exists, err := tmuxClient.HasSession(ctx, sessionName)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not check tmux session status: %v\n", err)
	}
	if !exists {
		return errors.NewWithDetails(
			errors.ETmuxSessionMissing,
			"tmux session not found",
			map[string]string{
				"session_name":  sessionName,
				"invocation_id": record.InvocationID,
				"hint":          "session may have exited; use 'agency agent kill' to finalize or 'agency agent start' to create a new invocation",
			},
		)
	}

	// Attach to session
	if err := tmuxClient.Attach(ctx, sessionName); err != nil {
		return errors.WrapWithDetails(
			errors.ETmuxAttachFailed,
			"failed to attach to tmux session",
			err,
			map[string]string{
				"session_name":  sessionName,
				"invocation_id": record.InvocationID,
			},
		)
	}

	return nil
}

// AgentStopOpts holds options for the agent stop command.
type AgentStopOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentStop sends a graceful stop signal (Ctrl-C) to a running headed invocation.
// This does not guarantee termination - the runner may ignore the signal.
// For headed mode: sends C-c via tmux send-keys.
// For headless mode: not yet implemented (PR-04).
func AgentStop(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentStopOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: false, // can only stop running invocations
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	// Only headed mode is supported for now
	if record.Meta.Mode != store.RunnerModeHeaded {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"stop is not yet supported for headless invocations",
			map[string]string{
				"invocation_id": record.InvocationID,
				"mode":          string(record.Meta.Mode),
				"hint":          "headless stop will be implemented in PR-04",
			},
		)
	}

	// Get tmux client
	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	// Get session name
	sessionName := record.Meta.TmuxSession
	if sessionName == "" {
		sessionName = tmux.SessionName(record.InvocationID)
	}

	// Send C-c to the session
	err = tmuxClient.SendKeys(ctx, sessionName, []tmux.Key{tmux.KeyCtrlC})
	if err != nil {
		// Session might be missing - log warning but still update meta
		_, _ = fmt.Fprintf(stderr, "warning: could not send interrupt to tmux session: %v\n", err)
	}

	// Update invocation meta: set stop_requested_at and flags.needs_attention
	// Note: status remains "running", exit_reason remains null
	// Actual termination is observed via reconcile (future) or kill
	now := time.Now().UTC().Format(time.RFC3339)
	err = st.UpdateInvocationMeta(repoIdentity.RepoID, record.InvocationID, func(meta *store.InvocationMeta) {
		meta.StopRequestedAt = now
		meta.Flags.NeedsAttention = true
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Stop signal sent to invocation %s\n", record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "Note: The runner may ignore the interrupt. Use 'agency agent kill' to force termination.\n")

	return nil
}

// AgentKillOpts holds options for the agent kill command.
type AgentKillOpts struct {
	// InvocationRef is the invocation reference (id or prefix).
	InvocationRef string

	// TmuxClient is the tmux client to use (optional, uses real client if nil).
	TmuxClient tmux.Client
}

// AgentKill forcefully terminates a running invocation.
// For headed mode: kills the tmux session.
// For headless mode: not yet implemented (PR-04).
func AgentKill(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentKillOpts, stdout, stderr io.Writer) error {
	// Resolve paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)

	// Get repo context
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return errors.New(errors.ENoRepo, "not inside a git repository")
	}
	originInfo := git.GetOriginInfo(ctx, cr, repoRoot.Path)
	repoIdentity := identity.DeriveRepoIdentity(repoRoot.Path, originInfo.URL)

	// Resolve invocation
	st := store.NewStore(fsys, dirs.DataDir, time.Now)
	invSvc := invocation.NewService(st, cr, fsys, time.Now)

	record, err := invSvc.Resolve(repoIdentity.RepoID, opts.InvocationRef, invocation.ResolveOpts{
		IncludeFinished: true, // allow killing already-finished to finalize state
	})
	if err != nil {
		return err
	}

	if record.Broken {
		return errors.NewWithDetails(
			errors.EInvocationBroken,
			"invocation exists but meta.json is unreadable or invalid",
			map[string]string{
				"invocation_id":  record.InvocationID,
				"invocation_dir": record.InvocationDir,
			},
		)
	}

	// Only headed mode is supported for now
	if record.Meta.Mode != store.RunnerModeHeaded {
		return errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"kill is not yet supported for headless invocations",
			map[string]string{
				"invocation_id": record.InvocationID,
				"mode":          string(record.Meta.Mode),
				"hint":          "headless kill will be implemented in PR-04",
			},
		)
	}

	// Get tmux client
	tmuxClient := opts.TmuxClient
	if tmuxClient == nil {
		tmuxClient = tmux.NewExecClient(cr)
	}

	// Get session name
	sessionName := record.Meta.TmuxSession
	if sessionName == "" {
		sessionName = tmux.SessionName(record.InvocationID)
	}

	// Kill the session
	err = tmuxClient.KillSession(ctx, sessionName)
	if err != nil {
		// Session might already be gone - log warning but still update meta
		if !tmux.IsNoSessionErr(err) {
			_, _ = fmt.Fprintf(stderr, "warning: could not kill tmux session: %v\n", err)
		}
	}

	// Update invocation meta: status = "failed", exit_reason = "killed", finished_at = now
	// Note: tmux_session is kept as historical value (not nulled)
	now := time.Now().UTC().Format(time.RFC3339)
	err = st.UpdateInvocationMeta(repoIdentity.RepoID, record.InvocationID, func(meta *store.InvocationMeta) {
		meta.Status = store.InvocationStatusFailed
		meta.ExitReason = "killed"
		meta.FinishedAt = now
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Killed invocation %s\n", record.InvocationID)
	_, _ = fmt.Fprintf(stdout, "Sandbox preserved at: %s\n", record.Meta.SandboxPath)

	return nil
}
