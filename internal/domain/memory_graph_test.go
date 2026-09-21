package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestNormalizeEntityLabel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Sari", "sari"},
		{"  SARI  ", "sari"},
		{"Project Atlas", "project atla"}, // naive: even natural -s words strip — documented, tolerated
		{"Projects", "project"},           // naive plural strip
		{"incidents", "incident"},         // plural strip
		{"business", "business"},          // "ss" endings are never stripped
		{"boss", "boss"},                  // "ss" endings are never stripped
		{"acme corp", "acme corp"},        // no trailing s
		{"  ", ""},                        // whitespace collapses to empty
		{"", ""},
		{"Ops", "op"}, // single-s words shorten — accepted naivete
	}
	for _, tc := range cases {
		if got := domain.NormalizeEntityLabel(tc.in); got != tc.want {
			t.Errorf("NormalizeEntityLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Identity variants fold together — the dedupe property the resolver and
	// the consolidator both rely on.
	variants := []string{"Sari", "SARI", " saris ", "Saris"}
	want := domain.NormalizeEntityLabel(variants[0])
	for _, v := range variants[1:] {
		if got := domain.NormalizeEntityLabel(v); got != want {
			t.Errorf("variants %q and %q normalize differently: %q vs %q", variants[0], v, want, got)
		}
	}
}

func TestValidMemoryTargetType(t *testing.T) {
	for _, valid := range []domain.MemoryTargetType{domain.MemoryTargetNote, domain.MemoryTargetEvent, domain.MemoryTargetRaw} {
		if !domain.ValidMemoryTargetType(valid) {
			t.Errorf("ValidMemoryTargetType(%q) expected true", valid)
		}
	}
	for _, invalid := range []domain.MemoryTargetType{"", "session", "NOTE"} {
		if domain.ValidMemoryTargetType(invalid) {
			t.Errorf("ValidMemoryTargetType(%q) expected false", invalid)
		}
	}
}

func TestValidMemoryEdgeTargetType(t *testing.T) {
	if !domain.ValidMemoryEdgeTargetType(domain.MemoryTargetNote) || !domain.ValidMemoryEdgeTargetType(domain.MemoryTargetEvent) {
		t.Error("notes and events must carry edges")
	}
	if domain.ValidMemoryEdgeTargetType(domain.MemoryTargetRaw) {
		t.Error("raw turns must not be graph endpoints")
	}
	if domain.ValidMemoryEdgeTargetType("") {
		t.Error("empty target type must not carry edges")
	}
}

func validMemoryEmbedding() *domain.MemoryEmbedding {
	user := "0b8fd6d7-8c3d-4b57-9d34-2b31a5eb1f21"
	vec := []float32{0.1, 0.2, 0.3}
	return &domain.MemoryEmbedding{
		WorkspaceID:   "2f0c9a2e-6f4c-4e2b-9d3f-6efba1e5cf10",
		TargetType:    domain.MemoryTargetRaw,
		TargetID:      "evt_123",
		Dimension:     3,
		Embedding:     vec,
		Visibility:    domain.MemoryVisibilityUser,
		UserID:        &user,
		SourceEventID: "evt_123",
		LearnedAt:     time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
	}
}

func TestValidateMemoryEmbedding(t *testing.T) {
	if err := domain.ValidateMemoryEmbedding(validMemoryEmbedding()); err != nil {
		t.Fatalf("expected valid embedding to pass, got %v", err)
	}

	// Nil and missing workspace.
	if err := domain.ValidateMemoryEmbedding(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for nil embedding, got %v", err)
	}
	e := validMemoryEmbedding()
	e.WorkspaceID = ""
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty workspace, got %v", err)
	}

	// Unknown target type.
	e = validMemoryEmbedding()
	e.TargetType = "session"
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for unknown target type, got %v", err)
	}

	// Empty target pointer.
	e = validMemoryEmbedding()
	e.TargetID = ""
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty target_id, got %v", err)
	}

	// Dimension mismatch and non-positive dimension.
	e = validMemoryEmbedding()
	e.Embedding = []float32{0.1, 0.2}
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for vector shorter than dimension, got %v", err)
	}
	e = validMemoryEmbedding()
	e.Dimension = 0
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for zero dimension, got %v", err)
	}

	// Unknown visibility.
	e = validMemoryEmbedding()
	e.Visibility = "public"
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for unknown visibility, got %v", err)
	}

	// Owner shape: user tier without owner, agent tier with a user owner.
	e = validMemoryEmbedding()
	e.UserID = nil
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for user-tier embedding without owner, got %v", err)
	}
	e = validMemoryEmbedding()
	agent := "6a5f4b3c-1d2e-4f5a-8b7c-9d0e1f2a3b4c"
	e.Visibility = domain.MemoryVisibilityAgent
	e.UserID = nil
	e.AgentID = &agent
	if err := domain.ValidateMemoryEmbedding(e); err != nil {
		t.Fatalf("expected agent-tier embedding with agent owner to pass, got %v", err)
	}
	e.AgentID = nil
	e.UserID = validMemoryEmbedding().UserID
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for agent-tier embedding with user owner, got %v", err)
	}

	// Evidence stamp: unset learned_at and empty source event id.
	e = validMemoryEmbedding()
	e.LearnedAt = time.Time{}
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for unset learned_at, got %v", err)
	}
	e = validMemoryEmbedding()
	e.SourceEventID = ""
	if err := domain.ValidateMemoryEmbedding(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for empty source_event_id, got %v", err)
	}
}

