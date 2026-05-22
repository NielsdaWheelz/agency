package servicemanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig(t *testing.T) ServiceConfig {
	t.Helper()
	return ServiceConfig{
		ExePath: "/usr/local/bin/agency",
		DataDir: "/home/user/.local/share/agency",
		HomeDir: t.TempDir(),
	}
}

// cmdKey builds the FakeCommandRunner lookup key "name arg1 arg2 ...".
func cmdKey(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func commandSucceeds(fr *testutil.FakeCommandRunner, name string, args ...string) {
	fr.Responses[cmdKey(name, args)] = testutil.FakeResponse{ExitCode: 0}
}

// --- Unit tests: plist/unit generation ---

func TestGenerateLaunchdPlist_ContainsRequiredFields(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	plist, err := generateLaunchdPlist(cfg)
	require.NoError(t, err)

	assert.Contains(t, plist, "<string>"+LaunchdLabel+"</string>")
	assert.Contains(t, plist, "<string>"+cfg.ExePath+"</string>")
	assert.Contains(t, plist, "<string>daemon</string>")
	assert.Contains(t, plist, "<string>start</string>")
	assert.Contains(t, plist, "<string>--foreground</string>")
	assert.Contains(t, plist, "<key>RunAtLoad</key>")
	assert.Contains(t, plist, "<true/>")
	assert.Contains(t, plist, "<key>KeepAlive</key>")
	assert.Contains(t, plist, daemonLogPath(cfg))
	assert.Contains(t, plist, "<?xml version=")
	assert.Contains(t, plist, "<!DOCTYPE plist")
}

func TestGenerateLaunchdPlist_XMLEscapesValues(t *testing.T) {
	t.Parallel()
	cfg := ServiceConfig{
		ExePath: `/usr/local/bin/agency<script>alert("xss")</script>`,
		DataDir: `/home/user/.local/share/agency&"quoted"`,
		HomeDir: t.TempDir(),
	}
	plist, err := generateLaunchdPlist(cfg)
	require.NoError(t, err)

	// Angle brackets, ampersands, and quotes must be escaped.
	assert.NotContains(t, plist, `<script>`)
	assert.NotContains(t, plist, `alert("xss")`)
	assert.Contains(t, plist, "&lt;script&gt;")
	assert.Contains(t, plist, "&amp;&#34;quoted&#34;")
}

func TestGenerateSystemdUnit_ContainsRequiredFields(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	unit, err := generateSystemdUnit(cfg)
	require.NoError(t, err)

	assert.Contains(t, unit, "[Unit]")
	assert.Contains(t, unit, "Description=Agency Daemon")
	assert.Contains(t, unit, "[Service]")
	assert.Contains(t, unit, "Type=simple")
	assert.Contains(t, unit, cfg.ExePath+" daemon start --foreground")
	assert.Contains(t, unit, "Restart=on-failure")
	assert.Contains(t, unit, "RestartSec=5")
	assert.Contains(t, unit, daemonLogPath(cfg))
	assert.Contains(t, unit, "[Install]")
	assert.Contains(t, unit, "WantedBy=default.target")
}

// --- Unit tests: DetectForOS ---

func TestDetectForOS_Darwin(t *testing.T) {
	t.Parallel()
	m, err := DetectForOS(nil, "darwin")
	require.NoError(t, err)
	assert.Equal(t, "launchd", m.Name())
}

func TestDetectForOS_Linux(t *testing.T) {
	t.Parallel()
	m, err := DetectForOS(nil, "linux")
	require.NoError(t, err)
	assert.Equal(t, "systemd", m.Name())
}

func TestDetectForOS_Unsupported(t *testing.T) {
	t.Parallel()
	_, err := DetectForOS(nil, "freebsd")
	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceUnsupported, ae.Code)
}

// --- Integration tests: launchd install/uninstall ---

func TestLaunchdManager_Install_Success(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	fr := testutil.NewFakeCommandRunner()

	m := NewLaunchdManager(fr)
	plistPath := m.ServiceFilePath(cfg)
	commandSucceeds(fr, "launchctl", "load", "-w", plistPath)

	err := m.Install(context.Background(), cfg)
	require.NoError(t, err)

	// Verify plist was written.
	assert.FileExists(t, plistPath)
	data, err := os.ReadFile(plistPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), cfg.ExePath)
	assert.Contains(t, string(data), "--foreground")

	// Verify launchctl was called.
	assert.Contains(t, fr.Calls, cmdKey("launchctl", []string{"load", "-w", plistPath}))
}

func TestLaunchdManager_Install_AlreadyInstalled(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	m := NewLaunchdManager(testutil.NewFakeCommandRunner())

	// Pre-create the plist file.
	plistPath := m.ServiceFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("existing"), 0o644))

	err := m.Install(context.Background(), cfg)
	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceAlreadyInstalled, ae.Code)
}

