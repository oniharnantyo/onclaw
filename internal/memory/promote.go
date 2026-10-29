package memory

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LineCandidate is one promotion candidate: a daily-note line the sweep may
// gate into the index's promoted subsection (design D8/D10). Its recall
// evidence accumulates across sweeps in the staging row.
type LineCandidate struct {
	// Line is the daily-note line, verbatim.
	Line string
	// SourceRef is the line's workspace reference, e.g.
	// "memory/2026-10-27.md#L12".
	SourceRef string
	// Evidence is the candidate's user-origin recall evidence.
	Evidence RecallEvidence
	// Concepts counts the candidate's recall handles — the [[wiki-links]]
	// the line carries, since daily lines have no frontmatter.
	Concepts int
}

// sourceRef renders a daily-note line's workspace reference: the
// memory-relative file path plus the one-based line number.
func sourceRef(dailyFile string, lineNo int) string {
	return fmt.Sprintf("memory/%s#L%d", filepath.ToSlash(dailyFile), lineNo)
}

// stagedSignals is one staging row's cumulative recall evidence: every
// user-origin recall the candidate has received since first staging, merged
// across sweeps. It is the Signals JSON document the store persists
// opaquely; only the sweep interprets it. Queries and times keep their
// duplicates — the distinct counts are derived at gate time.
type stagedSignals struct {
	Scores  []float64   `json:"scores"`
	Queries []string    `json:"queries"`
	Ats     []time.Time `json:"ats"`
}

// newStagedSignals seeds the cumulative state with one window's evidence.
func newStagedSignals(ev RecallEvidence) stagedSignals {
	return appendStagedSignals(stagedSignals{}, ev)
}

// appendStagedSignals merges one window's evidence into the cumulative
// state: evidence concatenates, nothing deduplicates — ComputeSignals
// derives the distinct counts itself.
func appendStagedSignals(cur stagedSignals, ev RecallEvidence) stagedSignals {
	cur.Scores = append(cur.Scores, ev.Scores...)
	cur.Queries = append(cur.Queries, ev.Queries...)
	cur.Ats = append(cur.Ats, ev.Ats...)
	return cur
}

// evidence returns the cumulative state in the RecallEvidence shape
// ComputeSignals arithmetic over.
func (s stagedSignals) evidence() RecallEvidence {
	return RecallEvidence{Scores: s.Scores, Queries: s.Queries, Ats: s.Ats}
}

// encode serializes the cumulative state for the staging row.
func (s stagedSignals) encode() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("memory: encode staging signals: %w", err)
	}
	return string(b), nil
}

// decodeStagedSignals parses a staging row's Signals document; an empty
// document is a fresh candidate.
func decodeStagedSignals(raw string) (stagedSignals, error) {
	if strings.TrimSpace(raw) == "" {
		return stagedSignals{}, nil
	}
	var s stagedSignals
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return stagedSignals{}, fmt.Errorf("memory: decode staging signals: %w", err)
	}
	return s, nil
}

// selectPromotions applies the three gates to each candidate's cumulative
// signals, drops candidates already promoted (their source reference is in
// the index's promoted subsection or their staging row is flagged), orders
// the survivors by score descending (source reference ascending on ties) and
// caps the result at MaxPromotionsPerSweep (design D8).
func selectPromotions(cands []LineCandidate, signals []Signals, alreadyPromoted map[string]bool) []LineCandidate {
	type scored struct {
		cand LineCandidate
		sig  Signals
	}
	var passing []scored
	for i, c := range cands {
		if alreadyPromoted[c.SourceRef] {
			continue
		}
		if !signals[i].PassesGates() {
			continue
		}
		passing = append(passing, scored{cand: c, sig: signals[i]})
	}
	sort.SliceStable(passing, func(i, j int) bool {
		if passing[i].sig.Score != passing[j].sig.Score {
			return passing[i].sig.Score > passing[j].sig.Score
		}
		return passing[i].cand.SourceRef < passing[j].cand.SourceRef
	})
	if len(passing) > MaxPromotionsPerSweep {
		passing = passing[:MaxPromotionsPerSweep]
	}
	out := make([]LineCandidate, len(passing))
	for i, p := range passing {
		out[i] = p.cand
	}
	return out
}

// promotedLine converts a promoted candidate into its index entry: the line
// verbatim, its source reference, and the promotion date (design D10).
func promotedLine(c LineCandidate, promotedOn time.Time) PromotedLine {
	return PromotedLine{
		Line:       c.Line,
		SourceRef:  c.SourceRef,
		PromotedOn: promotedOn.UTC(),
	}
}
