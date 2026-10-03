package handlers_test

import (
	"net/http"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestPatchAgent_SkillCurationOverride pins the PATCH semantics of the skill
// curation override pair (add-skill-curation-from-traces 2.1): the pair rule
// mirrors the memory side-call override (both set or both cleared, never
// half), a set provider must exist in the workspace, a patch carrying only
// the curation pair is a recognized update (the no-fields guard knows it),
// and empty strings clear back to fall-through.
func TestPatchAgent_SkillCurationOverride(t *testing.T) {
	env := newDenylistTestEnv(t)

	w := env.do(t, http.MethodPost, "/agents",
		`{"name":"Curator","slug":"curator","role":"scout","brief":"Curation target.","provider_id":"prov-denylist","model":"gpt-4o"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	t.Run("both fields set persists the pair", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/curator",
			`{"skill_curation_provider_id":"prov-denylist","skill_curation_model":"gpt-4o-mini"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.SkillCurationProviderID != "prov-denylist" || agent.SkillCurationModel != "gpt-4o-mini" {
			t.Errorf("expected the curation pair persisted, got provider=%q model=%q",
				agent.SkillCurationProviderID, agent.SkillCurationModel)
		}
	})

	t.Run("curation-only patch is a recognized update", func(t *testing.T) {
		// The no-fields-to-update guard must know the curation pair, or this
		// payload would 400 and the field would silently bind nowhere.
		w := env.do(t, http.MethodPatch, "/agents/curator",
			`{"skill_curation_provider_id":"prov-denylist","skill_curation_model":"gpt-4o-mini"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("half-set pair is rejected on a clean agent", func(t *testing.T) {
		// The pair computes over the stored row, so a one-field patch is only
		// half when the stored row carries no pair (tri-state: absent key
		// keeps its value).
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Clean Curator","slug":"clean-curator","role":"scout","brief":"No curation pair yet.","provider_id":"prov-denylist","model":"gpt-4o"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		w = env.do(t, http.MethodPatch, "/agents/clean-curator",
			`{"skill_curation_provider_id":"prov-denylist"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("model without provider is rejected on a clean agent", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/clean-curator",
			`{"skill_curation_model":"gpt-4o-mini"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown provider is rejected", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/curator",
			`{"skill_curation_provider_id":"prov-ghost","skill_curation_model":"gpt-4o-mini"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty strings clear back to fall-through", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/curator",
			`{"skill_curation_provider_id":"","skill_curation_model":""}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.SkillCurationProviderID != "" || agent.SkillCurationModel != "" {
			t.Errorf("expected the pair cleared, got provider=%q model=%q",
				agent.SkillCurationProviderID, agent.SkillCurationModel)
		}
	})
}

// TestCreateAgent_SkillCurationOverride pins the create path's curation pair:
// a fully-specified pair persists; a half-set pair is 400 invalid_request.
func TestCreateAgent_SkillCurationOverride(t *testing.T) {
	env := newDenylistTestEnv(t)

	t.Run("create with a full pair persists it", func(t *testing.T) {
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Pinned Curator","slug":"pinned-curator","role":"scout","brief":"Created with a curation pair.","provider_id":"prov-denylist","model":"gpt-4o","skill_curation_provider_id":"prov-denylist","skill_curation_model":"gpt-4o-mini"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.SkillCurationProviderID != "prov-denylist" || agent.SkillCurationModel != "gpt-4o-mini" {
			t.Errorf("expected the curation pair persisted, got provider=%q model=%q",
				agent.SkillCurationProviderID, agent.SkillCurationModel)
		}
	})

	t.Run("create with a half-set pair is rejected", func(t *testing.T) {
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Half Curator","slug":"half-curator","role":"scout","brief":"Half a pair.","provider_id":"prov-denylist","model":"gpt-4o","skill_curation_provider_id":"prov-denylist"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create with an unknown curation provider is rejected", func(t *testing.T) {
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Ghost Curator","slug":"ghost-curator","role":"scout","brief":"Unknown provider.","provider_id":"prov-denylist","model":"gpt-4o","skill_curation_provider_id":"prov-ghost","skill_curation_model":"gpt-4o-mini"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})

	// The pair validator is the domain rule; pin its parity with the memory
	// side-call rule directly.
	if err := domain.ValidateAgentSkillCuration("p", ""); err == nil {
		t.Error("expected a half-set pair to be rejected")
	}
	if err := domain.ValidateAgentSkillCuration("", ""); err != nil {
		t.Errorf("expected the empty pair to be valid, got %v", err)
	}
}
