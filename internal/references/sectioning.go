package references

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Section is one addressable chunk of a reference document: a row of the
// document_sections index (add-reference-documents design D3).
type Section struct {
	Heading     string // heading or title shown on hits and citation chips
	Locator     string // rendered locator ("p. 30", "slide 5", "sheet Users", heading text, "" when none)
	LocatorKind string // one of the domain.LocatorKind* constants
	Level       int    // ATX level 1-3; 0 = synthetic preamble/whole-doc
	Ordinal     int    // 0-based, sequential over Sectionize's result
	Body        string // search text: heading line included, anchors stripped, normalized
}

// docType is the lowercase extension without dot: "pdf", "docx", "pptx",
// "xlsx", "html", "htm", "csv", "tsv", "md", "txt". For md/txt the markdown
// is the raw file text (the caller passes it through unconverted); for the
// rest it is tools.ConvertDocument output. Unrecognized types degrade to the
// txt/csv whole-document shape.
//
// Sectionize is deterministic — keyed on markdown structure only, no content
// heuristics — and total: it never returns an empty slice (a wholly empty
// document yields one empty-body section).
func Sectionize(docName, docType, markdown string) []Section {
	var sections []Section
	switch normalizedDocType(docType) {
	case "pdf":
		sections = sectionizePDF(markdown)
	case "pptx":
		sections = sectionizePPTX(docName, markdown)
	case "xlsx":
		sections = sectionizeXLSX(docName, markdown)
	case "md", "html", "htm", "docx":
		sections = sectionizeHeadings(docName, markdown)
	default: // csv, tsv, txt, and unknown text-like types: one whole-doc section
		sections = []Section{wholeDocSection(docName, markdown)}
	}
	if len(sections) == 0 {
		sections = []Section{wholeDocSection(docName, "")}
	}
	for i := range sections {
		sections[i].Ordinal = i
		sections[i].Body = normalizeBody(sections[i].Body)
	}
	return sections
}

// normalizedDocType lowercases and strips a leading dot so callers can pass a
// raw extension.
func normalizedDocType(docType string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(docType), "."))
}

// wholeDocSection is the txt/csv shape: the entire document (or an empty
// fallback) as one section with no locator.
func wholeDocSection(docName, body string) Section {
	return Section{
		Heading:     baseFileName(docName),
		LocatorKind: domain.LocatorKindNone,
		Body:        body,
	}
}

// baseFileName renders a document's base filename for synthetic headings.
func baseFileName(docName string) string {
	return filepath.Base(docName)
}

// --- PDF: split on page anchors, ATX headings may split pages further ------

func sectionizePDF(markdown string) []Section {
	segments := tools.ParsePageSegments(markdown)
	if len(segments) == 0 {
		if strings.TrimSpace(markdown) == "" {
			return nil // Sectionize fills the empty-document fallback
		}
		// Anchor-less markdown: the whole text is one page-1 section.
		return []Section{{
			Heading:     "Page 1",
			Locator:     "p. 1",
			LocatorKind: domain.LocatorKindPage,
			Level:       1,
			Body:        markdown,
		}}
	}
	var sections []Section
	for _, seg := range segments {
		locator := fmt.Sprintf("p. %d", seg.Page)
		synthetic := Section{
			Heading:     fmt.Sprintf("Page %d", seg.Page),
			Locator:     locator,
			LocatorKind: domain.LocatorKindPage,
			Level:       1,
		}
		sections = append(sections, splitAtRuns(seg.Body, synthetic)...)
	}
	return sections
}

// --- PPTX: one section per "## Slide N" marker ------------------------------

// pptxSlideRe matches the converter's per-slide heading (convertPPTX).
var pptxSlideRe = regexp.MustCompile(`^ {0,3}## Slide ([0-9]+)[ \t]*$`)

func sectionizePPTX(docName, markdown string) []Section {
	type slideMark struct{ idx, num int }
	lines := splitLines(markdown)
	var marks []slideMark
	for i, line := range lines {
		if m := pptxSlideRe.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			marks = append(marks, slideMark{idx: i, num: n})
		}
	}
	if len(marks) == 0 {
		if strings.TrimSpace(markdown) == "" {
			return nil
		}
		return []Section{wholeDocSection(docName, markdown)}
	}
	var sections []Section
	if lead := strings.TrimSpace(strings.Join(lines[:marks[0].idx], "\n")); lead != "" {
		// Not emitted by convertPPTX, but total behavior: keep leading text.
		sections = append(sections, Section{
			Heading:     baseFileName(docName),
			LocatorKind: domain.LocatorKindNone,
			Level:       0,
			Body:        lead,
		})
	}
	for i, mark := range marks {
		end := len(lines)
		if i+1 < len(marks) {
			end = marks[i+1].idx
		}
		sections = append(sections, Section{
			Heading:     fmt.Sprintf("Slide %d", mark.num),
			Locator:     fmt.Sprintf("slide %d", mark.num),
			LocatorKind: domain.LocatorKindSlide,
			Level:       1,
			Body:        strings.Join(lines[mark.idx:end], "\n"),
		})
	}
	return sections
}

// --- XLSX: one section per "## <sheet name>" marker --------------------------

