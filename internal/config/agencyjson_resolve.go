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
			if errors.GetCode(err) == errors.EInvalidAgencyJSON {
				return ResolvedAgencyConfig{}, invalidResolvedAgencyConfigError(err, repoRoot, explicitPath, "explicit")
			}
			if errors.GetCode(err) != "" {
				return ResolvedAgencyConfig{}, err
			}
			return ResolvedAgencyConfig{}, errors.Wrap(errors.ENoAgencyJSON, "failed to read agency config", err)
		}
		return validateResolvedAgencyConfig(cfg, repoRoot, explicitPath, "explicit")
	}

	repoPath := filepath.Join(repoRoot, "agency.json")
	cfg, err := loadAgencyConfigPath(filesystem, repoPath)
	if err == nil {
		return validateResolvedAgencyConfig(cfg, repoRoot, repoPath, "repo")
	}
	if !os.IsNotExist(err) {
		if errors.GetCode(err) == errors.EInvalidAgencyJSON {
			return ResolvedAgencyConfig{}, invalidResolvedAgencyConfigError(err, repoRoot, repoPath, "repo")
		}
		if errors.GetCode(err) != "" {
			return ResolvedAgencyConfig{}, err
		}
		return ResolvedAgencyConfig{}, errors.Wrap(errors.ENoAgencyJSON, "failed to read agency config", err)
	}

	localPath := LocalAgencyConfigPath(configDir, repoID)
	cfg, err = loadAgencyConfigPath(filesystem, localPath)
	if err == nil {
		return validateResolvedAgencyConfig(cfg, repoRoot, localPath, "local")
	}
	if !os.IsNotExist(err) {
		if errors.GetCode(err) == errors.EInvalidAgencyJSON {
			return ResolvedAgencyConfig{}, invalidResolvedAgencyConfigError(err, repoRoot, localPath, "local")
		}
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

func validateResolvedAgencyConfig(cfg AgencyConfig, repoRoot, path, source string) (ResolvedAgencyConfig, error) {
	cfg, err := ValidateAgencyConfig(cfg)
	if err != nil {
		return ResolvedAgencyConfig{}, invalidResolvedAgencyConfigError(err, repoRoot, path, source)
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

func invalidResolvedAgencyConfigError(err error, repoRoot, path, source string) error {
	ae, ok := errors.AsAgencyError(err)
	if !ok || ae.Code != errors.EInvalidAgencyJSON {
		return err
	}

	details := map[string]string{
		"path":   path,
		"source": source,
	}
	switch source {
	case "repo":
		details["hint"] = "fix " + path + ", or regenerate it with `agency init --path " + repoRoot + " --repo-config --force`"
	case "local":
		details["hint"] = "fix " + path + ", or regenerate it with `agency init --path " + repoRoot + " --force`"
	case "explicit":
		details["hint"] = "fix " + path + ", or re-run with a different `--agency-config` file"
	}
	for key, value := range ae.Details {
		if details[key] == "" {
			details[key] = value
		}
	}
	return errors.NewWithDetails(ae.Code, ae.Msg, details)
}
