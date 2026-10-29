package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestStagedSignals_Accumulate covers the staging row's cumulative evidence:
// windows merge without deduplication — every recall is kept, so the derived
// distinct counts grow exactly when new queries or days arrive.
func TestStagedSignals_Accumulate(t *testing.T) {
	day1 := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	cur := newStagedSignals(RecallEvidence{
		Scores:  []float64{0.9},
		Queries: []string{"dark mode"},
		Ats:     []time.Time{day1},
	})
	cur = appendStagedSignals(cur, RecallEvidence{
		Scores:  []float64{0.8, 1.0},
		Queries: []string{"theme", "dark mode"},
		Ats:     []time.Time{day2, day2},
	})

	require.Equal(t, []float64{0.9, 0.8, 1.0}, cur.Scores)
	require.Equal(t, 3, len(cur.Ats))

	ev := cur.evidence()
	sig := ComputeSignals(ev, 0, day2)
	require.Equal(t, 3, sig.Recalls)
	require.Equal(t, 2, sig.DistinctQueries)
	require.Equal(t, 2, sig.DistinctDays)
	require.Equal(t, day2, sig.LastRecall)
}

// TestStagedSignals_EncodeDecode covers the JSON round trip of the staging
// row's signals document.
func TestStagedSignals_EncodeDecode(t *testing.T) {
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	cur := newStagedSignals(RecallEvidence{
		Scores:  []float64{0.9},
		Queries: []string{"dark mode"},
		Ats:     []time.Time{at},
	})

	encoded, err := cur.encode()
	require.NoError(t, err)

	decoded, err := decodeStagedSignals(encoded)
	require.NoError(t, err)
	require.Equal(t, cur, decoded)

	// An empty document is a fresh candidate, and garbage is an error.
	fresh, err := decodeStagedSignals("")
	require.NoError(t, err)
	require.Equal(t, stagedSignals{}, fresh)
	_, err = decodeStagedSignals("{not json")
	require.Error(t, err)
}

// TestSelectPromotions_CapAndOrder covers the cap and the ordering: twelve
// qualifying candidates yield at most ten, best score first and source
// reference ascending on ties.
func TestSelectPromotions_CapAndOrder(t *testing.T) {
	var cands []LineCandidate
	var signals []Signals
	for i := 1; i <= 12; i++ {
		cands = append(cands, LineCandidate{
			Line:      "line",
			SourceRef: sourceRef("2026-10-27.md", i),
		})
		signals = append(signals, Signals{Score: 0.9, Recalls: 4, DistinctQueries: 3})
	}

	chosen := selectPromotions(cands, signals, nil)

	require.Len(t, chosen, MaxPromotionsPerSweep)
	for i := 1; i < len(chosen); i++ {
		require.Less(t, chosen[i-1].SourceRef, chosen[i].SourceRef,
			"equal scores must order by source reference ascending")
	}
}

// TestSelectPromotions_SkipsPromoted covers the never-twice gate at
// selection time: a source reference already in the index never re-passes,
// however strong its evidence.
func TestSelectPromotions_SkipsPromoted(t *testing.T) {
	cands := []LineCandidate{
		{Line: "already in", SourceRef: "memory/2026-10-26.md#L1"},
		{Line: "fresh", SourceRef: "memory/2026-10-27.md#L1"},
	}
	signals := []Signals{
		{Score: 1.0, Recalls: 9, DistinctQueries: 9},
		{Score: 0.85, Recalls: 4, DistinctQueries: 3},
	}

	chosen := selectPromotions(cands, signals, map[string]bool{"memory/2026-10-26.md#L1": true})

	require.Len(t, chosen, 1)
	require.Equal(t, "memory/2026-10-27.md#L1", chosen[0].SourceRef)
}

// TestSelectPromotions_GateFilter covers the negative side: candidates below
// any gate never reach the cap.
func TestSelectPromotions_GateFilter(t *testing.T) {
	cands := []LineCandidate{
		{Line: "weak score", SourceRef: "memory/2026-10-27.md#L1"},
		{Line: "few recalls", SourceRef: "memory/2026-10-27.md#L2"},
		{Line: "few queries", SourceRef: "memory/2026-10-27.md#L3"},
	}
	signals := []Signals{
		{Score: 0.5, Recalls: 9, DistinctQueries: 9},
		{Score: 0.9, Recalls: 2, DistinctQueries: 9},
		{Score: 0.9, Recalls: 9, DistinctQueries: 2},
	}

	require.Empty(t, selectPromotions(cands, signals, nil))
}

// TestPromotedLine covers the index-entry conversion: the line verbatim, its
// source reference, and the promotion date.
func TestPromotedLine(t *testing.T) {
	now := time.Date(2026, 10, 27, 14, 30, 0, 0, time.UTC)
	c := LineCandidate{
		Line:      "The user prefers dark mode in every tool",
		SourceRef: "memory/2026-10-27.md#L12",
	}

	p := promotedLine(c, now)

	require.Equal(t, c.Line, p.Line)
	require.Equal(t, c.SourceRef, p.SourceRef)
	require.Equal(t, "2026-10-27", p.PromotedOn.Format(dailyFormat))
}

// TestSourceRef covers the workspace reference format: memory-relative file
// plus the one-based line number.
func TestSourceRef(t *testing.T) {
	require.Equal(t, "memory/2026-10-27.md#L12", sourceRef("2026-10-27.md", 12))
}
