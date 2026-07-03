package service_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestMapSkillToView_FullMapping(t *testing.T) {
	s := &store.Skill{
		Name:        "test-skill",
		Scope:       "global",
		Description: "A test skill",
		Enabled:     1,
		SkillPath:   "/path/to/skill",
	}

	view := service.MapSkillToView(s)
	if view.Name != "test-skill" {
		t.Errorf("expected Name 'test-skill', got %q", view.Name)
	}
	if view.Scope != "global" {
		t.Errorf("expected Scope 'global', got %q", view.Scope)
	}
	if view.Description != "A test skill" {
		t.Errorf("expected Description 'A test skill', got %q", view.Description)
	}
	if !view.Enabled {
		t.Error("expected Enabled to be true")
	}
	if view.SkillPath != "/path/to/skill" {
		t.Errorf("expected SkillPath '/path/to/skill', got %q", view.SkillPath)
	}
}

func TestMapSkillToView_DisabledSkill(t *testing.T) {
	s := &store.Skill{
		Name:    "disabled-skill",
		Enabled: 0,
	}

	view := service.MapSkillToView(s)
	if view.Enabled {
		t.Error("expected Enabled to be false")
	}
}
