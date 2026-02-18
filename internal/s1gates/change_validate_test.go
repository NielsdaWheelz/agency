package s1gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agencyerrors "github.com/NielsdaWheelz/agency/internal/errors"
)

func validSyncedFixture() string {
	return filepath.Join("testdata", "repo_change_validate", "valid_synced")
}

func TestValidateGateSetChange_ReasonRequired(t *testing.T) {
	t.Parallel()

	changeTypes := []string{ChangeTypeAdd, ChangeTypeRemove, ChangeTypeReplace, ChangeTypeReorder}
	for _, ct := range changeTypes {
		ct := ct
		t.Run(ct, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(GateSetChange{
				GateID:     GateIDA,
				ChangeType: ct,
				Reason:     "",
			}, "")
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateChangeReasonRequired, agencyerrors.GetCode(err))
		})
	}

	t.Run("whitespace_only_reason", func(t *testing.T) {
		t.Parallel()

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:     GateIDA,
			ChangeType: ChangeTypeAdd,
			Reason:     "   ",
		}, "")
		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateChangeReasonRequired, agencyerrors.GetCode(err))
	})
}

func TestValidateGateSetChange_TargetRequiredByChangeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  GateSetChange
	}{
		{"add_missing_issue_path", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd, Reason: "test",
		}},
		{"remove_missing_issue_path", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeRemove, Reason: "test",
		}},
		{"replace_nil_issue_paths", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
		}},
		{"replace_wrong_length_1", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePaths: []string{"one"},
		}},
		{"replace_wrong_length_3", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePaths: []string{"a", "b", "c"},
		}},
		{"reorder_nil_issue_paths", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReorder, Reason: "test",
		}},
		{"reorder_too_few", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReorder, Reason: "test",
			IssuePaths: []string{"one"},
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(tt.req, "")
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))

			ae, ok := agencyerrors.AsAgencyError(err)
			require.True(t, ok)
			assert.Equal(t, "missing", ae.Details["target_violation"])
		})
	}
}

func TestValidateGateSetChange_TargetFieldExclusivityRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  GateSetChange
	}{
		{"add_with_issue_paths", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd, Reason: "test",
			IssuePath: "a", IssuePaths: []string{"b"},
		}},
		{"remove_with_issue_paths", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeRemove, Reason: "test",
			IssuePath: "a", IssuePaths: []string{"b"},
		}},
		{"replace_with_issue_path", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePath: "a", IssuePaths: []string{"b", "c"},
		}},
		{"reorder_with_issue_path", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReorder, Reason: "test",
			IssuePath: "a", IssuePaths: []string{"b", "c"},
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(tt.req, "")
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))

			ae, ok := agencyerrors.AsAgencyError(err)
			require.True(t, ok)
			assert.Equal(t, "exclusivity", ae.Details["target_violation"])
		})
	}
}

func TestValidateGateSetChange_IssuePathMembershipIntentRules(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	tests := []struct {
		name string
		req  GateSetChange
	}{
		{"add_already_in_targeted_gate", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd, Reason: "test",
			IssuePath: "docs/issues/gate-a-1.md", SyncedIssueMap: true,
		}},
		{"add_already_in_other_gate", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd, Reason: "test",
			IssuePath: "docs/issues/gate-b-1.md", SyncedIssueMap: true,
		}},
		{"add_nonexistent_issue", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd, Reason: "test",
			IssuePath: "docs/issues/nonexistent.md", SyncedIssueMap: true,
		}},
		{"remove_not_in_targeted_gate", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeRemove, Reason: "test",
			IssuePath: "docs/issues/gate-b-1.md", ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(tt.req, repoRoot)
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))

			ae, ok := agencyerrors.AsAgencyError(err)
			require.True(t, ok)
			assert.Equal(t, "membership_intent", ae.Details["target_violation"])
		})
	}
}

func TestValidateGateSetChange_ReplaceTargetRules(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	tests := []struct {
		name string
		req  GateSetChange
	}{
		{"from_not_in_targeted_gate", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePaths: []string{"docs/issues/gate-b-1.md", "docs/issues/new-issue.md"},
			ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
		{"to_already_in_gate", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePaths: []string{"docs/issues/gate-a-1.md", "docs/issues/gate-b-1.md"},
			ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
		{"to_nonexistent", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePaths: []string{"docs/issues/gate-a-1.md", "docs/issues/nonexistent.md"},
			ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
		{"from_equals_to", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace, Reason: "test",
			IssuePaths: []string{"docs/issues/gate-a-1.md", "docs/issues/gate-a-1.md"},
			ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(tt.req, repoRoot)
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))

			ae, ok := agencyerrors.AsAgencyError(err)
			require.True(t, ok)
			assert.Equal(t, "membership_intent", ae.Details["target_violation"])
		})
	}
}

