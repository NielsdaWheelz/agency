package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

type stubRunner struct {
	paths map[string]string
}

func (s stubRunner) Run(_ context.Context, _ string, _ []string, _ exec.RunOpts) (exec.CmdResult, error) {
	return exec.CmdResult{}, nil
}

func (s stubRunner) LookPath(file string) (string, error) {
	if p, ok := s.paths[file]; ok {
		return p, nil
	}
	return "", os.ErrNotExist
}

func TestLoadUserConfig_MissingFile(t *testing.T) {
	stub := newStubFS()
	cfg, found, err := LoadUserConfig(stub, "/cfg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for missing config")
	}
	if cfg.Defaults.Runner != "claude" {
		t.Errorf("Defaults.Runner = %q, want %q", cfg.Defaults.Runner, "claude")
	}
	if cfg.Defaults.Editor != "code" {
		t.Errorf("Defaults.Editor = %q, want %q", cfg.Defaults.Editor, "code")
	}
}

func TestLoadUserConfig_InvalidJSON(t *testing.T) {
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{"version": 1, "defaults": {`)
	_, _, err := LoadUserConfig(stub, "/cfg")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if errors.GetCode(err) != errors.EInvalidUserConfig {
		t.Errorf("expected E_INVALID_USER_CONFIG, got %s", errors.GetCode(err))
	}
}

func TestLoadUserConfig_UnknownKeys(t *testing.T) {
	stub := newStubFS()
	stub.files["/cfg/config.json"] = []byte(`{
  "version": 1,
  "defaults": { "runner": "claude", "editor": "code" },
  "extra": "nope"
}`)
	_, _, err := LoadUserConfig(stub, "/cfg")
	if err == nil {
		t.Fatal("expected error for unknown keys")
	}
	if errors.GetCode(err) != errors.EInvalidUserConfig {
		t.Errorf("expected E_INVALID_USER_CONFIG, got %s", errors.GetCode(err))
	}
}

func TestValidateUserConfig_RequiredFields(t *testing.T) {
	cfg := UserConfig{Version: 1}
	_, err := ValidateUserConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if errors.GetCode(err) != errors.EInvalidUserConfig {
		t.Errorf("expected E_INVALID_USER_CONFIG, got %s", errors.GetCode(err))
	}
}

func TestResolveRunnerCmd_Path(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "bin", "runner")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	cfg := UserConfig{
		Version: 1,
		Defaults: UserDefaults{
			Runner: "custom",
			Editor: "code",
		},
		Runners: map[string]string{
			"custom": "bin/runner",
		},
	}

	cmd, err := ResolveRunnerCmd(stubRunner{}, fs.NewRealFS(), tmpDir, cfg, "custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != binPath {
		t.Errorf("cmd = %q, want %q", cmd, binPath)
	}
}

func TestResolveEditorCmd_Path(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "bin", "editor")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	cfg := UserConfig{
		Version: 1,
		Defaults: UserDefaults{
			Runner: "claude",
			Editor: "custom",
		},
		Editors: map[string]string{
			"custom": "bin/editor",
		},
	}

	cmd, err := ResolveEditorCmd(stubRunner{}, fs.NewRealFS(), tmpDir, cfg, "custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != binPath {
		t.Errorf("cmd = %q, want %q", cmd, binPath)
	}
}
