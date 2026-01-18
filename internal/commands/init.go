// Package commands implements agency CLI commands.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/git"
	"github.com/NielsdaWheelz/agency/internal/paths"
	"github.com/NielsdaWheelz/agency/internal/scaffold"
)

// InitOpts holds options for the init command.
type InitOpts struct {
	NoGitignore bool
	Force       bool
}

// InitResult holds the result of the init command for output formatting.
type InitResult struct {
	RepoRoot        string
	AgencyJSONState string // "created" or "overwritten"
	ScriptsCreated  []string
	GitignoreState  scaffold.GitignoreResult
	UserConfigPath  string
	UserConfigState string // "created" or "exists"
}

// Init implements the `agency init` command.
// Creates agency.json, stub scripts (if missing), and updates .gitignore (by default).
func Init(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts InitOpts, stdout, stderr io.Writer) error {
	// Discover repo root
	repoRoot, err := git.GetRepoRoot(ctx, cr, cwd)
	if err != nil {
		return err
	}

	// Ensure user config exists (user-level, not repo-level)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(errors.EInternal, "failed to get home directory", err)
	}
	dirs := paths.ResolveDirs(osEnv{}, homeDir)
	userConfigPath := config.UserConfigPath(dirs.ConfigDir)
	userConfigState := "exists"

	if _, err := fsys.Stat(userConfigPath); err != nil {
		if os.IsNotExist(err) {
			if err := fsys.MkdirAll(dirs.ConfigDir, 0o755); err != nil {
				return errors.Wrap(errors.EInvalidUserConfig, "failed to create user config directory", err)
			}
			cfg := config.DefaultUserConfig()
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to serialize user config", err)
			}
			data = append(data, '\n')
			if err := fs.WriteFileAtomic(fsys, userConfigPath, data, 0o644); err != nil {
				return errors.Wrap(errors.EInvalidUserConfig, "failed to write user config", err)
			}
			userConfigState = "created"
		} else {
			return errors.Wrap(errors.EInvalidUserConfig, "failed to check user config", err)
		}
	}

	agencyJSONPath := filepath.Join(repoRoot.Path, "agency.json")

	// Check if agency.json exists
	_, err = fsys.Stat(agencyJSONPath)
	agencyJSONExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(errors.ENoRepo, "failed to check agency.json", err)
	}

	// If exists and not --force, error
	if agencyJSONExists && !opts.Force {
		return errors.New(errors.EAgencyJSONExists, "agency.json already exists; use --force to overwrite")
	}

	// Determine state for output
	agencyJSONState := "created"
	if agencyJSONExists {
		agencyJSONState = "overwritten"
	}

	// Write agency.json atomically
	if err := fs.WriteFileAtomic(fsys, agencyJSONPath, []byte(scaffold.AgencyJSONTemplate), 0644); err != nil {
		return errors.Wrap(errors.ENoRepo, "failed to write agency.json", err)
	}

	// Create stub scripts (never overwrite existing)
	stubsResult, err := scaffold.CreateStubs(fsys, repoRoot.Path)
	if err != nil {
		return errors.Wrap(errors.ENoRepo, "failed to create stub scripts", err)
	}

	// Handle .gitignore
	var gitignoreState scaffold.GitignoreResult
	if opts.NoGitignore {
		gitignoreState = scaffold.GitignoreSkipped
	} else {
		gitignorePath := filepath.Join(repoRoot.Path, ".gitignore")
		gitignoreState, err = scaffold.EnsureGitignore(fsys, gitignorePath)
		if err != nil {
			return errors.Wrap(errors.ENoRepo, "failed to update .gitignore", err)
		}
	}

	// Build result
	result := InitResult{
		RepoRoot:        repoRoot.Path,
		AgencyJSONState: agencyJSONState,
		ScriptsCreated:  stubsResult.Created,
		GitignoreState:  gitignoreState,
		UserConfigPath:  userConfigPath,
		UserConfigState: userConfigState,
	}

	// Output result
	writeInitOutput(stdout, result)

	// Warning if gitignore skipped (informational output to user)
	if opts.NoGitignore {
		_, _ = fmt.Fprintln(stdout, "warning: gitignore_skipped")
	}

	return nil
}

// writeInitOutput writes the stable key: value output for init.
// All writes use explicit error ignoring since this is informational output
// where write failures cannot be meaningfully handled.
func writeInitOutput(w io.Writer, r InitResult) {
	_, _ = fmt.Fprintf(w, "repo_root: %s\n", r.RepoRoot)
	_, _ = fmt.Fprintf(w, "agency_json: %s\n", r.AgencyJSONState)
	_, _ = fmt.Fprintf(w, "user_config_path: %s\n", r.UserConfigPath)
	_, _ = fmt.Fprintf(w, "user_config: %s\n", r.UserConfigState)

	scriptsCreated := "none"
	if len(r.ScriptsCreated) > 0 {
		scriptsCreated = strings.Join(r.ScriptsCreated, ", ")
	}
	_, _ = fmt.Fprintf(w, "scripts_created: %s\n", scriptsCreated)

	_, _ = fmt.Fprintf(w, "gitignore: %s\n", r.GitignoreState)
}
