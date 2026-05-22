package runners

import (
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

const (
	RunnerClaudeCode = "claude-code"
	RunnerCodex      = "codex"
	RunnerAmp        = "amp"
	RunnerOpenCode   = "opencode"
	RunnerCursor     = "cursor"
	RunnerDroid      = "droid"

	launchTokenExtraArgs   = "{extra_args}"
	launchTokenPrompt      = "{prompt}"
	launchTokenSandboxPath = "{sandbox_path}"
)

// followUpMode describes how a runner accepts follow-up messages in headless mode.
type followUpMode string

const (
	// FollowUpModeStdin delivers messages via the runner's stdin pipe in real time (JSONL).
	FollowUpModeStdin followUpMode = "stdin"

	// FollowUpModeResume queues messages and delivers them by resuming the session.
	FollowUpModeResume followUpMode = "resume"
)

// initialPromptMode describes how a runner expects the first prompt in headless mode.
type initialPromptMode string

const (
	initialPromptPositional initialPromptMode = "positional"

	// InitialPromptStdin passes the initial prompt as the first stdin JSONL message.
	InitialPromptStdin initialPromptMode = "stdin"
)

// capability defines launch/validation policy for a runner identity.
type capability struct {
	id                string
	followUpMode      followUpMode // how follow-up messages are delivered in headless mode
	initialPromptMode initialPromptMode

	reservedArgs         []string // flags reserved in both headless and headed modes
	reservedHeadlessArgs []string // flags reserved only in headless mode (permission/approval)
	headlessTemplate     []string
	resumeTemplate       []string // template for session-resume follow-up turns (if supported)
	headedTemplate       []string
}

var canonicalIDs = []string{
	RunnerClaudeCode,
	RunnerCodex,
	RunnerAmp,
	RunnerOpenCode,
	RunnerCursor,
	RunnerDroid,
}

var capabilityByID = map[string]capability{
	RunnerClaudeCode: {
		id:                RunnerClaudeCode,
		followUpMode:      FollowUpModeResume,
		initialPromptMode: initialPromptPositional,
		reservedArgs:      []string{"--output-format", "--input-format", "-p", "--print", "--verbose", "-c", "--continue", "-r", "--resume", "--settings", "--bare", "--dangerously-skip-permissions"},
		headlessTemplate: []string{
			"-p",
			"--output-format", "stream-json",
			"--input-format", "text",
			"--verbose",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		resumeTemplate: []string{
			"-p",
			"--output-format", "stream-json",
			"--input-format", "text",
			"--verbose",
			"--continue",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs},
	},
	RunnerCodex: {
		id:                   RunnerCodex,
		followUpMode:         FollowUpModeResume,
		initialPromptMode:    initialPromptPositional,
		reservedArgs:         []string{"exec", "resume", "--json", "-C", "--cd", "--last"},
		reservedHeadlessArgs: []string{"-a", "--ask-for-approval", "-s", "--sandbox", "--full-auto", "--dangerously-bypass-approvals-and-sandbox", "--yolo"},
		headlessTemplate: []string{
			// Codex approval and sandbox policy are global flags, so they must precede `exec`.
			"--ask-for-approval", "never",
			"--sandbox", "workspace-write",
			"exec",
			"--cd", launchTokenSandboxPath,
			"--json",
			launchTokenExtraArgs,
			// codex unified_exec drops early command output for long-running commands.
			// keep this disabled so aggregated_output remains complete.
			"--disable", "unified_exec",
			launchTokenPrompt,
		},
		resumeTemplate: []string{
			"--ask-for-approval", "never",
			"--sandbox", "workspace-write",
			"exec",
			"resume",
			"--last",
			"--json",
			launchTokenExtraArgs,
			"--disable", "unified_exec",
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs, "--enable", "codex_hooks"},
	},
	RunnerAmp: {
		id:                RunnerAmp,
		followUpMode:      FollowUpModeStdin,
		initialPromptMode: InitialPromptStdin,
		reservedArgs:      []string{"-x", "--execute", "--stream-json", "--stream-json-input"},
		headlessTemplate:  []string{"-x", "--stream-json", "--stream-json-input", launchTokenExtraArgs},
		headedTemplate:    []string{launchTokenExtraArgs},
	},
	RunnerOpenCode: {
		id:                   RunnerOpenCode,
		initialPromptMode:    initialPromptPositional,
		reservedArgs:         []string{"run"},
		reservedHeadlessArgs: []string{"--mode"},
		headlessTemplate:     []string{"run", "--mode", "auto", launchTokenExtraArgs, launchTokenPrompt},
		headedTemplate:       []string{launchTokenExtraArgs},
	},
	RunnerCursor: {
		id:                   RunnerCursor,
		followUpMode:         FollowUpModeResume,
		initialPromptMode:    initialPromptPositional,
		reservedArgs:         []string{"-p", "--print", "--output-format", "--resume", "--continue", "--workspace"},
		reservedHeadlessArgs: []string{"--force", "-f", "--yolo", "--trust"},
		headlessTemplate: []string{
			"-p",
			"--output-format", "stream-json",
			"--force",
			"--workspace", launchTokenSandboxPath,
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		resumeTemplate: []string{
			"-p",
			"--output-format", "stream-json",
			"--force",
			"--continue",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs},
	},
	RunnerDroid: {
		id:                   RunnerDroid,
		followUpMode:         FollowUpModeStdin,
		initialPromptMode:    InitialPromptStdin,
		reservedArgs:         []string{"exec", "--output-format", "--input-format"},
		reservedHeadlessArgs: []string{"--auto", "--skip-permissions-unsafe"},
		headlessTemplate:     []string{"exec", "--auto", "medium", "--output-format", "stream-json", "--input-format", "stream-json", launchTokenExtraArgs},
		headedTemplate:       []string{launchTokenExtraArgs},
	},
}

// CanonicalIDs returns the supported canonical runner IDs in stable order.
func CanonicalIDs() []string {
	out := make([]string, len(canonicalIDs))
	copy(out, canonicalIDs)
	return out
}

// Canonicalize validates a canonical runner ID.
func Canonicalize(runner string) (string, error) {
	input := strings.TrimSpace(runner)
	if _, ok := capabilityByID[input]; !ok {
		return "", errors.NewWithDetails(
			errors.ERunnerNotFound,
			"unrecognized runner: "+input,
			map[string]string{
				"runner": input,
				"valid":  strings.Join(canonicalIDs, ", "),
			},
		)
	}
	return input, nil
}

func resolve(runner string) (capability, error) {
	canonical, err := Canonicalize(runner)
	if err != nil {
		return capability{}, err
	}
	return capabilityByID[canonical], nil
}

// FollowUpPolicy returns the headless follow-up delivery mode for a runner.
func FollowUpPolicy(runner string) (followUpMode, initialPromptMode, error) {
	capability, err := resolve(runner)
	if err != nil {
		return "", "", err
	}
	return capability.followUpMode, capability.initialPromptMode, nil
}

// ValidateArgs rejects user-supplied args that conflict with universal reserved flags.
// Use ValidateHeadlessArgs for headless invocations to also check permission flags.
func ValidateArgs(runner string, args []string) error {
	capability, err := resolve(runner)
	if err != nil {
		return err
	}
	return validateAgainst(capability, args, capability.reservedArgs)
}

// ValidateHeadlessArgs rejects user-supplied args that conflict with any reserved flag,
// including headless-only permission/approval flags that Agency injects for autonomous operation.
func ValidateHeadlessArgs(runner string, args []string) error {
	capability, err := resolve(runner)
	if err != nil {
		return err
	}
	all := make([]string, 0, len(capability.reservedArgs)+len(capability.reservedHeadlessArgs))
	all = append(all, capability.reservedArgs...)
	all = append(all, capability.reservedHeadlessArgs...)
	return validateAgainst(capability, args, all)
}

func validateAgainst(capability capability, args, reserved []string) error {
	for _, arg := range args {
		for _, r := range reserved {
			if arg == r || strings.HasPrefix(arg, r+"=") {
				return errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+r+"' cannot be passed via runner_args",
					map[string]string{
						"runner": capability.id,
						"flag":   r,
					},
				)
			}
		}
	}
	return nil
}

// BuildHeadlessArgs builds canonical headless argv for a runner.
func BuildHeadlessArgs(runner, prompt, sandboxPath string, extraArgs []string) ([]string, error) {
	capability, err := resolve(runner)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"prompt is required to render runner launch plan",
			map[string]string{"field": "prompt"},
		)
	}
	return renderLaunchTemplate(capability.headlessTemplate, prompt, sandboxPath, extraArgs)
}

