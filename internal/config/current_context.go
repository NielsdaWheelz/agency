package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

type CurrentContext struct {
	Version      int    `json:"version"`
	RepoID       string `json:"repo_id"`
	RepoName     string `json:"repo_name"`
	WorktreeID   string `json:"worktree_id"`
	WorktreeName string `json:"worktree_name"`
	UpdatedAt    string `json:"updated_at"`
}

func CurrentContextPath(configDir string) string {
	return filepath.Join(configDir, "current-context.json")
}

func LoadCurrentContext(filesystem fs.FS, configDir string) (CurrentContext, error) {
	path := CurrentContextPath(configDir)

	data, err := filesystem.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CurrentContext{}, errors.NewWithDetails(
				errors.ENoContext,
				"active context not set",
				map[string]string{
					"path": path,
					"hint": "run `agency context use <worktree-ref> [--repo <repo-ref>]`",
				},
			)
		}
		return CurrentContext{}, errors.Wrap(errors.EInvalidContext, "failed to read active context", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "invalid active context json: "+err.Error())
	}

	allowedKeys := map[string]bool{
		"version":       true,
		"repo_id":       true,
		"repo_name":     true,
		"worktree_id":   true,
		"worktree_name": true,
		"updated_at":    true,
	}
	for key := range raw {
		if !allowedKeys[key] {
			return CurrentContext{}, errors.New(errors.EInvalidContext, "active context contains unknown field: "+key)
		}
	}

	var current CurrentContext
	if err := json.Unmarshal(data, &current); err != nil {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "invalid active context json: "+err.Error())
	}
	if current.Version != 1 {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "active context version must be 1")
	}
	if current.RepoID == "" {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "active context is missing repo_id")
	}
	if current.RepoName == "" {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "active context is missing repo_name")
	}
	if current.WorktreeID == "" {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "active context is missing worktree_id")
	}
	if current.WorktreeName == "" {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "active context is missing worktree_name")
	}
	if current.UpdatedAt == "" {
		return CurrentContext{}, errors.New(errors.EInvalidContext, "active context is missing updated_at")
	}

	return current, nil
}

func SaveCurrentContext(filesystem fs.FS, configDir string, current CurrentContext) error {
	current.Version = 1
	if current.RepoID == "" {
		return errors.New(errors.EInvalidContext, "active context is missing repo_id")
	}
	if current.RepoName == "" {
		return errors.New(errors.EInvalidContext, "active context is missing repo_name")
	}
	if current.WorktreeID == "" {
		return errors.New(errors.EInvalidContext, "active context is missing worktree_id")
	}
	if current.WorktreeName == "" {
		return errors.New(errors.EInvalidContext, "active context is missing worktree_name")
	}
	if current.UpdatedAt == "" {
		return errors.New(errors.EInvalidContext, "active context is missing updated_at")
	}

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return errors.Wrap(errors.EInvalidContext, "failed to encode active context", err)
	}
	data = append(data, '\n')

	if err := filesystem.MkdirAll(configDir, 0o755); err != nil {
		return errors.Wrap(errors.EInvalidContext, "failed to create active context directory", err)
	}
	if err := fs.WriteFileAtomic(filesystem, CurrentContextPath(configDir), data, 0o644); err != nil {
		return errors.Wrap(errors.EInvalidContext, "failed to write active context", err)
	}

	return nil
}

func RemoveCurrentContext(filesystem fs.FS, configDir string) error {
	path := CurrentContextPath(configDir)
	if err := filesystem.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(errors.EInvalidContext, "failed to remove active context", err)
	}
	return nil
}
