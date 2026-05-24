package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/store"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const (
	claudeModelArgFlag                 = "--model"
	claudeEffortArgFlag                = "--effort"
	claudePermissionModeArgFlag        = "--permission-mode"
	claudeDangerousSkipPermissionsFlag = "--dangerously-skip-permissions"

	codexModelArgFlag             = "--model"
	codexConfigArgFlag            = "--config"
	codexReasoningEffortConfigKey = "model_reasoning_effort"
)

type startRunnerConfigOpts struct {
	Runner           string
	RunnerArgs       []string
	Model            string
	Effort           string
	PermissionMode   string
	AgencyConfigPath string
	Headless         bool
}

// startModeOptions carries the mode-related CLI flags shared by all start
// commands. Pass IsInteractive nil to default to checking os.Stdin.
type startModeOptions struct {
	Mode          string
	Prompt        string
	PromptFile    string
	Detached      bool
	IsInteractive func() bool
}

// validateStartMode normalizes opts.Mode against defaultMode, then enforces
// the shared start/retry rules: --detached only with headed mode, headed mode
// rejects --prompt/--prompt-file, and attached headed runs require a TTY. The
// noun ("agent start", "task start", "task retry") is embedded in user-facing
// error messages.
func validateStartMode(opts startModeOptions, defaultMode, noun string) (mode string, headless bool, err error) {
	mode = strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = defaultMode
	}
	headless = mode == string(store.RunnerModeHeadless)
	switch mode {
	case string(store.RunnerModeHeadless):
		if opts.Detached {
			return "", false, errors.NewWithDetails(errors.EUsage, "--detached is only valid with --mode headed", map[string]string{"hint": "omit --detached or pass --mode headed"})
		}
	case string(store.RunnerModeHeaded):
		if strings.TrimSpace(opts.Prompt) != "" || strings.TrimSpace(opts.PromptFile) != "" {
			return "", false, errors.NewWithDetails(errors.EUsage, "headed "+noun+" does not accept a prompt", map[string]string{"hint": "omit --prompt/--prompt-file or use --mode headless"})
		}
		if !opts.Detached {
			isInteractive := opts.IsInteractive
			if isInteractive == nil {
				isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) }
			}
			if !isInteractive() {
				return "", false, errors.NewWithDetails(errors.ENotInteractive, "headed "+noun+" requires an interactive terminal", map[string]string{"hint": "re-run in an interactive terminal or pass --detached"})
			}
		}
	default:
		return "", false, errors.New(errors.EInvalidArgument, "mode must be headless or headed")
	}
	return mode, headless, nil
}

func resolveAgentRunner(input, defaultRunner string) (string, error) {
	runner := strings.TrimSpace(input)
	if runner == "" {
		runner = strings.TrimSpace(defaultRunner)
	}
	if runner == "" {
		return "", errors.New(errors.EUsage, "runner is required; pass --runner or set defaults.runner")
	}

	canonicalRunner, err := runners.Canonicalize(runner)
	if err != nil {
		return "", errors.NewWithDetails(
			errors.EUsage,
			"invalid runner: "+runner,
			map[string]string{
				"runner": runner,
				"valid":  strings.Join(runners.CanonicalIDs(), ", "),
			},
		)
	}
	return canonicalRunner, nil
}

