// Package runnerstatus provides types and functions for reading and validating
// the runner status file (.agency/state/runner_status.json).
//
// The runner status file is the contract between agency and supported runners.
// Runners update this file at milestones to communicate their state.
package runnerstatus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// state represents the runner's current state.
type state string

// Valid runner state values.
const (
	StateRunning   state = "running"
	StateWaiting   state = "waiting"
	StateSucceeded state = "succeeded"
	StateFailed    state = "failed"
)

// Common runner reasons.
const (
	ReasonAwaitingUserInput = "awaiting_user_input"
	ReasonAwaitingApproval  = "awaiting_approval"
	ReasonTurnComplete      = "turn_complete"
)

// SchemaVersion is the current schema version for runner_status.json.
const SchemaVersion = "2.0"

// RunnerStatus represents the contents of .agency/state/runner_status.json.
type RunnerStatus struct {
	SchemaVersion string   `json:"schema_version"`
	State         state    `json:"state"`
	UpdatedAt     string   `json:"updated_at"`
	Reason        string   `json:"reason,omitempty"`
	Summary       string   `json:"summary"`
	Questions     []string `json:"questions"`
	HowToTest     string   `json:"how_to_test"`
}

// StatusPath returns the path to the runner_status.json file in a worktree.
func StatusPath(worktreePath string) string {
	return filepath.Join(worktreePath, ".agency", "state", "runner_status.json")
}

// Load reads and parses the runner_status.json file from the given worktree.
// Returns (nil, nil) if the file does not exist.
// Returns (nil, error) if the file exists but cannot be read or parsed.
func Load(worktreePath string) (*RunnerStatus, error) {
	path := StatusPath(worktreePath)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read runner status file: %w", err)
	}

	var status RunnerStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to parse runner status file: %w", err)
	}

	return &status, nil
}

// Validate checks that the RunnerStatus has valid values.
// Returns nil if valid, or an error describing the validation failure.
func (s *RunnerStatus) Validate() error {
	if s == nil {
		return fmt.Errorf("runner status is nil")
	}

	// Validate state value.
	switch s.State {
	case StateRunning, StateWaiting, StateSucceeded, StateFailed:
	case "":
		return fmt.Errorf("state is required")
	default:
		return fmt.Errorf("invalid state value: %q", s.State)
	}

	// Summary is required for all states.
	if s.Summary == "" {
		return fmt.Errorf("summary is required")
	}

	switch s.State {
	case StateWaiting:
		if len(s.Questions) > 0 && s.Reason != ReasonAwaitingUserInput {
			return fmt.Errorf("questions[] requires reason awaiting_user_input")
		}
		if s.Reason == ReasonAwaitingUserInput && len(s.Questions) == 0 {
			return fmt.Errorf("questions[] is required when state is waiting and reason is awaiting_user_input")
		}
	case StateSucceeded:
		if s.HowToTest == "" {
			return fmt.Errorf("how_to_test is required when state is succeeded")
		}
	}

	if s.State != StateWaiting && len(s.Questions) > 0 {
		return fmt.Errorf("questions[] is only valid when state is waiting")
	}

	return nil
}
