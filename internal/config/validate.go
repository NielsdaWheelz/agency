package config

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/NielsdaWheelz/agency/internal/errors"
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
	for name, runnerDefaults := range cfg.RunnerDefaults {
		canonicalRunner, err := runners.Canonicalize(name)
		if err != nil || canonicalRunner != name {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+" is not supported; typed runner defaults are supported for runners claude-code, codex, cursor")
		}
		if canonicalRunner != runners.RunnerClaudeCode && canonicalRunner != runners.RunnerCodex && canonicalRunner != runners.RunnerCursor {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+" is not supported; typed runner defaults are supported for runners claude-code, codex, cursor")
		}

		model := strings.TrimSpace(runnerDefaults.Model)
		effort := strings.TrimSpace(runnerDefaults.Effort)
		permissionMode := strings.TrimSpace(runnerDefaults.PermissionMode)
		if permissionMode != "" || runnerDefaults.PermissionMode != "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+".permission_mode is not supported in agency.json")
		}
		if model == "" && effort == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+" requires at least one of model or effort")
		}
		if runnerDefaults.Model != "" && model == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+".model must be a non-empty string")
		}
		if runnerDefaults.Effort != "" && effort == "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults."+name+".effort must be a non-empty string")
		}
		if canonicalRunner == runners.RunnerCursor && effort != "" {
			return cfg, errors.New(errors.EInvalidAgencyJSON, "runner_defaults.cursor.effort is not supported")
		}

		cfg.RunnerDefaults[canonicalRunner] = RunnerDefaults{
			Model:  model,
			Effort: effort,
		}
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
	canonicalRepoRoot, err := resolveSymlinkPath(repoRoot)
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
	resolved, err = resolveSymlinkPath(requested)
	if err != nil {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "checkout root could not be resolved safely", map[string]string{"checkout_root": filepath.Clean(requested), "repo_root": canonicalRepoRoot})
	}
	if pathContains(canonicalRepoRoot, resolved) {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "checkout root is inside the canonical repo root", map[string]string{"checkout_root": resolved, "repo_root": canonicalRepoRoot})
	}
	if pathContains(resolved, canonicalRepoRoot) {
		return "", errors.NewWithDetails(errors.ECheckoutRootUnsafe, "checkout root contains the canonical repo root", map[string]string{"checkout_root": resolved, "repo_root": canonicalRepoRoot})
	}
	return resolved, nil
}

func resolveSymlinkPath(path string) (string, error) {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	current := clean
	var missing []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return clean, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent

		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)))
}

// containsWhitespace returns true if s contains any whitespace character.
func containsWhitespace(s string) bool {
	return strings.ContainsFunc(s, unicode.IsSpace)
}