func resolveStartRunnerAndArgs(ctx context.Context, fsys fs.FS, cwd string, ns *daemonNavSetup, repoRoot, repoID string, opts startRunnerConfigOpts) (string, []string, error) {
	userCfg := config.UserConfig{}
	userCfgLoaded := false
	loadUserCfg := func(required bool) error {
		if userCfgLoaded {
			return nil
		}
		cfg, loadErr := config.LoadUserConfig(fsys, ns.dirs.ConfigDir)
		if loadErr != nil {
			if !required && errors.GetCode(loadErr) == errors.ENoUserConfig {
				return nil
			}
			return loadErr
		}
		userCfg = cfg
		userCfgLoaded = true
		return nil
	}
	if strings.TrimSpace(opts.Runner) == "" {
		if err := loadUserCfg(true); err != nil {
			return "", nil, err
		}
	}
	runner, err := resolveAgentRunner(opts.Runner, userCfg.Defaults.Runner)
	if err != nil {
		return "", nil, err
	}

	if repoID == "" {
		repo, err := ns.client.RegisterRepo(ctx, repoRoot)
		if err != nil {
			return "", nil, err
		}
		repoID = repo.Data.RepoID
	}

	agencyConfigPath := strings.TrimSpace(opts.AgencyConfigPath)
	if agencyConfigPath != "" && !filepath.IsAbs(agencyConfigPath) {
		agencyConfigPath = filepath.Join(cwd, agencyConfigPath)
	}
	shouldResolveAgencyConfig := agencyConfigPath != ""
	if !shouldResolveAgencyConfig {
		repoAgencyConfigPath := filepath.Join(repoRoot, "agency.json")
		if _, err := fsys.Stat(repoAgencyConfigPath); err == nil || !os.IsNotExist(err) {
			shouldResolveAgencyConfig = true
		}
	}
	if !shouldResolveAgencyConfig {
		localAgencyConfigPath := config.LocalAgencyConfigPath(ns.dirs.ConfigDir, repoID)
		if _, err := fsys.Stat(localAgencyConfigPath); err == nil || !os.IsNotExist(err) {
			shouldResolveAgencyConfig = true
		}
	}

	model := strings.TrimSpace(opts.Model)
	effort := strings.TrimSpace(opts.Effort)
	permissionMode := strings.TrimSpace(opts.PermissionMode)
	if model == "" || effort == "" || permissionMode == "" {
		if err := loadUserCfg(false); err != nil {
			return "", nil, err
		}
	}
	if shouldResolveAgencyConfig {
		resolvedAgencyConfig, err := config.ResolveAgencyConfig(fsys, repoRoot, ns.dirs.ConfigDir, repoID, agencyConfigPath)
		if err != nil {
			return "", nil, err
		}
		if runnerDefaults, ok := resolvedAgencyConfig.Config.RunnerDefaults[runner]; ok {
			if model == "" {
				model = runnerDefaults.Model
			}
			if effort == "" {
				effort = runnerDefaults.Effort
			}
		}
	}
	if rd, ok := userCfg.RunnerDefaults[runner]; ok {
		if model == "" {
			model = rd.Model
		}
		if effort == "" {
			effort = rd.Effort
		}
		if permissionMode == "" {
			permissionMode = rd.PermissionMode
		}
	}

	runnerArgs, err := resolveEffectiveRunnerArgs(runner, opts.RunnerArgs, model, effort, permissionMode, opts.Headless)
	if err != nil {
		return "", nil, err
	}
	return runner, runnerArgs, nil
}

