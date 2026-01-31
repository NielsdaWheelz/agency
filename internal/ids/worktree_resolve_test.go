package ids

import (
	"testing"
)

func TestResolveWorktreeRef_ByName(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "present"},
		{WorktreeID: "20260131120000-e5f6", Name: "feature-c", State: "archived"},
	}

	// Resolve by name (present)
	ref, err := ResolveWorktreeRef("feature-a", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.WorktreeID != "20260131100000-a1b2" {
		t.Errorf("got WorktreeID = %v, want 20260131100000-a1b2", ref.WorktreeID)
	}

	// Resolve by name (archived, excluded by default)
	_, err = ResolveWorktreeRef("feature-c", refs, ResolveWorktreeRefOpts{})
	if err == nil {
		t.Error("expected error for archived worktree")
	}
	if _, ok := err.(*ErrWorktreeNotFound); !ok {
		t.Errorf("expected ErrWorktreeNotFound, got %T", err)
	}

	// Resolve by name (archived, included)
	ref, err = ResolveWorktreeRef("feature-c", refs, ResolveWorktreeRefOpts{IncludeArchived: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.WorktreeID != "20260131120000-e5f6" {
		t.Errorf("got WorktreeID = %v, want 20260131120000-e5f6", ref.WorktreeID)
	}
}

func TestResolveWorktreeRef_ByExactID(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "archived"},
		{WorktreeID: "20260131120000-e5f6", Name: "feature-c", State: "present", Broken: true},
	}

	// Resolve by exact ID (present)
	ref, err := ResolveWorktreeRef("20260131100000-a1b2", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Name != "feature-a" {
		t.Errorf("got Name = %v, want feature-a", ref.Name)
	}

	// Exact ID should work for archived worktrees (escape hatch)
	ref, err = ResolveWorktreeRef("20260131110000-c3d4", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Name != "feature-b" {
		t.Errorf("got Name = %v, want feature-b", ref.Name)
	}

	// Exact ID should work for broken worktrees (escape hatch)
	ref, err = ResolveWorktreeRef("20260131120000-e5f6", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ref.Broken {
		t.Error("expected Broken=true")
	}
}

func TestResolveWorktreeRef_ByPrefix(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "present"},
		{WorktreeID: "20260131120000-e5f6", Name: "feature-c", State: "archived"},
	}

	// Unique prefix
	ref, err := ResolveWorktreeRef("2026013110", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.WorktreeID != "20260131100000-a1b2" {
		t.Errorf("got WorktreeID = %v, want 20260131100000-a1b2", ref.WorktreeID)
	}

	// Ambiguous prefix
	_, err = ResolveWorktreeRef("202601311", refs, ResolveWorktreeRefOpts{})
	if err == nil {
		t.Error("expected error for ambiguous prefix")
	}
	if _, ok := err.(*ErrWorktreeAmbiguous); !ok {
		t.Errorf("expected ErrWorktreeAmbiguous, got %T", err)
	}

	// Prefix excludes archived by default
	ref, err = ResolveWorktreeRef("2026013111", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.WorktreeID != "20260131110000-c3d4" {
		t.Errorf("got WorktreeID = %v, want 20260131110000-c3d4", ref.WorktreeID)
	}
}

func TestResolveWorktreeRef_NotFound(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
	}

	_, err := ResolveWorktreeRef("nonexistent", refs, ResolveWorktreeRefOpts{})
	if err == nil {
		t.Error("expected error")
	}
	if _, ok := err.(*ErrWorktreeNotFound); !ok {
		t.Errorf("expected ErrWorktreeNotFound, got %T", err)
	}
}

func TestResolveWorktreeRef_EmptyInput(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
	}

	_, err := ResolveWorktreeRef("", refs, ResolveWorktreeRefOpts{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestResolveWorktreeRef_BrokenExcluded(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present", Broken: true},
	}

	// Name match excludes broken
	_, err := ResolveWorktreeRef("feature-a", refs, ResolveWorktreeRefOpts{})
	if err == nil {
		t.Error("expected error for broken worktree name match")
	}

	// Prefix match excludes broken
	_, err = ResolveWorktreeRef("202601311", refs, ResolveWorktreeRefOpts{})
	if err == nil {
		t.Error("expected error for broken worktree prefix match")
	}

	// Exact ID works (escape hatch)
	ref, err := ResolveWorktreeRef("20260131100000-a1b2", refs, ResolveWorktreeRefOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ref.Broken {
		t.Error("expected Broken=true")
	}
}

func TestCheckWorktreeNameUnique(t *testing.T) {
	refs := []WorktreeRef{
		{WorktreeID: "20260131100000-a1b2", Name: "feature-a", State: "present"},
		{WorktreeID: "20260131110000-c3d4", Name: "feature-b", State: "archived"},
	}

	// Name in use by present worktree
	err := CheckWorktreeNameUnique("feature-a", refs)
	if err == nil {
		t.Error("expected error for duplicate name")
	}

	// Name in use by archived worktree (allowed)
	err = CheckWorktreeNameUnique("feature-b", refs)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// New name (allowed)
	err = CheckWorktreeNameUnique("feature-c", refs)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
