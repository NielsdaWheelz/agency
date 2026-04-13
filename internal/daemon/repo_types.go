package daemon

// RepoRegisterRequest is the request body for POST /repos/register.
type RepoRegisterRequest struct {
	// RepoRoot is the absolute path to the repository root (or a subdirectory).
	// Daemon normalizes to git toplevel.
	RepoRoot string `json:"repo_root"`
}

// RepoRegisterResponse is the response for POST /repos/register.
type RepoRegisterResponse struct {
	OK           bool   `json:"ok"`
	APIVersion   int    `json:"api_version"`
	BuildVersion string `json:"build_version,omitempty"`
	GitSHA       string `json:"git_sha,omitempty"`
	RequestID    string `json:"request_id,omitempty"`

	// Data (on success)
	Data *RepoRegisterData `json:"data,omitempty"`

	// Error fields (only set when OK is false)
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// RepoRegisterData is the data payload for a successful register response.
type RepoRegisterData struct {
	RepoID                  string   `json:"repo_id"`
	RepoKey                 string   `json:"repo_key"`
	Paths                   []string `json:"paths"`
	PreferredRoot           string   `json:"preferred_root"`
	PreferredRootAccessible bool     `json:"preferred_root_accessible"`
	LastSeenAt              string   `json:"last_seen_at"`
}

// RepoDTO is the data transfer object for a single repo.
type RepoDTO struct {
	RepoID                  string     `json:"repo_id"`
	RepoKey                 string     `json:"repo_key"`
	Paths                   []string   `json:"paths"`
	PreferredRoot           string     `json:"preferred_root"`
	PreferredRootAccessible bool       `json:"preferred_root_accessible"`
	Origin                  *OriginDTO `json:"origin,omitempty"`
	LastSeenAt              string     `json:"last_seen_at"`
	UpdatedAt               string     `json:"updated_at,omitempty"`
}

// OriginDTO is the origin info for a repo.
type OriginDTO struct {
	Present bool   `json:"present"`
	URL     string `json:"url,omitempty"`
	Host    string `json:"host,omitempty"`
}

// ListReposData is the data payload for GET /repos.
type ListReposData struct {
	Repos []RepoDTO `json:"repos"`
}
