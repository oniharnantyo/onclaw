package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTokenSetJaccard covers the similarity arithmetic: identical token sets
// score 1 regardless of case or punctuation, disjoint sets score 0, and
// partial overlap lands in between.
func TestTokenSetJaccard(t *testing.T) {
	require.Equal(t, 1.0, tokenSetJaccard(
		"The user prefers dark mode in every tool",
		"the user prefers DARK mode in every tool."))
	require.Equal(t, 0.0, tokenSetJaccard(
		"The user prefers dark mode",
		"Deploying services happens Thursdays"))
	require.InDelta(t, 0.5, tokenSetJaccard("dark mode theme", "dark mode colors"), 1e-12)
	require.Equal(t, 1.0, tokenSetJaccard("", ""))
	require.Equal(t, 0.0, tokenSetJaccard("word", ""))
}

// TestMergeDuplicateCandidates_MergesDuplicates covers the merge case: two
// paraphrase-free duplicates — the same line appended to two daily notes —
// collapse into the earliest candidate, pooling both lines' recall evidence.
func TestMergeDuplicateCandidates_MergesDuplicates(t *testing.T) {
	earlier := LineCandidate{
		Line:      "The user prefers dark mode in every tool",
		SourceRef: "memory/2026-10-26.md#L3",
		Evidence: RecallEvidence{
			Scores:  []float64{0.9},
			Queries: []string{"dark mode"},
			Ats:     []time.Time{time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)},
		},
	}
	later := LineCandidate{
		Line:      "The user prefers dark mode in every tool.",
		SourceRef: "memory/2026-10-27.md#L5",
		Evidence: RecallEvidence{
			Scores:  []float64{0.8},
			Queries: []string{"theme"},
			Ats:     []time.Time{time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)},
		},
	}

	merged := mergeDuplicateCandidates([]LineCandidate{earlier, later})

	require.Len(t, merged, 1)
	require.Equal(t, earlier.SourceRef, merged[0].SourceRef, "the earliest candidate survives")
	require.Equal(t, []float64{0.9, 0.8}, merged[0].Evidence.Scores, "both recalls are pooled")
	require.Equal(t, []string{"dark mode", "theme"}, merged[0].Evidence.Queries)
}

// TestMergeDuplicateCandidates_KeepsDistinctApart covers the negative case:
// two distinct lines never merge, however the sweep orders them.
func TestMergeDuplicateCandidates_KeepsDistinctApart(t *testing.T) {
	first := LineCandidate{Line: "The user prefers dark mode", SourceRef: "memory/2026-10-26.md#L1"}
	second := LineCandidate{Line: "Deploy the service on Thursdays at noon", SourceRef: "memory/2026-10-26.md#L2"}

	merged := mergeDuplicateCandidates([]LineCandidate{first, second})

	require.Len(t, merged, 2)
	require.Equal(t, first.SourceRef, merged[0].SourceRef)
	require.Equal(t, second.SourceRef, merged[1].SourceRef)
}

// TestMergeDuplicateCandidates_Empty covers the degenerate inputs: nothing
// in, nothing out.
func TestMergeDuplicateCandidates_Empty(t *testing.T) {
	require.Empty(t, mergeDuplicateCandidates(nil))
}
