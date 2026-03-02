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
	RunnerCursorCLI  = "cursor-cli"
	RunnerDroid      = "droid"

	LegacyRunnerClaude = "claude"
)

// Capability defines launch/validation policy for a runner identity.
type Capability struct {
	ID                 string
	SupportsHeadless   bool
	SupportsHeaded     bool
	HasSemanticAdapter bool

	reservedArgs []string
	aliases      []string
}

var canonicalIDs = []string{
	RunnerClaudeCode,
	RunnerCodex,
	RunnerAmp,
	RunnerOpenCode,
	RunnerCursorCLI,
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
	},
	RunnerCodex: {
		ID:                 RunnerCodex,
		SupportsHeadless:   true,
		SupportsHeaded:     true,
		HasSemanticAdapter: true,
		reservedArgs:       []string{"exec", "--json", "-C", "--cd"},
	},
	RunnerAmp: {
		ID:               RunnerAmp,
		SupportsHeadless: true,
		SupportsHeaded:   true,
	},
	RunnerOpenCode: {
		ID:               RunnerOpenCode,
		SupportsHeadless: true,
		SupportsHeaded:   true,
	},
	RunnerCursorCLI: {
		ID:               RunnerCursorCLI,
		SupportsHeadless: true,
		SupportsHeaded:   true,
	},
	RunnerDroid: {
		ID:               RunnerDroid,
		SupportsHeadless: true,
		SupportsHeaded:   true,
	},
}

var canonicalByInput = map[string]string{
	RunnerClaudeCode:   RunnerClaudeCode,
	LegacyRunnerClaude: RunnerClaudeCode,
	RunnerCodex:        RunnerCodex,
	RunnerAmp:          RunnerAmp,
	RunnerOpenCode:     RunnerOpenCode,
	RunnerCursorCLI:    RunnerCursorCLI,
	RunnerDroid:        RunnerDroid,
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

	args := make([]string, 0, 8+len(extraArgs))
	switch capability.ID {
	case RunnerClaudeCode:
		args = append(args, "-p", "--output-format", "stream-json", "--verbose")
	case RunnerCodex:
		args = append(args, "-C", sandboxPath, "exec", "--json")
	}

	args = append(args, extraArgs...)
	args = append(args, prompt)
	return args, nil
}

// HasSemanticAdapter reports whether semantic parsing is supported for runner.
func HasSemanticAdapter(runner string) bool {
	capability, err := Resolve(runner)
	if err != nil {
		return false
	}
	return capability.HasSemanticAdapter
}
