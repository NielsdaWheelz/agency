package ids

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRunRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		refs       []RunRef
		wantRef    RunRef
		wantErr    error // nil, *ErrNotFound, or *ErrAmbiguous
		wantCands  []RunRef
		wantBroken bool // check Broken field on resolved ref
	}{
		{
			name:  "exact match wins",
			input: "20260110-a3f2",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
				{RepoID: "r1", RunID: "20260110-a3ff", Broken: false},
			},
			wantRef: RunRef{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
			wantErr: nil,
		},
		{
			name:  "unique prefix resolves",
			input: "20260110-a3",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
			},
			wantRef: RunRef{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
			wantErr: nil,
		},
		{
			name:  "ambiguous prefix errors",
			input: "20260110-a3f",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
				{RepoID: "r1", RunID: "20260110-a3ff", Broken: false},
			},
			wantErr: &ErrAmbiguous{},
			wantCands: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
				{RepoID: "r1", RunID: "20260110-a3ff", Broken: false},
			},
		},
		{
			name:  "not found errors",
			input: "20260110-zzz",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
			},
			wantErr: &ErrNotFound{},
		},
		{
			name:  "exact wins over prefix ambiguity",
			input: "20260110-a3ff",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
				{RepoID: "r1", RunID: "20260110-a3ff", Broken: false},
				{RepoID: "r1", RunID: "20260110-a3ff9", Broken: false}, // would be prefix match
			},
			wantRef: RunRef{RepoID: "r1", RunID: "20260110-a3ff", Broken: false},
			wantErr: nil,
		},
		{
			name:  "broken preserved",
			input: "20260110-a3",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: true},
			},
			wantRef:    RunRef{RepoID: "r1", RunID: "20260110-a3f2", Broken: true},
			wantErr:    nil,
			wantBroken: true,
		},
		{
			name:    "empty input",
			input:   "   ",
			refs:    []RunRef{{RepoID: "r1", RunID: "20260110-a3f2", Broken: false}},
			wantErr: &ErrNotFound{},
		},
		{
			name:  "duplicate exact across repos",
			input: "x",
			refs: []RunRef{
				{RepoID: "r1", RunID: "x", Broken: false},
				{RepoID: "r2", RunID: "x", Broken: false},
			},
			wantErr: &ErrAmbiguous{},
			wantCands: []RunRef{
				{RepoID: "r1", RunID: "x", Broken: false},
				{RepoID: "r2", RunID: "x", Broken: false},
			},
		},
		// Additional edge cases
		{
			name:    "empty refs",
			input:   "20260110-a3f2",
			refs:    []RunRef{},
			wantErr: &ErrNotFound{},
		},
		{
			name:    "nil refs",
			input:   "20260110-a3f2",
			refs:    nil,
			wantErr: &ErrNotFound{},
		},
		{
			name:  "prefix match across multiple repos",
			input: "2026",
			refs: []RunRef{
				{RepoID: "r2", RunID: "20260110-bbbb", Broken: false},
				{RepoID: "r1", RunID: "20260110-aaaa", Broken: false},
			},
			wantErr: &ErrAmbiguous{},
			wantCands: []RunRef{
				// Sorted by RunID asc, then RepoID asc
				{RepoID: "r1", RunID: "20260110-aaaa", Broken: false},
				{RepoID: "r2", RunID: "20260110-bbbb", Broken: false},
			},
		},
		{
			name:  "deterministic ordering - same RunID different RepoID",
			input: "run",
			refs: []RunRef{
				{RepoID: "repo-c", RunID: "run-1", Broken: false},
				{RepoID: "repo-a", RunID: "run-1", Broken: false},
				{RepoID: "repo-b", RunID: "run-1", Broken: false},
			},
			wantErr: &ErrAmbiguous{},
			wantCands: []RunRef{
				// Same RunID, sorted by RepoID asc
				{RepoID: "repo-a", RunID: "run-1", Broken: false},
				{RepoID: "repo-b", RunID: "run-1", Broken: false},
				{RepoID: "repo-c", RunID: "run-1", Broken: false},
			},
		},
		{
			name:  "input with leading/trailing whitespace",
			input: "  20260110-a3f2  ",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
			},
			wantRef: RunRef{RepoID: "r1", RunID: "20260110-a3f2", Broken: false},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveRunRef(tt.input, tt.refs)

			// Check error type
			if tt.wantErr != nil {
				require.Error(t, err)

				// Type-assert expected error types
				switch tt.wantErr.(type) {
				case *ErrNotFound:
					var gotErr *ErrNotFound
					require.True(t, errors.As(err, &gotErr), "expected *ErrNotFound, got %T: %v", err, err)
				case *ErrAmbiguous:
					var gotErr *ErrAmbiguous
					require.True(t, errors.As(err, &gotErr), "expected *ErrAmbiguous, got %T: %v", err, err)
					// Verify candidates if specified
					if tt.wantCands != nil {
						require.Len(t, gotErr.Candidates, len(tt.wantCands))
						for i, wantCand := range tt.wantCands {
							gotCand := gotErr.Candidates[i]
							assert.Equal(t, wantCand.RunID, gotCand.RunID, "candidate[%d].RunID", i)
							assert.Equal(t, wantCand.RepoID, gotCand.RepoID, "candidate[%d].RepoID", i)
							assert.Equal(t, wantCand.Broken, gotCand.Broken, "candidate[%d].Broken", i)
						}
					}
				default:
					require.Fail(t, "unexpected expected error type: %T", tt.wantErr)
				}
				return
			}

			// No error expected
			require.NoError(t, err)

			// Check resolved ref
			assert.Equal(t, tt.wantRef.RunID, got.RunID)
			assert.Equal(t, tt.wantRef.RepoID, got.RepoID)
			if tt.wantBroken {
				assert.True(t, got.Broken, "expected Broken=true")
			} else {
				assert.Equal(t, tt.wantRef.Broken, got.Broken)
			}
		})
	}
}

