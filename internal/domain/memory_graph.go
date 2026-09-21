package domain

import (
	"fmt"
	"strings"
	"time"
)

// MemoryTargetType names the kind of memory row a pointer aims at: the
// polymorphic vector index (wave3-memory-vectors-and-graph D1) hosts
// embeddings for all three kinds, while the associative graph's edges
// (D7/D8) connect only to the two extracted kinds — raw turns have no row of
// their own in the memory schema, so they are citable through their source
// event id but not linkable as graph endpoints.
type MemoryTargetType string

const (
	MemoryTargetNote  MemoryTargetType = "note"
	MemoryTargetEvent MemoryTargetType = "event"
	MemoryTargetRaw   MemoryTargetType = "raw"
)

// ValidMemoryTargetType reports whether t is one of the three target kinds.
func ValidMemoryTargetType(t MemoryTargetType) bool {
	switch t {
	case MemoryTargetNote, MemoryTargetEvent, MemoryTargetRaw:
		return true
	default:
		return false
	}
}

// ValidMemoryEdgeTargetType reports whether t may carry a graph edge: notes
// and events only (D8 — depth-bounded entity → linked rows; raw evidence is
// reached through the vector channel, not the graph).
func ValidMemoryEdgeTargetType(t MemoryTargetType) bool {
	switch t {
	case MemoryTargetNote, MemoryTargetEvent:
		return true
	default:
		return false
	}
}

// NormalizeEntityLabel is the entity identity function (wave3 D7): trim,
// casefold, and a naive singular-tolerant English plural strip (a trailing
// "s" vanishes unless the label would collapse to nothing or already ends in
// "ss"). Pure and deterministic — the extraction side-call proposes labels
// with it (D6), the store verifies parity against it, and the consolidator's
// fold compares through it — so a label's normalized form is the same string
// everywhere it is computed. The plural strip is deliberately naive ("boxes"
// and "box" stay distinct): it tolerates the dominant English plural, not
// the language.
func NormalizeEntityLabel(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	if len(s) > 2 && strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") {
		s = s[:len(s)-1]
	}
	return s
}

// MemoryEmbedding is one vector-index row (wave3 D1): the embedding of a
// note, an event, or a raw turn at a specific dimension, with the visibility
// snapshot and the source event id as its permanent citation pointer (D9).
// TargetID is the linked row's id for note/event targets and the turn's
// evidence id for raw targets. Owner columns follow the memory_events shape:
// set exactly per the visibility tier. Reads return the row's scope fields
// and target pointer but never the vector itself — half precision readback
// is lossy and retrieval orders server-side on the stored vector.
type MemoryEmbedding struct {
	ID            string             `json:"id"`
	WorkspaceID   string             `json:"workspace_id"`
	TargetType    MemoryTargetType   `json:"target_type"`
	TargetID      string             `json:"target_id"`
	Dimension     int                `json:"dimension"`
	Embedding     []float32          `json:"embedding"` // write-only; never read back
	Visibility    MemoryVisibility   `json:"visibility"`
	UserID        *string            `json:"user_id"`  // set iff visibility=user
	AgentID       *string            `json:"agent_id"` // set iff visibility=agent
	SourceEventID string             `json:"source_event_id"`
	LearnedAt     time.Time          `json:"learned_at"`
	CreatedAt     time.Time          `json:"created_at"`
}

// ValidateMemoryEmbedding enforces the vector-index write shape: a known
// target kind, a non-empty target pointer, a positive dimension matching the
// vector length, the owner shape pinned by the SQL CHECK, and the evidence
// stamp (learned_at + source event id) every index row is born with — the
// citation pointer is what makes a raw hit citable when extraction failed
// (wave3 spec: failed extraction loses nothing).
func ValidateMemoryEmbedding(e *MemoryEmbedding) error {
	if e == nil || e.WorkspaceID == "" {
		return ErrInvalid
	}
	if !ValidMemoryTargetType(e.TargetType) {
		return fmt.Errorf("%w: unknown memory target type %q", ErrInvalid, e.TargetType)
	}
	if e.TargetID == "" {
		return fmt.Errorf("%w: memory embedding target_id is empty", ErrInvalid)
	}
	if e.Dimension <= 0 {
		return fmt.Errorf("%w: memory embedding dimension must be positive", ErrInvalid)
	}
	if len(e.Embedding) != e.Dimension {
		return fmt.Errorf("%w: memory embedding vector length %d does not match dimension %d", ErrInvalid, len(e.Embedding), e.Dimension)
	}
	if !ValidMemoryVisibility(e.Visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", ErrInvalid, e.Visibility)
	}
	if err := ValidateMemoryNoteOwner(e.Visibility, e.UserID, e.AgentID); err != nil {
		return err
	}
	if e.LearnedAt.IsZero() {
		return fmt.Errorf("%w: learned_at is unset", ErrMemoryProvenanceIncomplete)
	}
	if e.SourceEventID == "" {
		return fmt.Errorf("%w: source_event_id is empty", ErrMemoryProvenanceIncomplete)
	}
	return nil
}

