package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/runners"
	"github.com/NielsdaWheelz/agency/internal/version"
)

const (
	claudeModelArgFlag  = "--model"
	claudeEffortArgFlag = "--effort"

	cursorModelArgFlag = "--model"

	codexModelArgFlag              = "--model"
	codexModelShortArgFlag         = "-m"
	codexConfigArgFlag             = "--config"
	codexConfigShortArgFlag        = "-c"
	codexReasoningEffortConfigKey  = "model_reasoning_effort"
	codexReasoningEffortConfigFlag = "--config " + codexReasoningEffortConfigKey
)

type runnerArgOccurrence struct {
	tokens []string
	value  string
}

type claudeRunnerArgsParse struct {
	modelOccurrences  []runnerArgOccurrence
	effortOccurrences []runnerArgOccurrence
	otherArgs         []string
}

type codexRunnerArgsParse struct {
	modelOccurrences  []runnerArgOccurrence
	effortOccurrences []runnerArgOccurrence
	otherArgs         []string
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

func resolveEffectiveRunnerArgs(runner string, runnerArgs []string, cliModel, cliEffort string, defaults config.UserDefaults) ([]string, error) {
	model := strings.TrimSpace(cliModel)
	effort := strings.TrimSpace(cliEffort)
	supportedModelRunners := []string{runners.RunnerClaudeCode, runners.RunnerCodex, runners.RunnerCursor}
	supportedEffortRunners := []string{runners.RunnerClaudeCode, runners.RunnerCodex}

	canonicalRunner, err := runners.Canonicalize(runner)
	if err != nil {
		if model != "" || effort != "" {
			return nil, errors.NewWithDetails(
				errors.EUsage,
				"cannot apply --model/--effort to unrecognized runner: "+runner,
				map[string]string{
					"runner": runner,
					"valid":  strings.Join(runners.CanonicalIDs(), ", "),
				},
			)
		}
		return append([]string(nil), runnerArgs...), nil
	}

	supportsModel := canonicalRunner == runners.RunnerClaudeCode || canonicalRunner == runners.RunnerCodex || canonicalRunner == runners.RunnerCursor
	supportsEffort := canonicalRunner == runners.RunnerClaudeCode || canonicalRunner == runners.RunnerCodex

	if !supportsModel {
		if model != "" || effort != "" {
			return nil, errors.NewWithDetails(
				errors.EUsage,
				fmt.Sprintf("--model is supported for runners %s; --effort is supported for runners %s",
					strings.Join(supportedModelRunners, ", "),
					strings.Join(supportedEffortRunners, ", "),
				),
				map[string]string{
					"runner": canonicalRunner,
					"hint":   "for other runners, pass model/effort flags via --runner-arg",
				},
			)
		}
		return append([]string(nil), runnerArgs...), nil
	}

	if model == "" {
		model = strings.TrimSpace(defaults.Model)
	}
	if supportsEffort && effort == "" {
		effort = strings.TrimSpace(defaults.Effort)
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

	switch canonicalRunner {
	case runners.RunnerClaudeCode:
		return mergeClaudeRunnerArgs(runnerArgs, model, effort)
	case runners.RunnerCodex:
		return mergeCodexRunnerArgs(runnerArgs, model, effort)
	case runners.RunnerCursor:
		return mergeCursorRunnerArgs(runnerArgs, model, effort)
	default:
		return append([]string(nil), runnerArgs...), nil
	}
}

func mergeClaudeRunnerArgs(runnerArgs []string, targetModel, targetEffort string) ([]string, error) {
	parsed, err := parseClaudeRunnerArgs(runnerArgs)
	if err != nil {
		return nil, err
	}

	existingModel, err := resolveSingleRunnerOption(claudeModelArgFlag, parsed.modelOccurrences)
	if err != nil {
		return nil, err
	}
	existingEffort, err := resolveSingleRunnerOption(claudeEffortArgFlag, parsed.effortOccurrences)
	if err != nil {
		return nil, err
	}

	if targetModel != "" && existingModel != "" && existingModel != targetModel {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--model conflicts with value already provided via --runner-arg",
			map[string]string{
				"flag":       claudeModelArgFlag,
				"from_flag":  targetModel,
				"from_arg":   existingModel,
				"hint":       "use one source of truth for model selection",
				"runner_arg": claudeModelArgFlag,
			},
		)
	}
	if targetEffort != "" && existingEffort != "" && existingEffort != targetEffort {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--effort conflicts with value already provided via --runner-arg",
			map[string]string{
				"flag":       claudeEffortArgFlag,
				"from_flag":  targetEffort,
				"from_arg":   existingEffort,
				"hint":       "use one source of truth for effort selection",
				"runner_arg": claudeEffortArgFlag,
			},
		)
	}

	needsRebuild := targetModel != "" || targetEffort != ""
	if !needsRebuild {
		return append([]string(nil), runnerArgs...), nil
	}

	out := append([]string(nil), parsed.otherArgs...)
	switch {
	case targetModel != "":
		out = append(out, claudeModelArgFlag, targetModel)
	case len(parsed.modelOccurrences) == 1:
		out = append(out, parsed.modelOccurrences[0].tokens...)
	}
	switch {
	case targetEffort != "":
		out = append(out, claudeEffortArgFlag, targetEffort)
	case len(parsed.effortOccurrences) == 1:
		out = append(out, parsed.effortOccurrences[0].tokens...)
	}
	return out, nil
}

