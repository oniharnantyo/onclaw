package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// The lexical inverted index over one agent's memory directory (add-agent-memory
// design D5): note frontmatter and bodies, daily notes, and the index file are
// tokenized into an in-memory term set per document, scored by term overlap
// weighted by inverse document frequency, with a small boost for matches in a
// note's name, description, or concepts. No FTS5, no embeddings, and no model
// call — a few hundred small files index in microseconds.
//
// Rebuild policy: a Search refreshes the index only when some .md file's mtime
// changed since the last build (or the file set changed); otherwise the built
// state is reused. The sweep owns the long-lived per-agent instance; short-lived
// callers get the same correctness from a fresh Index per call.

// Search limits and scoring constants (design D5).
const (
	// DefaultSearchLimit bounds a search with no explicit limit.
	DefaultSearchLimit = 8
	// MaxSearchLimit caps an explicit limit so one query cannot flood a turn.
	MaxSearchLimit = 25
	// excerptLen bounds a matched excerpt in characters.
	excerptLen = 240

	// The small field boosts (design D5): a term matched in the note's name,
	// description, or concepts weighs more than one matched in the body.
	nameBoost        = 0.50
	descriptionBoost = 0.30
	conceptBoost     = 0.30
)

// SearchHit is one ranked search result: the note's name, description, path
// (workspace-relative), match score, and a matched excerpt.
type SearchHit struct {
	Name        string
	Description string
	Path        string
	Score       float64
	Excerpt     string
}

// indexDoc is one indexed memory file: its term set plus the boosted field
// subsets and the text excerpts are drawn from.
type indexDoc struct {
	name     string
	desc     string
	path     string // workspace-relative, e.g. memory/dark-mode.md
	terms    map[string]bool
	nameT    map[string]bool // terms from the note's name
	descT    map[string]bool // terms from the note's description
	conceptT map[string]bool // terms from the note's concepts
	text     string          // the body text a matched excerpt comes from
}

// Index is the lexical search index over one workspace's memory directory.
// It is safe for concurrent use.
type Index struct {
	layout Layout

	mu      sync.Mutex
	mtimes  map[string]time.Time // absolute path -> mtime at last build
	docs    []*indexDoc
	df      map[string]int // term -> document frequency
	rebuild int            // times the index rebuilt; a test observation
}

// NewIndex returns an empty (unbuilt) index over the layout's memory directory.
// The first Search builds it.
func NewIndex(layout Layout) *Index {
	return &Index{layout: layout}
}

// rebuilds reports how many times the index rebuilt; a test observation.
func (ix *Index) rebuilds() int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.rebuild
}

