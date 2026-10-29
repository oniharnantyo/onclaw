// Package markdown converts GFM-ish markdown to Telegram MarkdownV2, renders
// a stripped plain-text fallback, and chunks converted text within Telegram's
// UTF-16 message limit.
//
// The behavior ports the documented rules of Hermes' Telegram adapter:
// code regions are stashed and escaped per the MarkdownV2 code-span rules,
// markdown constructs become MarkdownV2 syntax, tables are flattened to
// bullet groups, and every remaining reserved character outside a protected
// region is backslash-escaped. A Telegram parse rejection is answered by
// resending PlainText of the same content.
package markdown

import (
	"regexp"
	"strconv"
	"strings"
)

// escapeRe matches every character MarkdownV2 requires to be
// backslash-escaped when it appears as literal text outside a code span or
// fence.
var escapeRe = regexp.MustCompile("([_*\\[\\]()~`>#\\+=|{}.!\\\\-])")

// escapeMDV2 backslash-escapes every MarkdownV2-reserved character in s.
func escapeMDV2(s string) string {
	return escapeRe.ReplaceAllString(s, "\\$1")
}

// placeholderSet stashes verbatim segments behind NUL-delimited tokens that
// survive the global escape pass. Segments are restored newest-first so
// nested stashes (e.g. inline code inside a link) resolve innermost-last.
type placeholderSet struct {
	keys []string
	vals map[string]string
	n    int
}

func newPlaceholderSet() *placeholderSet {
	return &placeholderSet{vals: make(map[string]string)}
}

func (p *placeholderSet) stash(v string) string {
	k := "\x00PH" + strconv.Itoa(p.n) + "\x00"
	p.n++
	p.keys = append(p.keys, k)
	p.vals[k] = v
	return k
}

func (p *placeholderSet) restore(text string) string {
	for i := len(p.keys) - 1; i >= 0; i-- {
		text = strings.ReplaceAll(text, p.keys[i], p.vals[p.keys[i]])
	}
	return text
}

var (
	fencedRe = regexp.MustCompile("```(?:[^\n]*\n)?[\\s\\S]*?```")
	inlineRe = regexp.MustCompile("`[^`]+`")
	linkRe   = regexp.MustCompile(`\[([^\]]+)\]\(([^()]*(?:\([^()]*\)[^()]*)*)\)`)
	headerRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe = regexp.MustCompile(`\*([^*\n]+)\*`)
	strikeRe = regexp.MustCompile(`~~(.+?)~~`)
	spoilRe  = regexp.MustCompile(`\|\|(.+?)\|\|`)
	quoteRe  = regexp.MustCompile(`(?m)^((?:\*\*)?>{1,3}) (.+)$`)

	// codeRegionRe matches fenced blocks and inline code spans; only text
	// between such regions goes through the bare-character safety net.
	codeRegionRe = regexp.MustCompile("```[\\s\\S]*?```|`[^`]+`")
	// bareRe matches the characters the safety net still escapes after the
	// stashed segments are restored: stray ( ) { } ' outside code regions.
	bareRe = regexp.MustCompile("[(){}']")

	tableSepRe = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)+\|?\s*$`)
)

// ToMarkdownV2 converts GFM-ish markdown (fenced code, inline code, bold,
// italic, strikethrough, spoiler, blockquotes, headings, links and pipe
// tables) to Telegram MarkdownV2.
//
// Code region contents are escaped per the MarkdownV2 code-span rules (only
// backslash and backtick); links keep the [text](url) shape with the URL
// escaped for ')' and '\'; tables are flattened to bold-heading bullet
// groups; headings become a bold line; every remaining reserved character
// outside a protected region is backslash-escaped.
func ToMarkdownV2(src string) string {
	if src == "" {
		return src
	}
	text := convertTables(src)
	ph := newPlaceholderSet()

	// Fenced code blocks: keep the info-string line, escape the body per the
	// MarkdownV2 code-span rules (backslash first, then backtick).
	text = fencedRe.ReplaceAllStringFunc(text, func(m string) string {
		openEnd := 3
		if i := strings.Index(m[3:], "\n"); i >= 0 {
			openEnd = 3 + i + 1
		}
		body := m[openEnd : len(m)-3]
		body = strings.ReplaceAll(body, "\\", "\\\\")
		body = strings.ReplaceAll(body, "`", "\\`")
		return ph.stash(m[:openEnd] + body + "```")
	})

	// Inline code spans: content preserved, backslash escaped.
	text = inlineRe.ReplaceAllStringFunc(text, func(m string) string {
		return ph.stash(strings.ReplaceAll(m, "\\", "\\\\"))
	})

	// Links: escape the display text fully; inside the URL only ')' and '\'
	// need escaping.
	text = linkRe.ReplaceAllStringFunc(text, func(m string) string {
		g := linkRe.FindStringSubmatch(m)
		url := strings.ReplaceAll(g[2], "\\", "\\\\")
		url = strings.ReplaceAll(url, ")", "\\)")
		return ph.stash("[" + escapeMDV2(g[1]) + "](" + url + ")")
	})

	// Headings become a bold line; redundant bold inside the heading is
	// stripped first.
	text = headerRe.ReplaceAllStringFunc(text, func(m string) string {
		g := headerRe.FindStringSubmatch(m)
		inner := strings.TrimSpace(boldRe.ReplaceAllString(g[1], "$1"))
		return ph.stash("*" + escapeMDV2(inner) + "*")
	})

	// Bold **x** -> *x*; italic *x* -> _x_ (single-line, so bullet-list '*'
	// markers survive); strikethrough ~~x~~ -> ~x~; spoiler ||x|| kept.
	wrapStash := func(re *regexp.Regexp, open, close string) {
		text = re.ReplaceAllStringFunc(text, func(m string) string {
			g := re.FindStringSubmatch(m)
			return ph.stash(open + escapeMDV2(g[1]) + close)
		})
	}
	wrapStash(boldRe, "*", "*")
	wrapStash(italicRe, "_", "_")
	text = convertUnderscoreItalic(text, ph)
	wrapStash(strikeRe, "~", "~")
	wrapStash(spoilRe, "||", "||")

	// Blockquotes: protect the leading '>' (and the expandable '**>' … '||'
	// form) from escaping; the content is escaped.
	text = quoteRe.ReplaceAllStringFunc(text, func(m string) string {
		g := quoteRe.FindStringSubmatch(m)
		prefix, content := g[1], g[2]
		if strings.HasPrefix(prefix, "**") && strings.HasSuffix(content, "||") {
			return ph.stash(prefix + " " + escapeMDV2(strings.TrimSuffix(content, "||")) + "||")
		}
		return ph.stash(prefix + " " + escapeMDV2(content))
	})

	// Escape every remaining reserved character in the plain text.
	text = escapeMDV2(text)

	// Restore stashed segments newest-first so nested placeholders resolve.
	text = ph.restore(text)

	// Safety net: escape bare ( ) { } ' that survived outside code regions
	// (e.g. reintroduced inside link URLs), never touching code spans.
	return escapeBare(text)
}

