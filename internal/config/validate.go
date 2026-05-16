package config

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// ValidateAgencyConfig validates the repo configuration (agency.json).
// Returns E_INVALID_AGENCY_JSON for schema/required-field errors.
func ValidateAgencyConfig(cfg AgencyConfig) (AgencyConfig, error) {
	// Validate version
	if cfg.Version != AgencyConfigVersion {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "version must be 4")
	}

	// Validate required fields in scripts
	if cfg.Scripts.Setup.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.setup.path")
	}
	if cfg.Scripts.Verify.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.verify.path")
	}
	if cfg.Scripts.Archive.Path == "" {
		return cfg, errors.New(errors.EInvalidAgencyJSON, "missing required field scripts.archive.path")
	}
	for name, rd := range cfg.RunnerDefaults {
		cleaned, err := validateRunnerDefaults(name, rd, errors.EInvalidAgencyJSON, false)
		if err != nil {
			return cfg, err
		}
		cfg.RunnerDefaults[name] = cleaned
	}
	cfg.Execution.Profile = strings.TrimSpace(cfg.Execution.Profile)
	if cfg.Execution.Profile != "" && !IsValidExecutionProfileLabel(cfg.Execution.Profile) {
		return cfg, errors.New(errors.EInvalidExecutionProfile, "execution.profile must contain only lowercase letters, digits, and hyphens")
	}
	cfg.Execution.CheckoutRoot = strings.TrimSpace(cfg.Execution.CheckoutRoot)
	if cfg.Execution.CheckoutRoot == "" {
		cfg.Execution.CheckoutRoot = CheckoutRootSibling
	}
	if cfg.Execution.CheckoutRoot != CheckoutRootSibling && !filepath.IsAbs(cfg.Execution.CheckoutRoot) {
		return cfg, errors.New(errors.EInvalidCheckoutRoot, "execution.checkout_root must be repo-sibling or an absolute path")
	}

	return cfg, nil
}

// IsValidExecutionProfileLabel validates user-defined profile labels.
func IsValidExecutionProfileLabel(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			prevHyphen = false
		case r >= '0' && r <= '9':
			prevHyphen = false
		case r == '-':
			if prevHyphen {
				return false
			}
			prevHyphen = true
		default:
			return false
		}
	}
	return true
}

func ExecutionProfileEnv(cfg UserConfig, profile string) (map[string]string, error) {
	selected, ok := cfg.ExecutionProfiles[profile]
	if !ok {
		return nil, errors.NewWithDetails(
			errors.EExecutionProfileNotFound,
			"execution profile not found: "+profile,
			map[string]string{"execution_profile": profile},
		)
	}
	env := make(map[string]string, len(selected.Env))
	for k, v := range selected.Env {
		env[k] = v
	}
	return env, nil
}

func ResolveCheckoutRoot(repoRoot, repoID, checkoutRoot string) (string, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	canonicalRepoRoot, err := fs.ResolveSymlinks(repoRoot)
	if err != nil {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "repo root could not be resolved safely", map[string]string{"repo_root": filepath.Clean(repoRoot)})
	}

	checkoutRoot = strings.TrimSpace(checkoutRoot)
	if checkoutRoot == "" {
		checkoutRoot = CheckoutRootSibling
	}
	var resolved string
	switch {
	case checkoutRoot == CheckoutRootSibling:
		resolved = filepath.Join(filepath.Dir(canonicalRepoRoot), ".agency", "checkouts", repoID)
	case filepath.IsAbs(checkoutRoot):
		resolved = filepath.Join(checkoutRoot, repoID)
	default:
		return "", errors.New(errors.EInvalidCheckoutRoot, "checkout_root must be repo-sibling or an absolute path")
	}
	requested := resolved
	resolved, err = fs.ResolveSymlinks(requested)
	if err != nil {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "checkout root could not be resolved safely", map[string]string{"checkout_root": filepath.Clean(requested), "repo_root": canonicalRepoRoot})
	}
	if fs.PathContains(canonicalRepoRoot, resolved) {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "checkout root is inside the canonical repo root", map[string]string{"checkout_root": resolved, "repo_root": canonicalRepoRoot})
	}
	if fs.PathContains(resolved, canonicalRepoRoot) {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "checkout root contains the canonical repo root", map[string]string{"checkout_root": resolved, "repo_root": canonicalRepoRoot})
	}
	return resolved, nil
}

// containsWhitespace returns true if s contains any whitespace character.
func containsWhitespace(s string) bool {
	return strings.ContainsFunc(s, unicode.IsSpace)
}

// validateRunnerDefaults validates and normalizes one runner_defaults entry.
// agency.json (repo config) forbids permission_mode entirely; config.json
// (user config) allows it for claude-code only. errCode tags the producing config.
func validateRunnerDefaults(name string, rd RunnerDefaults, errCode errors.Code, allowPermissionMode bool) (RunnerDefaults, error) {
	canonical, err := runners.Canonicalize(name)
	unsupported := "runner_defaults." + name + " is not supported; typed runner defaults are supported for runners claude-code, codex, cursor"
	if err != nil || canonical != name {
		return RunnerDefaults{}, errors.New(errCode, unsupported)
	}
	if canonical != runners.RunnerClaudeCode && canonical != runners.RunnerCodex && canonical != runners.RunnerCursor {
		return RunnerDefaults{}, errors.New(errCode, unsupported)
	}
	model := strings.TrimSpace(rd.Model)
	effort := strings.TrimSpace(rd.Effort)
	permissionMode := strings.TrimSpace(rd.PermissionMode)
	if !allowPermissionMode && rd.PermissionMode != "" {
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults."+name+".permission_mode is not supported in agency.json")
	}
	if model == "" && effort == "" && permissionMode == "" {
		need := "model or effort"
		if allowPermissionMode {
			need = "model, effort, or permission_mode"
		}
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults."+name+" requires at least one of "+need)
	}
	if rd.Model != "" && model == "" {
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults."+name+".model must be a non-empty string")
	}
	if rd.Effort != "" && effort == "" {
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults."+name+".effort must be a non-empty string")
	}
	if allowPermissionMode && rd.PermissionMode != "" && permissionMode == "" {
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults."+name+".permission_mode must be a non-empty string")
	}
	if canonical == runners.RunnerCursor && effort != "" {
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults.cursor.effort is not supported")
	}
	if canonical != runners.RunnerClaudeCode && permissionMode != "" {
		return RunnerDefaults{}, errors.New(errCode, "runner_defaults."+name+".permission_mode is only supported for claude-code")
	}
	return RunnerDefaults{Model: model, Effort: effort, PermissionMode: permissionMode}, nil
}
