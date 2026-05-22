// Package paths provides directory resolution for agency following XDG conventions.
package paths

import (
	"path/filepath"
	"runtime"
)

// Dirs holds the resolved directory paths for agency data, config, and cache.
type Dirs struct {
	DataDir   string
	ConfigDir string
	CacheDir  string
}

// ResolveDirs computes the data, config, and cache directories from explicit
// environment overrides and platform defaults. Callers pass os.Getenv in
// production; tests pass a closure over a map.
//
// Resolution order for data directory:
//  1. AGENCY_DATA_DIR env var (if set)
//  2. macOS: ~/Library/Application Support/agency
//  3. XDG_DATA_HOME/agency (if set)
//  4. ~/.local/share/agency
//
// Resolution order for config directory:
//  1. AGENCY_CONFIG_DIR env var (if set)
//  2. macOS: ~/Library/Preferences/agency
//  3. XDG_CONFIG_HOME/agency (if set)
//  4. ~/.config/agency
//
// Resolution order for cache directory:
//  1. AGENCY_CACHE_DIR env var (if set)
//  2. macOS: ~/Library/Caches/agency
//  3. XDG_CACHE_HOME/agency (if set)
//  4. ~/.cache/agency
//
// The homeDir parameter must be an absolute path to the user's home directory.
// This function does not touch the filesystem (no mkdir).
// Path joining is OS-correct via filepath.Join.
// ~ inside env vars is treated as literal (not expanded).
func ResolveDirs(getenv func(string) string, homeDir string) Dirs {
	return resolveDirsWithOS(getenv, homeDir, runtime.GOOS == "darwin")
}

func resolveDirsWithOS(getenv func(string) string, homeDir string, isDarwin bool) Dirs {
	return Dirs{
		DataDir:   resolveDataDirWithOS(getenv, homeDir, isDarwin),
		ConfigDir: resolveConfigDirWithOS(getenv, homeDir, isDarwin),
		CacheDir:  resolveCacheDirWithOS(getenv, homeDir, isDarwin),
	}
}

func resolveDataDirWithOS(getenv func(string) string, homeDir string, isDarwin bool) string {
	if v := getenv("AGENCY_DATA_DIR"); v != "" {
		return v
	}
	if isDarwin {
		return filepath.Join(homeDir, "Library", "Application Support", "agency")
	}
	if v := getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "agency")
	}
	return filepath.Join(homeDir, ".local", "share", "agency")
}

func resolveConfigDirWithOS(getenv func(string) string, homeDir string, isDarwin bool) string {
	if v := getenv("AGENCY_CONFIG_DIR"); v != "" {
		return v
	}
	if isDarwin {
		return filepath.Join(homeDir, "Library", "Preferences", "agency")
	}
	if v := getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "agency")
	}
	return filepath.Join(homeDir, ".config", "agency")
}

func resolveCacheDirWithOS(getenv func(string) string, homeDir string, isDarwin bool) string {
	if v := getenv("AGENCY_CACHE_DIR"); v != "" {
		return v
	}
	if isDarwin {
		return filepath.Join(homeDir, "Library", "Caches", "agency")
	}
	if v := getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "agency")
	}
	return filepath.Join(homeDir, ".cache", "agency")
}