// convertUnderscoreItalic stashes GFM _italic_ spans as MarkdownV2 _italic_.
// The span must be word-bounded, so snake_case stays literal text and is
// escaped by the general pass instead of being misread as emphasis.
func convertUnderscoreItalic(text string, ph *placeholderSet) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '_' || (i > 0 && isWordByte(text[i-1])) {
			b.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i+1:], '_')
		if end < 0 {
			b.WriteByte(text[i])
			i++
			continue
		}
		end += i + 1
		if end+1 < len(text) && isWordByte(text[end+1]) {
			b.WriteByte(text[i])
			i++
			continue
		}
		b.WriteString(ph.stash("_" + escapeMDV2(text[i+1:end]) + "_"))
		i = end + 1
	}
	return b.String()
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// escapeBare applies the bare-character safety net to everything outside the
// code regions of text.
func escapeBare(text string) string {
	var b strings.Builder
	pos := 0
	for _, loc := range codeRegionRe.FindAllStringIndex(text, -1) {
		b.WriteString(escapeBareSegment(text[pos:loc[0]]))
		b.WriteString(text[loc[0]:loc[1]])
		pos = loc[1]
	}
	b.WriteString(escapeBareSegment(text[pos:]))
	return b.String()
}

// escapeBareSegment backslash-escapes stray ( ) { } ' unless already escaped
// or delimiting a [text](url) link.
func escapeBareSegment(seg string) string {
	var b strings.Builder
	last := 0
	for _, loc := range bareRe.FindAllStringIndex(seg, -1) {
		b.WriteString(seg[last:loc[0]])
		last = loc[1]
		i, ch := loc[0], seg[loc[0]]
		switch {
		case i > 0 && seg[i-1] == '\\':
			b.WriteByte(ch) // already escaped
		case ch == '(' && i > 0 && seg[i-1] == ']':
			b.WriteByte(ch) // opens a [text](url) link
		case ch == ')':
			if closesLinkURL(seg, i) {
				b.WriteByte(ch)
			} else {
				b.WriteByte('\\')
				b.WriteByte(ch)
			}
		default:
			b.WriteByte('\\')
			b.WriteByte(ch)
		}
	}
	b.WriteString(seg[last:])
	return b.String()
}

// closesLinkURL reports whether the ')' at index i closes a [text](url) link:
// walk back to the matching '(' and check it is preceded by ']'.
func closesLinkURL(seg string, i int) bool {
	if !strings.Contains(seg[:i], "](") {
		return false
	}
	depth := 0
	for j := i - 1; j >= 0 && j > i-2000; j-- {
		switch seg[j] {
		case '(':
			depth--
			if depth < 0 {
				return j > 0 && seg[j-1] == ']'
			}
		case ')':
			depth++
		}
	}
	return false
}

