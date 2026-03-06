package servicemanager

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

var systemdUnitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=Agency Daemon
After=default.target

[Service]
Type=simple
ExecStart={{ .ExePath }} daemon start --foreground
Restart=on-failure
RestartSec=5
StandardOutput=append:{{ .LogPath }}
StandardError=append:{{ .LogPath }}

[Install]
WantedBy=default.target
`))

// SystemdManager implements Manager for Linux systemd (user mode).
type SystemdManager struct {
	cr exec.CommandRunner
}

// NewSystemdManager creates a SystemdManager.
func NewSystemdManager(cr exec.CommandRunner) *SystemdManager {
	return &SystemdManager{cr: cr}
}

func (m *SystemdManager) Name() string { return "systemd" }

func (m *SystemdManager) ServiceFilePath(cfg ServiceConfig) string {
	return filepath.Join(cfg.HomeDir, ".config", "systemd", "user", SystemdUnit+".service")
}

func (m *SystemdManager) IsInstalled(cfg ServiceConfig) bool {
	_, err := os.Stat(m.ServiceFilePath(cfg))
	return err == nil
}

// GenerateSystemdUnit returns the rendered systemd unit file content.
func GenerateSystemdUnit(cfg ServiceConfig) (string, error) {
	data := struct {
		ExePath string
		LogPath string
	}{
		ExePath: cfg.ExePath,
		LogPath: filepath.Join(cfg.DataDir, "agencyd.log"),
	}
	var buf bytes.Buffer
	if err := systemdUnitTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *SystemdManager) Install(ctx context.Context, cfg ServiceConfig) error {
	unitPath := m.ServiceFilePath(cfg)

	if m.IsInstalled(cfg) {
		return errors.New(errors.EDaemonServiceAlreadyInstalled,
			fmt.Sprintf("service already installed at %s", unitPath))
	}

	content, err := GenerateSystemdUnit(cfg)
	if err != nil {
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to generate unit file", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to create systemd user directory", err)
	}

	if err := os.WriteFile(unitPath, []byte(content), 0o600); err != nil {
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to write unit file", err)
	}

	// Reload, enable, and start. On failure, roll back prior steps.
	rollback := func() {
		_, _ = m.cr.Run(ctx, "systemctl", []string{"--user", "stop", SystemdUnit}, exec.RunOpts{})
		_, _ = m.cr.Run(ctx, "systemctl", []string{"--user", "disable", SystemdUnit}, exec.RunOpts{})
		_ = os.Remove(unitPath)
		_, _ = m.cr.Run(ctx, "systemctl", []string{"--user", "daemon-reload"}, exec.RunOpts{})
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", SystemdUnit},
		{"--user", "start", SystemdUnit},
	} {
		result, err := m.cr.Run(ctx, "systemctl", args, exec.RunOpts{})
		if err != nil {
			rollback()
			return errors.Wrap(errors.EDaemonServiceInstallFailed,
				fmt.Sprintf("systemctl %s failed", args[1]), err)
		}
		if result.ExitCode != 0 {
			rollback()
			return errors.New(errors.EDaemonServiceInstallFailed,
				fmt.Sprintf("systemctl %s failed (exit %d): %s", args[1], result.ExitCode, result.Stderr))
		}
	}

	return nil
}

func (m *SystemdManager) Uninstall(ctx context.Context, cfg ServiceConfig) error {
	unitPath := m.ServiceFilePath(cfg)

	if !m.IsInstalled(cfg) {
		return errors.New(errors.EDaemonServiceNotInstalled, "service is not installed")
	}

	// Stop and disable (ignore errors — service may not be running).
	for _, args := range [][]string{
		{"--user", "stop", SystemdUnit},
		{"--user", "disable", SystemdUnit},
	} {
		_, _ = m.cr.Run(ctx, "systemctl", args, exec.RunOpts{})
	}

	if err := os.Remove(unitPath); err != nil {
		return errors.Wrap(errors.EDaemonServiceUninstallFailed, "failed to remove unit file", err)
	}

	// Reload after removal.
	_, _ = m.cr.Run(ctx, "systemctl", []string{"--user", "daemon-reload"}, exec.RunOpts{})

	return nil
}
