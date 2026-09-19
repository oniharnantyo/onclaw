package memory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMorningReportJSONContract pins the report wire shape — the API
// contract the Memory pane reads.
func TestMorningReportJSONContract(t *testing.T) {
	report := MorningReport{
		GeneratedAt: time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC),
		Conflicts: []ConflictFlag{{
			NoteID:    "note-1",
			Document:  "WORKSPACE.md",
			Excerpt:   "The headquarters is in Bandung",
			FlaggedAt: time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC),
		}},
		Merges: []MergeRecord{{
			CanonicalID: "note-canonical",
			MergedIDs:   []string{"note-a", "note-b"},
		}},
		ExtractionFailures: 3,
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	got := string(raw)
	for _, key := range []string{
		`"generated_at":`, `"conflicts":`, `"merges":`, `"extraction_failures":3`,
		`"note_id":"note-1"`, `"document":"WORKSPACE.md"`, `"excerpt":`, `"flagged_at":`,
		`"canonical_id":"note-canonical"`, `"merged_ids":["note-a","note-b"]`,
	} {
		if !strings.Contains(got, key) {
			t.Fatalf("report JSON missing %s:\n%s", key, got)
		}
	}
}

// TestMorningReportEmptySerializesAsArrays: an empty report carries empty
// arrays, never nulls — the producer contract the consolidator's RunNow
// already follows.
func TestMorningReportEmptySerializesAsArrays(t *testing.T) {
	raw, err := json.Marshal(MorningReport{Conflicts: []ConflictFlag{}, Merges: []MergeRecord{}})
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, `"conflicts":null`) || strings.Contains(got, `"merges":null`) {
		t.Fatalf("empty report must serialize arrays, not nulls: %s", got)
	}
}
