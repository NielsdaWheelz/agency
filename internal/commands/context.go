package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/ids"
)

type currentContextEnvelope struct {
	OK           bool   `json:"ok"`
	Active       bool   `json:"active"`
	RepoID       string `json:"repo_id,omitempty"`
	RepoName     string `json:"repo_name,omitempty"`
	WorktreeID   string `json:"worktree_id,omitempty"`
	WorktreeName string `json:"worktree_name,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type validatedCurrentContext struct {
	RepoID       string
	RepoName     string
	WorktreeID   string
	WorktreeName string
	UpdatedAt    string
}

type ContextShowOpts struct {
	JSON bool
}

type ContextUseOpts struct {
	RepoRef     string
	WorktreeRef string
	JSON        bool
}

type ContextUnsetOpts struct {
	JSON bool
}

func ContextShow(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts ContextShowOpts, stdout, stderr io.Writer) error {
	_ = cr
	_ = cwd
	_ = stderr

	if fsys == nil {
		fsys = fs.NewRealFS()
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	current, err := loadValidatedCurrentContext(ctx, ns.client, fsys, ns.dirs.ConfigDir)
	if err != nil {
		if errors.GetCode(err) != errors.ENoContext {
			return err
		}
		if opts.JSON {
			return writeCurrentContextJSON(stdout, currentContextEnvelope{OK: true, Active: false})
		}
		_, _ = fmt.Fprintln(stdout, "No active context set.")
		return nil
	}

	if opts.JSON {
		return writeCurrentContextJSON(stdout, currentContextEnvelope{
			OK:           true,
			Active:       true,
			RepoID:       current.RepoID,
			RepoName:     current.RepoName,
			WorktreeID:   current.WorktreeID,
			WorktreeName: current.WorktreeName,
			UpdatedAt:    current.UpdatedAt,
		})
	}

	_, _ = fmt.Fprintf(stdout, "repo:       %s (%s)\n", current.RepoName, current.RepoID)
	_, _ = fmt.Fprintf(stdout, "worktree:   %s (%s)\n", current.WorktreeName, current.WorktreeID)
	_, _ = fmt.Fprintf(stdout, "updated_at: %s\n", current.UpdatedAt)
	return nil
}

func ContextUse(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts ContextUseOpts, stdout, stderr io.Writer) error {
	_ = stderr

	if cr == nil {
		cr = exec.NewRealRunner()
	}
	if fsys == nil {
		fsys = fs.NewRealFS()
	}

	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       strings.TrimSpace(opts.RepoRef),
		AllowAllRepos: false,
		CmdName:       "context use",
	})
	if err != nil {
		return err
	}

	repo, err := resolveAccessibleRepo(ctx, ns.client, repoCtx.RepoID)
	if err != nil {
		return err
	}
	result, err := ns.client.GetWorktree(ctx, strings.TrimSpace(opts.WorktreeRef), repoCtx.RepoID)
	if err != nil {
		return err
	}
	worktree, err := requirePresentWorktree(result.Data, "active context requires a present integration worktree")
	if err != nil {
		return err
	}

	current := config.CurrentContext{
		RepoID:       repo.RepoID,
		RepoName:     repoDisplayName(repo),
		WorktreeID:   worktree.WorktreeID,
		WorktreeName: worktree.WorktreeName,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := config.SaveCurrentContext(fsys, ns.dirs.ConfigDir, current); err != nil {
		return err
	}

	if opts.JSON {
		return writeCurrentContextJSON(stdout, currentContextEnvelope{
			OK:           true,
			Active:       true,
			RepoID:       current.RepoID,
			RepoName:     current.RepoName,
			WorktreeID:   current.WorktreeID,
			WorktreeName: current.WorktreeName,
			UpdatedAt:    current.UpdatedAt,
		})
	}

	_, _ = fmt.Fprintln(stdout, "Active context updated.")
	_, _ = fmt.Fprintf(stdout, "  repo:       %s (%s)\n", current.RepoName, current.RepoID)
	_, _ = fmt.Fprintf(stdout, "  worktree:   %s (%s)\n", current.WorktreeName, current.WorktreeID)
	return nil
}

func ContextUnset(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts ContextUnsetOpts, stdout, stderr io.Writer) error {
	_ = ctx
	_ = cr
	_ = cwd
	_ = stderr

	if fsys == nil {
		fsys = fs.NewRealFS()
	}

	dirs, err := resolveCommandDirs("", "")
	if err != nil {
		return err
	}

	_, statErr := fsys.Stat(config.CurrentContextPath(dirs.ConfigDir))
	hadCurrent := statErr == nil
	if err := config.RemoveCurrentContext(fsys, dirs.ConfigDir); err != nil {
		return err
	}

	if opts.JSON {
		return writeCurrentContextJSON(stdout, currentContextEnvelope{OK: true, Active: false})
	}
	if hadCurrent {
		_, _ = fmt.Fprintln(stdout, "Unset active context.")
		return nil
	}
	_, _ = fmt.Fprintln(stdout, "No active context set.")
	return nil
}

func loadValidatedCurrentContext(ctx context.Context, client *daemonclient.Client, fsys fs.FS, configDir string) (*validatedCurrentContext, error) {
	current, err := config.LoadCurrentContext(fsys, configDir)
	if err != nil {
		return nil, err
	}

	repo, err := resolveAccessibleRepo(ctx, client, current.RepoID)
	if err != nil {
		return nil, errors.NewWithDetails(
			errors.EInvalidContext,
			"active context repo is no longer available",
			map[string]string{
				"hint": "run `agency context unset` or `agency context use <worktree-ref> [--repo <repo-ref>]`",
			},
		)
	}
	worktree, err := client.GetWorktree(ctx, current.WorktreeID, current.RepoID)
	if err != nil {
		return nil, errors.NewWithDetails(
			errors.EInvalidContext,
			"active context worktree is no longer available",
			map[string]string{
				"hint": "run `agency context unset` or `agency context use <worktree-ref> [--repo <repo-ref>]`",
			},
		)
	}
	if _, err := requirePresentWorktree(worktree.Data, "active context worktree is archived"); err != nil {
		return nil, errors.NewWithDetails(
			errors.EInvalidContext,
			"active context worktree is archived",
			map[string]string{
				"hint": "run `agency context unset` or `agency context use <worktree-ref> [--repo <repo-ref>]`",
			},
		)
	}

	worktreeName := strings.TrimSpace(worktree.Data.WorktreeName)
	if worktreeName == "" {
		worktreeName = current.WorktreeName
	}

	return &validatedCurrentContext{
		RepoID:       repo.RepoID,
		RepoName:     repoDisplayName(repo),
		WorktreeID:   worktree.Data.WorktreeID,
		WorktreeName: worktreeName,
		UpdatedAt:    current.UpdatedAt,
	}, nil
}

func repoDisplayName(repo daemon.RepoDTO) string {
	name := strings.TrimSpace(ids.RepoShortName(repo.RepoKey))
	if name != "" {
		return name
	}
	root := strings.TrimSpace(repo.PreferredRoot)
	if root != "" {
		base := filepath.Base(root)
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return strings.TrimSpace(repo.RepoID)
}

func writeCurrentContextJSON(w io.Writer, payload currentContextEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
