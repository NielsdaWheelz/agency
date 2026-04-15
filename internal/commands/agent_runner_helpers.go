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

	codexModelArgFlag             = "--model"
	codexConfigArgFlag            = "--config"
	codexReasoningEffortConfigKey = "model_reasoning_effort"
)

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

	cliModel = strings.TrimSpace(cliModel)
	cliEffort = strings.TrimSpace(cliEffort)
	defaultModel := strings.TrimSpace(defaults.Model)
	defaultEffort := strings.TrimSpace(defaults.Effort)

	supportsModel := canonicalRunner == runners.RunnerClaudeCode || canonicalRunner == runners.RunnerCodex || canonicalRunner == runners.RunnerCursor
	supportsEffort := canonicalRunner == runners.RunnerClaudeCode || canonicalRunner == runners.RunnerCodex

	if !supportsModel {
		if cliModel != "" || cliEffort != "" {
			return nil, errors.NewWithDetails(
				errors.EUsage,
				fmt.Sprintf("--model is supported for runners %s; --effort is supported for runners %s",
					strings.Join([]string{runners.RunnerClaudeCode, runners.RunnerCodex, runners.RunnerCursor}, ", "),
					strings.Join([]string{runners.RunnerClaudeCode, runners.RunnerCodex}, ", "),
				),
				map[string]string{
					"runner": canonicalRunner,
					"hint":   "for other runners, use passthrough args only",
				},
			)
		}
		return append([]string(nil), runnerArgs...), nil
	}

	model := cliModel
	if model == "" {
		model = defaultModel
	}

	effort := cliEffort
	if supportsEffort && effort == "" {
		effort = defaultEffort
	}
	if !supportsEffort {
		if cliEffort != "" {
			return nil, errors.NewWithDetails(
				errors.EUsage,
				"--effort is not supported for runner "+canonicalRunner,
				map[string]string{
					"runner": canonicalRunner,
					"hint":   "select thinking-capable models via --model (for example: sonnet-4.6-thinking)",
				},
			)
		}
		effort = ""
	}

	out := append([]string(nil), runnerArgs...)
	switch canonicalRunner {
	case runners.RunnerClaudeCode:
		if model != "" {
			out = append(out, claudeModelArgFlag, model)
		}
		if effort != "" {
			out = append(out, claudeEffortArgFlag, effort)
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
	RepoKey                 string                    `json:"repo_key,omitempty"`
	PreferredRoot           string                    `json:"preferred_root,omitempty"`
	RemovedFromIndex        bool                      `json:"removed_from_index,omitempty"`
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
	ArchiveLogPath          string                    `json:"archive_log_path,omitempty"`
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
