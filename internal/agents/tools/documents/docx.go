package documents

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"regexp"
	"strings"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"github.com/mmonterroca/docxgo/v2/pkg/template"
)

// docxBodyPath is the archive member holding the main document body.
const docxBodyPath = "word/document.xml"

// placeholderPattern matches the {{key}} tokens the fill_placeholders
// operations replace. It mirrors docxgo's merge machinery so both engines
// speak the same template dialect: an optional leading dot, then a key of
// word characters and dots.
var placeholderPattern = regexp.MustCompile(`\{\{\.?(\w+(?:\.\w+)*)\}\}`)

// placeholderKey extracts the values-map key from a placeholderPattern match.
func placeholderKey(match string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(match, "{{"), "."), "}}")
}

// DocxEngine applies targeted operations to a staged DOCX file and writes
// the result preserving every other aspect of the original: docxgo's
// round-trip machinery keeps untouched blocks' original XML, and the
// structural operations splice document.xml at byte level.
type DocxEngine struct{}

// NewDocxEngine constructs the DOCX edit engine.
func NewDocxEngine() *DocxEngine { return &DocxEngine{} }

// Edit applies ops to the DOCX at src in the order given — each operation
// addresses the document as left by the previous one — and writes the result
// to dst. Rejected operations and processing failures come back as readable
// Result values and nothing is written; a Go error is only for
// infrastructure failures.
func (e *DocxEngine) Edit(_ context.Context, src, dst string, ops []Op) (Result, error) {
	return runGuarded(func() (Result, error) { return e.edit(src, dst, ops) })
}

// docxOpKind separates the two application passes: docxgo-level content
// operations and byte-level body splices. docxgo exposes no body-level
// paragraph removal or insertion, so those operations splice document.xml.
type docxOpKind int

const (
	docxContentOp docxOpKind = iota
	docxSpliceOp
)

// docxOpGroup is a maximal run of consecutive operations sharing one pass.
type docxOpGroup struct {
	kind docxOpKind
	ops  []Op
}

// docxOpKindOf classifies an operation, rejecting unknown types readably.
func docxOpKindOf(op Op) (docxOpKind, error) {
	switch op.Type {
	case OpReplaceParagraph, OpFillPlaceholders:
		return docxContentOp, nil
	case OpInsertParagraph, OpDeleteParagraph:
		return docxSpliceOp, nil
	default:
		return 0, readablef("unknown operation %q for a DOCX — supported: replace_paragraph, insert_paragraph, delete_paragraph, fill_placeholders", op.Type)
	}
}

// groupDocxOps validates every operation's type up front, then groups
// consecutive same-pass operations so each pass runs once.
func groupDocxOps(ops []Op) ([]docxOpGroup, error) {
	var groups []docxOpGroup
	for _, op := range ops {
		kind, err := docxOpKindOf(op)
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 || groups[len(groups)-1].kind != kind {
			groups = append(groups, docxOpGroup{kind: kind})
		}
		groups[len(groups)-1].ops = append(groups[len(groups)-1].ops, op)
	}
	return groups, nil
}

func (e *DocxEngine) edit(src, dst string, ops []Op) (Result, error) {
	data, res, err := gateInput(src)
	if err != nil {
		return Result{}, err
	}
	if !res.OK {
		return res, nil
	}
	groups, err := groupDocxOps(ops)
	if err != nil {
		return asResult(err)
	}
	for _, group := range groups {
		switch group.kind {
		case docxContentOp:
			data, res, err = e.applyContentPass(data, group.ops)
		case docxSpliceOp:
			data, res, err = applySplicePass(data, group.ops)
		}
		if err != nil {
			return Result{}, err
		}
		if !res.OK {
			return res, nil
		}
	}
	return writeResult(dst, data)
}