func TestValidateGateSetChange_InvalidEnumValuesReturnTargetRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  GateSetChange
	}{
		{"lowercase_gate_a", GateSetChange{
			GateID: "a", ChangeType: ChangeTypeAdd, Reason: "test", IssuePath: "x",
		}},
		{"lowercase_gate_b", GateSetChange{
			GateID: "b", ChangeType: ChangeTypeAdd, Reason: "test", IssuePath: "x",
		}},
		{"capitalized_Add", GateSetChange{
			GateID: GateIDA, ChangeType: "Add", Reason: "test", IssuePath: "x",
		}},
		{"uppercase_REORDER", GateSetChange{
			GateID: GateIDA, ChangeType: "REORDER", Reason: "test",
			IssuePaths: []string{"x", "y"},
		}},
		{"unknown_gate_C", GateSetChange{
			GateID: "C", ChangeType: ChangeTypeAdd, Reason: "test", IssuePath: "x",
		}},
		{"unknown_change_delete", GateSetChange{
			GateID: GateIDA, ChangeType: "delete", Reason: "test", IssuePath: "x",
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(tt.req, "")
			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))

			ae, ok := agencyerrors.AsAgencyError(err)
			require.True(t, ok)
			assert.Equal(t, "invalid_enum", ae.Details["target_violation"])
		})
	}
}

func TestValidateGateSetChange_RemoveRequiresApproval(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	result, err := ValidateGateSetChange(GateSetChange{
		GateID:         GateIDA,
		ChangeType:     ChangeTypeRemove,
		IssuePath:      "docs/issues/gate-a-1.md",
		Reason:         "superseded",
		ApprovedBy:     "",
		SyncedIssueMap: true,
	}, repoRoot)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateChangeApprovalRequired, agencyerrors.GetCode(err))
}

func TestValidateGateSetChange_ReplaceRequiresApproval(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	result, err := ValidateGateSetChange(GateSetChange{
		GateID:         GateIDA,
		ChangeType:     ChangeTypeReplace,
		IssuePaths:     []string{"docs/issues/gate-a-1.md", "docs/issues/new-issue.md"},
		Reason:         "replacing",
		ApprovedBy:     "",
		SyncedIssueMap: true,
	}, repoRoot)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateChangeApprovalRequired, agencyerrors.GetCode(err))
}

func TestValidateGateSetChange_ReorderMembershipRules(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	t.Run("valid_permutation_succeeds", func(t *testing.T) {
		t.Parallel()

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeReorder,
			IssuePaths:     []string{"docs/issues/gate-a-2.md", "docs/issues/gate-a-1.md"},
			Reason:         "reordering",
			SyncedIssueMap: true,
		}, repoRoot)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Valid)
	})

	t.Run("membership_changing_rejected_even_with_approval", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			issuePaths []string
		}{
			{"extra_member", []string{
				"docs/issues/gate-a-1.md",
				"docs/issues/gate-a-2.md",
				"docs/issues/gate-b-1.md",
			}},
			{"cross_gate_member", []string{
				"docs/issues/gate-a-1.md",
				"docs/issues/gate-b-1.md",
			}},
			{"duplicate_member", []string{
				"docs/issues/gate-a-1.md",
				"docs/issues/gate-a-1.md",
			}},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				result, err := ValidateGateSetChange(GateSetChange{
					GateID:         GateIDA,
					ChangeType:     ChangeTypeReorder,
					IssuePaths:     tt.issuePaths,
					Reason:         "reordering",
					ApprovedBy:     "@owner",
					SyncedIssueMap: true,
				}, repoRoot)

				assert.Nil(t, result)
				require.Error(t, err)
				assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))

				ae, ok := agencyerrors.AsAgencyError(err)
				require.True(t, ok)
				assert.Equal(t, "membership_reorder", ae.Details["target_violation"])
			})
		}
	})
}

