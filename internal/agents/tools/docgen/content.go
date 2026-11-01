package docgen

import (
	"strings"
)

// blockKind is the minimal markdown construct the DOCX and PPTX engines map
// (design D6): headings, paragraphs, list items, and pipe tables. The
// mapping is deliberately minimal — it is the content carrier for
// generation, not a markdown renderer; the one inline concession is emphasis
// spans (see spanOf), which both engines honor.
type blockKind int

const (
	blockHeading1 blockKind = iota
	blockHeading2
	blockHeading3
	blockParagraph
	blockBullet
	blockNumbered
	blockTable
)

// block is one mapped content block in document order.
type block struct {
	Kind blockKind
	// Level is the list nesting level, 0-based (leading indentation of two
	// spaces per level). Zero for non-list blocks.
	Level int
	Text  string
	// Rows carries a table's parsed cells — the header row first — for
	// blockTable blocks. Text degrades the same table to one readable line
	// for the engines that cannot draw one (PPTX token fills).
	Rows [][]string
}

// isList reports whether the block is a list item.
func (b block) isList() bool { return b.Kind == blockBullet || b.Kind == blockNumbered }

// isNumbered reports whether the block is an ordered list item — the DOCX
// engine's renumbering counter resets when the previous block wasn't one.
func (b block) isNumbered() bool { return b.Kind == blockNumbered }

// parseBlocks maps minimal markdown onto content blocks in document order:
// h1–h3 headings, paragraphs (consecutive plain lines join into one),
// bullet ("-", "*", "+") and numbered ("1.", "1)") list items with
// two-space-per-level nesting, and pipe tables (header row, |---| separator,
// body rows). Other constructs (code fences, images) degrade to their
// literal lines as paragraph text.
func parseBlocks(markdown string) []block {
	var blocks []block
	var para []string // the plain lines accumulating into one paragraph

	flushPara := func() {
		if len(para) > 0 {
			blocks = append(blocks, block{Kind: blockParagraph, Text: strings.Join(para, " ")})
			para = nil
		}
	}

	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			flushPara()
			continue
		}
		if kind, text, ok := headingOf(trimmed); ok {
			flushPara()
			blocks = append(blocks, block{Kind: kind, Text: text})
			continue
		}
		if level, text, ok := bulletOf(lines[i]); ok {
			flushPara()
			blocks = append(blocks, block{Kind: blockBullet, Level: level, Text: text})
			continue
		}
		if level, text, ok := numberedOf(lines[i]); ok {
			flushPara()
			blocks = append(blocks, block{Kind: blockNumbered, Level: level, Text: text})
			continue
		}
		// A table starts at a pipe line whose next line is the |---|
		// separator; the whole region becomes one block.
		if strings.HasPrefix(trimmed, "|") {
			if rows, consumed, ok := tableOf(lines[i:]); ok {
				flushPara()
				blocks = append(blocks, block{Kind: blockTable, Text: tableDigest(rows), Rows: rows})
				i += consumed - 1
				continue
			}
		}
		para = append(para, trimmed)
	}
	flushPara()
	return blocks
}

// tableOf parses one markdown pipe table off the front of lines: a header
// row, the |---|---| separator (columns may carry :alignment: colons), then
// body rows — every line a |…| row until a non-row line ends the region.
// Rows pad or trim to the header's column count; the separator is consumed
// but not stored. consumed is the number of lines the table spans. Escaped
// pipes (\|) are not supported — a cell containing one splits there.
func tableOf(lines []string) (rows [][]string, consumed int, ok bool) {
	if len(lines) < 2 {
		return nil, 0, false
	}
	header := cellsOf(lines[0])
	if len(header) == 0 {
		return nil, 0, false
	}
	if !tableSeparatorOf(lines[1], len(header)) {
		return nil, 0, false
	}
	rows = append(rows, header)
	consumed = 2
	for _, line := range lines[2:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		rows = append(rows, padCells(cellsOf(line), len(header)))
		consumed++
	}
	return rows, consumed, true
}

// cellsOf splits one |…| row into its trimmed cells: the boundary pipes are
// optional, a leading or trailing separator pipe adds no cell.
func cellsOf(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

// padCells fits cells to cols: short rows pad with empty cells, long rows
// trim — a table stays rectangular.
func padCells(cells []string, cols int) []string {
	if len(cells) >= cols {
		return cells[:cols]
	}
	padded := make([]string, cols)
	copy(padded, cells)
	return padded
}

// tableSeparatorOf reports whether line is a markdown table's |---|---|
// delimiter row with the header's column count: nothing but pipes, dashes,
// colons, and blanks, with at least one dash per column.
func tableSeparatorOf(line string, cols int) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	segments := strings.Split(trimmed, "|")
	if len(segments) != cols {
		return false
	}
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return false
		}
		dashes := 0
		for _, ch := range segment {
			switch ch {
			case '-':
				dashes++
			case ':', ' ':
			default:
				return false
			}
		}
		if dashes == 0 {
			return false
		}
	}
	return true
}

