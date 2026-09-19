package postgres

import (
	"regexp"
	"strings"
)

// memoryQueryTermCap bounds the shaped tsquery: a long turn text must not
// grow the lexical predicate without limit (the injected-char budget and the
// GIN index both assume a bounded query).
const memoryQueryTermCap = 16

// memoryQueryTermRe splits a lowercased query into terms in one pass.
// Deliberately alphanumeric-only: Postgres's text-search parser turns an
// underscore-joined token like "foo_bar" into a phrase query ('foo' <->
// 'bar'), which would smuggle adjacency semantics into what must stay a
// plain any-term match, and pure-underscore runs are to_tsquery syntax
// hazards. Every tsquery operator character (& | ! ( ) : *) and quoting
// lives outside this class, so the match splits and sanitizes at once.
var memoryQueryTermRe = regexp.MustCompile(`[a-z0-9]+`)

// memoryLexicalQuery is a shaped free-text memory query: the OR-joined
// tsquery string ("t1 | t2 | …") to bind as a single parameter, plus the
// count of surviving terms.
type memoryLexicalQuery struct {
	tsquery   string
	termCount int
}

// shapeMemoryLexicalQuery tokenizes a raw memory query — lowercase, split on
// non-alphanumeric, drop empties, cap at memoryQueryTermCap terms — and
// joins the survivors with "|" for to_tsquery. A query whose terms all
// tokenize away (pure punctuation, say) yields a zero-term shape so callers
// can drop the tsquery leg entirely instead of erroring; the full-string
// ILIKE fallback stays live in that path.
func shapeMemoryLexicalQuery(query string) memoryLexicalQuery {
	terms := memoryQueryTermRe.FindAllString(strings.ToLower(query), -1)
	if len(terms) > memoryQueryTermCap {
		terms = terms[:memoryQueryTermCap]
	}
	return memoryLexicalQuery{tsquery: strings.Join(terms, " | "), termCount: len(terms)}
}