func resolveEffectiveRunnerArgs(runner string, runnerArgs []string, model, effort, permissionMode string, headless bool) ([]string, error) {
	canonicalRunner, err := runners.Canonicalize(runner)
	if err != nil {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"invalid runner: "+runner,
			map[string]string{
				"runner": runner,
				"valid":  strings.Join(runners.CanonicalIDs(), ", "),
			},
		)
	}

	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	permissionMode = strings.TrimSpace(permissionMode)

	supportsModel := canonicalRunner == runners.RunnerClaudeCode || canonicalRunner == runners.RunnerCodex || canonicalRunner == runners.RunnerCursor
	supportsEffort := canonicalRunner == runners.RunnerClaudeCode || canonicalRunner == runners.RunnerCodex

	if !supportsModel {
		if model != "" || effort != "" || permissionMode != "" {
			return nil, errors.NewWithDetails(
				errors.EUsage,
				fmt.Sprintf("--model is supported for runners %s; --effort is supported for runners %s; --permission-mode is supported for runner %s",
					strings.Join([]string{runners.RunnerClaudeCode, runners.RunnerCodex, runners.RunnerCursor}, ", "),
					strings.Join([]string{runners.RunnerClaudeCode, runners.RunnerCodex}, ", "),
					runners.RunnerClaudeCode,
				),
				map[string]string{
					"runner": canonicalRunner,
					"hint":   "for other runners, use passthrough args only",
				},
			)
		}
		return slices.Clone(runnerArgs), nil
	}
	if !supportsEffort && effort != "" {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--effort is not supported for runner "+canonicalRunner,
			map[string]string{
				"runner": canonicalRunner,
				"hint":   "select thinking-capable models via --model (for example: sonnet-4.6-thinking)",
			},
		)
	}
	if !supportsEffort {
		effort = ""
	}
	if canonicalRunner != runners.RunnerClaudeCode && permissionMode != "" {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--permission-mode is not supported for runner "+canonicalRunner,
			map[string]string{
				"runner": canonicalRunner,
			},
		)
	}

	out := slices.Clone(runnerArgs)
	switch canonicalRunner {
	case runners.RunnerClaudeCode:
		for _, arg := range runnerArgs {
			switch {
			case arg == claudeModelArgFlag || strings.HasPrefix(arg, claudeModelArgFlag+"="):
				return nil, errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+claudeModelArgFlag+"' cannot be passed via runner_args",
					map[string]string{
						"runner": canonicalRunner,
						"flag":   claudeModelArgFlag,
						"hint":   "use --model instead of --runner-arg",
					},
				)
			case arg == claudeEffortArgFlag || strings.HasPrefix(arg, claudeEffortArgFlag+"="):
				return nil, errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+claudeEffortArgFlag+"' cannot be passed via runner_args",
					map[string]string{
						"runner": canonicalRunner,
						"flag":   claudeEffortArgFlag,
						"hint":   "use --effort instead of --runner-arg",
					},
				)
			case arg == claudePermissionModeArgFlag || strings.HasPrefix(arg, claudePermissionModeArgFlag+"="):
				return nil, errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+claudePermissionModeArgFlag+"' cannot be passed via runner_args",
					map[string]string{
						"runner": canonicalRunner,
						"flag":   claudePermissionModeArgFlag,
						"hint":   "use --permission-mode instead of --runner-arg",
					},
				)
			case arg == claudeDangerousSkipPermissionsFlag || strings.HasPrefix(arg, claudeDangerousSkipPermissionsFlag+"="):
				return nil, errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+claudeDangerousSkipPermissionsFlag+"' cannot be passed via runner_args",
					map[string]string{
						"runner": canonicalRunner,
						"flag":   claudeDangerousSkipPermissionsFlag,
						"hint":   "use --permission-mode bypassPermissions instead of --runner-arg",
					},
				)
			}
		}
		switch permissionMode {
		case "":
			if headless {
				permissionMode = "bypassPermissions"
			}
		case "default", "acceptEdits", "plan", "auto", "dontAsk", "bypassPermissions":
		default:
			return nil, errors.NewWithDetails(
				errors.EUsage,
				"invalid Claude permission mode: "+permissionMode,
				map[string]string{
					"runner": canonicalRunner,
					"hint":   "valid Claude permission modes: default, acceptEdits, plan, auto, dontAsk, bypassPermissions",
				},
			)
		}
		if headless && (permissionMode == "default" || permissionMode == "acceptEdits" || permissionMode == "plan") {
			return nil, errors.NewWithDetails(
				errors.EUsage,
				"headless Claude requires an autonomous permission mode",
				map[string]string{
					"runner": canonicalRunner,
					"hint":   "use --permission-mode auto, dontAsk, or bypassPermissions",
				},
			)
		}
		if model != "" {
			out = append(out, claudeModelArgFlag, model)
		}
		if effort != "" {
			out = append(out, claudeEffortArgFlag, effort)
		}
		if permissionMode != "" {
			out = append(out, claudePermissionModeArgFlag, permissionMode)
		}
	case runners.RunnerCodex:
		if model != "" {
			out = append(out, codexModelArgFlag, model)
		}
		if effort != "" {
			out = append(out, codexConfigArgFlag, fmt.Sprintf("%s=%s", codexReasoningEffortConfigKey, effort))
		}
	case runners.RunnerCursor:
		if model != "" {
			out = append(out, claudeModelArgFlag, model)
		}
	default:
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"invalid runner: "+canonicalRunner,
			map[string]string{
				"runner": canonicalRunner,
				"valid":  strings.Join(runners.CanonicalIDs(), ", "),
			},
		)
	}

	return out, nil
}

