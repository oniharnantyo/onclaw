package docgen

import (
	"strings"
)

// blockKind is the minimal markdown construct the DOCX and PPTX engines map
// (design D6): headings, paragraphs, and list items. The mapping is
// deliberately minimal — it is the content carrier for generation, not a
// markdown renderer; inline markup travels as literal text.
type blockKind int

const (
	blockHeading1 blockKind = iota
	blockHeading2
	blockHeading3
	blockParagraph
	blockBullet
	blockNumbered
)

// block is one mapped content block in document order.
type block struct {
	Kind blockKind
	// Level is the list nesting level, 0-based (leading indentation of two
	// spaces per level). Zero for non-list blocks.
	Level int
	Text  string
}

// isList reports whether the block is a list item.
func (b block) isList() bool { return b.Kind == blockBullet || b.Kind == blockNumbered }

// isNumbered reports whether the block is an ordered list item — the DOCX
// engine's renumbering counter resets when the previous block wasn't one.
func (b block) isNumbered() bool { return b.Kind == blockNumbered }

// parseBlocks maps minimal markdown onto content blocks in document order:
// h1–h3 headings, paragraphs (consecutive plain lines join into one), and
// bullet ("-", "*", "+") and numbered ("1.", "1)") list items with
// two-space-per-level nesting. Other constructs (code fences, tables,
// images) degrade to their literal lines as paragraph text.
func parseBlocks(markdown string) []block {
	var blocks []block
	var para []string // the plain lines accumulating into one paragraph

	flushPara := func() {
		if len(para) > 0 {
			blocks = append(blocks, block{Kind: blockParagraph, Text: strings.Join(para, " ")})
			para = nil
		}
	}

	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushPara()
			continue
		}
		if kind, text, ok := headingOf(trimmed); ok {
			flushPara()
			blocks = append(blocks, block{Kind: kind, Text: text})
			continue
		}
		if level, text, ok := bulletOf(line); ok {
			flushPara()
			blocks = append(blocks, block{Kind: blockBullet, Level: level, Text: text})
			continue
		}
		if level, text, ok := numberedOf(line); ok {
			flushPara()
			blocks = append(blocks, block{Kind: blockNumbered, Level: level, Text: text})
			continue
		}
		para = append(para, trimmed)
	}
	flushPara()
	return blocks
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
