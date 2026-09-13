package gateways

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// Telegram wire-format limits (design D5). Telegram rejects messages over
// 4,096 characters; the splitter budgets 4,000 to leave margin for the
// duplicate-warning prefix and entity overhead.
const (
	telegramHardLimit   = 4096
	telegramChunkBudget = 4000
)

// RenderTelegramHTML converts GFM markdown into Telegram-compatible HTML:
// bold/italic/strikethrough, inline code, fenced code blocks (with language
// tags), links, headings, block quotes, lists, and horizontal rules. GFM
// tables degrade to ASCII-art <pre> blocks in v1 (design D5). Text outside
// markup is HTML-escaped, so raw LLM output can never inject Telegram
// entities.
func RenderTelegramHTML(markdown string) string {
	b := &htmlBuilder{}
	renderBlocks(b, markdown)
	b.closeAll() // stack-based balancing: closers for anything left open
	return b.sb.String()
}

// PlainTextFallback strips markup from rendered Telegram HTML and unescapes
// the entities the renderer emits — the single plain-text retry the adapter
// performs when the platform rejects formatted text with "can't parse
// entities" (design D5).
func PlainTextFallback(html string) string {
	var sb strings.Builder
	for i := 0; i < len(html); {
		if html[i] == '<' {
			if j := strings.IndexByte(html[i:], '>'); j >= 0 {
				i += j + 1
				continue
			}
		}
		sb.WriteByte(html[i])
		i++
	}
	out := sb.String()
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&quot;", "\"")
	out = strings.ReplaceAll(out, "&amp;", "&")
	return out
}

// StreamRenderer accumulates streaming text deltas and renders them to
// balanced Telegram HTML at any point mid-stream (design D5): tags and code
// fences opened but not yet closed by the model are auto-closed, and every
// intermediate edit is therefore well-formed. Safe for concurrent use.
type StreamRenderer struct {
	mu  sync.Mutex
	buf []byte
}

// NewStreamRenderer creates an empty StreamRenderer.
func NewStreamRenderer() *StreamRenderer {
	return &StreamRenderer{}
}

// Write appends one text delta to the buffer.
func (r *StreamRenderer) Write(delta string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, delta...)
}

// Markdown returns the raw markdown accumulated so far.
func (r *StreamRenderer) Markdown() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// Set replaces the accumulated markdown (the message_completed recovery
// path: a completed message longer than the accumulated deltas is
// authoritative).
func (r *StreamRenderer) Set(markdown string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf[:0], markdown...)
}

// Reset empties the buffer (the next message of a split reply starts fresh).
func (r *StreamRenderer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = r.buf[:0]
}

// HTML renders the accumulated markdown to balanced Telegram HTML.
func (r *StreamRenderer) HTML() string {
	return RenderTelegramHTML(r.Markdown())
}

// SplitForTelegram splits markdown into rendered HTML chunks each within the
// platform budget, preferring paragraph → line → sentence → space cut
// boundaries, and closing/reopening code fences across parts (design D5).
// The result is never empty: empty input yields one empty part so callers
// always have a message body.
func SplitForTelegram(markdown string) []string {
	out := []string{}
	queue := SplitMarkdown(markdown, telegramChunkBudget)
	if len(queue) == 0 {
		queue = []string{""}
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		html := RenderTelegramHTML(p)
		if runeLen(html) <= telegramChunkBudget || runeLen(p) <= 1 {
			out = append(out, html)
			continue
		}
		// Emphasis/link markup expands when rendered; re-split a part whose
		// rendered form overshoots the budget. The degenerate tail (one
		// unbreakable stretch of markup) degrades to a hard-cut of its plain
		// text rather than an unparseable half-tag.
		sub := SplitMarkdown(p, runeLen(p)/2)
		if len(sub) == 1 {
			out = append(out, hardCut(PlainTextFallback(html), telegramChunkBudget))
			continue
		}
		queue = append(sub, queue...)
	}
	return out
}

// SplitMarkdown splits src into markdown chunks each at most limit runes,
// never losing content. Cut points prefer paragraph boundaries (blank line),
// then line ends, then sentence ends (". "! "? "), then spaces; a stretch
// with no boundary at all is hard-cut. Code fences split cleanly: the part
// that ends inside a fence gets the fence closed, the next part reopens it
// with the same language tag.
func SplitMarkdown(src string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	runes := []rune(src)
	if len(runes) <= limit {
		return []string{src}
	}

	parts := []string{}
	rest := runes
	for len(rest) > limit {
		cut := cutPoint(rest, limit)
		head := string(rest[:cut])
		tail := rest[cut:]
		if fenceOpenAt(string(rest[:cut])) {
			// Close the fence the chunk cuts through; the next part reopens
			// it with the same language tag.
			lang := fenceLanguageAt(string(rest[:cut]))
			head += "\n```"
			reopen := "```" + lang + "\n"
			tail = append([]rune(reopen), tail...)
		}
		parts = append(parts, head)
		rest = tail
	}
	parts = append(parts, string(rest))
	return parts
}

