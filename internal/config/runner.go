// Package config handles loading and validation of agency configuration files.
// This file provides shared runner/editor resolution logic.
package config

import (
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
	agencyexec "github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

// ResolveRunnerCmd resolves the runner command from user config and runner name.
func ResolveRunnerCmd(cr agencyexec.CommandRunner, fsys fs.FS, configDir string, cfg UserConfig, runnerName string) (string, error) {
	runnerKeys, capabilityErr := runners.ConfigLookupKeys(runnerName)
	cmd := ""

	if capabilityErr == nil {
		// For known runners, prefer canonical key first, then compatibility aliases.
		if cfg.Runners != nil {
			for _, key := range runnerKeys {
				if val, ok := cfg.Runners[key]; ok && val != "" {
					cmd = val
					break
				}
			}
		}
	} else if cfg.Runners != nil {
		// Preserve support for explicitly configured non-target custom runner names.
		if val, ok := cfg.Runners[runnerName]; ok && val != "" {
			cmd = val
		}
	}

	if cmd == "" {
		if capabilityErr == nil {
			return "", errors.New(errors.ERunnerNotConfigured,
				"runner \""+runnerName+"\" not configured; set runners."+runnerName+
					" (explicit runner config is required; supported runners: "+strings.Join(runners.CanonicalIDs(), ", ")+")")
		}
		return "", errors.New(errors.ERunnerNotConfigured,
			"runner \""+runnerName+"\" not configured; set runners."+runnerName+" (explicit runner config is required)")
	}

	return resolveCommand(cr, fsys, configDir, cmd, errors.ERunnerNotConfigured, "runner")
}

// ResolveEditorCmd resolves the editor command from user config and editor name.
func ResolveEditorCmd(cr agencyexec.CommandRunner, fsys fs.FS, configDir string, cfg UserConfig, editorName string) (string, error) {
	cmd := ""
	if cfg.Editors != nil {
		if val, ok := cfg.Editors[editorName]; ok {
			cmd = val
		}
	}
	if cmd == "" {
		cmd = editorName
	}

	return resolveCommand(cr, fsys, configDir, cmd, errors.EEditorNotConfigured, "editor")
}

func resolveCommand(cr agencyexec.CommandRunner, fsys fs.FS, configDir, cmd string, errCode errors.Code, label string) (string, error) {
	if strings.Contains(cmd, string(filepath.Separator)) || strings.HasPrefix(cmd, ".") {
		absPath := cmd
		if !filepath.IsAbs(cmd) {
			absPath = filepath.Join(configDir, cmd)
		}
		info, err := fsys.Stat(absPath)
		if err != nil {
			return "", errors.New(errCode, label+" command not found: "+cmd)
		}
		if info.Mode().Perm()&0111 == 0 {
			return "", errors.New(errCode, label+" command is not executable: "+cmd)
		}
		return absPath, nil
	}

	path, err := cr.LookPath(cmd)
	if err != nil {
		return "", errors.New(errCode, label+" command not found on PATH: "+cmd)
	}
	return path, nil
}
