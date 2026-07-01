package sqlite

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestSkillStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ss := NewSkillStore(db)

	// Test adding invalid skill (empty name)
	invalidSk := &store.Skill{Name: ""}
	if err := ss.AddSkill(ctx, invalidSk); err == nil {
		t.Error("expected error adding empty skill, got nil")
	}

	// Test adding valid skill
	sk := &store.Skill{
		Name:        "test-skill",
		Scope:       "global",
		SourceType:  "local",
		Source:      "/path/to/source",
		SkillPath:   "/path/to/dest",
		Version:     "1.0.0",
		Hash:        "abc123hash",
		Description: "A test skill description",
		Enabled:     1,
	}

	if err := ss.AddSkill(ctx, sk); err != nil {
		t.Fatalf("failed to AddSkill: %v", err)
	}

	// Test getting skill
	gotSk, err := ss.GetSkill(ctx, sk.Name, sk.Scope)
	if err != nil {
		t.Fatalf("failed to GetSkill: %v", err)
	}

	// Verify fields match
	if gotSk.Name != sk.Name ||
		gotSk.Scope != sk.Scope ||
		gotSk.SourceType != sk.SourceType ||
		gotSk.Source != sk.Source ||
		gotSk.SkillPath != sk.SkillPath ||
		gotSk.Version != sk.Version ||
		gotSk.Hash != sk.Hash ||
		gotSk.Description != sk.Description ||
		gotSk.Enabled != sk.Enabled {
		t.Errorf("skill fields mismatch. got: %+v, want: %+v", gotSk, sk)
	}

	// Verify timestamps
	if gotSk.InstalledAt == "" || gotSk.UpdatedAt == "" {
		t.Error("expected InstalledAt and UpdatedAt to be set, got empty strings")
	}

	// Test getting non-existent skill
	_, err = ss.GetSkill(ctx, "nonexistent", "global")
	if err == nil {
		t.Error("expected error getting nonexistent skill, got nil")
	}

	// Test listing skills
	list, err := ss.ListSkills(ctx)
	if err != nil {
		t.Fatalf("failed to ListSkills: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected list length 1, got %d", len(list))
	}
	if list[0].Name != sk.Name {
		t.Errorf("expected skill name %s, got %s", sk.Name, list[0].Name)
	}

	// Test listing by scope
	globalList, err := ss.ListSkillsByScope(ctx, "global")
	if err != nil {
		t.Fatalf("failed to ListSkillsByScope: %v", err)
	}
	if len(globalList) != 1 {
		t.Errorf("expected global list length 1, got %d", len(globalList))
	}

	agentList, err := ss.ListSkillsByScope(ctx, "some-agent")
	if err != nil {
		t.Fatalf("failed to ListSkillsByScope: %v", err)
	}
	if len(agentList) != 0 {
		t.Errorf("expected agent list length 0, got %d", len(agentList))
	}

	// Test updating skill
	sk.Description = "Updated description"
	sk.Version = "1.0.1"
	sk.Hash = "newhash456"
	if err := ss.UpdateSkill(ctx, sk); err != nil {
		t.Fatalf("failed to UpdateSkill: %v", err)
	}

	gotSk, err = ss.GetSkill(ctx, sk.Name, sk.Scope)
	if err != nil {
		t.Fatalf("failed to GetSkill after update: %v", err)
	}
	if gotSk.Description != "Updated description" || gotSk.Version != "1.0.1" || gotSk.Hash != "newhash456" {
		t.Errorf("updated fields mismatch. got: %+v", gotSk)
	}

	// Test adding duplicate skill fails
	if err := ss.AddSkill(ctx, sk); err == nil {
		t.Error("expected error when adding duplicate skill, got nil")
	}

	// Test removing skill
	if err := ss.RemoveSkill(ctx, sk.Name, sk.Scope); err != nil {
		t.Fatalf("failed to RemoveSkill: %v", err)
	}

	// Verify skill was removed
	_, err = ss.GetSkill(ctx, sk.Name, sk.Scope)
	if err == nil {
		t.Error("expected error getting removed skill, got nil")
	}

	// Test removing non-existent skill
	if err := ss.RemoveSkill(ctx, "nonexistent", "global"); err == nil {
		t.Error("expected RemoveSkill to return error for nonexistent skill")
	}
}
