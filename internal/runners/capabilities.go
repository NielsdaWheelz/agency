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

// FollowUpMode describes how a runner accepts follow-up messages in headless mode.
type FollowUpMode string

const (
	// FollowUpModeStdin delivers messages via the runner's stdin pipe in real time (JSONL).
	FollowUpModeStdin FollowUpMode = "stdin"

	// FollowUpModeResume queues messages and delivers them by resuming the session.
	FollowUpModeResume FollowUpMode = "resume"
)

// InitialPromptMode describes how a runner expects the first prompt in headless mode.
type InitialPromptMode string

const (
	// InitialPromptPositional passes the initial prompt as a CLI positional argument.
	InitialPromptPositional InitialPromptMode = "positional"

	// InitialPromptStdin passes the initial prompt as the first stdin JSONL message.
	InitialPromptStdin InitialPromptMode = "stdin"
)

// Capability defines launch/validation policy for a runner identity.
type Capability struct {
	ID                 string
	SupportsHeadless   bool
	SupportsHeaded     bool
	HasSemanticAdapter bool
	FollowUpMode       FollowUpMode // how follow-up messages are delivered in headless mode
	InitialPromptMode  InitialPromptMode

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

var capabilityByID = map[string]Capability{
	RunnerClaudeCode: {
		ID:                   RunnerClaudeCode,
		SupportsHeadless:     true,
		SupportsHeaded:       true,
		HasSemanticAdapter:   true,
		FollowUpMode:         FollowUpModeResume,
		InitialPromptMode:    InitialPromptPositional,
		reservedArgs:         []string{"--output-format", "--input-format", "-p", "--print", "--verbose", "-c", "--continue", "-r", "--resume", "--settings", "--bare"},
		reservedHeadlessArgs: []string{"--dangerously-skip-permissions", "--permission-mode"},
		headlessTemplate: []string{
			"-p",
			"--output-format", "stream-json",
			"--input-format", "text",
			"--verbose",
			"--dangerously-skip-permissions",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		resumeTemplate: []string{
			"-p",
			"--output-format", "stream-json",
			"--input-format", "text",
			"--verbose",
			"--dangerously-skip-permissions",
			"--continue",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs},
	},
	RunnerCodex: {
		ID:                   RunnerCodex,
		SupportsHeadless:     true,
		SupportsHeaded:       true,
		HasSemanticAdapter:   true,
		FollowUpMode:         FollowUpModeResume,
		InitialPromptMode:    InitialPromptPositional,
		reservedArgs:         []string{"exec", "resume", "--json", "-C", "--cd", "--last"},
		reservedHeadlessArgs: []string{"--full-auto", "--dangerously-bypass-approvals-and-sandbox", "--yolo"},
		headlessTemplate: []string{
			"exec",
			"--cd", launchTokenSandboxPath,
			"--json",
			"--full-auto",
			launchTokenExtraArgs,
			// codex unified_exec drops early command output for long-running commands.
			// keep this disabled so aggregated_output remains complete.
			"--disable", "unified_exec",
			launchTokenPrompt,
		},
		resumeTemplate: []string{
			"exec",
			"resume",
			"--last",
			"--json",
			"--full-auto",
			launchTokenExtraArgs,
			"--disable", "unified_exec",
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs, "--enable", "codex_hooks"},
	},
	RunnerAmp: {
		ID:                RunnerAmp,
		SupportsHeadless:  true,
		SupportsHeaded:    true,
		FollowUpMode:      FollowUpModeStdin,
		InitialPromptMode: InitialPromptStdin,
		reservedArgs:      []string{"-x", "--execute", "--stream-json", "--stream-json-input"},
		headlessTemplate:  []string{"-x", "--stream-json", "--stream-json-input", launchTokenExtraArgs},
		headedTemplate:    []string{launchTokenExtraArgs},
		// TODO: verify upstream amp CLI auto-approve flags for headless mode.
	},
	RunnerOpenCode: {
		ID:                   RunnerOpenCode,
		SupportsHeadless:     true,
		SupportsHeaded:       true,
		FollowUpMode:         FollowUpModeResume,
		InitialPromptMode:    InitialPromptPositional,
		reservedArgs:         []string{"run"},
		reservedHeadlessArgs: []string{"--mode"},
		headlessTemplate:     []string{"run", "--mode", "auto", launchTokenExtraArgs, launchTokenPrompt},
		headedTemplate:       []string{launchTokenExtraArgs},
	},
	RunnerCursor: {
		ID:                   RunnerCursor,
		SupportsHeadless:     true,
		SupportsHeaded:       true,
		HasSemanticAdapter:   true,
		FollowUpMode:         FollowUpModeResume,
		InitialPromptMode:    InitialPromptPositional,
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
		// TODO: verify upstream cursor/agent CLI sandbox/approval flags for headless mode.
	},
	RunnerDroid: {
		ID:                RunnerDroid,
		SupportsHeadless:  true,
		SupportsHeaded:    true,
		FollowUpMode:      FollowUpModeStdin,
		InitialPromptMode: InitialPromptStdin,
		reservedArgs:      []string{"exec", "--output-format", "--input-format"},
		headlessTemplate:  []string{"exec", "--output-format", "stream-json", "--input-format", "stream-json", launchTokenExtraArgs},
		headedTemplate:    []string{launchTokenExtraArgs},
		// TODO: verify upstream droid CLI auto-approve flags for headless mode.
	},
}

var canonicalByInput = map[string]string{
	RunnerClaudeCode: RunnerClaudeCode,
	RunnerCodex:      RunnerCodex,
	RunnerAmp:        RunnerAmp,
	RunnerOpenCode:   RunnerOpenCode,
	RunnerCursor:     RunnerCursor,
	RunnerDroid:      RunnerDroid,
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
	canonical, ok := canonicalByInput[input]
	if !ok {
		return "", errors.NewWithDetails(
			errors.ERunnerNotFound,
			"unrecognized runner: "+input,
			map[string]string{
				"runner": input,
				"valid":  strings.Join(canonicalIDs, ", "),
			},
		)
	}
	return canonical, nil
}

// Resolve resolves a runner input to its canonical capability.
func Resolve(runner string) (Capability, error) {
	canonical, err := Canonicalize(runner)
	if err != nil {
		return Capability{}, err
	}
	capability, ok := capabilityByID[canonical]
	if !ok {
		return Capability{}, errors.NewWithDetails(
			errors.ERunnerNotFound,
			"unrecognized runner: "+runner,
			map[string]string{
				"runner": runner,
				"valid":  strings.Join(canonicalIDs, ", "),
			},
		)
	}
	return capability, nil
}

// ConfigLookupKeys returns config keys to probe in order of precedence.
func ConfigLookupKeys(runner string) ([]string, error) {
	capability, err := Resolve(runner)
	if err != nil {
		return nil, err
	}
	return []string{capability.ID}, nil
}

// ValidateArgs rejects user-supplied args that conflict with universal reserved flags.
// Use ValidateHeadlessArgs for headless invocations to also check permission flags.
func ValidateArgs(runner string, args []string) error {
	capability, err := Resolve(runner)
	if err != nil {
		return err
	}
	return validateAgainst(capability, args, capability.reservedArgs)
}

// ValidateHeadlessArgs rejects user-supplied args that conflict with any reserved flag,
// including headless-only permission/approval flags that Agency injects for autonomous operation.
func ValidateHeadlessArgs(runner string, args []string) error {
	capability, err := Resolve(runner)
	if err != nil {
		return err
	}
	all := make([]string, 0, len(capability.reservedArgs)+len(capability.reservedHeadlessArgs))
	all = append(all, capability.reservedArgs...)
	all = append(all, capability.reservedHeadlessArgs...)
	return validateAgainst(capability, args, all)
}

func validateAgainst(capability Capability, args, reserved []string) error {
	for _, arg := range args {
		for _, r := range reserved {
			if arg == r || strings.HasPrefix(arg, r+"=") {
				return errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+r+"' cannot be passed via runner_args",
					map[string]string{
						"runner": capability.ID,
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
	capability, err := Resolve(runner)
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
	if !capability.SupportsHeadless {
		return nil, errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"runner '"+capability.ID+"' does not support headless mode",
			map[string]string{
				"runner": capability.ID,
				"mode":   "headless",
			},
		)
	}
	return renderLaunchTemplate(capability.headlessTemplate, prompt, sandboxPath, extraArgs)
}

// BuildResumeArgs builds canonical follow-up resume argv for a runner.
// resumeSessionID is optional; when provided, runners that support explicit
// session targeting use it instead of generic "last session" semantics.
func BuildResumeArgs(runner, prompt, resumeSessionID string, extraArgs []string) ([]string, error) {
	capability, err := Resolve(runner)
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
			"runner '"+capability.ID+"' does not support session-resume follow-up mode",
			map[string]string{
				"runner": capability.ID,
				"mode":   "resume",
			},
		)
	}
	args, err := renderLaunchTemplate(capability.resumeTemplate, prompt, "", extraArgs)
	if err != nil {
		return nil, err
	}

	resumeSessionID = strings.TrimSpace(resumeSessionID)
	if capability.ID == RunnerCodex && resumeSessionID != "" {
		for i := range args {
			if args[i] == "--last" {
				args[i] = resumeSessionID
				break
			}
		}
	}
	if (capability.ID == RunnerCursor || capability.ID == RunnerClaudeCode) && resumeSessionID != "" {
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
	capability, err := Resolve(runner)
	if err != nil {
		return false
	}
	return len(capability.resumeTemplate) > 0
}

// BuildHeadedArgs builds canonical headed argv for a runner.
func BuildHeadedArgs(runner string, extraArgs []string) ([]string, error) {
	capability, err := Resolve(runner)
	if err != nil {
		return nil, err
	}
	if !capability.SupportsHeaded {
		return nil, errors.NewWithDetails(
			errors.EInvocationInvalidMode,
			"runner '"+capability.ID+"' does not support headed mode",
			map[string]string{
				"runner": capability.ID,
				"mode":   "headed",
			},
		)
	}
	return renderLaunchTemplate(capability.headedTemplate, "", "", extraArgs)
}

// ResolveFollowUpMode returns the FollowUpMode for a runner.
func ResolveFollowUpMode(runner string) (FollowUpMode, error) {
	capability, err := Resolve(runner)
	if err != nil {
		return "", err
	}
	return capability.FollowUpMode, nil
}

// ResolveInitialPromptMode returns how the runner expects the first headless prompt.
func ResolveInitialPromptMode(runner string) (InitialPromptMode, error) {
	capability, err := Resolve(runner)
	if err != nil {
		return "", err
	}
	return capability.InitialPromptMode, nil
}

// HasSemanticAdapter reports whether semantic parsing is supported for runner.
func HasSemanticAdapter(runner string) bool {
	capability, err := Resolve(runner)
	if err != nil {
		return false
	}
	return capability.HasSemanticAdapter
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