// cutPoint picks the best cut index in runes[:limit], scanning backwards for
// the most preferred boundary. Returns limit when no boundary exists.
func cutPoint(runes []rune, limit int) int {
	window := string(runes[:limit])

	// Paragraph boundary: a blank line (preferred).
	if i := strings.LastIndex(window, "\n\n"); i >= 0 {
		return len(string(window[:i+1])) // cut after the first newline
	}
	// Line boundary — but never immediately after a fence-delimiter line:
	// that would emit a degenerate empty fence chunk before the reopen.
	for i := strings.LastIndexByte(window, '\n'); i > 0; i = strings.LastIndexByte(window[:i], '\n') {
		lineStart := strings.LastIndexByte(window[:i], '\n') + 1
		if _, isFence := fenceDelimiter(window[lineStart:i]); isFence {
			continue
		}
		return utf8.RuneCountInString(window[:i+1])
	}
	// Sentence boundary: ". " / "! " / "? ".
	for _, mark := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(window, mark); i >= 0 {
			return utf8.RuneCountInString(window[:i+len(mark)])
		}
	}
	// Space boundary.
	if i := strings.LastIndexByte(window, ' '); i > 0 {
		return utf8.RuneCountInString(window[:i+1])
	}
	return limit
}

// fenceOpenAt reports whether src leaves a code fence open at EOF. Fence
// delimiters are lines of three or more backticks (optionally followed by a
// language).
func fenceOpenAt(src string) bool {
	open := false
	for _, line := range strings.Split(src, "\n") {
		if _, isFence := fenceDelimiter(line); isFence {
			open = !open
		}
	}
	return open
}

// fenceLanguageAt returns the language tag of the fence open at EOF.
// It re-walks the fence state; only meaningful when fenceOpenAt is true.
func fenceLanguageAt(src string) string {
	lang := ""
	open := false
	for _, line := range strings.Split(src, "\n") {
		if l, isFence := fenceDelimiter(line); isFence {
			if !open {
				open = true
				lang = l
			} else {
				open = false
				lang = ""
			}
		}
	}
	return lang
}

// fenceDelimiter reports whether line is a ``` fence delimiter and returns
// its language tag when it opens one.
func fenceDelimiter(line string) (lang string, isFence bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "```")), true
}

