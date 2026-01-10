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

// Env is the interface for environment variable lookups.
// Implementations must return "" for unset variables.
type Env interface {
	Get(key string) string
}

// ResolveDirs computes the data, config, and cache directories based on
// environment variables and platform defaults per the constitution.
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
func ResolveDirs(env Env, homeDir string) Dirs {
	return Dirs{
		DataDir:   resolveDataDir(env, homeDir),
		ConfigDir: resolveConfigDir(env, homeDir),
		CacheDir:  resolveCacheDir(env, homeDir),
	}
}

// IsDarwin returns true if the current OS is macOS.
// Exported for testing purposes.
func IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

// ResolveDirsWithOS is like ResolveDirs but accepts an explicit OS flag for testing.
func ResolveDirsWithOS(env Env, homeDir string, isDarwin bool) Dirs {
	return Dirs{
		DataDir:   resolveDataDirWithOS(env, homeDir, isDarwin),
		ConfigDir: resolveConfigDirWithOS(env, homeDir, isDarwin),
		CacheDir:  resolveCacheDirWithOS(env, homeDir, isDarwin),
	}
}

func resolveDataDir(env Env, homeDir string) string {
	return resolveDataDirWithOS(env, homeDir, IsDarwin())
}

func resolveDataDirWithOS(env Env, homeDir string, isDarwin bool) string {
	// 1. AGENCY_DATA_DIR override
	if v := env.Get("AGENCY_DATA_DIR"); v != "" {
		return v
	}
	// 2. macOS default
	if isDarwin {
		return filepath.Join(homeDir, "Library", "Application Support", "agency")
	}
	// 3. XDG_DATA_HOME fallback
	if v := env.Get("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "agency")
	}
	// 4. Default fallback
	return filepath.Join(homeDir, ".local", "share", "agency")
}

func resolveConfigDir(env Env, homeDir string) string {
	return resolveConfigDirWithOS(env, homeDir, IsDarwin())
}

func resolveConfigDirWithOS(env Env, homeDir string, isDarwin bool) string {
	// 1. AGENCY_CONFIG_DIR override
	if v := env.Get("AGENCY_CONFIG_DIR"); v != "" {
		return v
	}
	// 2. macOS default
	if isDarwin {
		return filepath.Join(homeDir, "Library", "Preferences", "agency")
	}
	// 3. XDG_CONFIG_HOME fallback
	if v := env.Get("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "agency")
	}
	// 4. Default fallback
	return filepath.Join(homeDir, ".config", "agency")
}

func resolveCacheDir(env Env, homeDir string) string {
	return resolveCacheDirWithOS(env, homeDir, IsDarwin())
}

func resolveCacheDirWithOS(env Env, homeDir string, isDarwin bool) string {
	// 1. AGENCY_CACHE_DIR override
	if v := env.Get("AGENCY_CACHE_DIR"); v != "" {
		return v
	}
	// 2. macOS default
	if isDarwin {
		return filepath.Join(homeDir, "Library", "Caches", "agency")
	}
	// 3. XDG_CACHE_HOME fallback
	if v := env.Get("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "agency")
	}
	// 4. Default fallback
	return filepath.Join(homeDir, ".cache", "agency")
}
