package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/version"
)

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

// namedLabel formats a (name, id) pair as "name (id)" or just "id" when name
// is empty. Used for worktree, repo, and other "human label plus stable id"
// rendering across CLI output.
func namedLabel(name, id string) string {
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
	_, _ = fmt.Fprintf(w, "  worktree:       %s\n", namedLabel(worktreeName, worktreeID))
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
		ErrorCode:    string(code),
		Message:      err.Error(),
		APIVersion:   daemon.APIVersion,
		BuildVersion: version.FullVersion(),
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
