// Package config handles loading and validation of agency.json configuration files.
// This file provides shared runner resolution logic.
package config

import "github.com/NielsdaWheelz/agency/internal/errors"

// ResolveRunnerCmd resolves the runner command from config and runner name.
//
// Resolution rules (per constitution):
//  1. If cfg.Runners[runnerName] exists and is non-empty, use it
//  2. Else if runnerName is "claude" or "codex", use that string (PATH lookup)
//  3. Else return E_RUNNER_NOT_CONFIGURED
//
// Returns the resolved command string (single executable, no args in v1).
func ResolveRunnerCmd(cfg *AgencyConfig, runnerName string) (string, error) {
	// Check if the runner is explicitly configured
	if cfg.Runners != nil {
		if cmd, ok := cfg.Runners[runnerName]; ok {
			if cmd != "" {
				return cmd, nil
			}
			// Empty string in runners map is an error
			return "", errors.New(errors.ERunnerNotConfigured,
				"runner \""+runnerName+"\" has empty command in runners config")
		}
	}

	// Standard runners fallback to PATH
	if runnerName == "claude" || runnerName == "codex" {
		return runnerName, nil
	}

	// Unknown runner without explicit config
	return "", errors.New(errors.ERunnerNotConfigured,
		"runner \""+runnerName+"\" not configured; set runners."+runnerName+" or choose claude/codex")
}
