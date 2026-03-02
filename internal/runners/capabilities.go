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

	LegacyRunnerClaude    = "claude"
	LegacyRunnerCursorCLI = "cursor-cli"

	launchTokenExtraArgs   = "{extra_args}"
	launchTokenPrompt      = "{prompt}"
	launchTokenSandboxPath = "{sandbox_path}"
)

// Capability defines launch/validation policy for a runner identity.
type Capability struct {
	ID                 string
	SupportsHeadless   bool
	SupportsHeaded     bool
	HasSemanticAdapter bool

	reservedArgs     []string
	aliases          []string
	headlessTemplate []string
	headedTemplate   []string
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
		ID:                 RunnerClaudeCode,
		SupportsHeadless:   true,
		SupportsHeaded:     true,
		HasSemanticAdapter: true,
		reservedArgs:       []string{"--output-format", "-p", "--print", "--verbose"},
		aliases:            []string{LegacyRunnerClaude},
		headlessTemplate: []string{
			"-p",
			"--output-format",
			"stream-json",
			"--verbose",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs},
	},
	RunnerCodex: {
		ID:                 RunnerCodex,
		SupportsHeadless:   true,
		SupportsHeaded:     true,
		HasSemanticAdapter: true,
		reservedArgs:       []string{"exec", "--json", "-C", "--cd"},
		headlessTemplate: []string{
			"exec",
			"--cd",
			launchTokenSandboxPath,
			"--json",
			launchTokenExtraArgs,
			launchTokenPrompt,
		},
		headedTemplate: []string{launchTokenExtraArgs},
	},
	RunnerAmp: {
		ID:               RunnerAmp,
		SupportsHeadless: true,
		SupportsHeaded:   true,
		reservedArgs:     []string{"-x", "--execute"},
		headlessTemplate: []string{"-x", launchTokenExtraArgs, launchTokenPrompt},
		headedTemplate:   []string{launchTokenExtraArgs},
	},
	RunnerOpenCode: {
		ID:               RunnerOpenCode,
		SupportsHeadless: true,
		SupportsHeaded:   true,
		reservedArgs:     []string{"run"},
		headlessTemplate: []string{"run", launchTokenExtraArgs, launchTokenPrompt},
		headedTemplate:   []string{launchTokenExtraArgs},
	},
	RunnerCursor: {
		ID:               RunnerCursor,
		SupportsHeadless: true,
		SupportsHeaded:   true,
		reservedArgs:     []string{"-p", "--print"},
		aliases:          []string{LegacyRunnerCursorCLI},
		headlessTemplate: []string{"-p", launchTokenExtraArgs, launchTokenPrompt},
		headedTemplate:   []string{launchTokenExtraArgs},
	},
	RunnerDroid: {
		ID:               RunnerDroid,
		SupportsHeadless: true,
		SupportsHeaded:   true,
		reservedArgs:     []string{"exec"},
		headlessTemplate: []string{"exec", launchTokenExtraArgs, launchTokenPrompt},
		headedTemplate:   []string{launchTokenExtraArgs},
	},
}

var canonicalByInput = map[string]string{
	RunnerClaudeCode:      RunnerClaudeCode,
	LegacyRunnerClaude:    RunnerClaudeCode,
	RunnerCodex:           RunnerCodex,
	RunnerAmp:             RunnerAmp,
	RunnerOpenCode:        RunnerOpenCode,
	RunnerCursor:          RunnerCursor,
	LegacyRunnerCursorCLI: RunnerCursor,
	RunnerDroid:           RunnerDroid,
}

// CanonicalIDs returns the supported canonical runner IDs in stable order.
func CanonicalIDs() []string {
	out := make([]string, len(canonicalIDs))
	copy(out, canonicalIDs)
	return out
}

// Canonicalize resolves a runner input (including aliases) to canonical ID.
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
	keys := []string{capability.ID}
	keys = append(keys, capability.aliases...)
	return keys, nil
}

// ValidateArgs rejects user-supplied args that conflict with reserved flags.
func ValidateArgs(runner string, args []string) error {
	capability, err := Resolve(runner)
	if err != nil {
		return err
	}
	for _, arg := range args {
		for _, reserved := range capability.reservedArgs {
			if arg == reserved || strings.HasPrefix(arg, reserved+"=") {
				return errors.NewWithDetails(
					errors.ERunnerArgConflict,
					"reserved flag '"+reserved+"' cannot be passed via runner_args",
					map[string]string{
						"runner": capability.ID,
						"flag":   reserved,
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