type commandJSONBase struct {
	OK              bool   `json:"ok"`
	ErrorCode       string `json:"error_code"`
	Message         string `json:"message"`
	Hint            string `json:"hint"`
	RequestID       string `json:"request_id"`
	APIVersion      int    `json:"api_version"`
	BuildVersion    string `json:"build_version"`
	ClientRequestID string `json:"client_request_id"`
}

// agentStartJSON is the shared CLI JSON shape for agent start (headed/headless)
// and agent recreate. Mode-specific fields use omitempty: TmuxSession is set
// for headed/recreate; PID/PGID are set for headless.
type agentStartJSON struct {
	commandJSONBase
	InvocationID     string           `json:"invocation_id,omitempty"`
	RepoID           string           `json:"repo_id,omitempty"`
	RepoName         string           `json:"repo_name,omitempty"`
	WorktreeID       string           `json:"worktree_id,omitempty"`
	WorktreeName     string           `json:"worktree_name,omitempty"`
	SandboxPath      string           `json:"sandbox_path,omitempty"`
	ExecutionProfile string           `json:"execution_profile,omitempty"`
	CheckoutRoot     string           `json:"checkout_root,omitempty"`
	CustomEnvKeys    []string         `json:"custom_env_keys,omitempty"`
	PID              int              `json:"pid,omitempty"`
	PGID             int              `json:"pgid,omitempty"`
	TmuxSession      string           `json:"tmux_session,omitempty"`
	DaemonInstanceID string           `json:"daemon_instance_id,omitempty"`
	AlreadyRunning   bool             `json:"already_running,omitempty"`
	LogPaths         *daemon.LogPaths `json:"log_paths,omitempty"`
}

func agentStartHeadedJSON(resp *daemon.ControlPlaneStartHeadedResponse) agentStartJSON {
	return agentStartJSON{
		commandJSONBase:  newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, resp.ClientRequestID, resp.RequestID),
		InvocationID:     resp.InvocationID,
		RepoID:           resp.RepoID,
		RepoName:         resp.RepoName,
		WorktreeID:       resp.WorktreeID,
		WorktreeName:     resp.WorktreeName,
		SandboxPath:      resp.SandboxPath,
		ExecutionProfile: resp.ExecutionProfile,
		CheckoutRoot:     resp.CheckoutRoot,
		CustomEnvKeys:    slices.Clone(resp.CustomEnvKeys),
		TmuxSession:      resp.TmuxSession,
		DaemonInstanceID: resp.DaemonInstanceID,
		AlreadyRunning:   resp.AlreadyRunning,
		LogPaths:         resp.LogPaths,
	}
}

func agentStartHeadlessJSON(resp *daemon.ControlPlaneStartResponse) agentStartJSON {
	return agentStartJSON{
		commandJSONBase:  newCommandJSONSuccess(resp.APIVersion, resp.BuildVersion, resp.ClientRequestID, resp.RequestID),
		InvocationID:     resp.InvocationID,
		RepoID:           resp.RepoID,
		RepoName:         resp.RepoName,
		WorktreeID:       resp.WorktreeID,
		WorktreeName:     resp.WorktreeName,
		SandboxPath:      resp.SandboxPath,
		ExecutionProfile: resp.ExecutionProfile,
		CheckoutRoot:     resp.CheckoutRoot,
		CustomEnvKeys:    slices.Clone(resp.CustomEnvKeys),
		PID:              resp.PID,
		PGID:             resp.PGID,
		DaemonInstanceID: resp.DaemonInstanceID,
		AlreadyRunning:   resp.AlreadyRunning,
		LogPaths:         resp.LogPaths,
	}
}