// BuildResumeArgs builds canonical follow-up resume argv for a runner.
// resumeSessionID is optional; when provided, runners that support explicit
// session targeting use it instead of generic "last session" semantics.
func BuildResumeArgs(runner, prompt, resumeSessionID string, extraArgs []string) ([]string, error) {
	capability, err := resolve(runner)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.NewWithDetails(
			errors.EInvalidArgument,
			"prompt is required to render runner launch plan",
			map[string]string{"field": "prompt"},
		)
	}
	if len(capability.resumeTemplate) == 0 {
		return nil, errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"runner '"+capability.id+"' does not support session-resume follow-up mode",
			map[string]string{
				"runner": capability.id,
				"mode":   "resume",
			},
		)
	}
	args, err := renderLaunchTemplate(capability.resumeTemplate, prompt, "", extraArgs)
	if err != nil {
		return nil, err
	}

	resumeSessionID = strings.TrimSpace(resumeSessionID)
	if capability.id == RunnerCodex && resumeSessionID != "" {
		for i := range args {
			if args[i] == "--last" {
				args[i] = resumeSessionID
				break
			}
		}
	}
	if (capability.id == RunnerCursor || capability.id == RunnerClaudeCode) && resumeSessionID != "" {
		rewritten := make([]string, 0, len(args)+1)
		replaced := false
		for _, arg := range args {
			if !replaced && arg == "--continue" {
				rewritten = append(rewritten, "--resume", resumeSessionID)
				replaced = true
				continue
			}
			rewritten = append(rewritten, arg)
		}
		args = rewritten
	}
	return args, nil
}