func TestErrNotFoundError(t *testing.T) {
	t.Parallel()
	err := &ErrNotFound{Input: "test-input"}
	got := err.Error()
	want := `run not found: "test-input"`
	assert.Equal(t, want, got)
}

func TestErrAmbiguousError(t *testing.T) {
	t.Parallel()
	err := &ErrAmbiguous{
		Input: "abc",
		Candidates: []RunRef{
			{RepoID: "r1", RunID: "abc123"},
			{RepoID: "r2", RunID: "abc456"},
		},
	}
	got := err.Error()
	want := `ambiguous run id "abc" matches: abc123, abc456`
	assert.Equal(t, want, got)
}

func TestSortCandidates(t *testing.T) {
	t.Parallel()
	// Test deterministic ordering
	refs := []RunRef{
		{RepoID: "r2", RunID: "b"},
		{RepoID: "r1", RunID: "b"},
		{RepoID: "r2", RunID: "a"},
		{RepoID: "r1", RunID: "a"},
	}

	sortCandidates(refs)

	expected := []RunRef{
		{RepoID: "r1", RunID: "a"},
		{RepoID: "r2", RunID: "a"},
		{RepoID: "r1", RunID: "b"},
		{RepoID: "r2", RunID: "b"},
	}

	for i, want := range expected {
		assert.Equal(t, want.RunID, refs[i].RunID, "index %d RunID", i)
		assert.Equal(t, want.RepoID, refs[i].RepoID, "index %d RepoID", i)
	}
}