// worktreeLabel formats a worktree as "name (id)" or just "id" if name is empty.
func worktreeLabel(name, id string) string {
	if strings.TrimSpace(name) == "" {
		return id
	}
	return name + " (" + id + ")"
}

// printAgentStartLines writes the shared body of the agent start/recreate
// human output: invocation_id, optional name/runner, mode, worktree, profile,
// checkout_root, sandbox_path. Callers write the heading and the trailing
// mode-specific fields (tmux_session for headed, pid/logs for headless).
func printAgentStartLines(w io.Writer, invocationID, invocationName, runner, mode, worktreeName, worktreeID, executionProfile, checkoutRoot, sandboxPath string) {
	_, _ = fmt.Fprintf(w, "  invocation_id:  %s\n", invocationID)
	if invocationName != "" {
		_, _ = fmt.Fprintf(w, "  name:           %s\n", invocationName)
	}
	if runner != "" {
		_, _ = fmt.Fprintf(w, "  runner:         %s\n", runner)
	}
	_, _ = fmt.Fprintf(w, "  mode:           %s\n", mode)
	_, _ = fmt.Fprintf(w, "  worktree:       %s\n", worktreeLabel(worktreeName, worktreeID))
	_, _ = fmt.Fprintf(w, "  profile:        %s\n", executionProfile)
	_, _ = fmt.Fprintf(w, "  checkout_root:  %s\n", checkoutRoot)
	_, _ = fmt.Fprintf(w, "  sandbox_path:   %s\n", sandboxPath)
}

func newCommandJSONSuccess(apiVersion int, buildVersion, clientRequestID, requestID string) commandJSONBase {
	if apiVersion <= 0 {
		apiVersion = daemon.APIVersion
	}
	if buildVersion == "" {
		buildVersion = version.FullVersion()
	}
	return commandJSONBase{
		OK:              true,
		ErrorCode:       "",
		Message:         "",
		Hint:            "",
		RequestID:       requestID,
		APIVersion:      apiVersion,
		BuildVersion:    buildVersion,
		ClientRequestID: clientRequestID,
	}
}

func writeCommandJSON(w io.Writer, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// writeInvocationActionJSON renders the canonical {invocation_id} JSON success body
// used by stop/kill/discard and other invocation-action commands.
func writeInvocationActionJSON(w io.Writer, env daemon.ResponseEnvelope, invocationID string) error {
	return writeCommandJSON(w, struct {
		commandJSONBase
		InvocationID string `json:"invocation_id,omitempty"`
	}{
		commandJSONBase: newCommandJSONSuccess(env.APIVersion, env.BuildVersion, "", env.RequestID),
		InvocationID:    invocationID,
	})
}

// commandFail returns the error handler used by command entrypoints. When
// jsonMode is true, errors are rendered as a JSON envelope to stdout and the
// returned error is nil. When false, the error passes through unchanged.
func commandFail(stdout io.Writer, jsonMode bool) func(error) error {
	return func(err error) error {
		if err == nil || !jsonMode {
			return err
		}
		return writeCommandJSONError(stdout, err)
	}
}

func writeCommandJSONError(w io.Writer, err error) error {
	code := errors.CodeOr(err, errors.EInternal)
	payload := commandJSONBase{
		OK:              false,
		ErrorCode:       string(code),
		Message:         err.Error(),
		Hint:            "",
		RequestID:       "",
		APIVersion:      daemon.APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: "",
	}
	if ae, ok := errors.AsAgencyError(err); ok {
		payload.Message = ae.Msg
		if ae.Details != nil {
			payload.Hint = ae.Details["hint"]
			payload.RequestID = ae.Details["request_id"]
		}
	}
	return writeCommandJSON(w, payload)
}