// SupportsResumeTurns reports whether runner has a configured resume launch template.
func SupportsResumeTurns(runner string) bool {
	capability, err := resolve(runner)
	if err != nil {
		return false
	}
	return len(capability.resumeTemplate) > 0
}

// BuildHeadedArgs builds canonical headed argv for a runner.
func BuildHeadedArgs(runner string, extraArgs []string) ([]string, error) {
	capability, err := resolve(runner)
	if err != nil {
		return nil, err
	}
	return renderLaunchTemplate(capability.headedTemplate, "", "", extraArgs)
}

func renderLaunchTemplate(template []string, prompt, sandboxPath string, extraArgs []string) ([]string, error) {
	args := make([]string, 0, len(template)+len(extraArgs)+2)
	for _, token := range template {
		switch token {
		case launchTokenExtraArgs:
			args = append(args, extraArgs...)
		case launchTokenPrompt:
			if strings.TrimSpace(prompt) == "" {
				return nil, errors.NewWithDetails(
					errors.EInvalidArgument,
					"prompt is required to render runner launch plan",
					map[string]string{"field": "prompt"},
				)
			}
			args = append(args, prompt)
		case launchTokenSandboxPath:
			if strings.TrimSpace(sandboxPath) == "" {
				return nil, errors.NewWithDetails(
					errors.EInvalidArgument,
					"sandbox path is required to render runner launch plan",
					map[string]string{"field": "sandbox_path"},
				)
			}
			args = append(args, sandboxPath)
		default:
			args = append(args, token)
		}
	}
	return args, nil
}
