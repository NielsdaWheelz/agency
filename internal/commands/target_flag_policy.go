package commands

// targetFlagPolicy describes which target-level flags are valid for one user-facing action.
type targetFlagPolicy struct {
	Action       string
	AllowedFlags []string
}

func newTargetFlagPolicy(action string, allowed ...string) targetFlagPolicy {
	return targetFlagPolicy{
		Action:       action,
		AllowedFlags: allowed,
	}
}
