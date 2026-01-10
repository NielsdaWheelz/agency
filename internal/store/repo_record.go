package store

import (
	"encoding/json"
	"os"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// Capabilities represents the capabilities of a repository.
type Capabilities struct {
	GitHubOrigin bool   `json:"github_origin"`
	OriginHost   string `json:"origin_host"`
	GhAuthed     bool   `json:"gh_authed"`
}

// RepoRecord represents the repo.json file for a repository.
// This is the public contract for v1.
type RepoRecord struct {
	SchemaVersion    string       `json:"schema_version"`
	RepoKey          string       `json:"repo_key"`
	RepoID           string       `json:"repo_id"`
	RepoRootLastSeen string       `json:"repo_root_last_seen"`
	AgencyJSONPath   string       `json:"agency_json_path"`
	OriginPresent    bool         `json:"origin_present"`
	OriginURL        string       `json:"origin_url"`
	OriginHost       string       `json:"origin_host"`
	Capabilities     Capabilities `json:"capabilities"`
	CreatedAt        string       `json:"created_at"`
	UpdatedAt        string       `json:"updated_at"`
}

// BuildRepoRecordInput contains the input for building a RepoRecord.
type BuildRepoRecordInput struct {
	RepoKey          string
	RepoID           string
	RepoRootLastSeen string
	AgencyJSONPath   string
	OriginPresent    bool
	OriginURL        string
	OriginHost       string
	Capabilities     Capabilities
}

// LoadRepoRecord reads repo.json for the given repoID.
// Returns (record, true, nil) if the file exists and is valid.
// Returns (zero, false, nil) if the file does not exist.
// Returns (zero, false, error) with E_STORE_CORRUPT if the JSON is invalid or schema_version is missing/invalid.
func (s *Store) LoadRepoRecord(repoID string) (RepoRecord, bool, error) {
	path := s.RepoRecordPath(repoID)

	data, err := s.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RepoRecord{}, false, nil
		}
		return RepoRecord{}, false, errors.Wrap(errors.EStoreCorrupt, "failed to read repo.json", err)
	}

	var rec RepoRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return RepoRecord{}, false, errors.Wrap(errors.EStoreCorrupt, "invalid json in repo.json", err)
	}

	// Validate schema_version
	if rec.SchemaVersion == "" {
		return RepoRecord{}, false, errors.New(errors.EStoreCorrupt, "repo.json: missing schema_version")
	}
	if rec.SchemaVersion != SchemaVersion {
		return RepoRecord{}, false, errors.New(errors.EStoreCorrupt, "repo.json: unsupported schema_version: "+rec.SchemaVersion)
	}

	return rec, true, nil
}

// UpsertRepoRecord creates or updates a RepoRecord.
// If existing is non-nil, preserves CreatedAt and updates UpdatedAt.
// If existing is nil, sets both CreatedAt and UpdatedAt to now.
func (s *Store) UpsertRepoRecord(existing *RepoRecord, input BuildRepoRecordInput) RepoRecord {
	now := s.Now().UTC().Format("2006-01-02T15:04:05Z")

	rec := RepoRecord{
		SchemaVersion:    SchemaVersion,
		RepoKey:          input.RepoKey,
		RepoID:           input.RepoID,
		RepoRootLastSeen: input.RepoRootLastSeen,
		AgencyJSONPath:   input.AgencyJSONPath,
		OriginPresent:    input.OriginPresent,
		OriginURL:        input.OriginURL,
		OriginHost:       input.OriginHost,
		Capabilities:     input.Capabilities,
		UpdatedAt:        now,
	}

	if existing != nil {
		// Preserve original creation time
		rec.CreatedAt = existing.CreatedAt
	} else {
		// New record
		rec.CreatedAt = now
	}

	return rec
}

// SaveRepoRecord writes repo.json atomically.
// Creates the repo directory if it doesn't exist.
func (s *Store) SaveRepoRecord(rec RepoRecord) error {
	// Ensure repo directory exists
	repoDir := s.RepoDir(rec.RepoID)
	if err := s.FS.MkdirAll(repoDir, 0755); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to create repo directory", err)
	}

	// Marshal with indentation for human readability
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to marshal repo.json", err)
	}

	// Add trailing newline
	data = append(data, '\n')

	path := s.RepoRecordPath(rec.RepoID)
	if err := fs.WriteFileAtomic(s.FS, path, data, 0644); err != nil {
		return errors.Wrap(errors.EStoreCorrupt, "failed to write repo.json", err)
	}

	return nil
}