// applyContentPass runs the docxgo-level operations: it opens the current
// bytes, applies each operation against the live document, and
// re-serializes. Unchanged blocks keep their original XML (docxgo's
// round-trip machinery), which is what preserves untouched text and styling.
// Replace and fill never change the paragraph count, so one enumeration
// serves the whole pass.
func (e *DocxEngine) applyContentPass(data []byte, ops []Op) ([]byte, Result, error) {
	doc, err := docx.OpenDocumentFromBytes(data)
	if err != nil {
		return nil, failf("not a readable DOCX file: %v", err), nil
	}
	paras := docxParagraphs(doc)
	for _, op := range ops {
		switch op.Type {
		case OpReplaceParagraph:
			if res := replaceDocxParagraph(paras, op); !res.OK {
				return nil, res, nil
			}
		case OpFillPlaceholders:
			if res := fillDocxPlaceholders(doc, op); !res.OK {
				return nil, res, nil
			}
		}
	}
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, failf("serialize the edited document: %v", err), nil
	}
	return buf.Bytes(), Result{OK: true}, nil
}

// replaceDocxParagraph sets ¶index's text, keeping its style and the
// character formatting of its first text run.
func replaceDocxParagraph(paras []domain.Paragraph, op Op) Result {
	if op.Index < 1 {
		return failf("replace_paragraph needs a 1-based paragraph index (¶n as the views print it)")
	}
	if op.Index > len(paras) {
		return failf("paragraph ¶%d not found: the document has %d paragraphs", op.Index, len(paras))
	}
	if err := replaceParagraphText(paras[op.Index-1], op.Text); err != nil {
		return failf("replace ¶%d: %v", op.Index, err)
	}
	return Result{OK: true}
}

// fillDocxPlaceholders fills {{tokens}} with op.Values, riding docxgo's
// merge machinery: it consolidates the runs Word splits tokens across and
// replaces in place, keeping the template's styling.
func fillDocxPlaceholders(doc domain.Document, op Op) Result {
	if len(op.Values) == 0 {
		return failf("fill_placeholders needs a values map of placeholder keys to replacements")
	}
	if err := template.MergeTemplate(doc, template.MergeData(op.Values)); err != nil {
		return failf("fill placeholders: %v", err)
	}
	mergeSplitPlaceholderRuns(doc, op.Values)
	return Result{OK: true}
}

// mergeSplitPlaceholderRuns handles the one case docxgo's merge leaves: a
// token split across differently-formatted runs. Such a paragraph's runs
// collapse onto its first text-only run (keeping that run's formatting) so
// the token becomes matchable; paragraphs carrying fields, breaks, or
// images are left untouched rather than risk dropping non-text content.
func mergeSplitPlaceholderRuns(doc domain.Document, values map[string]string) {
	for _, para := range docxParagraphs(doc) {
		runs := para.Runs()
		if len(runs) < 2 {
			continue
		}
		joined := paragraphText(runs)
		if !placeholderPattern.MatchString(joined) {
			continue
		}
		split := true
		for _, run := range runs {
			if placeholderPattern.MatchString(run.Text()) {
				split = false
				break
			}
		}
		if !split {
			continue
		}
		merged := placeholderPattern.ReplaceAllStringFunc(joined, func(match string) string {
			if v, ok := values[placeholderKey(match)]; ok {
				return v
			}
			return match
		})
		if err := runs[0].SetText(merged); err != nil {
			return
		}
		for i := len(runs) - 1; i >= 1; i-- {
			if err := para.RemoveRun(i); err != nil {
				return
			}
		}
	}
}

// replaceParagraphText rewrites the paragraph's text onto one fresh run,
// keeping the paragraph's style (its pPr is untouched) and copying the
// visible character formatting of its first text run.
func replaceParagraphText(para domain.Paragraph, text string) error {
	var carrier domain.Run
	for _, run := range para.Runs() {
		if runIsTextOnly(run) {
			carrier = run
			break
		}
	}
	para.ClearRuns()
	run, err := para.AddRun()
	if err != nil {
		return err
	}
	if err := run.SetText(text); err != nil {
		return err
	}
	copyRunFormatting(carrier, run)
	return nil
}

// runIsTextOnly reports whether a run carries nothing but text, mirroring
// docxgo's consolidation criteria.
func runIsTextOnly(run domain.Run) bool {
	return len(run.Fields()) == 0 && len(run.Breaks()) == 0 && run.Image() == nil
}

// paragraphText concatenates the runs' text.
func paragraphText(runs []domain.Run) string {
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.Text())
	}
	return b.String()
}