func mergeCodexRunnerArgs(runnerArgs []string, targetModel, targetEffort string) ([]string, error) {
	targetModel = strings.TrimSpace(targetModel)
	targetEffort = normalizeRunnerArgValue(targetEffort)

	parsed, err := parseCodexRunnerArgs(runnerArgs)
	if err != nil {
		return nil, err
	}

	existingModel, err := resolveSingleRunnerOption(codexModelArgFlag, parsed.modelOccurrences)
	if err != nil {
		return nil, err
	}
	existingEffort, err := resolveSingleRunnerOption(codexReasoningEffortConfigFlag, parsed.effortOccurrences)
	if err != nil {
		return nil, err
	}

	if targetModel != "" && existingModel != "" && existingModel != targetModel {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--model conflicts with value already provided via --runner-arg",
			map[string]string{
				"flag":       codexModelArgFlag,
				"from_flag":  targetModel,
				"from_arg":   existingModel,
				"hint":       "use one source of truth for model selection",
				"runner_arg": codexModelArgFlag,
			},
		)
	}
	if targetEffort != "" && existingEffort != "" && existingEffort != targetEffort {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--effort conflicts with value already provided via --runner-arg",
			map[string]string{
				"flag":       codexReasoningEffortConfigFlag,
				"from_flag":  targetEffort,
				"from_arg":   existingEffort,
				"hint":       "use one source of truth for effort selection",
				"runner_arg": codexReasoningEffortConfigFlag,
			},
		)
	}

	needsRebuild := targetModel != "" || targetEffort != ""
	if !needsRebuild {
		return append([]string(nil), runnerArgs...), nil
	}

	out := append([]string(nil), parsed.otherArgs...)
	switch {
	case targetModel != "":
		out = append(out, codexModelArgFlag, targetModel)
	case len(parsed.modelOccurrences) == 1:
		out = append(out, parsed.modelOccurrences[0].tokens...)
	}
	switch {
	case targetEffort != "":
		out = append(out, codexConfigArgFlag, fmt.Sprintf("%s=%s", codexReasoningEffortConfigKey, targetEffort))
	case len(parsed.effortOccurrences) == 1:
		out = append(out, parsed.effortOccurrences[0].tokens...)
	}
	return out, nil
}

