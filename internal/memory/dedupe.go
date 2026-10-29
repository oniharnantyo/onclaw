package memory

import (
	"strings"
	"unicode"
)

// DuplicateJaccard is the merge threshold (add-agent-memory design D11):
// two candidates whose token-set Jaccard similarity reaches it are
// near-duplicates and merge into one candidate before scoring.
const DuplicateJaccard = 0.9

// dedupeTokens splits text into its lowercase token set: runs of letters and
// digits are tokens, every other rune separates. Punctuation and case never
// affect similarity — two lines differing only there are duplicates.
func dedupeTokens(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens[strings.ToLower(b.String())] = struct{}{}
			b.Reset()
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// tokenSetJaccard returns the Jaccard similarity of the two texts' token
// sets — |A∩B| / |A∪B| — in [0,1]. Two empty texts are identical (1).
func tokenSetJaccard(a, b string) float64 {
	as, bs := dedupeTokens(a), dedupeTokens(b)
	if len(as) == 0 && len(bs) == 0 {
		return 1
	}
	small, large := as, bs
	if len(small) > len(large) {
		small, large = large, small
	}
	inter := 0
	for tok := range small {
		if _, ok := large[tok]; ok {
			inter++
		}
	}
	union := len(as) + len(bs) - inter
	return float64(inter) / float64(union)
}

// mergeDuplicateCandidates merges near-duplicate candidates (token-set
// Jaccard at least DuplicateJaccard) into one candidate before scoring
// (design D11). The earliest candidate in the input order is the survivor —
// its line and source reference stand — and the duplicates' recall evidence
// folds into it, so nothing the traces saw is lost. The input order must be
// stable (earliest daily note first) for the result to be deterministic.
func mergeDuplicateCandidates(cands []LineCandidate) []LineCandidate {
	out := make([]LineCandidate, 0, len(cands))
	for _, c := range cands {
		merged := false
		for i := range out {
			if tokenSetJaccard(out[i].Line, c.Line) >= DuplicateJaccard {
				out[i].Evidence = concatEvidence(out[i].Evidence, c.Evidence)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, c)
		}
	}
	return out
}

// concatEvidence appends one evidence onto another, preserving order.
func concatEvidence(base, extra RecallEvidence) RecallEvidence {
	base.Scores = append(base.Scores, extra.Scores...)
	base.Queries = append(base.Queries, extra.Queries...)
	base.Ats = append(base.Ats, extra.Ats...)
	return base
}