// copyRunFormatting carries the visible character formatting from src onto
// dst, skipping zero values so style-inherited defaults stay inherited.
func copyRunFormatting(src, dst domain.Run) {
	if src == nil || dst == nil {
		return
	}
	if f := src.Font(); f != (domain.Font{}) {
		_ = dst.SetFont(f)
	}
	if c := src.Color(); c != (domain.Color{}) {
		_ = dst.SetColor(c)
	}
	if s := src.Size(); s > 0 {
		_ = dst.SetSize(s)
	}
	if src.Bold() {
		_ = dst.SetBold(true)
	}
	if src.Italic() {
		_ = dst.SetItalic(true)
	}
	if u := src.Underline(); u != domain.UnderlineNone {
		_ = dst.SetUnderline(u)
	}
	if src.Strike() {
		_ = dst.SetStrike(true)
	}
	if h := src.Highlight(); h != domain.HighlightNone {
		_ = dst.SetHighlight(h)
	}
	if lang := src.Language(); lang != nil {
		_ = dst.SetLanguage(lang)
	}
}

// docxParagraphs flattens the document into the ¶-addressable paragraphs the
// views print: body paragraphs and table-cell paragraphs in document order.
// Within a cell, paragraphs precede its nested tables' paragraphs — the
// library exposes no interleaved view.
func docxParagraphs(doc domain.Document) []domain.Paragraph {
	var paras []domain.Paragraph
	for _, block := range doc.Blocks() {
		switch {
		case block.Paragraph != nil:
			paras = append(paras, block.Paragraph)
		case block.Table != nil:
			paras = appendDocxTableParagraphs(paras, block.Table)
		}
	}
	return paras
}

func appendDocxTableParagraphs(paras []domain.Paragraph, table domain.Table) []domain.Paragraph {
	for _, row := range table.Rows() {
		for _, cell := range row.Cells() {
			paras = append(paras, cell.Paragraphs()...)
			for _, nested := range cell.Tables() {
				paras = appendDocxTableParagraphs(paras, nested)
			}
		}
	}
	return paras
}

// applySplicePass applies insert_paragraph and delete_paragraph by splicing
// word/document.xml at the byte spans of the addressed paragraphs, keeping
// every other byte of the package untouched. Each operation addresses the
// body as left by the previous one.
func applySplicePass(data []byte, ops []Op) ([]byte, Result, error) {
	for _, op := range ops {
		var (
			out []byte
			res Result
			err error
		)
		switch op.Type {
		case OpDeleteParagraph:
			out, res, err = spliceDeleteParagraph(data, op.Index)
		case OpInsertParagraph:
			out, res, err = spliceInsertParagraph(data, op.AfterIndex, op.Text, op.Style)
		}
		if err != nil {
			return nil, Result{}, err
		}
		if !res.OK {
			return nil, res, nil
		}
		data = out
	}
	return data, Result{OK: true}, nil
}

// spliceDeleteParagraph removes the ¶index paragraph element from the body.
func spliceDeleteParagraph(pkg []byte, index int) ([]byte, Result, error) {
	if index < 1 {
		return nil, failf("delete_paragraph needs a 1-based paragraph index (¶n as the views print it)"), nil
	}
	return rewriteDocxBody(pkg, func(body []byte) ([]byte, Result, error) {
		scan, err := scanDocxBody(body)
		if err != nil {
			return nil, Result{}, err
		}
		if index > len(scan.paragraphs) {
			return nil, failf("paragraph ¶%d not found: the document has %d paragraphs", index, len(scan.paragraphs)), nil
		}
		span := scan.paragraphs[index-1]
		if span.cell >= 0 && scan.cells[span.cell].paragraphs <= 1 {
			return nil, failf("cannot delete ¶%d: it is the only paragraph of its table cell, and a cell cannot be left empty", index), nil
		}
		out := make([]byte, 0, len(body)-(int(span.end-span.start)))
		out = append(out, body[:span.start]...)
		out = append(out, body[span.end:]...)
		return out, Result{OK: true}, nil
	})
}

