package daemon

// LandingMode indicates which landing strategy was used.
type LandingMode string

const (
	// LandingModeCherryPick indicates commits were cherry-picked.
	LandingModeCherryPick LandingMode = "cherry_pick"

	// LandingModeApplyPatch indicates a patch was applied (no commits).
	LandingModeApplyPatch LandingMode = "apply_patch"
)

// LandRequest is the request body for POST /invocations/{id}/land.
type LandRequest struct {
	// Apply enables apply mode (for uncommitted changes).
	// If false and no commits exist, returns error with hint.
	Apply bool `json:"apply,omitempty"`

	// RequireBase fails if integration branch has diverged from base_commit.
	// If false (default), cherry-pick onto current HEAD.
	RequireBase bool `json:"require_base,omitempty"`
}

// LandResponse is the response body for POST /invocations/{id}/land.
type LandResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	InvocationID          string      `json:"invocation_id,omitempty"`
	AppliedMode           LandingMode `json:"applied_mode,omitempty"`
	IntegrationHeadBefore string      `json:"integration_head_before,omitempty"`
	IntegrationHeadAfter  string      `json:"integration_head_after,omitempty"`
	CommitsLanded         int         `json:"commits_landed,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode     string   `json:"error_code,omitempty"`
	Message       string   `json:"message,omitempty"`
	Hint          string   `json:"hint,omitempty"`
	ConflictFiles []string `json:"conflict_files,omitempty"`
}

// DiscardResponse is the response body for POST /invocations/{id}/discard.
type DiscardResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Success fields
	InvocationID string `json:"invocation_id,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}
