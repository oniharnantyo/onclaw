package suite

import (
	"strings"
)

// normalizeReplacer folds curly apostrophes to straight ones, strips backticks,
// and turns commas/semicolons into spaces. Commas/semicolons become spaces so
// date and list forms match regardless of punctuation ("12 February, 2026" →
// "12 february 2026"). Backticks vanish: the house design contract has the
// model chip file names, tool names, and values in monospace, so probes must
// be markup-blind ("no `.trk` files" must match the token "no .trk").
var normalizeReplacer = strings.NewReplacer("’", "'", ",", " ", ";", " ", "`", "")

// NormalizeText defines the matching semantics for every text probe in this
// package (answer_contains, answer_not_contains, the *_contains probe fields,
// and the unintervention lint): lowercase, curly-apostrophe fold, backtick
// strip, then commas/semicolons to spaces, then whitespace collapse. Periods
// and @ are preserved so emails and abbreviations match verbatim.
//
// Ported verbatim from internal/memory/eval/score.go's normalizeText — the
// bench module cannot import it (design D1), so the semantics live here.
func NormalizeText(s string) string {
	s = strings.ToLower(s)
	s = normalizeReplacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