func validMemoryEntity() *domain.MemoryEntity {
	return &domain.MemoryEntity{
		WorkspaceID:     "2f0c9a2e-6f4c-4e2b-9d3f-6efba1e5cf10",
		Label:           "Sari",
		NormalizedLabel: "sari",
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "evt_123",
		LearnedAt:       time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
	}
}

func TestValidateMemoryEntity(t *testing.T) {
	if err := domain.ValidateMemoryEntity(validMemoryEntity()); err != nil {
		t.Fatalf("expected valid entity to pass, got %v", err)
	}

	if err := domain.ValidateMemoryEntity(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for nil entity, got %v", err)
	}

	// Empty label.
	e := validMemoryEntity()
	e.Label = "   "
	if err := domain.ValidateMemoryEntity(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for blank label, got %v", err)
	}

	// Normalization parity: the stored normalized form must be exactly what
	// NormalizeEntityLabel computes from the label.
	e = validMemoryEntity()
	e.NormalizedLabel = "Sari"
	if err := domain.ValidateMemoryEntity(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for non-casefolded normalized label, got %v", err)
	}
	e = validMemoryEntity()
	e.Label = "Saris"
	e.NormalizedLabel = "saris" // ignores the plural strip
	if err := domain.ValidateMemoryEntity(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid when normalized label ignores the plural strip, got %v", err)
	}
	e.Label = "Saris"
	e.NormalizedLabel = domain.NormalizeEntityLabel(e.Label)
	if err := domain.ValidateMemoryEntity(e); err != nil {
		t.Fatalf("expected parity-passing entity to be valid, got %v", err)
	}

	// Provenance: unknown origin, unset learned_at, empty source event.
	e = validMemoryEntity()
	e.Origin = "guessed"
	if err := domain.ValidateMemoryEntity(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for unknown origin, got %v", err)
	}
	e = validMemoryEntity()
	e.LearnedAt = time.Time{}
	if err := domain.ValidateMemoryEntity(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for unset learned_at, got %v", err)
	}
	e = validMemoryEntity()
	e.SourceEventID = ""
	if err := domain.ValidateMemoryEntity(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for empty source_event_id, got %v", err)
	}
}

func validMemoryEntityEdge() *domain.MemoryEntityEdge {
	return &domain.MemoryEntityEdge{
		EntityID:      "9d2b7c1a-3e4f-4a5b-8c6d-7e8f9a0b1c2d",
		TargetType:    domain.MemoryTargetEvent,
		TargetID:      "5c4b3a2f-1e0d-4c5b-9a8f-7e6d5c4b3a2f",
		Visibility:    domain.MemoryVisibilityUser,
		Origin:        domain.MemoryOriginInfer,
		SourceEventID: "evt_123",
	}
}

func TestValidateMemoryEntityEdge(t *testing.T) {
	if err := domain.ValidateMemoryEntityEdge(validMemoryEntityEdge()); err != nil {
		t.Fatalf("expected valid edge to pass, got %v", err)
	}

	if err := domain.ValidateMemoryEntityEdge(nil); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for nil edge, got %v", err)
	}

	e := validMemoryEntityEdge()
	e.EntityID = ""
	if err := domain.ValidateMemoryEntityEdge(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty entity id, got %v", err)
	}

	// Raw turns are not graph endpoints.
	e = validMemoryEntityEdge()
	e.TargetType = domain.MemoryTargetRaw
	if err := domain.ValidateMemoryEntityEdge(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for raw-target edge, got %v", err)
	}

	e = validMemoryEntityEdge()
	e.TargetID = ""
	if err := domain.ValidateMemoryEntityEdge(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty target_id, got %v", err)
	}

	e = validMemoryEntityEdge()
	e.Visibility = "workspace"
	if err := domain.ValidateMemoryEntityEdge(e); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for unknown visibility, got %v", err)
	}

	e = validMemoryEntityEdge()
	e.Origin = "guessed"
	if err := domain.ValidateMemoryEntityEdge(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for unknown origin, got %v", err)
	}

	e = validMemoryEntityEdge()
	e.SourceEventID = ""
	if err := domain.ValidateMemoryEntityEdge(e); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Errorf("expected ErrMemoryProvenanceIncomplete for empty source_event_id, got %v", err)
	}
}