// spliceInsertParagraph inserts a new paragraph element after ¶afterIndex
// (before the first block when afterIndex is 0).
func spliceInsertParagraph(pkg []byte, afterIndex int, text, style string) ([]byte, Result, error) {
	if afterIndex < 0 {
		return nil, failf("insert_paragraph needs afterIndex of 0 (start of the body) or a 1-based ¶ index to insert after"), nil
	}
	return rewriteDocxBody(pkg, func(body []byte) ([]byte, Result, error) {
		scan, err := scanDocxBody(body)
		if err != nil {
			return nil, Result{}, err
		}
		if afterIndex > len(scan.paragraphs) {
			return nil, failf("paragraph ¶%d not found: the document has %d paragraphs", afterIndex, len(scan.paragraphs)), nil
		}
		if afterIndex == 0 && scan.bodyStart == 0 {
			return nil, failf("cannot insert at the start: no document body part was found"), nil
		}
		fragment := docxParagraphFragment(text, style, scan.wPrefix)
		at := scan.bodyStart
		if afterIndex > 0 {
			at = scan.paragraphs[afterIndex-1].end
		}
		out := make([]byte, 0, len(body)+len(fragment))
		out = append(out, body[:at]...)
		out = append(out, fragment...)
		out = append(out, body[at:]...)
		return out, Result{OK: true}, nil
	})
}

// rewriteDocxBody rewrites word/document.xml inside the package archive,
// copying every other member's raw compressed bytes untouched.
func rewriteDocxBody(pkg []byte, edit func(body []byte) ([]byte, Result, error)) ([]byte, Result, error) {
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return nil, failf("not a readable DOCX file: %v", err), nil
	}
	var body []byte
	for _, f := range zr.File {
		if f.Name != docxBodyPath {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, failf("cannot read the document body: %v", err), nil
		}
		body, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, failf("cannot read the document body: %v", err), nil
		}
		break
	}
	if body == nil {
		return nil, failf("not a readable DOCX file: no %s part", docxBodyPath), nil
	}
	edited, res, err := edit(body)
	if err != nil || !res.OK {
		return nil, res, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if f.Name == docxBodyPath {
			w, cerr := zw.Create(f.Name)
			if cerr == nil {
				_, cerr = w.Write(edited)
			}
			if cerr != nil {
				return nil, failf("rewrite the document body: %v", cerr), nil
			}
			continue
		}
		fw, cerr := zw.CreateRaw(&f.FileHeader)
		if cerr == nil {
			var rc io.Reader
			rc, cerr = f.OpenRaw()
			if cerr == nil {
				_, cerr = io.Copy(fw, rc)
			}
		}
		if cerr != nil {
			return nil, failf("rewrite the document: %v", cerr), nil
		}
	}
	if err := zw.Close(); err != nil {
		return nil, failf("rewrite the document: %v", err), nil
	}
	return buf.Bytes(), Result{OK: true}, nil
}

// wmlNamespaces are the transitional and strict OOXML wordprocessingml main
// namespaces, both matched when scanning document.xml.
var wmlNamespaces = map[string]bool{
	"http://schemas.openxmlformats.org/wordprocessingml/2006/main": true,
	"http://purl.oclc.org/ooxml/wordprocessingml/main":             true,
}

// docxParagraphSpan is the byte span of one addressable <w:p> element —
// one whose parent is the body or a table cell, which is exactly the ¶
// enumeration docxgo reconstructs — plus the index of its enclosing cell
// (-1 at body level).
type docxParagraphSpan struct {
	start, end int64
	cell       int
}

// docxCellSpan is the byte span of one <w:tc> element and how many
// addressable paragraphs its subtree holds.
type docxCellSpan struct {
	start, end int64
	paragraphs int
}

// docxBodyScan locates every addressable paragraph in document.xml, in
// document order — the same ¶ order the views print.
type docxBodyScan struct {
	paragraphs []docxParagraphSpan
	cells      []docxCellSpan
	bodyStart  int64  // offset just after the <w:body> opening tag
	wPrefix    string // the root element's prefix for the wordprocessingml namespace
}

