package config

import (
	"os"
	"path/filepath"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// ResolvedAgencyConfig is an agency config plus the path it came from.
type ResolvedAgencyConfig struct {
	Config AgencyConfig
	Path   string
	Source string
}

func LocalAgencyConfigPath(configDir, repoID string) string {
	return filepath.Join(configDir, "repos", repoID, "agency.json")
}

func ResolveAgencyConfig(filesystem fs.FS, repoRoot, configDir, repoID, explicitPath string) (ResolvedAgencyConfig, error) {
	if explicitPath != "" {
		cfg, err := loadAgencyConfigPath(filesystem, explicitPath)
		if err != nil {
			if os.IsNotExist(err) {
				return ResolvedAgencyConfig{}, errors.New(errors.ENoAgencyJSON, "agency config not found: "+explicitPath)
			}
			if errors.GetCode(err) != "" {
				return ResolvedAgencyConfig{}, err
			}
			return ResolvedAgencyConfig{}, errors.Wrap(errors.ENoAgencyJSON, "failed to read agency config", err)
		}
		return validateResolvedAgencyConfig(cfg, explicitPath, "explicit")
	}

	repoPath := filepath.Join(repoRoot, "agency.json")
	cfg, err := loadAgencyConfigPath(filesystem, repoPath)
	if err == nil {
		return validateResolvedAgencyConfig(cfg, repoPath, "repo")
	}
	if !os.IsNotExist(err) {
		if errors.GetCode(err) != "" {
			return ResolvedAgencyConfig{}, err
		}
		return ResolvedAgencyConfig{}, errors.Wrap(errors.ENoAgencyJSON, "failed to read agency config", err)
	}

	localPath := LocalAgencyConfigPath(configDir, repoID)
	cfg, err = loadAgencyConfigPath(filesystem, localPath)
	if err == nil {
		return validateResolvedAgencyConfig(cfg, localPath, "local")
	}
	if !os.IsNotExist(err) {
		if errors.GetCode(err) != "" {
			return ResolvedAgencyConfig{}, err
		}
		return ResolvedAgencyConfig{}, errors.Wrap(errors.ENoAgencyJSON, "failed to read agency config", err)
	}

	return ResolvedAgencyConfig{}, errors.New(
		errors.ENoAgencyJSON,
		"agency config not found; run 'agency init' for local config or 'agency init --repo-config' for repo-shared config",
	)
}

func validateResolvedAgencyConfig(cfg AgencyConfig, path, source string) (ResolvedAgencyConfig, error) {
	cfg, err := ValidateAgencyConfig(cfg)
	if err != nil {
		return ResolvedAgencyConfig{}, err
	}

	baseDir := filepath.Dir(path)
	if !filepath.IsAbs(cfg.Scripts.Setup.Path) {
		cfg.Scripts.Setup.Path = filepath.Join(baseDir, cfg.Scripts.Setup.Path)
	}
	if !filepath.IsAbs(cfg.Scripts.Verify.Path) {
		cfg.Scripts.Verify.Path = filepath.Join(baseDir, cfg.Scripts.Verify.Path)
	}
	if !filepath.IsAbs(cfg.Scripts.Archive.Path) {
		cfg.Scripts.Archive.Path = filepath.Join(baseDir, cfg.Scripts.Archive.Path)
	}

	return ResolvedAgencyConfig{
		Config: cfg,
		Path:   path,
		Source: source,
	}, nil
}
