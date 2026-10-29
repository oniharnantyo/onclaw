package memory

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Signal weights (add-agent-memory design D8, OpenClaw's verbatim constants).
// The candidate score is the weighted sum; every signal is normalized to the
// unit interval first.
const (
	WeightRelevance      = 0.30
	WeightFrequency      = 0.24
	WeightDiversity      = 0.15
	WeightRecency        = 0.15
	WeightConsolidation  = 0.10
	WeightConceptDensity = 0.06
)

// Promotion gates and staging bounds (design D8, verbatim): a candidate is
// promoted only when all three gates hold, at most ten promotions happen per
// sweep, and a staged candidate expires 30 days after first staging.
const (
	PromotionScoreGate    = 0.8
	PromotionRecallGate   = 3
	PromotionDistinctGate = 3
	MaxPromotionsPerSweep = 10
	StagingExpiryDays     = 30
)

// Signal normalizations (v1 defaults, fixed in code — the design's non-goal
// of no configuration surface): counts saturate at their constant, and
// recency decays with a 14-day half-life.
const (
	RecencyHalfLifeDays     = 14
	FrequencySaturation     = 10
	DiversitySaturation     = 3
	ConsolidationSaturation = 3
	ConceptSaturation       = 5
)

// RecallEvidence is the user-origin recall history of one candidate: one
// entry per recall trace whose hits include the candidate. It is the pure
// arithmetic input of ComputeSignals.
type RecallEvidence struct {
	// Scores are the candidate's own hit score in each recall trace.
	Scores []float64
	// Queries are the recall queries, one per trace (with duplicates).
	Queries []string
	// Ats are the recall times, one per trace.
	Ats []time.Time
}

// EvidenceFromTraces extracts a candidate's recall evidence from raw traces —
// the taint gate (design D12): only origin-user traces count toward
// curation, and only those whose hits include the candidate's memory-relative
// path (e.g. "2026-10-27.md" or "prefers-dark-mode.md"). Hit paths may be
// recorded memory-relative or workspace-relative ("memory/2026-10-27.md");
// both match. Traces of every other origin are stored but ignored here.
func EvidenceFromTraces(traces []store.MemoryTrace, memRelPath string) RecallEvidence {
	var ev RecallEvidence
	for _, tr := range traces {
		if tr.Origin != store.MemoryOriginUser {
			continue
		}
		for _, hit := range tr.Hits {
			if !hitPathMatches(hit.Path, memRelPath) {
				continue
			}
			ev.Scores = append(ev.Scores, hit.Score)
			ev.Queries = append(ev.Queries, tr.Query)
			ev.Ats = append(ev.Ats, tr.CreatedAt)
			break
		}
	}
	return ev
}

// hitPathMatches reports whether a recorded hit path refers to the candidate
// at memRelPath: either the paths are equal or the hit carries the candidate
// as its tail segment (the workspace-relative form).
func hitPathMatches(hitPath, memRelPath string) bool {
	if hitPath == memRelPath {
		return true
	}
	return strings.HasSuffix(hitPath, "/"+memRelPath)
}

// Signals is one candidate's normalized signal set plus its weighted score
// and the raw recall statistics the gates count.
type Signals struct {
	Relevance      float64
	Frequency      float64
	Diversity      float64
	Recency        float64
	Consolidation  float64
	ConceptDensity float64
	// Score is the weighted sum of the six signals (design D8).
	Score float64
	// Recalls, DistinctQueries and DistinctDays are the raw user-origin
	// recall statistics; the gates count the first two.
	Recalls         int
	DistinctQueries int
	DistinctDays    int
	// LastRecall is the most recent recall time, zero when never recalled.
	LastRecall time.Time
}

// ComputeSignals normalizes one candidate's recall evidence into the six
// signals and their weighted score (design D8): relevance is the mean hit
// score; frequency saturates at ten recalls; query diversity and multi-day
// consolidation saturate at three; recency decays with a 14-day half-life
// from the last recall; concept density saturates at five concepts. With no
// evidence every signal is zero.
func ComputeSignals(ev RecallEvidence, conceptCount int, now time.Time) Signals {
	s := Signals{
		Recalls:         len(ev.Scores),
		DistinctQueries: len(distinctStrings(ev.Queries)),
		DistinctDays:    len(distinctDays(ev.Ats)),
		LastRecall:      latest(ev.Ats),
	}
	if s.Recalls == 0 {
		return s
	}
	s.Relevance = mean(ev.Scores)
	s.Frequency = saturate(s.Recalls, FrequencySaturation)
	s.Diversity = saturate(s.DistinctQueries, DiversitySaturation)
	s.Recency = recency(now, s.LastRecall)
	s.Consolidation = saturate(s.DistinctDays, ConsolidationSaturation)
	s.ConceptDensity = saturate(conceptCount, ConceptSaturation)
	s.Score = WeightRelevance*s.Relevance +
		WeightFrequency*s.Frequency +
		WeightDiversity*s.Diversity +
		WeightRecency*s.Recency +
		WeightConsolidation*s.Consolidation +
		WeightConceptDensity*s.ConceptDensity
	return s
}

// PassesGates reports whether the candidate clears all three promotion gates
// (design D8): score at least 0.8, at least three recalls, and at least
// three distinct queries — all three, never a subset.
func (s Signals) PassesGates() bool {
	return s.Score >= PromotionScoreGate &&
		s.Recalls >= PromotionRecallGate &&
		s.DistinctQueries >= PromotionDistinctGate
}

// recency applies the 14-day half-life: exp(-ln(2)·days/14), floored at the
// moment of the last recall. A future timestamp (clock skew) counts as now.
func recency(now, last time.Time) float64 {
	days := now.Sub(last).Hours() / 24
	if days < 0 {
		days = 0
	}
	return math.Exp2(-days / RecencyHalfLifeDays)
}

// saturate maps a count onto the unit interval, saturating at max.
func saturate(count, max int) float64 {
	if count >= max {
		return 1
	}
	return float64(count) / float64(max)
}

// mean averages the scores; an empty input averages to zero.
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// distinctStrings returns the deduplicated non-empty strings, sorted.
func distinctStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// distinctDays returns the deduplicated UTC calendar days of the times,
// sorted.
func distinctDays(in []time.Time) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		day := t.UTC().Format(dailyFormat)
		if seen[day] {
			continue
		}
		seen[day] = true
		out = append(out, day)
	}
	sort.Strings(out)
	return out
}

// latest returns the most recent of the times, or zero for an empty input.
func latest(in []time.Time) time.Time {
	var out time.Time
	for _, t := range in {
		if t.After(out) {
			out = t
		}
	}
	return out
}