// xlsxSheetRe matches the converter's per-sheet heading (convertXLSX).
var xlsxSheetRe = regexp.MustCompile(`^ {0,3}## (.+?)[ \t]*$`)

func sectionizeXLSX(docName, markdown string) []Section {
	type sheetMark struct {
		idx  int
		name string
	}
	lines := splitLines(markdown)
	var marks []sheetMark
	for i, line := range lines {
		if m := xlsxSheetRe.FindStringSubmatch(line); m != nil {
			marks = append(marks, sheetMark{idx: i, name: strings.TrimSpace(m[1])})
		}
	}
	if len(marks) == 0 {
		if strings.TrimSpace(markdown) == "" {
			return nil
		}
		return []Section{wholeDocSection(docName, markdown)}
	}
	var sections []Section
	if lead := strings.TrimSpace(strings.Join(lines[:marks[0].idx], "\n")); lead != "" {
		sections = append(sections, Section{
			Heading:     baseFileName(docName),
			LocatorKind: domain.LocatorKindNone,
			Level:       0,
			Body:        lead,
		})
	}
	for i, mark := range marks {
		end := len(lines)
		if i+1 < len(marks) {
			end = marks[i+1].idx
		}
		sections = append(sections, Section{
			Heading:     mark.name,
			Locator:     "sheet " + mark.name,
			LocatorKind: domain.LocatorKindSheet,
			Level:       1,
			Body:        strings.Join(lines[mark.idx:end], "\n"),
		})
	}
	return sections
}

// --- MD / HTML / DOCX: ATX headings (1-3) start sections ---------------------

func sectionizeHeadings(docName, markdown string) []Section {
	var sections []Section
	preamble := Section{
		Heading:     baseFileName(docName),
		LocatorKind: domain.LocatorKindNone,
		Level:       0,
	}
	for _, run := range splitATXRuns(markdown) {
		if run.level == 0 {
			// Content before the first heading: preamble-shaped section.
			preamble.Body = run.body
			sections = append(sections, preamble)
			continue
		}
		sections = append(sections, Section{
			Heading:     run.heading,
			Locator:     run.heading,
			LocatorKind: domain.LocatorKindHeading,
			Level:       run.level,
			Body:        run.body,
		})
	}
	return sections
}

// --- Markdown run splitting ---------------------------------------------------

// atxRun is one section-sized run of lines: either the content before the
// first ATX heading (level 0) or a heading and its following lines.
type atxRun struct {
	heading string // ATX heading text; empty for the level-0 lead run
	level   int    // ATX level 1-3; 0 for the lead run
	body    string // run text including its heading line, if any
}

// atxHeadingRe matches an ATX heading of level 1-3 (deeper levels are body
// text), tolerating up to three leading spaces and requiring the separating
// space per CommonMark.
var atxHeadingRe = regexp.MustCompile(`^ {0,3}(#{1,3})[ \t]+(.*)$`)

// atxHeading reports the ATX level and text when line is a level 1-3 heading.
func atxHeading(line string) (level int, text string, ok bool) {
	m := atxHeadingRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	return len(m[1]), strings.TrimSpace(m[2]), true
}

// splitATXRuns splits text into runs: lines before the first ATX 1-3 heading
// form a level-0 lead run (dropped when blank); each heading opens a new run
// whose body includes the heading line.
func splitATXRuns(text string) []atxRun {
	type runAcc struct {
		heading string
		level   int
		lines   []string
	}
	var runs []atxRun
	cur := runAcc{} // level-0 lead accumulator until a heading opens a run
	flush := func() {
		if cur.heading == "" && strings.TrimSpace(strings.Join(cur.lines, "\n")) == "" {
			return // blank lead: no preamble section
		}
		runs = append(runs, atxRun{heading: cur.heading, level: cur.level, body: strings.Join(cur.lines, "\n")})
	}
	for _, line := range splitLines(text) {
		if level, heading, ok := atxHeading(line); ok {
			flush()
			cur = runAcc{heading: heading, level: level, lines: []string{line}}
			continue
		}
		cur.lines = append(cur.lines, line)
	}
	flush()
	return runs
}

// splitLines splits on \n, trimming a trailing \r so CRLF input matches the
// heading regexes; converter output is LF-only.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// splitAtRuns splits one piece of markdown at ATX headings, mapping each run
// to a section cloned from synthetic (kind/locator/synthetic heading) and
// overriding heading/level/body per run. splitAtRuns never returns empty for
// non-blank input: a heading-less piece becomes the synthetic section itself.
func splitAtRuns(piece string, synthetic Section) []Section {
	var sections []Section
	for _, run := range splitATXRuns(piece) {
		sec := synthetic
		sec.Body = run.body
		if run.level > 0 {
			sec.Heading = run.heading
			sec.Level = run.level
		}
		sections = append(sections, sec)
	}
	return sections
}

// --- Body normalization --------------------------------------------------------

// multiBlankRe matches runs of 3+ newlines, collapsed to one blank line.
var multiBlankRe = regexp.MustCompile(`\n{3,}`)

// normalizeBody strips page anchors, collapses runs of 3+ newlines to 2, and
// trims the edges.
func normalizeBody(body string) string {
	return strings.TrimSpace(multiBlankRe.ReplaceAllString(tools.StripPageMarkers(body), "\n\n"))
}
