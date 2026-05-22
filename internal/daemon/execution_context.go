package daemon

import (
	"maps"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/config"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

type executionContext struct {
	Profile      string
	ProfileEnv   map[string]string
	CheckoutRoot string
}

func (s *Server) resolveExecutionContext(repoRoot, repoID, agencyConfigPath, profileOverride string) (executionContext, error) {
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		return executionContext{}, err
	}
	agencyCfg, err := config.ResolveAgencyConfig(s.fsys, repoRoot, s.configDir, repoID, strings.TrimSpace(agencyConfigPath))
	if err != nil {
		return executionContext{}, err
	}
	profile := strings.TrimSpace(profileOverride)
	if profile == "" {
		profile = strings.TrimSpace(agencyCfg.Config.Execution.Profile)
	}
	if profile == "" {
		profile = strings.TrimSpace(userCfg.Defaults.ExecutionProfile)
	}
	if !config.IsValidExecutionProfileLabel(profile) {
		return executionContext{}, errors.New(errors.EInvalidExecutionProfile, "execution profile must contain only lowercase letters, digits, and hyphens")
	}
	profileEnv, err := config.ExecutionProfileEnv(userCfg, profile)
	if err != nil {
		return executionContext{}, err
	}
	checkoutRoot, err := config.ResolveCheckoutRoot(repoRoot, repoID, agencyCfg.Config.Execution.CheckoutRoot)
	if err != nil {
		return executionContext{}, err
	}
	return executionContext{
		Profile:      profile,
		ProfileEnv:   profileEnv,
		CheckoutRoot: checkoutRoot,
	}, nil
}

func envForLaunch(profileEnv, requestEnv map[string]string) map[string]string {
	if len(profileEnv) == 0 && len(requestEnv) == 0 {
		return nil
	}
	env := maps.Clone(profileEnv)
	if env == nil {
		env = make(map[string]string, len(requestEnv))
	}
	maps.Copy(env, requestEnv)
	return env
}

func (s *Server) executionProfileEnv(profile string) (map[string]string, error) {
	userCfg, err := s.LoadUserConfig()
	if err != nil {
		return nil, err
	}
	return config.ExecutionProfileEnv(userCfg, profile)
}
