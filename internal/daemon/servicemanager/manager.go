// Package servicemanager provides OS service manager integration for the
// agency daemon. It supports launchd (macOS) and systemd (Linux).
package servicemanager

import (
	"context"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
)

const (
	// LaunchdLabel is the launchd service label.
	LaunchdLabel = "com.agency.daemon"
	// SystemdUnit is the systemd unit name.
	SystemdUnit = "agency-daemon"
)

// ServiceConfig holds the configuration needed to generate and install
// a service definition.
type ServiceConfig struct {
	ExePath string // absolute path to the agency binary
	DataDir string // agency data directory (for log paths)
	HomeDir string // user home directory (for service file paths)
}

// Manager abstracts OS-level service installation.
type Manager interface {
	// Install writes the service configuration file and registers it
	// with the OS service manager.
	Install(ctx context.Context, cfg ServiceConfig) error

	// Uninstall deregisters the service and removes its configuration file.
	Uninstall(ctx context.Context, cfg ServiceConfig) error

	// IsInstalled returns true if the service configuration file exists.
	IsInstalled(cfg ServiceConfig) bool

	// ServiceFilePath returns the absolute path to the service configuration file.
	ServiceFilePath(cfg ServiceConfig) string

	// Name returns a human-readable name for the service manager (e.g. "launchd").
	Name() string
}

// DetectForOS returns the service manager for the given GOOS value.
func DetectForOS(cr exec.CommandRunner, goos string) (Manager, error) {
	switch goos {
	case "darwin":
		return NewLaunchdManager(cr), nil
	case "linux":
		return NewSystemdManager(cr), nil
	default:
		return nil, errors.New(errors.EDaemonServiceUnsupported,
			"service manager integration is not supported on "+goos+"; use 'agency daemon start' instead")
	}
}