// convertTables rewrites GFM pipe tables into bold-heading + bullet groups
// (MarkdownV2 has no table syntax). The separator row is dropped and fenced
// code is left alone.
func convertTables(text string) string {
	if !strings.Contains(text, "|") || !strings.Contains(text, "-") {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for i := 0; i < len(lines); {
		line := lines[i]
		isFenceLine := strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
		if isFenceLine {
			inFence = !inFence
		}
		if !inFence && !isFenceLine && strings.Contains(line, "|") &&
			i+1 < len(lines) && tableSepRe.MatchString(lines[i+1]) {
			j := i + 2
			for j < len(lines) && isTableRow(lines[j]) {
				j++
			}
			out = append(out, renderTableBlock(lines[i:j]))
			i = j
			continue
		}
		out = append(out, line)
		i++
	}
	return strings.Join(out, "\n")
}

// renderTableBlock renders one GFM table (header row, separator row, data
// rows) as bold-heading + bullet groups, mirroring Telegram's own table
// renderer: when the first data row has one more cell than the header the
// leading column is a row label; otherwise the first non-empty cell of each
// row becomes the heading and is not repeated as a bullet.
func renderTableBlock(block []string) string {
	var headers []string
	if len(block) >= 3 {
		headers = splitTableRow(block[0])
	}
	if len(headers) < 2 {
		return strings.Join(block, "\n")
	}
	hasRowLabelCol := len(splitTableRow(block[2])) == len(headers)+1
	groups := make([]string, 0, len(block)-2)
	for idx, row := range block[2:] {
		cells := splitTableRow(row)
		var heading string
		var data []string
		if hasRowLabelCol {
			if len(cells) > 0 && cells[0] != "" {
				heading = cells[0]
			} else {
				heading = "Row " + strconv.Itoa(idx+1)
			}
			if len(cells) > 1 {
				data = cells[1:]
			}
		} else {
			heading = "Row " + strconv.Itoa(idx+1)
			for _, c := range cells {
				if c != "" {
					heading = c
					break
				}
			}
			data = cells
		}
		lines := []string{"**" + heading + "**"}
		for ci, h := range headers {
			v := ""
			if ci < len(data) {
				v = data[ci]
			}
			if hasRowLabelCol || v != heading {
				lines = append(lines, "• "+h+": "+v)
			}
		}
		groups = append(groups, strings.Join(lines, "\n"))
	}
	return strings.Join(groups, "\n\n")
}

// splitTableRow splits a GFM table row into trimmed cells; leading and
// trailing pipes are optional.
func splitTableRow(row string) []string {
	r := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(row), "|"), "|")
	parts := strings.Split(r, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// isTableRow reports whether line could plausibly be a table data row.
func isTableRow(line string) bool {
	return strings.Contains(strings.TrimSpace(line), "|")
}

var (
	unescapeRe        = regexp.MustCompile("\\\\([_*\\[\\]()~`>#\\+=|{}.!\\\\-])")
	boldStripRe       = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	starItalicStripRe = regexp.MustCompile(`\*([^*]+)\*`)
	strikeStripRe     = regexp.MustCompile(`~([^~]+)~`)
	spoilStripRe      = regexp.MustCompile(`\|\|([^|]+)\|\|`)
	fenceLineRe       = regexp.MustCompile("(?m)^```[^\\n]*\\n?")
)

// PlainText renders src as readable plain text with no MarkdownV2 markup.
// src is expected to be MarkdownV2-formatted text (the output of ToMarkdownV2
// or a converted chunk, as resent after a Telegram parse rejection) but raw
// markdown is tolerated. Escapes are removed, formatting markers are
// stripped, links become "text (url)", fence lines are dropped while the
// code content stays as plain lines, and inline backticks are removed.
func PlainText(src string) string {
	if src == "" {
		return src
	}
	s := unescapeRe.ReplaceAllString(src, "$1")
	s = linkRe.ReplaceAllString(s, "$1 ($2)")
	s = boldStripRe.ReplaceAllString(s, "$1") // **bold** before *italic*
	s = starItalicStripRe.ReplaceAllString(s, "$1")
	s = stripUnderscoreItalic(s) // word-bounded, so snake_case survives
	s = strikeStripRe.ReplaceAllString(s, "$1")
	s = spoilStripRe.ReplaceAllString(s, "$1")
	s = fenceLineRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

// stripUnderscoreItalic removes _italic_ markers, word-bounded so
// snake_case survives.
func stripUnderscoreItalic(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '_' || (i > 0 && isWordByte(s[i-1])) {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i+1:], '_')
		if end < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		end += i + 1
		if end+1 < len(s) && isWordByte(s[end+1]) {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString(s[i+1 : end])
		i = end + 1
	}
	return b.String()
}
