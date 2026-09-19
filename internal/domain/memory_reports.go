package domain

import "time"

// MemoryNoteEvidence is one raw-evidence link on a memory note (D12's
// multi-evidence): a pointer back into the session event stream that the
// fact was extracted from, recorded when the link was added. Consolidation
// folds a near-duplicate cluster into one canonical note and links every
// member's source event on it, so the surviving fact cites all of its
// origins. Links are additive and idempotent per (note, source event) —
// raw evidence is never rewritten or deleted (copy-out only).
type MemoryNoteEvidence struct {
	SourceEventID string    `json:"source_event_id"`
	AddedAt       time.Time `json:"added_at"`
}

// MemoryReport is the persisted last morning report of a workspace
// (integrate-agent-zero-memory D12). The report body is the consolidator's
// marshaled MorningReport JSON kept as bytes — the shape is owned by the
// memory package's wire contract; the store only persists and returns it
// verbatim. One row per workspace: saving replaces the previous report.
type MemoryReport struct {
	WorkspaceID string
	Report      []byte
	GeneratedAt time.Time
}
