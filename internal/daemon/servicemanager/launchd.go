package servicemanager

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

// xmlEscape returns the XML-escaped representation of s, preventing injection
// in the generated plist. Uses encoding/xml.EscapeText.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

var plistFuncMap = template.FuncMap{"xmlesc": xmlEscape}

var launchdPlistTmpl = template.Must(template.New("plist").Funcs(plistFuncMap).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{ .Label | xmlesc }}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{ .ExePath | xmlesc }}</string>
        <string>daemon</string>
        <string>start</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>{{ .LogPath | xmlesc }}</string>
    <key>StandardErrorPath</key>
    <string>{{ .LogPath | xmlesc }}</string>
    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
`))

// LaunchdManager implements Manager for macOS launchd.
type LaunchdManager struct {
	cr exec.CommandRunner
}

// NewLaunchdManager creates a LaunchdManager.
func NewLaunchdManager(cr exec.CommandRunner) *LaunchdManager {
	return &LaunchdManager{cr: cr}
}

func (m *LaunchdManager) Name() string { return "launchd" }

func (m *LaunchdManager) ServiceFilePath(cfg ServiceConfig) string {
	return filepath.Join(cfg.HomeDir, "Library", "LaunchAgents", LaunchdLabel+".plist")
}

func (m *LaunchdManager) IsInstalled(cfg ServiceConfig) bool {
	_, err := os.Stat(m.ServiceFilePath(cfg))
	return err == nil
}

// GenerateLaunchdPlist returns the rendered launchd plist XML.
// All dynamic values are XML-escaped to prevent injection.
func GenerateLaunchdPlist(cfg ServiceConfig) (string, error) {
	data := struct {
		Label   string
		ExePath string
		LogPath string
	}{
		Label:   LaunchdLabel,
		ExePath: cfg.ExePath,
		LogPath: filepath.Join(cfg.DataDir, "agencyd.log"),
	}
	var buf bytes.Buffer
	if err := launchdPlistTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *LaunchdManager) Install(ctx context.Context, cfg ServiceConfig) error {
	plistPath := m.ServiceFilePath(cfg)

	if m.IsInstalled(cfg) {
		return errors.New(errors.EDaemonServiceAlreadyInstalled,
			fmt.Sprintf("service already installed at %s", plistPath))
	}

	content, err := GenerateLaunchdPlist(cfg)
	if err != nil {
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to generate plist", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o700); err != nil {
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to create LaunchAgents directory", err)
	}

	if err := os.WriteFile(plistPath, []byte(content), 0o600); err != nil {
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to write plist", err)
	}

	// Load the service.
	result, err := m.cr.Run(ctx, "launchctl", []string{"load", "-w", plistPath}, exec.RunOpts{})
	if err != nil {
		// Clean up the plist file on load failure.
		_ = os.Remove(plistPath)
		return errors.Wrap(errors.EDaemonServiceInstallFailed, "failed to load service with launchctl", err)
	}
	if result.ExitCode != 0 {
		_ = os.Remove(plistPath)
		return errors.New(errors.EDaemonServiceInstallFailed,
			fmt.Sprintf("launchctl load failed (exit %d): %s", result.ExitCode, result.Stderr))
	}

	return nil
}

func (m *LaunchdManager) Uninstall(ctx context.Context, cfg ServiceConfig) error {
	plistPath := m.ServiceFilePath(cfg)

	if !m.IsInstalled(cfg) {
		return errors.New(errors.EDaemonServiceNotInstalled, "service is not installed")
	}

	// Unload the service (ignore errors — service may not be loaded).
	_, _ = m.cr.Run(ctx, "launchctl", []string{"unload", "-w", plistPath}, exec.RunOpts{})

	if err := os.Remove(plistPath); err != nil {
		return errors.Wrap(errors.EDaemonServiceUninstallFailed, "failed to remove plist", err)
	}

	return nil
}