// scanDocxBody walks document.xml with the standard library, recording the
// byte spans of the addressable paragraphs. Text-box and content-control
// paragraphs are descendants of a run or a non-block element, not of the
// body or a cell, so they are not addressable — matching docxgo's blocks.
func scanDocxBody(data []byte) (docxBodyScan, error) {
	var scan docxBodyScan
	dec := xml.NewDecoder(bytes.NewReader(data))

	type openPara struct {
		span int // index into scan.paragraphs, -1 when not addressable
	}
	type openCell struct {
		span int // index into scan.cells
	}
	var (
		wmlStack  []string // local names of open wordprocessingml elements
		paraStack []openPara
		cellStack []openCell
		rootSeen  bool
	)
	parent := func() string {
		if len(wmlStack) == 0 {
			return ""
		}
		return wmlStack[len(wmlStack)-1]
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return scan, nil
		}
		if err != nil {
			return scan, readablef("cannot parse the document body: %v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			endTag := dec.InputOffset()
			startTag := int64(bytes.LastIndexByte(data[:endTag], '<'))
			if startTag < 0 {
				return scan, readablef("cannot parse the document body: malformed element")
			}
			if !rootSeen {
				rootSeen = true
				scan.wPrefix = rootWMLPrefix(t.Attr)
			}
			if !wmlNamespaces[t.Name.Space] {
				continue // not a wordprocessingml element; never stacked
			}
			switch t.Name.Local {
			case "body":
				scan.bodyStart = endTag
			case "p":
				span := -1
				if p := parent(); p == "body" || p == "tc" {
					entry := docxParagraphSpan{start: startTag, cell: -1}
					if len(cellStack) > 0 {
						entry.cell = cellStack[len(cellStack)-1].span
						scan.cells[entry.cell].paragraphs++
					}
					scan.paragraphs = append(scan.paragraphs, entry)
					span = len(scan.paragraphs) - 1
				}
				paraStack = append(paraStack, openPara{span: span})
			case "tc":
				scan.cells = append(scan.cells, docxCellSpan{start: startTag})
				cellStack = append(cellStack, openCell{span: len(scan.cells) - 1})
			}
			wmlStack = append(wmlStack, t.Name.Local)
		case xml.EndElement:
			if !wmlNamespaces[t.Name.Space] {
				continue
			}
			switch t.Name.Local {
			case "p":
				if len(paraStack) > 0 {
					top := paraStack[len(paraStack)-1]
					paraStack = paraStack[:len(paraStack)-1]
					if top.span >= 0 {
						scan.paragraphs[top.span].end = dec.InputOffset()
					}
				}
			case "tc":
				if len(cellStack) > 0 {
					top := cellStack[len(cellStack)-1]
					cellStack = cellStack[:len(cellStack)-1]
					scan.cells[top.span].end = dec.InputOffset()
				}
			}
			if len(wmlStack) > 0 && wmlStack[len(wmlStack)-1] == t.Name.Local {
				wmlStack = wmlStack[:len(wmlStack)-1]
			}
		}
	}
}

// rootWMLPrefix returns the prefix the root element binds to the
// wordprocessingml namespace ("" for a default namespace), defaulting to
// Word's conventional "w".
func rootWMLPrefix(attrs []xml.Attr) string {
	for _, a := range attrs {
		if !wmlNamespaces[a.Value] {
			continue
		}
		if a.Name.Space == "xmlns" {
			return a.Name.Local
		}
		if a.Name.Local == "xmlns" {
			return ""
		}
	}
	return "w"
}

// docxParagraphFragment builds a minimal paragraph element for splicing:
// the text in one run (xml:space preserved), with an optional paragraph
// style. The fragment uses the document's own namespace prefix so it parses
// in place.
func docxParagraphFragment(text, style, prefix string) []byte {
	tag := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + ":" + name
	}
	var b strings.Builder
	b.WriteString("<" + tag("p") + ">")
	if style != "" {
		b.WriteString("<" + tag("pPr") + "><" + tag("pStyle") + " " + tag("val") + "=\"" + xmlEscape(style) + "\"/></" + tag("pPr") + ">")
	}
	b.WriteString("<" + tag("r") + "><" + tag("t") + ` xml:space="preserve">`)
	b.WriteString(xmlEscape(text))
	b.WriteString("</" + tag("t") + "></" + tag("r") + "></" + tag("p") + ">")
	return []byte(b.String())
}

// xmlEscape escapes text for XML content.
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