// Search runs one lexical query and returns the ranked hits, best first. A
// missing memory directory is an empty result, not an error (fail-soft: memory
// that does not exist yet is memory with nothing in it); an unreadable one is
// an error the caller surfaces as a readable result.
func (ix *Index) Search(_ context.Context, query string, limit int) ([]SearchHit, error) {
	if err := ix.refresh(); err != nil {
		return nil, err
	}
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	n := len(ix.docs)
	type scored struct {
		doc   *indexDoc
		score float64
	}
	var matches []scored
	for _, doc := range ix.docs {
		score := 0.0
		for _, t := range terms {
			if !doc.terms[t] {
				continue
			}
			idf := math.Log(1 + float64(n)/float64(ix.df[t]))
			weight := 1.0
			switch {
			case doc.nameT[t]:
				weight = 1 + nameBoost
			case doc.descT[t]:
				weight = 1 + descriptionBoost
			case doc.conceptT[t]:
				weight = 1 + conceptBoost
			}
			score += idf * weight
		}
		if score > 0 {
			matches = append(matches, scored{doc: doc, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].doc.path < matches[j].doc.path
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}

	hits := make([]SearchHit, 0, len(matches))
	for _, m := range matches {
		hits = append(hits, SearchHit{
			Name:        m.doc.name,
			Description: m.doc.desc,
			Path:        m.doc.path,
			Score:       m.score,
			Excerpt:     excerpt(m.doc, terms),
		})
	}
	return hits, nil
}

// refresh rebuilds the index when any .md file's mtime changed since the last
// build (or the file set changed); otherwise it reuses the built state.
func (ix *Index) refresh() error {
	entries, err := os.ReadDir(ix.layout.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		// No memory directory yet: an empty index, not a failure (spec
		// agent-memory: a missing memory directory never fails a turn).
		ix.mu.Lock()
		ix.reset()
		ix.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("memory: read %s: %w", ix.layout.Dir, err)
	}

	mtimes := make(map[string]time.Time, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue // the archive subtree and non-markdown files are not indexed
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("memory: stat %s: %w", filepath.Join(ix.layout.Dir, entry.Name()), err)
		}
		mtimes[filepath.Join(ix.layout.Dir, entry.Name())] = info.ModTime()
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.sameAs(mtimes) {
		return nil // reuse
	}
	ix.mtimes = mtimes
	ix.rebuild++
	return ix.rebuildLocked()
}

// sameAs reports whether the built mtimes equal the scanned ones exactly —
// same file set, same mtimes.
func (ix *Index) sameAs(mtimes map[string]time.Time) bool {
	if ix.mtimes == nil || len(ix.mtimes) != len(mtimes) {
		return false
	}
	for path, mtime := range mtimes {
		if have, ok := ix.mtimes[path]; !ok || !have.Equal(mtime) {
			return false
		}
	}
	return true
}

// reset clears the built state; the caller holds ix.mu.
func (ix *Index) reset() {
	ix.mtimes = nil
	ix.docs = nil
	ix.df = nil
}

// rebuildLocked reads every memory file and rebuilds the term sets; the caller
// holds ix.mu and has set ix.mtimes. One unreadable file is skipped with a
// logged reason — like a malformed note, it must not fail the search.
func (ix *Index) rebuildLocked() error {
	ix.docs = nil
	ix.df = make(map[string]int)
	for path := range ix.mtimes {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // unreadable file: skipped, the rest still searches
		}
		doc := ix.buildDoc(path, raw)
		if doc == nil {
			continue
		}
		ix.docs = append(ix.docs, doc)
		for t := range doc.terms {
			ix.df[t]++
		}
	}
	// Stable ranking input: documents in path order.
	sort.Slice(ix.docs, func(i, j int) bool { return ix.docs[i].path < ix.docs[j].path })
	return nil
}

// buildDoc indexes one memory file: MEMORY.md and daily notes as raw markdown,
// fact notes through their parsed frontmatter and body. A note whose
// frontmatter cannot be parsed is skipped (ParseNote logs the reason; spec
// agent-memory: a malformed note is skipped, never fatal). The result is nil
// for a skipped file.
func (ix *Index) buildDoc(path string, raw []byte) *indexDoc {
	base := filepath.Base(path)
	rel := filepath.ToSlash(filepath.Join(DirName, base))
	name := strings.TrimSuffix(base, ".md")
	switch {
	case base == IndexFileName:
		return rawDoc(name, rel, string(raw))
	case isDailyName(name):
		return rawDoc(name, rel, string(raw))
	default:
		note, ok := ParseNote(path, raw)
		if !ok {
			return nil // malformed note: skipped with a logged reason
		}
		return noteDoc(note, rel)
	}
}

// isDailyName reports whether name is a YYYY-MM-DD daily-note stem.
func isDailyName(name string) bool {
	_, err := time.Parse(dailyFormat, name)
	return err == nil
}

// rawDoc indexes a frontmatter-less file (the index, a daily note): one term
// set over the whole content.
func rawDoc(name, path, text string) *indexDoc {
	terms := termSet(Tokenize(text))
	return &indexDoc{
		name: name, path: path, terms: terms,
		nameT: map[string]bool{}, descT: map[string]bool{}, conceptT: map[string]bool{},
		text: text,
	}
}

// noteDoc indexes a fact note: name, description, and concepts carry their
// boosted sets, the body and links the general one; the excerpt comes from the
// body, falling back to the description for notes matched on their frontmatter
// alone.
func noteDoc(note Note, rel string) *indexDoc {
	doc := &indexDoc{
		name: note.Name, desc: note.Description, path: rel,
		terms:    map[string]bool{},
		nameT:    termSet(Tokenize(note.Name)),
		descT:    termSet(Tokenize(note.Description)),
		conceptT: map[string]bool{},
		text:     note.Body,
	}
	add := func(text string) {
		for _, t := range Tokenize(text) {
			doc.terms[t] = true
		}
	}
	for _, c := range note.Concepts {
		for _, t := range Tokenize(c) {
			doc.terms[t] = true
			doc.conceptT[t] = true
		}
	}
	add(note.Body)
	for _, l := range note.Links {
		add(l)
	}
	for _, r := range note.Relations {
		add(r.Predicate)
		add(r.Target)
	}
	for _, s := range note.Supersedes {
		add(s)
	}
	// The boosted field terms search like any other term.
	for _, set := range []map[string]bool{doc.nameT, doc.descT, doc.conceptT} {
		for t := range set {
			doc.terms[t] = true
		}
	}
	return doc
}

// excerpt renders the matched excerpt for one document: the first line
// carrying a query term, trimmed to the excerpt bound; the description when
// only frontmatter matched.
func excerpt(doc *indexDoc, terms []string) string {
	lower := strings.ToLower(doc.text)
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 {
			return capExcerpt(lineAround(doc.text, i))
		}
	}
	if doc.desc != "" {
		return capExcerpt(doc.desc)
	}
	return capExcerpt(doc.name)
}