func TestValidateGateSetChange_SyncedFlagFalseReturnsDrift(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	result, err := ValidateGateSetChange(GateSetChange{
		GateID:         GateIDA,
		ChangeType:     ChangeTypeAdd,
		IssuePath:      "docs/issues/new-issue.md",
		Reason:         "adding new issue",
		SyncedIssueMap: false,
	}, repoRoot)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

	ae, ok := agencyerrors.AsAgencyError(err)
	require.True(t, ok)
	assert.Equal(t, "unsynced_flag", ae.Details["drift_kind"])
	assert.Equal(t, "release-gates_vs_issue-map", ae.Details["sync_source"])
	assert.Equal(t, "", ae.Details["issue_path"])
	assert.Equal(t, "", ae.Details["issue_map_count"])
}

func TestValidateGateSetChange_DetectsIssueMapDrift(t *testing.T) {
	t.Parallel()

	t.Run("missing_issue_in_map", func(t *testing.T) {
		t.Parallel()

		repoRoot := filepath.Join("testdata", "repo_change_validate", "drift_missing")

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeAdd,
			IssuePath:      "docs/issues/new-issue.md",
			Reason:         "adding",
			SyncedIssueMap: true,
		}, repoRoot)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, "docs/issues/gate-b-2.md", ae.Details["issue_path"])
		assert.Equal(t, "0", ae.Details["issue_map_count"])
		assert.Equal(t, "missing", ae.Details["drift_kind"])
		assert.Equal(t, "release-gates_vs_issue-map", ae.Details["sync_source"])
	})

	t.Run("duplicate_issue_in_map", func(t *testing.T) {
		t.Parallel()

		repoRoot := filepath.Join("testdata", "repo_change_validate", "drift_duplicate")

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeAdd,
			IssuePath:      "docs/issues/new-issue.md",
			Reason:         "adding",
			SyncedIssueMap: true,
		}, repoRoot)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, "docs/issues/gate-a-1.md", ae.Details["issue_path"])
		assert.Equal(t, "2", ae.Details["issue_map_count"])
		assert.Equal(t, "duplicate", ae.Details["drift_kind"])
		assert.Equal(t, "release-gates_vs_issue-map", ae.Details["sync_source"])
	})
}

func TestValidateGateSetChange_SourceInvalidReturnsDrift(t *testing.T) {
	t.Parallel()

	t.Run("malformed_release_gates", func(t *testing.T) {
		t.Parallel()

		repoRoot := filepath.Join("testdata", "repo_change_validate", "source_invalid")

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeAdd,
			IssuePath:      "docs/issues/something.md",
			Reason:         "test",
			SyncedIssueMap: true,
		}, repoRoot)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, "source_invalid", ae.Details["drift_kind"])
		assert.Equal(t, "release-gates_vs_issue-map", ae.Details["sync_source"])
		assert.Equal(t, "", ae.Details["issue_path"])
		assert.Equal(t, "", ae.Details["issue_map_count"])
	})

	t.Run("missing_release_gates_file", func(t *testing.T) {
		t.Parallel()

		repoRoot := t.TempDir()

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeAdd,
			IssuePath:      "docs/issues/something.md",
			Reason:         "test",
			SyncedIssueMap: true,
		}, repoRoot)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, "source_invalid", ae.Details["drift_kind"])
	})

	t.Run("malformed_issue_map", func(t *testing.T) {
		t.Parallel()

		repoRoot := copyFixtureRepo(t, validSyncedFixture())
		require.NoError(t, os.WriteFile(
			filepath.Join(repoRoot, CanonicalIssueMapPath),
			[]byte("# empty\n"), 0o644,
		))

		result, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeAdd,
			IssuePath:      "docs/issues/new-issue.md",
			Reason:         "test",
			SyncedIssueMap: true,
		}, repoRoot)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateSetDrift, agencyerrors.GetCode(err))

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, "source_invalid", ae.Details["drift_kind"])
	})
}

func TestValidateGateSetChange_SuccessWhenSynchronized(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	tests := []struct {
		name string
		req  GateSetChange
	}{
		{"add", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd,
			IssuePath: "docs/issues/new-issue.md",
			Reason:    "adding", SyncedIssueMap: true,
		}},
		{"remove_with_approval", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeRemove,
			IssuePath: "docs/issues/gate-a-1.md",
			Reason:    "superseded", ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
		{"replace_with_approval", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReplace,
			IssuePaths: []string{"docs/issues/gate-a-1.md", "docs/issues/new-issue.md"},
			Reason:     "replacing", ApprovedBy: "@owner", SyncedIssueMap: true,
		}},
		{"reorder", GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeReorder,
			IssuePaths: []string{"docs/issues/gate-a-2.md", "docs/issues/gate-a-1.md"},
			Reason:     "reordering", SyncedIssueMap: true,
		}},
		{"add_to_gate_b", GateSetChange{
			GateID: GateIDB, ChangeType: ChangeTypeAdd,
			IssuePath: "docs/issues/new-issue.md",
			Reason:    "adding to B", SyncedIssueMap: true,
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidateGateSetChange(tt.req, repoRoot)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.Valid)
		})
	}
}

