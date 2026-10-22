package hooks

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Package-level caps shared by the matcher, the handlers, and the save-time
// validation the services layer adds.
const (
	// DefaultTimeoutMS bounds a hook delivery when the definition sets no
	// timeout of its own.
	DefaultTimeoutMS = 5000
	// MaxTimeoutMS caps a definition's configured timeout.
	MaxTimeoutMS = 60000
	// MaxPatternLength caps regex-tier matcher strings.
	MaxPatternLength = 256
	// MaxScriptBytes caps a script hook's JavaScript source.
	MaxScriptBytes = 65536
)

// Matcher selects which occurrences of an event a hook applies to. Values
// are opaque: tool names on tool events, the origin on run-start/prompt
// events, the terminal status on run_finished. CompileMatcher compiles a
// hook's stored matcher string once per run; dispatch then calls Matches
// before any handler execution, so a non-matching hook costs nothing.
type Matcher interface {
	Matches(value string) bool
}

// hookToolEntryPattern is the tool-entry grammar: charset [A-Za-z0-9_.-]
// with at most one trailing ".*" wildcard. It doubles as the tier boundary:
// when EVERY list entry fits, the matcher is a list; any other character
// puts the WHOLE string into the regex tier.
var hookToolEntryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(\.\*)?$`)

// CompileMatcher compiles the stored matcher string into a Matcher using
// Claude Code's tiered interpretation, adapted for dotted tool names:
//
//   - "" or "*" matches everything.
//   - LIST tier: the string splits on ",", "|", and whitespace; if EVERY
//     entry fits the tool-entry grammar, entries match exactly or by
//     trailing-".*" family ("browser.*" selects browser itself plus every
//     browser.* descendant; "mcp__github.*" selects an MCP server's tools;
//     dots inside names are literal, so "web_search" stays exact).
//   - REGEX tier: otherwise the WHOLE unsplit string compiles as an
//     unanchored RE2 regex, capped at MaxPatternLength.
//
// An over-long or un-compilable regex returns an error — the dispatcher
// skips such hooks gracefully (warning logged, on_failure policy applies),
// never failing the run over a definition it cannot interpret.
func CompileMatcher(matcher string) (Matcher, error) {
	if matcher == "" || matcher == "*" {
		return matchAllMatcher{}, nil
	}
	entries := splitMatcherEntries(matcher)
	list := make([]toolEntry, 0, len(entries))
	for _, entry := range entries {
		if !hookToolEntryPattern.MatchString(entry) {
			// REGEX tier: one entry outside the tool-entry grammar switches
			// the WHOLE unsplit string to the regex reading.
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

// splitMatcherEntries splits a matcher into its list-tier entries on ",",
// "|", and whitespace, collapsing repeated separators. A string of only
// separators splits to zero entries, which the list tier reads as selecting
// nothing.
func splitMatcherEntries(matcher string) []string {
	return strings.FieldsFunc(matcher, func(r rune) bool {
		return r == ',' || r == '|' || unicode.IsSpace(r)
	})
}

// compileRegexMatcher compiles the regex tier: the whole matcher string as
// an unanchored RE2 regex within MaxPatternLength.
func compileRegexMatcher(matcher string) (Matcher, error) {
	if len(matcher) > MaxPatternLength {
		return nil, fmt.Errorf("%w: matcher: exceeds maximum length of %d", ErrInvalid, MaxPatternLength)
	}
	re, err := regexp.Compile(matcher)
	if err != nil {
		return nil, fmt.Errorf("%w: matcher: %v", ErrInvalid, err)
	}
	return regexMatcher{re: re}, nil
}

// toolEntry is one compiled list entry: an exact name or a prefix family.
type toolEntry struct {
	exact  string
	prefix string // non-empty when the entry ends in ".*"
}

func compileToolEntry(entry string) (toolEntry, error) {
	if !hookToolEntryPattern.MatchString(entry) {
		return toolEntry{}, fmt.Errorf("%w: entry %q must match [A-Za-z0-9_.-]+ with at most one trailing \".*\" wildcard", ErrInvalid, entry)
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

// MatchedValue is the event-aware value a matcher compares: the tool name on
// tool events, the origin on run-start/prompt events, the terminal status on
// run_finished. One matcher shape serves every event because the compared
// value changes with the event.
func MatchedValue(ev Event) string {
	switch ev.Event {
	case EventPreToolUse, EventPostToolUse:
		if ev.Tool != nil {
			return ev.Tool.Name
		}
		return ""
	case EventRunFinished:
		return ev.Status
	case EventRunStarted, EventUserPromptSubmit:
		return ev.Origin
	default:
		return ""
	}
}