func mergeCursorRunnerArgs(runnerArgs []string, targetModel, targetEffort string) ([]string, error) {
	targetModel = strings.TrimSpace(targetModel)
	targetEffort = strings.TrimSpace(targetEffort)

	parsed, err := parseClaudeRunnerArgs(runnerArgs)
	if err != nil {
		return nil, err
	}

	existingModel, err := resolveSingleRunnerOption(cursorModelArgFlag, parsed.modelOccurrences)
	if err != nil {
		return nil, err
	}
	existingEffort, err := resolveSingleRunnerOption(claudeEffortArgFlag, parsed.effortOccurrences)
	if err != nil {
		return nil, err
	}

	if targetEffort != "" || existingEffort != "" {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--effort is not supported for runner "+runners.RunnerCursor,
			map[string]string{
				"runner": runners.RunnerCursor,
				"hint":   "select thinking-capable models via --model (for example: sonnet-4.6-thinking)",
			},
		)
	}
	if targetModel != "" && existingModel != "" && existingModel != targetModel {
		return nil, errors.NewWithDetails(
			errors.EUsage,
			"--model conflicts with value already provided via --runner-arg",
			map[string]string{
				"flag":       cursorModelArgFlag,
				"from_flag":  targetModel,
				"from_arg":   existingModel,
				"hint":       "use one source of truth for model selection",
				"runner_arg": cursorModelArgFlag,
			},
		)
	}

	needsRebuild := targetModel != ""
	if !needsRebuild {
		return append([]string(nil), runnerArgs...), nil
	}

	out := append([]string(nil), parsed.otherArgs...)
	switch {
	case targetModel != "":
		out = append(out, cursorModelArgFlag, targetModel)
	case len(parsed.modelOccurrences) == 1:
		out = append(out, parsed.modelOccurrences[0].tokens...)
	}
	return out, nil
}

func resolveSingleRunnerOption(flag string, occurrences []runnerArgOccurrence) (string, error) {
	if len(occurrences) == 0 {
		return "", nil
	}

	value := occurrences[0].value
	if len(occurrences) == 1 {
		return value, nil
	}

	for _, occurrence := range occurrences[1:] {
		if occurrence.value != value {
			return "", errors.NewWithDetails(
				errors.EUsage,
				fmt.Sprintf("conflicting %s values passed via --runner-arg", flag),
				map[string]string{
					"flag": flag,
					"hint": "pass this option only once",
				},
			)
		}
	}

	return "", errors.NewWithDetails(
		errors.EUsage,
		fmt.Sprintf("duplicate %s passed via --runner-arg", flag),
		map[string]string{
			"flag": flag,
			"hint": "pass this option only once",
		},
	)
}

func normalizeRunnerArgValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') ||
			(trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
			return strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		}
	}
	return trimmed
}

func hasTypedOptionRunnerArgs(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}
		switch {
		case arg == claudeModelArgFlag,
			arg == claudeEffortArgFlag,
			arg == codexModelArgFlag,
			arg == codexModelShortArgFlag,
			arg == codexConfigArgFlag,
			arg == codexConfigShortArgFlag,
			strings.HasPrefix(arg, claudeModelArgFlag+"="),
			strings.HasPrefix(arg, claudeEffortArgFlag+"="),
			strings.HasPrefix(arg, codexModelArgFlag+"="),
			strings.HasPrefix(arg, codexModelShortArgFlag+"="):
			return true
		case strings.HasPrefix(arg, codexConfigArgFlag+"="):
			_, _, ok := parseCodexConfigAssignment(strings.TrimPrefix(arg, codexConfigArgFlag+"="))
			return ok
		case strings.HasPrefix(arg, codexConfigShortArgFlag+"="):
			_, _, ok := parseCodexConfigAssignment(strings.TrimPrefix(arg, codexConfigShortArgFlag+"="))
			return ok
		case arg == codexConfigArgFlag || arg == codexConfigShortArgFlag:
			if i+1 >= len(args) {
				return true
			}
			key, _, ok := parseCodexConfigAssignment(args[i+1])
			return ok && key == codexReasoningEffortConfigKey
		}
	}
	return false
}