// spansText flattens one block's text through the span parser: the inline
// markers come off, the plain text remains — the token-fill degradation.
func spansText(text string) string {
	spans := spansOf(text)
	parts := make([]string, 0, len(spans))
	for _, sp := range spans {
		parts = append(parts, sp.Text)
	}
	return strings.Join(parts, "")
}

// tableDigest degrades a parsed table to one readable line for the engines
// that cannot draw one: cells join with " — ", rows with "; ".
func tableDigest(rows [][]string) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, strings.Join(row, " — "))
	}
	return strings.Join(parts, "; ")
}

// span is one inline run of a block's text: the literal text plus the
// emphasis the markdown asked for.
type span struct {
	Text   string
	Bold   bool
	Italic bool
}

// spansOf splits one block's text into inline spans, honoring **bold**,
// *italic*, and `code` (code renders as its content, the markers coming
// off). An emphasis opener must hug non-space on the inside — the
// CommonMark flanking rule — so arithmetic like "50 * 3 * 4" stays literal;
// an unmatched marker is literal text too. Emphasis does not nest.
func spansOf(text string) []span {
	var spans []span
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			spans = append(spans, span{Text: literal.String()})
			literal.Reset()
		}
	}

	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "**"):
			if end := closeEmphasis(text, i+2, "**"); end > i+2 {
				flush()
				spans = append(spans, span{Text: text[i+2 : end-2], Bold: true})
				i = end
				continue
			}
		case strings.HasPrefix(text[i:], "`"):
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				flush()
				spans = append(spans, span{Text: text[i+1 : i+1+end]})
				i += end + 2
				continue
			}
		case text[i] == '*':
			// Italic opens on a non-space (not "50 * 3") and closes on one
			// immediately before the marker.
			if i+1 < len(text) && text[i+1] != ' ' {
				if end := closeEmphasis(text, i+1, "*"); end > i+1 {
					flush()
					spans = append(spans, span{Text: text[i+1 : end-1], Italic: true})
					i = end
					continue
				}
			}
		}
		literal.WriteByte(text[i])
		i++
	}
	flush()
	return spans
}

// closeEmphasis returns the index just past the closing marker, requiring a
// non-space immediately before it ("bold ** text **" stays literal). -1
// semantics come back as a start-sized value the callers discard.
func closeEmphasis(text string, from int, marker string) int {
	for at := from; at+len(marker) <= len(text); at++ {
		if text[at] == ' ' {
			continue
		}
		if strings.HasPrefix(text[at:], marker) && text[at-1] != ' ' {
			return at + len(marker)
		}
	}
	return from
}

// headingOf maps an ATX heading onto a heading block: one # is h1, two h2,
// three or more degrade to h3 (the mapping's depth).
func headingOf(line string) (blockKind, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	depth := 0
	for depth < len(line) && line[depth] == '#' {
		depth++
	}
	if depth >= len(line) || line[depth] != ' ' {
		return 0, "", false // a bare #### or #hashtag is not a heading
	}
	text := strings.TrimSpace(line[depth+1:])
	switch depth {
	case 1:
		return blockHeading1, text, true
	case 2:
		return blockHeading2, text, true
	default:
		return blockHeading3, text, true
	}
}

// bulletOf maps a bullet list line ("- ", "* ", "+ ") onto a bullet block;
// the nesting level comes from the line's leading whitespace, two spaces
// per level.
func bulletOf(line string) (int, string, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	rest := strings.TrimSpace(line)
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(rest, marker) {
			text := strings.TrimSpace(strings.TrimPrefix(rest, marker))
			return indent / 2, text, true
		}
	}
	return 0, "", false
}

// numberedOf maps an ordered list line ("1. ", "1) ") onto a numbered
// block, with bulletOf's nesting rule. The literal number is dropped — the
// engine renumbers, so a skeleton's lists stay consistent.
func numberedOf(line string) (int, string, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	rest := strings.TrimSpace(line)
	dot := strings.IndexAny(rest, ".)")
	if dot <= 0 || dot > 4 { // "1." through "9999." — not a sentence's first period
		return 0, "", false
	}
	for _, ch := range rest[:dot] {
		if ch < '0' || ch > '9' {
			return 0, "", false
		}
	}
	after := rest[dot+1:]
	if !strings.HasPrefix(after, " ") {
		return 0, "", false
	}
	return indent / 2, strings.TrimSpace(after), true
}
