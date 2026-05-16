package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
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

func resolveAgentRunner(input, defaultRunner string) (string, error) {
	runner := strings.TrimSpace(input)
	if runner == "" {
		runner = strings.TrimSpace(defaultRunner)
	}
	if runner == "" {
		runner = runners.RunnerClaudeCode
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
		repoID, err = repoIDForRepoRoot(ctx, ns.client, repoRoot)
		if err != nil {
			return "", nil, err
		}
	}

	agencyConfigPath := strings.TrimSpace(opts.AgencyConfigPath)
	if agencyConfigPath != "" && !filepath.IsAbs(agencyConfigPath) {
		agencyConfigPath = filepath.Join(cwd, agencyConfigPath)
	}
	shouldResolveAgencyConfig := shouldResolveStartAgencyConfig(fsys, ns.dirs.ConfigDir, repoRoot, repoID, agencyConfigPath)

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

func shouldResolveStartAgencyConfig(fsys fs.FS, configDir, repoRoot, repoID, agencyConfigPath string) bool {
	if agencyConfigPath != "" {
		return true
	}
	repoAgencyConfigPath := filepath.Join(repoRoot, "agency.json")
	if _, err := fsys.Stat(repoAgencyConfigPath); err == nil || !os.IsNotExist(err) {
		return true
	}
	localAgencyConfigPath := config.LocalAgencyConfigPath(configDir, repoID)
	if _, err := fsys.Stat(localAgencyConfigPath); err == nil || !os.IsNotExist(err) {
		return true
	}
	return false
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
		return append([]string(nil), runnerArgs...), nil
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

	out := append([]string(nil), runnerArgs...)
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
