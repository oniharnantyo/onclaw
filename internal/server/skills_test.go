package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestSkills_PermissionMatrix_And_CRUD(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, "admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")
	nonMemberUser, nonMemberToken := createTestUser(t, env, "nonmember@example.com", "Non Member", "pwd")
	_ = nonMemberUser

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, "skills-ws", "Skills WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	t.Run("non-member gets 404 across all skill endpoints", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/v1/workspaces/skills-ws/skills", nil},
			{http.MethodGet, "/api/v1/workspaces/skills-ws/skills/some-id", nil},
			{http.MethodPost, "/api/v1/workspaces/skills-ws/skills", map[string]any{"name": "Skill"}},
			{http.MethodPatch, "/api/v1/workspaces/skills-ws/skills/some-id", map[string]any{"name": "Renamed"}},
			{http.MethodDelete, "/api/v1/workspaces/skills-ws/skills/some-id", nil},
		}

		for _, ep := range endpoints {
			w := doRequest(env.router, ep.method, ep.path, nonMemberToken, ep.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d", ep.method, ep.path, w.Code)
			}
		}
	})

	var createdSkillID string

	t.Run("admin creates skill", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/skills-ws/skills", adminToken, map[string]any{
			"name":        "incident-runbook",
			"description": "Runbook for production incidents",
			"body":        "# Incident Response\n\n1. Identify triage lead\n2. Open channel",
			"enabled":     true,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Skill domain.WorkspaceSkill `json:"skill"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode skill: %v", err)
		}
		if res.Skill.Name != "incident-runbook" || res.Skill.Body == "" {
			t.Errorf("unexpected created skill: %+v", res.Skill)
		}
		createdSkillID = res.Skill.ID
	})

	t.Run("duplicate skill name in same workspace returns 409 conflict", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/skills-ws/skills", adminToken, map[string]any{
			"name":        "incident-runbook",
			"description": "Duplicate skill",
			"body":        "dup body",
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for duplicate skill name, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty skill name is rejected with 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/skills-ws/skills", adminToken, map[string]any{
			"name": "   ",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on empty skill name, got %d", w.Code)
		}
	})

	t.Run("progressive disclosure: list omits body, get returns body", func(t *testing.T) {
		// Member lists skills -> 200 OK, body is empty/omitted
		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/skills-ws/skills", memberToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list skills, got %d", wList.Code)
		}
		var listRes struct {
			Skills []domain.WorkspaceSkill `json:"skills"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &listRes)
		if len(listRes.Skills) != 1 {
			t.Fatalf("expected 1 skill in list, got %d", len(listRes.Skills))
		}
		if listRes.Skills[0].Body != "" {
			t.Errorf("expected skill body to be omitted in list view, got %q", listRes.Skills[0].Body)
		}
		if listRes.Skills[0].Name != "incident-runbook" || listRes.Skills[0].Description == "" {
			t.Errorf("expected metadata to be present, got %+v", listRes.Skills[0])
		}

		// Member gets single skill -> 200 OK, body is populated
		wGet := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/skills-ws/skills/%s", createdSkillID), memberToken, nil)
		if wGet.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on get skill, got %d", wGet.Code)
		}
		var getRes struct {
			Skill domain.WorkspaceSkill `json:"skill"`
		}
		_ = json.Unmarshal(wGet.Body.Bytes(), &getRes)
		if getRes.Skill.Body != "# Incident Response\n\n1. Identify triage lead\n2. Open channel" {
			t.Errorf("expected full body in single get, got %q", getRes.Skill.Body)
		}
	})

	t.Run("member cannot write/patch/delete skills (403)", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/skills-ws/skills", memberToken, map[string]any{
			"name": "member-skill",
		})
		if wCreate.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member POST skill, got %d", wCreate.Code)
		}

		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/skills-ws/skills/%s", createdSkillID), memberToken, map[string]any{
			"name": "hacked-skill",
		})
		if wPatch.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member PATCH skill, got %d", wPatch.Code)
		}

		wDel := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/skills-ws/skills/%s", createdSkillID), memberToken, nil)
		if wDel.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member DELETE skill, got %d", wDel.Code)
		}
	})

	t.Run("owner updates skill and deletes skill", func(t *testing.T) {
		enabledFalse := false
		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/skills-ws/skills/%s", createdSkillID), ownerToken, map[string]any{
			"description": "Updated Runbook description",
			"enabled":     enabledFalse,
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on patch skill, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		var patchRes struct {
			Skill domain.WorkspaceSkill `json:"skill"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &patchRes)
		if patchRes.Skill.Description != "Updated Runbook description" || patchRes.Skill.Enabled != false {
			t.Errorf("unexpected patch result: %+v", patchRes.Skill)
		}

		wDel := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/skills-ws/skills/%s", createdSkillID), ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on delete skill, got %d", wDel.Code)
		}

		// Subsequent GET returns 404
		wGet := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/skills-ws/skills/%s", createdSkillID), ownerToken, nil)
		if wGet.Code != http.StatusNotFound {
			t.Errorf("expected 404 on deleted skill, got %d", wGet.Code)
		}
	})
}