func parseClaudeRunnerArgs(args []string) (claudeRunnerArgsParse, error) {
	parsed := claudeRunnerArgsParse{
		otherArgs: make([]string, 0, len(args)),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			parsed.otherArgs = append(parsed.otherArgs, args[i:]...)
			break
		}

		switch {
		case arg == claudeModelArgFlag:
			if i+1 >= len(args) {
				return claudeRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					claudeModelArgFlag+" in --runner-arg requires a value",
					map[string]string{
						"flag": claudeModelArgFlag,
						"hint": "pass --runner-arg \"--model=<value>\" or --runner-arg \"--model\" --runner-arg \"<value>\"",
					},
				)
			}
			value := strings.TrimSpace(args[i+1])
			if value == "" {
				return claudeRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					claudeModelArgFlag+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": claudeModelArgFlag,
					},
				)
			}
			parsed.modelOccurrences = append(parsed.modelOccurrences, runnerArgOccurrence{
				tokens: []string{arg, args[i+1]},
				value:  value,
			})
			i++

		case strings.HasPrefix(arg, claudeModelArgFlag+"="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, claudeModelArgFlag+"="))
			if value == "" {
				return claudeRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					claudeModelArgFlag+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": claudeModelArgFlag,
					},
				)
			}
			parsed.modelOccurrences = append(parsed.modelOccurrences, runnerArgOccurrence{
				tokens: []string{arg},
				value:  value,
			})

		case arg == claudeEffortArgFlag:
			if i+1 >= len(args) {
				return claudeRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					claudeEffortArgFlag+" in --runner-arg requires a value",
					map[string]string{
						"flag": claudeEffortArgFlag,
						"hint": "pass --runner-arg \"--effort=<value>\" or --runner-arg \"--effort\" --runner-arg \"<value>\"",
					},
				)
			}
			value := strings.TrimSpace(args[i+1])
			if value == "" {
				return claudeRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					claudeEffortArgFlag+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": claudeEffortArgFlag,
					},
				)
			}
			parsed.effortOccurrences = append(parsed.effortOccurrences, runnerArgOccurrence{
				tokens: []string{claudeEffortArgFlag, args[i+1]},
				value:  value,
			})
			i++

		case strings.HasPrefix(arg, claudeEffortArgFlag+"="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, claudeEffortArgFlag+"="))
			if value == "" {
				return claudeRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					claudeEffortArgFlag+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": claudeEffortArgFlag,
					},
				)
			}
			parsed.effortOccurrences = append(parsed.effortOccurrences, runnerArgOccurrence{
				tokens: []string{arg},
				value:  value,
			})

		default:
			parsed.otherArgs = append(parsed.otherArgs, arg)
		}
	}

	return parsed, nil
}

