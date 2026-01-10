package paths

import (
	"path/filepath"
	"testing"
)

// mapEnv is a simple map-backed Env implementation for testing.
type mapEnv map[string]string

func (m mapEnv) Get(key string) string {
	return m[key]
}

func TestResolveDirs_DataDir(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")

	tests := []struct {
		name     string
		env      mapEnv
		isDarwin bool
		want     string
	}{
		{
			name:     "AGENCY_DATA_DIR override (darwin)",
			env:      mapEnv{"AGENCY_DATA_DIR": "/custom/data"},
			isDarwin: true,
			want:     "/custom/data",
		},
		{
			name:     "AGENCY_DATA_DIR override (linux)",
			env:      mapEnv{"AGENCY_DATA_DIR": "/custom/data"},
			isDarwin: false,
			want:     "/custom/data",
		},
		{
			name:     "darwin default",
			env:      mapEnv{},
			isDarwin: true,
			want:     filepath.FromSlash("/home/testuser/Library/Application Support/agency"),
		},
		{
			name:     "XDG_DATA_HOME fallback (linux)",
			env:      mapEnv{"XDG_DATA_HOME": "/xdg/data"},
			isDarwin: false,
			want:     filepath.FromSlash("/xdg/data/agency"),
		},
		{
			name:     "default fallback (linux)",
			env:      mapEnv{},
			isDarwin: false,
			want:     filepath.FromSlash("/home/testuser/.local/share/agency"),
		},
		{
			name:     "AGENCY_DATA_DIR takes precedence over XDG",
			env:      mapEnv{"AGENCY_DATA_DIR": "/override", "XDG_DATA_HOME": "/xdg/data"},
			isDarwin: false,
			want:     "/override",
		},
		{
			name:     "darwin ignores XDG_DATA_HOME",
			env:      mapEnv{"XDG_DATA_HOME": "/xdg/data"},
			isDarwin: true,
			want:     filepath.FromSlash("/home/testuser/Library/Application Support/agency"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := ResolveDirsWithOS(tt.env, home, tt.isDarwin)
			if dirs.DataDir != tt.want {
				t.Errorf("DataDir = %q, want %q", dirs.DataDir, tt.want)
			}
		})
	}
}

func TestResolveDirs_ConfigDir(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")

	tests := []struct {
		name     string
		env      mapEnv
		isDarwin bool
		want     string
	}{
		{
			name:     "AGENCY_CONFIG_DIR override (darwin)",
			env:      mapEnv{"AGENCY_CONFIG_DIR": "/custom/config"},
			isDarwin: true,
			want:     "/custom/config",
		},
		{
			name:     "AGENCY_CONFIG_DIR override (linux)",
			env:      mapEnv{"AGENCY_CONFIG_DIR": "/custom/config"},
			isDarwin: false,
			want:     "/custom/config",
		},
		{
			name:     "darwin default",
			env:      mapEnv{},
			isDarwin: true,
			want:     filepath.FromSlash("/home/testuser/Library/Preferences/agency"),
		},
		{
			name:     "XDG_CONFIG_HOME fallback (linux)",
			env:      mapEnv{"XDG_CONFIG_HOME": "/xdg/config"},
			isDarwin: false,
			want:     filepath.FromSlash("/xdg/config/agency"),
		},
		{
			name:     "default fallback (linux)",
			env:      mapEnv{},
			isDarwin: false,
			want:     filepath.FromSlash("/home/testuser/.config/agency"),
		},
		{
			name:     "AGENCY_CONFIG_DIR takes precedence over XDG",
			env:      mapEnv{"AGENCY_CONFIG_DIR": "/override", "XDG_CONFIG_HOME": "/xdg/config"},
			isDarwin: false,
			want:     "/override",
		},
		{
			name:     "darwin ignores XDG_CONFIG_HOME",
			env:      mapEnv{"XDG_CONFIG_HOME": "/xdg/config"},
			isDarwin: true,
			want:     filepath.FromSlash("/home/testuser/Library/Preferences/agency"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := ResolveDirsWithOS(tt.env, home, tt.isDarwin)
			if dirs.ConfigDir != tt.want {
				t.Errorf("ConfigDir = %q, want %q", dirs.ConfigDir, tt.want)
			}
		})
	}
}