// hardCut truncates s to at most limit runes.
func hardCut(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

// -------------------------------------------------------------------------
// HTML builder with a tag stack
// -------------------------------------------------------------------------

// htmlBuilder accumulates Telegram HTML while tracking open tags on a stack;
// closeAll appends the closers for every tag still open (the mid-stream
// balancing of design D5).
type htmlBuilder struct {
	sb    strings.Builder
	stack []string
}

// text writes s HTML-escaped: Telegram parses & < > as entity markup, so
// they must always be escaped in text and code positions.
func (b *htmlBuilder) text(s string) {
	for _, r := range s {
		switch r {
		case '&':
			b.sb.WriteString("&amp;")
		case '<':
			b.sb.WriteString("&lt;")
		case '>':
			b.sb.WriteString("&gt;")
		default:
			b.sb.WriteRune(r)
		}
	}
}

func (b *htmlBuilder) raw(s string) { b.sb.WriteString(s) }

// open pushes tag onto the stack and emits its opener.
func (b *htmlBuilder) open(tag string) {
	b.stack = append(b.stack, tag)
	b.sb.WriteByte('<')
	b.sb.WriteString(tag)
	b.sb.WriteByte('>')
}

// openAttr pushes tag and emits an opener with raw attributes (links).
func (b *htmlBuilder) openAttr(tag, name, value string) {
	b.stack = append(b.stack, tag)
	b.sb.WriteByte('<')
	b.sb.WriteString(tag)
	b.sb.WriteByte(' ')
	b.sb.WriteString(name)
	b.sb.WriteString(`="`)
	b.text(value)
	b.sb.WriteString(`">`)
}

func (b *htmlBuilder) closeOne() {
	if len(b.stack) == 0 {
		return
	}
	tag := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	b.sb.WriteString("</" + tag + ">")
}

func (b *htmlBuilder) closeAll() {
	for len(b.stack) > 0 {
		b.closeOne()
	}
}

// toggle closes tag if it is open (including tags opened after it, which are
// re-opened afterwards) or opens it if not — the emphasis-marker semantics
// that let `**a *b**` resolve gracefully mid-stream.
func (b *htmlBuilder) toggle(tag string) {
	idx := -1
	for i := len(b.stack) - 1; i >= 0; i-- {
		if b.stack[i] == tag {
			idx = i
			break
		}
	}
	if idx < 0 {
		b.open(tag)
		return
	}
	inner := append([]string(nil), b.stack[idx+1:]...)
	for i := len(b.stack) - 1; i >= idx; i-- {
		b.sb.WriteString("</" + b.stack[i] + ">")
	}
	b.stack = b.stack[:idx]
	for _, t := range inner {
		b.open(t)
	}
}

// -------------------------------------------------------------------------
// Block-level rendering
// -------------------------------------------------------------------------

// renderBlocks converts markdown to HTML line by line: fenced code, tables,
// headings, block quotes, lists, paragraphs. Any construct left open at EOF
// (mid-stream) is closed by the builder's stack when the caller invokes
// closeAll.
func renderBlocks(b *htmlBuilder, src string) {
	lines := strings.Split(src, "\n")

	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		renderInline(b, strings.Join(para, "\n"))
		b.raw("\n")
		para = nil
	}

	inFence := false
	fenceLang := ""

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if inFence {
			if _, isClose := fenceDelimiter(line); isClose {
				inFence = false
				b.raw("</code></pre>\n")
			} else {
				b.text(line)
				b.raw("\n")
			}
			continue
		}
		if lang, isFence := fenceDelimiter(line); isFence {
			flushPara()
			inFence = true
			fenceLang = lang
			b.raw("<pre><code")
			if fenceLang != "" {
				b.raw(` class="language-`)
				b.raw(escapeAttr(fenceLang))
				b.raw(`"`)
			}
			b.raw(">")
			continue
		}

		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			flushPara()

		case isTableStart(lines, i):
			i = renderTable(b, lines, i)

		case strings.HasPrefix(trimmed, ">"):
			flushPara()
			b.open("blockquote")
			renderInline(b, strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " "))
			b.raw("\n")
			b.closeOne()

		case isHeading(trimmed):
			flushPara()
			b.open("b")
			renderInline(b, strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			b.closeOne()
			b.raw("\n")

		case isListItem(trimmed):
			flushPara()
			item, ok := listItemBody(trimmed)
			if !ok {
				para = append(para, line)
				continue
			}
			b.raw("• ")
			renderInline(b, item)
			b.raw("\n")

		case isHorizontalRule(trimmed):
			flushPara()
			b.raw("────────\n")

		default:
			para = append(para, line)
		}
	}
	flushPara()
	if inFence {
		// Stream ended mid-fence: close the block so the edit is well-formed.
		b.raw("</code></pre>\n")
	}
}

func isHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	return i <= 6 && i < len(line) && line[i] == ' '
}

func isListItem(line string) bool {
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return true
	}
	// Ordered: digits then '.' or ')' then space.
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i < len(line) && (line[i] == '.' || line[i] == ')') && i+1 < len(line) && line[i+1] == ' '
}

// listItemBody strips the list marker, returning ok=false when the line only
// looked like a list item (the caller then treats it as paragraph text).
func listItemBody(line string) (string, bool) {
	switch {
	case len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ':
		return strings.TrimSpace(line[2:]), true
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i < len(line) && (line[i] == '.' || line[i] == ')') && i+1 < len(line) && line[i+1] == ' ' {
		return strings.TrimSpace(line[i+2:]), true
	}
	return line, false
}

func isHorizontalRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, r := range line {
		if r != '-' && r != '*' && r != '_' {
			return false
		}
	}
	return true
}

func escapeAttr(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// -------------------------------------------------------------------------
// Tables: degrade to ASCII <pre> (design D5, v1)
// -------------------------------------------------------------------------

// isTableStart reports whether lines[i] opens a GFM table: the row contains a
// pipe and the next line is a delimiter row (--- | ---).
func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) {
		return false
	}
	if !strings.Contains(lines[i], "|") {
		return false
	}
	return isTableDelimiter(lines[i+1])
}

// isTableDelimiter reports whether line is a GFM table separator row such as
// "--- | :---: | ---".
func isTableDelimiter(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" || !strings.Contains(t, "-") {
		return false
	}
	for _, r := range t {
		switch r {
		case '-', ':', '|', ' ':
		default:
			return false
		}
	}
	return true
}