func TestValidateGateSetChange_ErrorPrecedenceIsDeterministic(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	t.Run("reason_beats_target", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID:     "invalid",
			ChangeType: "invalid",
			Reason:     "",
		}, repoRoot)

		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateChangeReasonRequired, agencyerrors.GetCode(err))
	})

	t.Run("target_beats_approval", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID:     GateIDA,
			ChangeType: ChangeTypeRemove,
			IssuePath:  "",
			Reason:     "test",
			ApprovedBy: "",
		}, repoRoot)

		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateChangeTargetRequired, agencyerrors.GetCode(err))
	})

	t.Run("approval_beats_drift", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID:         GateIDA,
			ChangeType:     ChangeTypeRemove,
			IssuePath:      "docs/issues/gate-a-1.md",
			Reason:         "test",
			ApprovedBy:     "",
			SyncedIssueMap: false,
		}, repoRoot)

		require.Error(t, err)
		assert.Equal(t, agencyerrors.EGateChangeApprovalRequired, agencyerrors.GetCode(err))
	})
}

func TestValidateGateSetChange_ErrorDetailSchemaIsDeterministic(t *testing.T) {
	t.Parallel()

	repoRoot := validSyncedFixture()

	t.Run("reason_error_has_fixed_keys", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd, Reason: "",
		}, repoRoot)

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, agencyerrors.EGateChangeReasonRequired, ae.Code)
		assert.Contains(t, ae.Details, "gate_id")
		assert.Contains(t, ae.Details, "change_type")
		assert.Contains(t, ae.Details, "field")
		assert.Equal(t, "reason", ae.Details["field"])
	})

	t.Run("target_error_has_fixed_keys", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd,
			IssuePath: "", Reason: "test",
		}, repoRoot)

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, agencyerrors.EGateChangeTargetRequired, ae.Code)
		assert.Contains(t, ae.Details, "gate_id")
		assert.Contains(t, ae.Details, "change_type")
		assert.Contains(t, ae.Details, "target_kind")
		assert.Contains(t, ae.Details, "target_violation")

		allowedViolations := map[string]bool{
			"missing": true, "invalid_enum": true, "exclusivity": true,
			"membership_intent": true, "membership_reorder": true,
		}
		assert.True(t, allowedViolations[ae.Details["target_violation"]],
			"target_violation %q not in allowed set", ae.Details["target_violation"])
	})

	t.Run("approval_error_has_fixed_keys", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeRemove,
			IssuePath: "docs/issues/gate-a-1.md",
			Reason:    "test", ApprovedBy: "",
			SyncedIssueMap: true,
		}, repoRoot)

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, agencyerrors.EGateChangeApprovalRequired, ae.Code)
		assert.Contains(t, ae.Details, "gate_id")
		assert.Contains(t, ae.Details, "change_type")
		assert.Contains(t, ae.Details, "field")
		assert.Equal(t, "approved_by", ae.Details["field"])
	})

	t.Run("drift_error_has_fixed_keys", func(t *testing.T) {
		t.Parallel()

		_, err := ValidateGateSetChange(GateSetChange{
			GateID: GateIDA, ChangeType: ChangeTypeAdd,
			IssuePath: "docs/issues/new-issue.md",
			Reason:    "test", SyncedIssueMap: false,
		}, repoRoot)

		ae, ok := agencyerrors.AsAgencyError(err)
		require.True(t, ok)
		assert.Equal(t, agencyerrors.EGateSetDrift, ae.Code)
		assert.Contains(t, ae.Details, "issue_path")
		assert.Contains(t, ae.Details, "issue_map_count")
		assert.Contains(t, ae.Details, "drift_kind")
		assert.Contains(t, ae.Details, "sync_source")

		allowedDriftKinds := map[string]bool{
			"missing": true, "duplicate": true, "unsynced_flag": true, "source_invalid": true,
		}
		assert.True(t, allowedDriftKinds[ae.Details["drift_kind"]],
			"drift_kind %q not in allowed set", ae.Details["drift_kind"])
	})
}