func TestLaunchdManager_Uninstall_Success(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	fr := testutil.NewFakeCommandRunner()

	m := NewLaunchdManager(fr)

	// Pre-create the plist file.
	plistPath := m.ServiceFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("existing"), 0o644))
	commandSucceeds(fr, "launchctl", "unload", "-w", plistPath)

	err := m.Uninstall(context.Background(), cfg)
	require.NoError(t, err)
	assert.NoFileExists(t, plistPath)
}

func TestLaunchdManager_Uninstall_NotInstalled(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	m := NewLaunchdManager(testutil.NewFakeCommandRunner())

	err := m.Uninstall(context.Background(), cfg)
	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceNotInstalled, ae.Code)
}

func TestLaunchdManager_Install_LaunchctlLoadFails(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	fr := testutil.NewFakeCommandRunner()
	plistPath := filepath.Join(cfg.HomeDir, "Library", "LaunchAgents", LaunchdLabel+".plist")
	fr.Responses["launchctl load -w "+plistPath] = testutil.FakeResponse{ExitCode: 1, Stderr: "load error"}

	m := NewLaunchdManager(fr)
	err := m.Install(context.Background(), cfg)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceInstallFailed, ae.Code)
	// Plist should be cleaned up on failure.
	assert.NoFileExists(t, plistPath)
}

// --- Integration tests: systemd install/uninstall ---

func TestSystemdManager_Install_Success(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	fr := testutil.NewFakeCommandRunner()
	commandSucceeds(fr, "systemctl", "--user", "daemon-reload")
	commandSucceeds(fr, "systemctl", "--user", "enable", SystemdUnit)
	commandSucceeds(fr, "systemctl", "--user", "start", SystemdUnit)

	m := NewSystemdManager(fr)
	err := m.Install(context.Background(), cfg)
	require.NoError(t, err)

	// Verify unit file was written.
	unitPath := m.ServiceFilePath(cfg)
	assert.FileExists(t, unitPath)
	data, err := os.ReadFile(unitPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), cfg.ExePath)
	assert.Contains(t, string(data), "--foreground")

	// Verify systemctl was called.
	assert.Contains(t, fr.Calls, cmdKey("systemctl", []string{"--user", "daemon-reload"}))
	assert.Contains(t, fr.Calls, cmdKey("systemctl", []string{"--user", "enable", SystemdUnit}))
	assert.Contains(t, fr.Calls, cmdKey("systemctl", []string{"--user", "start", SystemdUnit}))
}

func TestSystemdManager_Install_SystemctlEnableFails(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	fr := testutil.NewFakeCommandRunner()
	commandSucceeds(fr, "systemctl", "--user", "daemon-reload")
	fr.Responses["systemctl --user enable "+SystemdUnit] = testutil.FakeResponse{ExitCode: 1, Stderr: "enable error"}
	commandSucceeds(fr, "systemctl", "--user", "stop", SystemdUnit)
	commandSucceeds(fr, "systemctl", "--user", "disable", SystemdUnit)

	m := NewSystemdManager(fr)
	err := m.Install(context.Background(), cfg)

	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceInstallFailed, ae.Code)
	// Unit file should be cleaned up on failure.
	assert.NoFileExists(t, m.ServiceFilePath(cfg))
}

func TestSystemdManager_Install_AlreadyInstalled(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	m := NewSystemdManager(testutil.NewFakeCommandRunner())

	// Pre-create the unit file.
	unitPath := m.ServiceFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("existing"), 0o644))

	err := m.Install(context.Background(), cfg)
	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceAlreadyInstalled, ae.Code)
}

func TestSystemdManager_Uninstall_Success(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	fr := testutil.NewFakeCommandRunner()

	m := NewSystemdManager(fr)

	// Pre-create the unit file.
	unitPath := m.ServiceFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("existing"), 0o644))
	commandSucceeds(fr, "systemctl", "--user", "stop", SystemdUnit)
	commandSucceeds(fr, "systemctl", "--user", "disable", SystemdUnit)
	commandSucceeds(fr, "systemctl", "--user", "daemon-reload")

	err := m.Uninstall(context.Background(), cfg)
	require.NoError(t, err)
	assert.NoFileExists(t, unitPath)
}

func TestSystemdManager_Uninstall_NotInstalled(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	m := NewSystemdManager(testutil.NewFakeCommandRunner())

	err := m.Uninstall(context.Background(), cfg)
	require.Error(t, err)
	ae, ok := errors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, errors.EDaemonServiceNotInstalled, ae.Code)
}

// --- Integration test: IsInstalled ---

func TestManager_IsInstalled(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)

	m := NewLaunchdManager(nil)
	assert.False(t, m.IsInstalled(cfg))

	// Create the file.
	plistPath := m.ServiceFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("x"), 0o644))
	assert.True(t, m.IsInstalled(cfg))
}