func parseCodexRunnerArgs(args []string) (codexRunnerArgsParse, error) {
	parsed := codexRunnerArgsParse{
		otherArgs: make([]string, 0, len(args)),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			parsed.otherArgs = append(parsed.otherArgs, args[i:]...)
			break
		}

		switch {
		case arg == codexModelArgFlag || arg == codexModelShortArgFlag:
			if i+1 >= len(args) {
				return codexRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					arg+" in --runner-arg requires a value",
					map[string]string{
						"flag": arg,
						"hint": "pass --runner-arg \"--model=<value>\" or --runner-arg \"-m\" --runner-arg \"<value>\"",
					},
				)
			}
			value := strings.TrimSpace(args[i+1])
			if value == "" {
				return codexRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					arg+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": arg,
					},
				)
			}
			parsed.modelOccurrences = append(parsed.modelOccurrences, runnerArgOccurrence{
				tokens: []string{arg, args[i+1]},
				value:  value,
			})
			i++

		case strings.HasPrefix(arg, codexModelArgFlag+"="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, codexModelArgFlag+"="))
			if value == "" {
				return codexRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					codexModelArgFlag+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": codexModelArgFlag,
					},
				)
			}
			parsed.modelOccurrences = append(parsed.modelOccurrences, runnerArgOccurrence{
				tokens: []string{arg},
				value:  value,
			})

		case strings.HasPrefix(arg, codexModelShortArgFlag+"="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, codexModelShortArgFlag+"="))
			if value == "" {
				return codexRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					codexModelShortArgFlag+" in --runner-arg requires a non-empty value",
					map[string]string{
						"flag": codexModelShortArgFlag,
					},
				)
			}
			parsed.modelOccurrences = append(parsed.modelOccurrences, runnerArgOccurrence{
				tokens: []string{arg},
				value:  value,
			})

		case arg == codexConfigArgFlag || arg == codexConfigShortArgFlag:
			if i+1 >= len(args) {
				return codexRunnerArgsParse{}, errors.NewWithDetails(
					errors.EUsage,
					arg+" in --runner-arg requires a value",
					map[string]string{
						"flag": arg,
						"hint": "pass --runner-arg \"--config=" + codexReasoningEffortConfigKey + "=<value>\"",
					},
				)
			}
			configToken := args[i+1]
			key, value, ok := parseCodexConfigAssignment(configToken)
			if ok && key == codexReasoningEffortConfigKey {
				normalized := normalizeRunnerArgValue(value)
				if normalized == "" {
					return codexRunnerArgsParse{}, errors.NewWithDetails(
						errors.EUsage,
						codexReasoningEffortConfigFlag+" in --runner-arg requires a non-empty value",
						map[string]string{
							"flag": codexReasoningEffortConfigFlag,
							"hint": "pass --runner-arg \"--config " + codexReasoningEffortConfigKey + "=<value>\"",
						},
					)
				}
				parsed.effortOccurrences = append(parsed.effortOccurrences, runnerArgOccurrence{
					tokens: []string{arg, configToken},
					value:  normalized,
				})
			} else {
				parsed.otherArgs = append(parsed.otherArgs, arg, configToken)
			}
			i++

		case strings.HasPrefix(arg, codexConfigArgFlag+"="):
			configToken := strings.TrimPrefix(arg, codexConfigArgFlag+"=")
			key, value, ok := parseCodexConfigAssignment(configToken)
			if ok && key == codexReasoningEffortConfigKey {
				normalized := normalizeRunnerArgValue(value)
				if normalized == "" {
					return codexRunnerArgsParse{}, errors.NewWithDetails(
						errors.EUsage,
						codexReasoningEffortConfigFlag+" in --runner-arg requires a non-empty value",
						map[string]string{
							"flag": codexReasoningEffortConfigFlag,
							"hint": "pass --runner-arg \"--config=" + codexReasoningEffortConfigKey + "=<value>\"",
						},
					)
				}
				parsed.effortOccurrences = append(parsed.effortOccurrences, runnerArgOccurrence{
					tokens: []string{arg},
					value:  normalized,
				})
			} else {
				parsed.otherArgs = append(parsed.otherArgs, arg)
			}

		case strings.HasPrefix(arg, codexConfigShortArgFlag+"="):
			configToken := strings.TrimPrefix(arg, codexConfigShortArgFlag+"=")
			key, value, ok := parseCodexConfigAssignment(configToken)
			if ok && key == codexReasoningEffortConfigKey {
				normalized := normalizeRunnerArgValue(value)
				if normalized == "" {
					return codexRunnerArgsParse{}, errors.NewWithDetails(
						errors.EUsage,
						codexReasoningEffortConfigFlag+" in --runner-arg requires a non-empty value",
						map[string]string{
							"flag": codexReasoningEffortConfigFlag,
							"hint": "pass --runner-arg \"-c " + codexReasoningEffortConfigKey + "=<value>\"",
						},
					)
				}
				parsed.effortOccurrences = append(parsed.effortOccurrences, runnerArgOccurrence{
					tokens: []string{arg},
					value:  normalized,
				})
			} else {
				parsed.otherArgs = append(parsed.otherArgs, arg)
			}

		default:
			parsed.otherArgs = append(parsed.otherArgs, arg)
		}
	}

	return parsed, nil
}

func parseCodexConfigAssignment(raw string) (string, string, bool) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", "", false
	}
	parts := strings.SplitN(token, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}
	value := strings.TrimSpace(parts[1])
	return key, value, true
}