func TestResolveDirs_CacheDir(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")

	tests := []struct {
		name     string
		env      mapEnv
		isDarwin bool
		want     string
	}{
		{
			name:     "AGENCY_CACHE_DIR override (darwin)",
			env:      mapEnv{"AGENCY_CACHE_DIR": "/custom/cache"},
			isDarwin: true,
			want:     "/custom/cache",
		},
		{
			name:     "AGENCY_CACHE_DIR override (linux)",
			env:      mapEnv{"AGENCY_CACHE_DIR": "/custom/cache"},
			isDarwin: false,
			want:     "/custom/cache",
		},
		{
			name:     "darwin default",
			env:      mapEnv{},
			isDarwin: true,
			want:     filepath.FromSlash("/home/testuser/Library/Caches/agency"),
		},
		{
			name:     "XDG_CACHE_HOME fallback (linux)",
			env:      mapEnv{"XDG_CACHE_HOME": "/xdg/cache"},
			isDarwin: false,
			want:     filepath.FromSlash("/xdg/cache/agency"),
		},
		{
			name:     "default fallback (linux)",
			env:      mapEnv{},
			isDarwin: false,
			want:     filepath.FromSlash("/home/testuser/.cache/agency"),
		},
		{
			name:     "AGENCY_CACHE_DIR takes precedence over XDG",
			env:      mapEnv{"AGENCY_CACHE_DIR": "/override", "XDG_CACHE_HOME": "/xdg/cache"},
			isDarwin: false,
			want:     "/override",
		},
		{
			name:     "darwin ignores XDG_CACHE_HOME",
			env:      mapEnv{"XDG_CACHE_HOME": "/xdg/cache"},
			isDarwin: true,
			want:     filepath.FromSlash("/home/testuser/Library/Caches/agency"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := ResolveDirsWithOS(tt.env, home, tt.isDarwin)
			if dirs.CacheDir != tt.want {
				t.Errorf("CacheDir = %q, want %q", dirs.CacheDir, tt.want)
			}
		})
	}
}

func TestResolveDirs_AllDirs(t *testing.T) {
	home := filepath.FromSlash("/home/x")

	// Test that all three dirs are resolved together correctly
	env := mapEnv{
		"AGENCY_DATA_DIR":   "/d",
		"AGENCY_CONFIG_DIR": "/c",
		"AGENCY_CACHE_DIR":  "/ca",
	}

	dirs := ResolveDirsWithOS(env, home, false)

	if dirs.DataDir != "/d" {
		t.Errorf("DataDir = %q, want %q", dirs.DataDir, "/d")
	}
	if dirs.ConfigDir != "/c" {
		t.Errorf("ConfigDir = %q, want %q", dirs.ConfigDir, "/c")
	}
	if dirs.CacheDir != "/ca" {
		t.Errorf("CacheDir = %q, want %q", dirs.CacheDir, "/ca")
	}
}

func TestResolveDirs_TildeNotExpanded(t *testing.T) {
	// Per spec: ~ inside env vars is treated as literal (not expanded)
	home := filepath.FromSlash("/home/testuser")
	env := mapEnv{"AGENCY_DATA_DIR": "~/data"}

	dirs := ResolveDirsWithOS(env, home, false)

	// Should be literal ~/data, not /home/testuser/data
	if dirs.DataDir != "~/data" {
		t.Errorf("DataDir = %q, want %q (tilde should not be expanded)", dirs.DataDir, "~/data")
	}
}

func TestResolveDirs_EmptyEnvVarIgnored(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")
	// Empty string should be treated as unset
	env := mapEnv{"AGENCY_DATA_DIR": ""}

	dirs := ResolveDirsWithOS(env, home, false)

	// Should fall through to default, not use empty string
	want := filepath.FromSlash("/home/testuser/.local/share/agency")
	if dirs.DataDir != want {
		t.Errorf("DataDir = %q, want %q (empty env var should be ignored)", dirs.DataDir, want)
	}
}
