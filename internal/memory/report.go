package memory

import "time"

// MorningReport is the consolidator wave's output surface (tasks 6.1–6.3):
// what conflicted with the kept documents, what merged, and how many
// extractions failed overnight — surfaced in the Memory pane. The JSON tags
// are the API contract.
type MorningReport struct {
	GeneratedAt        time.Time      `json:"generated_at"`
	Conflicts          []ConflictFlag `json:"conflicts"`
	Merges             []MergeRecord  `json:"merges"`
	ExtractionFailures int            `json:"extraction_failures"`
}

// ConflictFlag is one doc-over-notes precedence review item (D7): the note
// that contradicts a kept document, which document, and the note excerpt
// under review. The pipeline never edits the documents — the flag is the
// human review surface.
type ConflictFlag struct {
	NoteID    string    `json:"note_id"`
	Document  string    `json:"document"`
	Excerpt   string    `json:"excerpt"`
	FlaggedAt time.Time `json:"flagged_at"`
}

// MergeRecord is one consolidation merge: the canonical note that survived
// and the near-duplicates folded into it as multi-evidence sources (D12).
type MergeRecord struct {
	CanonicalID string   `json:"canonical_id"`
	MergedIDs   []string `json:"merged_ids"`
}