// renderTable renders rows [start, end) as an ASCII table inside <pre> and
// returns the index of the last consumed line. Cells are stripped of inline
// markup (tables are already a degradation path in v1).
func renderTable(b *htmlBuilder, lines []string, start int) int {
	var rows [][]string
	i := start
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" || !strings.Contains(lines[i], "|") {
			break
		}
		rows = append(rows, tableCells(lines[i]))
	}

	widths := tableWidths(rows)
	b.raw("<pre>")
	for r, row := range rows {
		for c, cell := range row {
			b.raw(cell)
			if pad := widths[c] - runeLen(cell); pad > 0 {
				b.raw(strings.Repeat(" ", pad))
			}
			if c < len(row)-1 {
				b.raw(" | ")
			}
		}
		b.raw("\n")
		if r == 0 && len(rows) > 1 {
			for c := range row {
				b.raw(strings.Repeat("-", widths[c]))
				if c < len(row)-1 {
					b.raw("-+-")
				}
			}
			b.raw("\n")
		}
	}
	b.raw("</pre>\n")
	return i
}

// tableCells splits one table row into its trimmed cells.
func tableCells(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	cells := strings.Split(t, "|")
	out := make([]string, 0, len(cells))
	for _, c := range cells {
		out = append(out, strings.TrimSpace(c))
	}
	return out
}

func tableWidths(rows [][]string) []int {
	var widths []int
	for _, row := range rows {
		for c, cell := range row {
			for c >= len(widths) {
				widths = append(widths, 0)
			}
			if n := runeLen(cell); n > widths[c] {
				widths[c] = n
			}
		}
	}
	return widths
}

// -------------------------------------------------------------------------
// Inline rendering
// -------------------------------------------------------------------------

// renderInline converts one stretch of markdown text (no block constructs)
// into HTML, pushing open emphasis/code/link tags onto the builder's stack.
// Delimiters without a partner emit literally; a delimiter that opens and is
// never closed leaves its tag on the stack for closeAll — the mid-stream
// balancing path.
func renderInline(b *htmlBuilder, src string) {
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		switch c {
		case '\\':
			if i+1 < n {
				b.text(src[i+1 : i+2])
				i += 2
				continue
			}
			b.text("\\")
			i++

		case '`':
			if j := strings.IndexByte(src[i+1:], '`'); j >= 0 {
				b.open("code")
				b.text(src[i+1 : i+1+j])
				b.closeOne()
				i += j + 2
				continue
			}
			// Unclosed code span (mid-stream): everything after is code.
			b.open("code")
			b.text(src[i+1:])
			i = n

		case '*':
			if i+1 < n && src[i+1] == '*' {
				b.toggle("b")
				i += 2
				continue
			}
			b.toggle("i")
			i++

		case '_':
			// Intraword underscores stay literal (GFM rule): foo_bar_baz.
			// An underscore opens when not preceded by a word char (and the
			// next char is not whitespace); it closes when not followed by a
			// word char (and the previous char is not whitespace).
			canOpen := (i == 0 || !isWordByte(src[i-1])) && i+1 < n && src[i+1] != ' ' && src[i+1] != '\n'
			canClose := (i+1 >= n || !isWordByte(src[i+1])) && i > 0 && src[i-1] != ' ' && src[i-1] != '\n'
			if canOpen || canClose {
				b.toggle("i")
			} else {
				b.text("_")
			}
			i++

		case '~':
			if i+1 < n && src[i+1] == '~' {
				b.toggle("s")
				i += 2
				continue
			}
			b.text("~")
			i++

		case '[':
			text, url, ok := parseLink(src, i)
			if !ok {
				b.text("[")
				i++
				continue
			}
			b.openAttr("a", "href", url)
			renderInline(b, text)
			b.closeOne()
			i = linkEnd(src, i)
			continue

		case '&', '<', '>':
			b.text(src[i : i+1])
			i++

		default:
			j := i
			for j < n && !strings.ContainsRune("\\`*_~[&<>", rune(src[j])) {
				j++
			}
			if j == i {
				j++
			}
			b.text(src[i:j])
			i = j
		}
	}
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// parseLink parses "[text](url)" starting at src[i] == '['.
func parseLink(src string, i int) (text, url string, ok bool) {
	close := strings.IndexByte(src[i:], ']')
	if close < 0 || i+close+1 >= len(src) || src[i+close+1] != '(' {
		return "", "", false
	}
	end := strings.IndexByte(src[i+close+2:], ')')
	if end < 0 {
		return "", "", false
	}
	text = src[i+1 : i+close]
	url = strings.TrimSpace(src[i+close+2 : i+close+2+end])
	return text, url, true
}

// linkEnd returns the index just past the link parsed at i (parseLink's
// contract keeps the scanner arithmetic in one place).
func linkEnd(src string, i int) int {
	close := strings.IndexByte(src[i:], ']')
	if close < 0 || i+close+1 >= len(src) || src[i+close+1] != '(' {
		return i + 1
	}
	end := strings.IndexByte(src[i+close+2:], ')')
	if end < 0 {
		return i + 1
	}
	return i + close + 2 + end + 1
}
