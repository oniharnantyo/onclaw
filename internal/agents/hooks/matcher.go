package hooks

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Matcher selects which occurrences of an event a hook applies to (D19).
// Values are opaque: tool names on tool events, origins on run-start/prompt
// events, statuses on run_finished. CompileMatcher compiles a hook's stored
// matcher string once per run; dispatch then calls Matches before any handler
// execution, so a non-matching hook costs nothing.
type Matcher interface {
	Matches(value string) bool
}

// hookToolEntryPattern is the tool-entry grammar (same shape as the domain
// save-time rule): charset [A-Za-z0-9_.-] with at most one trailing ".*"
// wildcard. It doubles as the D19 tier boundary: when EVERY list entry fits,
// the matcher is a list; any other character puts the WHOLE string into the
// regex tier. Re-declared here so the runtime's tier reading (D7: a stored
// matcher an older binary cannot interpret must error, not misbehave) does
// not depend on the domain package's unexported state.
var hookToolEntryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(\.\*)?$`)

// CompileMatcher compiles the stored matcher string into a Matcher using
// Claude Code's tiered interpretation (D19), adapted for dotted tool names:
//
//   - "" or "*" matches everything.
//   - LIST tier: the string splits on ",", "|", and whitespace; if EVERY
//     entry fits the tool-entry grammar, entries match exactly or by
//     trailing-".*" family ("browser.*" selects browser itself plus every
//     browser.* descendant; "mcp__github.*" selects an MCP server's tools;
//     dots inside names are literal, so "web.search" stays exact).
//   - REGEX tier: otherwise the WHOLE unsplit string compiles as an
//     unanchored RE2 regex, capped at domain.MaxHookPatternLength.
//
// An over-long or un-compilable regex returns an error — the dispatcher
// skips such hooks gracefully (D7: warning + error status), never failing
// the run over a definition it cannot interpret.
func CompileMatcher(matcher string) (Matcher, error) {
	if matcher == "" || matcher == "*" {
		return matchAllMatcher{}, nil
	}
	entries := splitHookMatcherEntries(matcher)
	list := make([]toolEntry, 0, len(entries))
	for _, entry := range entries {
		if !hookToolEntryPattern.MatchString(entry) {
			// REGEX tier: one entry outside the tool-entry grammar switches
			// the WHOLE unsplit string to the regex reading (D19).
			return compileRegexMatcher(matcher)
		}
		compiled, err := compileToolEntry(entry)
		if err != nil {
			return nil, err // unreachable: the grammar check above passed
		}
		list = append(list, compiled)
	}
	return listMatcher{entries: list}, nil
}

// splitHookMatcherEntries splits a matcher into its list-tier entries on
// ",", "|", and whitespace, collapsing repeated separators (Claude Code's
// list syntax). A string of only separators splits to zero entries, which
// the list tier reads as selecting nothing.
func splitHookMatcherEntries(matcher string) []string {
	return strings.FieldsFunc(matcher, func(r rune) bool {
		return r == ',' || r == '|' || unicode.IsSpace(r)
	})
}

// compileRegexMatcher compiles the regex tier: the whole matcher string as
// an unanchored RE2 regex within domain.MaxHookPatternLength.
func compileRegexMatcher(matcher string) (Matcher, error) {
	if len(matcher) > domain.MaxHookPatternLength {
		return nil, fmt.Errorf("%w: matcher: exceeds maximum length of %d", domain.ErrInvalid, domain.MaxHookPatternLength)
	}
	re, err := regexp.Compile(matcher)
	if err != nil {
		return nil, fmt.Errorf("%w: matcher: %v", domain.ErrInvalid, err)
	}
	return regexMatcher{re: re}, nil
}

// toolEntry is one compiled list entry: an exact name or a prefix family.
type toolEntry struct {
	exact  string
	prefix string // non-empty when the entry ends in ".*"
}

// compileToolEntry compiles one tool-entry string into its exact-or-family
// form. Shared with the D20 if gate's name part, which follows the same
// matcher entry rules.
func compileToolEntry(entry string) (toolEntry, error) {
	if !hookToolEntryPattern.MatchString(entry) {
		return toolEntry{}, fmt.Errorf("%w: entry %q must match [A-Za-z0-9_.-]+ with at most one trailing \".*\" wildcard", domain.ErrInvalid, entry)
	}
	if strings.HasSuffix(entry, ".*") {
		return toolEntry{prefix: strings.TrimSuffix(entry, ".*")}, nil
	}
	return toolEntry{exact: entry}, nil
}

func (e toolEntry) matches(value string) bool {
	if e.prefix != "" {
		return strings.HasPrefix(value, e.prefix)
	}
	return value == e.exact
}

// matchAllMatcher is the ""/"*" matcher: every occurrence applies.
type matchAllMatcher struct{}

func (matchAllMatcher) Matches(string) bool { return true }

// listMatcher is a compiled LIST-tier selection: any entry matching selects
// the occurrence.
type listMatcher struct {
	entries []toolEntry
}

func (m listMatcher) Matches(value string) bool {
	for _, entry := range m.entries {
		if entry.matches(value) {
			return true
		}
	}
	return false
}

// regexMatcher is a compiled RE2 pattern evaluated unanchored: a pattern
// matches when it matches anywhere in the value.
type regexMatcher struct {
	re *regexp.Regexp
}

func (m regexMatcher) Matches(value string) bool {
	return m.re.MatchString(value)
}