// lineAround returns the line containing the byte offset i, trimmed.
func lineAround(text string, i int) string {
	start := strings.LastIndexByte(text[:i], '\n') + 1
	end := strings.IndexByte(text[i:], '\n')
	if end < 0 {
		end = len(text)
	} else {
		end += i
	}
	return strings.TrimSpace(text[start:end])
}

// capExcerpt bounds an excerpt to excerptLen characters with an ellipsis.
func capExcerpt(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= excerptLen {
		return string(runes)
	}
	return string(runes[:excerptLen]) + "…"
}

// Tokenize splits text into search terms (add-agent-memory design D5):
// lowercased, deduplicated in first-appearance order. Backticked spans and
// identifiers — snake_case words and words with an internal capital (camelCase)
// — are kept whole as single terms; other words are their alphanumeric runs.
func Tokenize(text string) []string {
	var terms []string
	seen := make(map[string]bool)
	add := func(term string) {
		if term != "" && !seen[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	for {
		tick := strings.IndexByte(text, '`')
		if tick < 0 {
			tokenizePlain(text, add)
			return terms
		}
		tokenizePlain(text[:tick], add)
		text = text[tick+1:]
		end := strings.IndexByte(text, '`')
		if end < 0 {
			// An unclosed backtick span: the rest is one whole term.
			add(strings.ToLower(strings.TrimSpace(text)))
			return terms
		}
		add(strings.ToLower(strings.TrimSpace(text[:end])))
		text = text[end+1:]
	}
}

// tokenizePlain emits the terms of a backtick-free segment. Words that read as
// identifiers — they carry an underscore or an internal capital (camelCase) —
// stay whole except around separator punctuation (a hyphen between prose words
// splits); other words are their alphanumeric runs.
func tokenizePlain(segment string, add func(string)) {
	for _, word := range strings.Fields(segment) {
		word = strings.Trim(word, ".,;:!?()[]{}\"'-")
		if word == "" {
			continue
		}
		lower := strings.ToLower(word)
		if strings.ContainsRune(word, '_') || hasInternalUpper(word) {
			for _, run := range splitIdentifier(lower) {
				add(run)
			}
			continue
		}
		for _, run := range splitAlphanumeric(lower) {
			add(run)
		}
	}
}

// hasInternalUpper reports whether a word carries a capital letter after its
// first rune — the camelCase signature.
func hasInternalUpper(word string) bool {
	for i, r := range word {
		if i > 0 && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// splitIdentifier splits a lowercased word into runs of letters, digits, and
// underscores — the characters an identifier keeps.
func splitIdentifier(word string) []string {
	var runs []string
	var current strings.Builder
	for _, r := range word {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		runs = append(runs, current.String())
	}
	return runs
}

// splitAlphanumeric splits a lowercased word into its alphanumeric runs.
func splitAlphanumeric(word string) []string {
	var runs []string
	var current strings.Builder
	for _, r := range word {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		runs = append(runs, current.String())
	}
	return runs
}

// termSet builds a term set from Tokenize's output.
func termSet(terms []string) map[string]bool {
	set := make(map[string]bool, len(terms))
	for _, t := range terms {
		set[t] = true
	}
	return set
}
