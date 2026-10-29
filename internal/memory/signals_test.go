package memory

import (
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// traceAt builds one recall trace with the given origin, query, time and hit
// paths-plus-scores.
func traceAt(origin, query string, at time.Time, hits ...store.MemoryTraceHit) store.MemoryTrace {
	return store.MemoryTrace{
		AgentID:   "agent-1",
		Origin:    origin,
		Query:     query,
		Hits:      hits,
		CreatedAt: at,
	}
}

// TestComputeSignals_WeightedSum covers the six-signal arithmetic (design
// D8): each signal normalized, the score the weighted sum of the weights.
func TestComputeSignals_WeightedSum(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ev := RecallEvidence{
		Scores:  []float64{1.0, 0.5, 1.0, 0.5},
		Queries: []string{"dark mode", "theme", "dark mode", "colors"},
		Ats: []time.Time{
			now.Add(-24 * time.Hour),
			now.Add(-24 * time.Hour),
			now,
			now,
		},
	}

	got := ComputeSignals(ev, 3, now)

	require.Equal(t, 4, got.Recalls)
	require.Equal(t, 3, got.DistinctQueries)
	require.Equal(t, 2, got.DistinctDays)
	require.Equal(t, now, got.LastRecall)
	require.Equal(t, 0.75, got.Relevance)        // mean(1, .5, 1, .5)
	require.Equal(t, 0.4, got.Frequency)         // 4/10
	require.Equal(t, 1.0, got.Diversity)         // 3/3 saturates
	require.Equal(t, 1.0, got.Recency)           // the last recall is at now
	require.Equal(t, 2.0/3.0, got.Consolidation) // 2/3 days
	require.Equal(t, 0.6, got.ConceptDensity)    // 3/5

	want := WeightRelevance*got.Relevance +
		WeightFrequency*got.Frequency +
		WeightDiversity*got.Diversity +
		WeightRecency*got.Recency +
		WeightConsolidation*got.Consolidation +
		WeightConceptDensity*got.ConceptDensity
	require.Equal(t, want, got.Score)
}

// TestComputeSignals_EmptyEvidence covers the zero case: a candidate no
// user-origin trace ever hit scores zero on every signal.
func TestComputeSignals_EmptyEvidence(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	got := ComputeSignals(RecallEvidence{}, 4, now)

	require.Equal(t, Signals{DistinctQueries: 0, ConceptDensity: 0}, got)
	require.Equal(t, 0.0, got.Score)
	require.False(t, got.PassesGates())
}

// TestComputeSignals_RecencyHalfLife covers the 14-day half-life: 14 days
// since the last recall halves recency, 28 days quarters it, and a same-
// moment recall is full.
func TestComputeSignals_RecencyHalfLife(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	full := ComputeSignals(RecallEvidence{
		Scores:  []float64{1},
		Queries: []string{"q"},
		Ats:     []time.Time{now},
	}, 0, now)
	require.Equal(t, 1.0, full.Recency)

	half := ComputeSignals(RecallEvidence{
		Scores:  []float64{1},
		Queries: []string{"q"},
		Ats:     []time.Time{now.Add(-14 * 24 * time.Hour)},
	}, 0, now)
	require.InDelta(t, 0.5, half.Recency, 1e-12)

	quarter := ComputeSignals(RecallEvidence{
		Scores:  []float64{1},
		Queries: []string{"q"},
		Ats:     []time.Time{now.Add(-28 * 24 * time.Hour)},
	}, 0, now)
	require.InDelta(t, 0.25, quarter.Recency, 1e-12)
}

// TestComputeSignals_UserOriginOnly is the taint gate (design D12): adding
// scheduler-, subagent- and other-origin traces — however perfect their
// scores — leaves every signal unchanged; only user-origin traces count.
func TestComputeSignals_UserOriginOnly(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	user := []store.MemoryTrace{
		traceAt(store.MemoryOriginUser, "dark mode", now.Add(-time.Hour),
			store.MemoryTraceHit{Path: "memory/2026-10-27.md", Score: 1}),
	}
	tainted := append([]store.MemoryTrace{}, user...)
	tainted = append(tainted,
		traceAt(store.MemoryOriginScheduler, "dark mode", now,
			store.MemoryTraceHit{Path: "memory/2026-10-27.md", Score: 1}),
		traceAt(store.MemoryOriginSubagent, "dark mode", now,
			store.MemoryTraceHit{Path: "memory/2026-10-27.md", Score: 1}),
		traceAt(store.MemoryOriginOther, "dark mode", now,
			store.MemoryTraceHit{Path: "memory/2026-10-27.md", Score: 1}),
	)

	clean := ComputeSignals(EvidenceFromTraces(user, "2026-10-27.md"), 0, now)
	taintedSignals := ComputeSignals(EvidenceFromTraces(tainted, "2026-10-27.md"), 0, now)

	require.Equal(t, clean, taintedSignals)
	require.Equal(t, 1, taintedSignals.Recalls)
}

// TestEvidenceFromTraces_PathForms covers the hit-path matching: both the
// memory-relative and the workspace-relative forms attribute the trace to the
// candidate, and a non-hitting trace is ignored. A trace with several hits
// contributes only its own hit's score for the candidate.
func TestEvidenceFromTraces_PathForms(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	traces := []store.MemoryTrace{
		traceAt(store.MemoryOriginUser, "q1", now,
			store.MemoryTraceHit{Path: "memory/other.md", Score: 0.9},
			store.MemoryTraceHit{Path: "memory/2026-10-27.md", Score: 0.7}),
		traceAt(store.MemoryOriginUser, "q2", now,
			store.MemoryTraceHit{Path: "2026-10-27.md", Score: 0.4}),
		traceAt(store.MemoryOriginUser, "q3", now,
			store.MemoryTraceHit{Path: "memory/2026-10-26.md", Score: 1}),
	}

	ev := EvidenceFromTraces(traces, "2026-10-27.md")

	require.Equal(t, 2, len(ev.Scores))
	require.Equal(t, 0.7, ev.Scores[0])
	require.Equal(t, 0.4, ev.Scores[1])
	require.Equal(t, []string{"q1", "q2"}, ev.Queries)
}

// TestGates_SpecScenarios reproduces the spec's promotion-gate scenarios
// verbatim: a score of 0.85 from four recalls across three distinct queries
// passes; a score of 0.9 from five recalls over only two distinct queries
// fails the diversity gate and stays staged.
func TestGates_SpecScenarios(t *testing.T) {
	passing := Signals{Score: 0.85, Recalls: 4, DistinctQueries: 3}
	require.True(t, passing.PassesGates(), "0.85 with four recalls over three queries must pass")

	failing := Signals{Score: 0.9, Recalls: 5, DistinctQueries: 2}
	require.False(t, failing.PassesGates(), "0.9 with five recalls over two queries must fail")
}

// TestGates_Boundaries covers each gate's threshold behavior: all three gates
// are conjunctive and each passes at exactly its threshold.
func TestGates_Boundaries(t *testing.T) {
	tests := []struct {
		name    string
		signals Signals
		want    bool
	}{
		{name: "all at threshold", signals: Signals{Score: 0.8, Recalls: 3, DistinctQueries: 3}, want: true},
		{name: "score below", signals: Signals{Score: 0.79, Recalls: 3, DistinctQueries: 3}, want: false},
		{name: "recalls below", signals: Signals{Score: 0.9, Recalls: 2, DistinctQueries: 3}, want: false},
		{name: "queries below", signals: Signals{Score: 0.9, Recalls: 3, DistinctQueries: 2}, want: false},
		{name: "above all", signals: Signals{Score: 1, Recalls: 10, DistinctQueries: 10}, want: true},
		{name: "nothing recalled", signals: Signals{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.signals.PassesGates())
		})
	}
}
