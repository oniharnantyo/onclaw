//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func newWorkspaceSkill(wsID, name string, enabled bool) *domain.WorkspaceSkill {
	return &domain.WorkspaceSkill{
		WorkspaceID:  wsID,
		Name:         name,
		Description:  "Integration test skill",
		Version:      domain.DefaultSkillVersion,
		Source:       domain.SkillSourceAuthored,
		Enabled:      enabled,
		Dependencies: domain.SkillDependencies{Tools: []string{"execute"}, Python: []string{"pypdf>=4.0"}},
	}
}

// TestIntegration_WorkspaceSkillStore covers the workspace_skills migration
// round-trip: tenant-scoped CRUD, the per-workspace unique-name constraint,
// and the enabled master-switch toggle (design.md D1/D2).
func TestIntegration_WorkspaceSkillStore(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "skills-it-ws", Name: "Skills IT", Timezone: "UTC"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	other := &domain.Workspace{Slug: "skills-it-other", Name: "Other", Timezone: "UTC"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	// Create.
	skill := newWorkspaceSkill(ws.ID, "changelog-sweeper", true)
	if err := s.WorkspaceSkills().Create(ctx, skill); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if skill.ID == "" || skill.CreatedAt.IsZero() {
		t.Fatalf("expected ID and created_at to be assigned: %+v", skill)
	}

	// Get (tenant-scoped).
	found, err := s.WorkspaceSkills().Get(ctx, ws.ID, skill.ID)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if found.Name != skill.Name || found.Source != domain.SkillSourceAuthored || !found.Enabled {
		t.Fatalf("get mismatch: %+v", found)
	}
	if len(found.Dependencies.Tools) != 1 || found.Dependencies.Tools[0] != "execute" {
		t.Fatalf("dependencies.tools round-trip mismatch: %+v", found.Dependencies)
	}
	if len(found.Dependencies.Python) != 1 || found.Dependencies.Python[0] != "pypdf>=4.0" {
		t.Fatalf("dependencies.python round-trip mismatch: %+v", found.Dependencies)
	}

	// GetByName.
	byName, err := s.WorkspaceSkills().GetByName(ctx, ws.ID, "changelog-sweeper")
	if err != nil || byName.ID != skill.ID {
		t.Fatalf("get by name = %+v, err %v", byName, err)
	}

	// Cross-tenant access is not-found.
	if _, err := s.WorkspaceSkills().Get(ctx, other.ID, skill.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign get should be ErrNotFound, got %v", err)
	}

	// Unique name per workspace.
	dup := newWorkspaceSkill(ws.ID, "changelog-sweeper", true)
	if err := s.WorkspaceSkills().Create(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate name should be ErrConflict, got %v", err)
	}
	// Same name in another workspace is fine.
	if err := s.WorkspaceSkills().Create(ctx, newWorkspaceSkill(other.ID, "changelog-sweeper", false)); err != nil {
		t.Fatalf("same name in other workspace should succeed: %v", err)
	}

	// Update.
	found.Description = "Updated description"
	found.Version = "0.2.0"
	found.Dependencies = domain.SkillDependencies{Binaries: []string{"pdftotext"}}
	if err := s.WorkspaceSkills().Update(ctx, found); err != nil {
		t.Fatalf("update skill: %v", err)
	}
	updated, err := s.WorkspaceSkills().GetByName(ctx, ws.ID, "changelog-sweeper")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if updated.Description != "Updated description" || updated.Version != "0.2.0" {
		t.Fatalf("update not persisted: %+v", updated)
	}
	if len(updated.Dependencies.Binaries) != 1 || len(updated.Dependencies.Tools) != 0 {
		t.Fatalf("dependencies not replaced on update: %+v", updated.Dependencies)
	}
	if !updated.Enabled {
		t.Fatal("update must not touch the enabled master switch")
	}

	// List is workspace-scoped and ordered.
	list, err := s.WorkspaceSkills().List(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d skills, err %v", len(list), err)
	}
	otherList, err := s.WorkspaceSkills().List(ctx, other.ID)
	if err != nil || len(otherList) != 1 || otherList[0].Enabled {
		t.Fatalf("other list = %+v, err %v", otherList, err)
	}

	// Enabled toggle round-trip.
	if err := s.WorkspaceSkills().SetEnabled(ctx, ws.ID, skill.ID, false); err != nil {
		t.Fatalf("disable skill: %v", err)
	}
	names, err := s.WorkspaceSkills().ListEnabled(ctx, "skills-it-ws")
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("disabled skill must not be listed enabled, got %v", names)
	}
	if err := s.WorkspaceSkills().SetEnabled(ctx, ws.ID, skill.ID, true); err != nil {
		t.Fatalf("enable skill: %v", err)
	}
	names, err = s.WorkspaceSkills().ListEnabled(ctx, "skills-it-ws")
	if err != nil || len(names) != 1 || names[0] != "changelog-sweeper" {
		t.Fatalf("list enabled after re-enable = %v, err %v", names, err)
	}

	// ListEnabled for an unknown slug is not-found.
	if _, err := s.WorkspaceSkills().ListEnabled(ctx, "no-such-ws"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown slug should be ErrNotFound, got %v", err)
	}

	// Delete.
	if err := s.WorkspaceSkills().Delete(ctx, ws.ID, skill.ID); err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	if _, err := s.WorkspaceSkills().Get(ctx, ws.ID, skill.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get after delete should be ErrNotFound, got %v", err)
	}
	if err := s.WorkspaceSkills().Delete(ctx, ws.ID, skill.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("double delete should be ErrNotFound, got %v", err)
	}

	// Invalid input is rejected before touching the database.
	if err := s.WorkspaceSkills().Create(ctx, newWorkspaceSkill(ws.ID, "Not A Slug", true)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid name should be ErrInvalid, got %v", err)
	}
	if err := s.WorkspaceSkills().Create(ctx, newWorkspaceSkill(ws.ID, "api", true)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("reserved name should be ErrInvalid, got %v", err)
	}
}