type agentMutationEnvelope struct {
	OK              bool   `json:"ok"`
	ErrorCode       string `json:"error_code"`
	Message         string `json:"message"`
	Hint            string `json:"hint"`
	RequestID       string `json:"request_id"`
	APIVersion      int    `json:"api_version"`
	BuildVersion    string `json:"build_version"`
	ClientRequestID string `json:"client_request_id"`

	InvocationID            string                    `json:"invocation_id,omitempty"`
	RepoID                  string                    `json:"repo_id,omitempty"`
	IntegrationWorktreeID   string                    `json:"integration_worktree_id,omitempty"`
	IntegrationWorktreeName string                    `json:"integration_worktree_name,omitempty"`
	SandboxPath             string                    `json:"sandbox_path,omitempty"`
	LogPaths                *daemon.LogPaths          `json:"log_paths,omitempty"`
	PID                     int                       `json:"pid,omitempty"`
	PGID                    int                       `json:"pgid,omitempty"`
	DaemonInstanceID        string                    `json:"daemon_instance_id,omitempty"`
	AlreadyRunning          bool                      `json:"already_running,omitempty"`
	AlreadyApplied          bool                      `json:"already_applied,omitempty"`
	TimelineEntryID         string                    `json:"timeline_entry_id,omitempty"`
	CheckpointID            int                       `json:"checkpoint_id,omitempty"`
	SnapshotCommit          string                    `json:"snapshot_commit,omitempty"`
	RestoredAt              string                    `json:"restored_at,omitempty"`
	AppliedMode             daemon.LandingMode        `json:"applied_mode,omitempty"`
	IntegrationHeadBefore   string                    `json:"integration_head_before,omitempty"`
	IntegrationHeadAfter    string                    `json:"integration_head_after,omitempty"`
	CommitsLanded           int                       `json:"commits_landed,omitempty"`
	Branch                  string                    `json:"branch,omitempty"`
	PRNumber                int                       `json:"pr_number,omitempty"`
	PRURL                   string                    `json:"pr_url,omitempty"`
	PRAction                string                    `json:"pr_action,omitempty"`
	Strategy                string                    `json:"strategy,omitempty"`
	DeleteBranch            bool                      `json:"delete_branch,omitempty"`
	MergeLogPath            string                    `json:"merge_log_path,omitempty"`
	VerifyLogPath           string                    `json:"verify_log_path,omitempty"`
	ReportSource            string                    `json:"report_source,omitempty"`
	ReportDiagnostics       []daemon.ReportDiagnostic `json:"report_diagnostics,omitempty"`
}

func newAgentMutationEnvelope() agentMutationEnvelope {
	return agentMutationEnvelope{
		OK:              false,
		ErrorCode:       "",
		Message:         "",
		Hint:            "",
		RequestID:       "",
		APIVersion:      daemon.APIVersion,
		BuildVersion:    version.FullVersion(),
		ClientRequestID: "",
	}
}

func writeAgentMutationJSON(w io.Writer, envelope agentMutationEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

func writeAgentMutationJSONSuccess(w io.Writer, mutate func(*agentMutationEnvelope)) error {
	envelope := newAgentMutationEnvelope()
	envelope.OK = true
	if mutate != nil {
		mutate(&envelope)
	}
	return writeAgentMutationJSON(w, envelope)
}

func writeAgentMutationJSONError(w io.Writer, err error) error {
	envelope := newAgentMutationEnvelope()
	code := errors.GetCode(err)
	if code == "" {
		code = errors.EInternal
	}
	envelope.ErrorCode = string(code)
	envelope.Message = err.Error()
	if ae, ok := errors.AsAgencyError(err); ok {
		envelope.Message = ae.Msg
		if ae.Details != nil {
			envelope.Hint = ae.Details["hint"]
			envelope.RequestID = ae.Details["request_id"]
		}
	}
	return writeAgentMutationJSON(w, envelope)
}

// WriteAgentMutationJSONError writes a stable mutation error envelope.
// Exported for CLI preflight validation paths that occur before command dispatch.
func WriteAgentMutationJSONError(w io.Writer, err error) error {
	return writeAgentMutationJSONError(w, err)
}