// MemoryEntity is one associative-store label (wave3 D7): a tenant-scoped
// pointer with a provenance stamp at birth. Labels carry no visibility tier
// — access control lives on the edges and linked rows — and the
// (workspace, normalized label) pair is the identity: resolving the same
// label twice yields one row.
type MemoryEntity struct {
	ID              string       `json:"id"`
	WorkspaceID     string       `json:"workspace_id"`
	Label           string       `json:"label"`
	NormalizedLabel string       `json:"normalized_label"`
	Origin          MemoryOrigin `json:"origin"`
	SourceEventID   string       `json:"source_event_id"`
	LearnedAt       time.Time    `json:"learned_at"`
	CreatedAt       time.Time    `json:"created_at"`
}

// ValidateMemoryEntity enforces the entity write shape: a non-empty label
// whose normalized form matches what NormalizeEntityLabel computes (parity
// verification — the pipeline proposes label + normalized label, and the
// store is the last line of defense against normalization drift), plus the
// provenance stamp. The label keeps its original spelling at birth; identity
// is the normalized form.
func ValidateMemoryEntity(e *MemoryEntity) error {
	if e == nil || e.WorkspaceID == "" {
		return ErrInvalid
	}
	if strings.TrimSpace(e.Label) == "" {
		return fmt.Errorf("%w: entity label is empty", ErrInvalid)
	}
	if e.NormalizedLabel != NormalizeEntityLabel(e.Label) {
		return fmt.Errorf("%w: entity normalized label %q does not match label %q", ErrInvalid, e.NormalizedLabel, e.Label)
	}
	if !ValidMemoryOrigin(e.Origin) {
		return fmt.Errorf("%w: unknown memory origin %q", ErrMemoryProvenanceIncomplete, e.Origin)
	}
	if e.LearnedAt.IsZero() {
		return fmt.Errorf("%w: learned_at is unset", ErrMemoryProvenanceIncomplete)
	}
	if e.SourceEventID == "" {
		return fmt.Errorf("%w: source_event_id is empty", ErrMemoryProvenanceIncomplete)
	}
	return nil
}

// MemoryEntityEdge is one bipartite graph link (wave3 D7/D8): an entity
// pointing at an extracted note or event, stamped with the narrowest
// visibility of what it links at birth. Edges are never widened — promotion
// widens through the linked row, and edge reads take scope from that live
// row. Like entities, edges carry provenance (origin + source event id) and
// never get deleted: a fold re-points them onto the surviving entity.
type MemoryEntityEdge struct {
	ID            string           `json:"id"`
	EntityID      string           `json:"entity_id"`
	TargetType    MemoryTargetType `json:"target_type"`
	TargetID      string           `json:"target_id"`
	Visibility    MemoryVisibility `json:"visibility"`
	Origin        MemoryOrigin     `json:"origin"`
	SourceEventID string           `json:"source_event_id"`
	CreatedAt     time.Time        `json:"created_at"`
}

// ValidateMemoryEntityEdge enforces the edge write shape: a non-empty entity
// pointer, a note/event target (raw turns are not graph endpoints), a valid
// visibility tier, and the provenance stamp. The narrowest-endpoint rule is
// the caller's to compute (the pipeline knows what it is linking); the store
// pins only that the tier is real.
func ValidateMemoryEntityEdge(edge *MemoryEntityEdge) error {
	if edge == nil || edge.EntityID == "" {
		return ErrInvalid
	}
	if !ValidMemoryEdgeTargetType(edge.TargetType) {
		return fmt.Errorf("%w: memory entity edges link notes and events only, got %q", ErrInvalid, edge.TargetType)
	}
	if edge.TargetID == "" {
		return fmt.Errorf("%w: memory entity edge target_id is empty", ErrInvalid)
	}
	if !ValidMemoryVisibility(edge.Visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", ErrInvalid, edge.Visibility)
	}
	if !ValidMemoryOrigin(edge.Origin) {
		return fmt.Errorf("%w: unknown memory origin %q", ErrMemoryProvenanceIncomplete, edge.Origin)
	}
	if edge.SourceEventID == "" {
		return fmt.Errorf("%w: source_event_id is empty", ErrMemoryProvenanceIncomplete)
	}
	return nil
}