func TestResolveRunRefWithName(t *testing.T) {
	t.Parallel()
	// isActive returns true for non-archived runs (simulated by Name != "archived")
	isActive := func(ref RunRef) bool {
		return ref.Name != "archived"
	}

	tests := []struct {
		name      string
		input     string
		refs      []RunRef
		isActive  func(RunRef) bool
		wantRef   RunRef
		wantErr   error
		wantCands []RunRef
	}{
		{
			name:  "exact name match wins over run_id prefix",
			input: "my-feature",
			refs: []RunRef{
				{RepoID: "r1", RunID: "my-feature-a3f2", Name: "other"},    // would match as prefix
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "my-feature"}, // exact name match
			},
			isActive: isActive,
			wantRef:  RunRef{RepoID: "r1", RunID: "20260110-bbbb", Name: "my-feature"},
			wantErr:  nil,
		},
		{
			name:  "falls back to run_id when no name match",
			input: "20260110-a3f2",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Name: "some-name"},
			},
			isActive: isActive,
			wantRef:  RunRef{RepoID: "r1", RunID: "20260110-a3f2", Name: "some-name"},
			wantErr:  nil,
		},
		{
			name:  "falls back to run_id prefix when no name match",
			input: "20260110-a3",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-a3f2", Name: "some-name"},
			},
			isActive: isActive,
			wantRef:  RunRef{RepoID: "r1", RunID: "20260110-a3f2", Name: "some-name"},
			wantErr:  nil,
		},
		{
			name:  "archived runs excluded from name matching",
			input: "test-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "archived"},  // simulated archived
				{RepoID: "r1", RunID: "20260110-bbbb", Name: "test-name"}, // active
			},
			isActive: func(ref RunRef) bool { return ref.RunID == "20260110-bbbb" },
			wantRef:  RunRef{RepoID: "r1", RunID: "20260110-bbbb", Name: "test-name"},
			wantErr:  nil,
		},
		{
			name:  "archived run with matching name falls back to run_id",
			input: "archived",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "archived"}, // isActive returns false
			},
			isActive: isActive,
			wantErr:  &ErrNotFound{}, // no active name match, no run_id match
		},
		{
			name:  "ambiguous name match",
			input: "dup-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "dup-name"},
				{RepoID: "r2", RunID: "20260110-bbbb", Name: "dup-name"},
			},
			isActive: isActive,
			wantErr:  &ErrAmbiguous{},
			wantCands: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "dup-name"},
				{RepoID: "r2", RunID: "20260110-bbbb", Name: "dup-name"},
			},
		},
		{
			name:  "nil isActive matches all names",
			input: "my-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-name"},
			},
			isActive: nil,
			wantRef:  RunRef{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-name"},
			wantErr:  nil,
		},
		{
			name:    "empty input",
			input:   "   ",
			refs:    []RunRef{{RepoID: "r1", RunID: "20260110-a3f2", Name: "test"}},
			wantErr: &ErrNotFound{},
		},
		{
			name:  "name match is case-sensitive",
			input: "My-Feature",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "my-feature"},
			},
			isActive: isActive,
			wantErr:  &ErrNotFound{}, // no match - case matters
		},
		{
			name:  "broken run still returned if name matches",
			input: "broken-run",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "broken-run", Broken: true},
			},
			isActive: func(ref RunRef) bool { return true },
			wantRef:  RunRef{RepoID: "r1", RunID: "20260110-aaaa", Name: "broken-run", Broken: true},
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveRunRefWithName(tt.input, tt.refs, tt.isActive)

			// Check error type
			if tt.wantErr != nil {
				require.Error(t, err)

				switch tt.wantErr.(type) {
				case *ErrNotFound:
					var gotErr *ErrNotFound
					require.True(t, errors.As(err, &gotErr), "expected *ErrNotFound, got %T: %v", err, err)
				case *ErrAmbiguous:
					var gotErr *ErrAmbiguous
					require.True(t, errors.As(err, &gotErr), "expected *ErrAmbiguous, got %T: %v", err, err)
					if tt.wantCands != nil {
						require.Len(t, gotErr.Candidates, len(tt.wantCands))
						for i, wantCand := range tt.wantCands {
							gotCand := gotErr.Candidates[i]
							assert.Equal(t, wantCand.RunID, gotCand.RunID, "candidate[%d].RunID", i)
							assert.Equal(t, wantCand.RepoID, gotCand.RepoID, "candidate[%d].RepoID", i)
							assert.Equal(t, wantCand.Name, gotCand.Name, "candidate[%d].Name", i)
						}
					}
				default:
					require.Fail(t, "unexpected expected error type: %T", tt.wantErr)
				}
				return
			}

			// No error expected
			require.NoError(t, err)

			// Check resolved ref
			assert.Equal(t, tt.wantRef.RunID, got.RunID)
			assert.Equal(t, tt.wantRef.RepoID, got.RepoID)
			assert.Equal(t, tt.wantRef.Name, got.Name)
			assert.Equal(t, tt.wantRef.Broken, got.Broken)
		})
	}
}

func TestCheckNameUnique(t *testing.T) {
	t.Parallel()
	// isArchived simulates archived status by checking Name
	isArchived := func(ref RunRef) bool {
		return ref.Name == "archived-run"
	}

	tests := []struct {
		name       string
		checkName  string
		refs       []RunRef
		isArchived func(RunRef) bool
		wantErr    bool
	}{
		{
			name:      "unique name passes",
			checkName: "new-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "existing-name"},
			},
			isArchived: isArchived,
			wantErr:    false,
		},
		{
			name:      "duplicate name fails",
			checkName: "existing-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "existing-name"},
			},
			isArchived: isArchived,
			wantErr:    true,
		},
		{
			name:      "archived run with same name passes",
			checkName: "archived-run",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "archived-run"},
			},
			isArchived: isArchived,
			wantErr:    false,
		},
		{
			name:      "nil isArchived treats all as active",
			checkName: "some-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "some-name"},
			},
			isArchived: nil,
			wantErr:    true,
		},
		{
			name:       "empty refs passes",
			checkName:  "any-name",
			refs:       []RunRef{},
			isArchived: isArchived,
			wantErr:    false,
		},
		{
			name:      "multiple refs one match fails",
			checkName: "target-name",
			refs: []RunRef{
				{RepoID: "r1", RunID: "20260110-aaaa", Name: "other-name"},
				{RepoID: "r2", RunID: "20260110-bbbb", Name: "target-name"},
				{RepoID: "r3", RunID: "20260110-cccc", Name: "another-name"},
			},
			isArchived: isArchived,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckNameUnique(tt.checkName, tt.refs, tt.isArchived)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
