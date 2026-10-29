package backend

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gopptx "github.com/kenny-not-dead/gopptx"
	"github.com/xuri/excelize/v2"
)

// officeViewExtensions maps the OOXML extensions the read path renders as
// structured views (design D4) to their format names. Reading such a file
// never touches the bytes on disk: the view is the model's eyes, not an
// export — nothing is written back and no markdown copy is kept.
var officeViewExtensions = map[string]string{
	".docx": "DOCX",
	".pptx": "PPTX",
	".xlsx": "XLSX",
}

// officeMaxViewBytes caps the input file a view renders (design D9),
// mirroring the document tools' 20 MiB input cap.
const officeMaxViewBytes = 20 << 20

// officeMaxDecompressedBytes caps the total uncompressed archive size a view
// will parse (design D9's zip-bomb defeat), mirroring the document tools'
// decompression budget. A var so tests can shrink it.
var officeMaxDecompressedBytes int64 = 512 << 20

// officeView is one rendered office view: a summary header naming the format
// and element counts, plus one line per addressable element.
type officeView struct {
	summary string
	lines   []string
}

// renderOfficeView renders the structured view for an office document at the
// resolved path (design D4): DOCX and PPTX as structured text views, XLSX as
// a cell-grid view. ok is false when the path is not an office document and
// the caller should read it as text. The offset and limit window the element
// lines exactly like the text read path's (readTextWindow's) mechanics; the
// header is always present so the model keeps the element counts on every
// page.
func renderOfficeView(resolved string, offset, limit int) (string, bool, error) {
	format, ok := officeViewExtensions[strings.ToLower(filepath.Ext(resolved))]
	if !ok {
		return "", false, nil
	}
	view, err := guardedOfficeView(resolved, format)
	if err != nil {
		return "", true, err
	}
	lines := append([]string{view.summary}, windowLines(view.lines, offset, limit)...)
	return strings.Join(lines, "\n"), true, nil
}

// guardedOfficeView reads the file and renders its view under panic recovery
// (design D9): buddies is an always-on tray gateway, so a library panic on a
// hostile document becomes a read error, not a gateway crash.
func guardedOfficeView(resolved, format string) (view officeView, err error) {
	defer func() {
		if r := recover(); r != nil {
			view = officeView{}
			err = fmt.Errorf("render the %s view: %v", format, r)
		}
	}()

	data, err := os.ReadFile(resolved)
	if err != nil {
		return officeView{}, fmt.Errorf("read file: %w", err)
	}
	if len(data) > officeMaxViewBytes {
		return officeView{}, fmt.Errorf("file is %d bytes; %s views are capped at %d bytes", len(data), format, officeMaxViewBytes)
	}
	if err := checkZipBudget(data); err != nil {
		return officeView{}, err
	}

	switch format {
	case "DOCX":
		return renderDocxView(data)
	case "PPTX":
		return renderPptxView(data)
	case "XLSX":
		return renderXlsxView(data)
	default:
		return officeView{}, fmt.Errorf("unknown office format %q", format)
	}
}

// checkZipBudget pre-scans an OOXML archive with the standard library before
// any document library decompresses it (design D9): every member is
// decompressed under a running budget and the read is rejected once the total
// uncompressed size exceeds the cap, defeating zip bombs — including members
// whose declared sizes lie. An archive the standard library cannot open is
// left to the format renderers to report readably.
func checkZipBudget(data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	var total int64
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("read archive member %s: %w", zf.Name, err)
		}
		// Read one byte past the remaining budget: a member that yields it
		// pushes the total over the cap even though its declared size may not.
		n, err := io.CopyN(io.Discard, rc, officeMaxDecompressedBytes-total+1)
		closeErr := rc.Close()
		total += n
		if errors.Is(err, io.EOF) {
			continue
		}
		if err == nil || total > officeMaxDecompressedBytes {
			return fmt.Errorf("archive decompresses beyond the %d MiB safety cap — refusing a possible zip bomb", officeMaxDecompressedBytes>>20)
		}
		if closeErr != nil {
			err = closeErr
		}
		return fmt.Errorf("read archive member %s: %w", zf.Name, err)
	}
	return nil
}

// windowLines applies the text read path's 1-based line offset and line limit
// (readTextWindow's mechanics) to the view's element lines.
func windowLines(lines []string, offset, limit int) []string {
	if offset > 1 {
		if offset > len(lines) {
			return nil
		}
		lines = lines[offset-1:]
	}
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}
	return lines
}

// plural returns the noun in its simple English plural for counts other than
// one.
func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// escapeLine keeps one element's text on one view line: newlines, carriage
// returns, and tabs render as their backslash escapes. For PPTX shape text
// the escaped newline doubles as the paragraph separator the edit engines'
// replace_shape_text splits on.
func escapeLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return strings.ReplaceAll(s, "\t", "\\t")
}

// — DOCX —

// wmlNamespaces are the transitional and strict OOXML wordprocessingml main
// namespaces, both matched when scanning document.xml. It matches the
// document tools' body scanner so both see the same elements.
var wmlNamespaces = map[string]bool{
	"http://schemas.openxmlformats.org/wordprocessingml/2006/main": true,
	"http://purl.oclc.org/ooxml/wordprocessingml/main":             true,
}

// docxBodyPath is the archive member holding the main document body.
const docxBodyPath = "word/document.xml"

// renderDocxView renders the DOCX structured text view: one line per
// addressable paragraph, in document order, naming its style when it has
// one. The ¶ enumeration is the document tools' engines' — body paragraphs
// and table-cell paragraphs, 1-based, text-box and content-control paragraphs
// excluded.
func renderDocxView(data []byte) (officeView, error) {
	paras, err := docxViewParagraphs(data)
	if err != nil {
		return officeView{}, err
	}
	lines := make([]string, 0, len(paras))
	for i, para := range paras {
		var b strings.Builder
		fmt.Fprintf(&b, "¶%d", i+1)
		if para.Style != "" {
			fmt.Fprintf(&b, " [%s]", para.Style)
		}
		if para.Text != "" {
			fmt.Fprintf(&b, " %s", escapeLine(para.Text))
		}
		lines = append(lines, b.String())
	}
	return officeView{
		summary: fmt.Sprintf("DOCX view: %d %s", len(paras), plural(len(paras), "paragraph")),
		lines:   lines,
	}, nil
}

// docxViewPara is one addressable paragraph: the text the model sees and the
// paragraph style ID (as it appears in the document's styles.xml) the edit
// engines' insert_paragraph takes.
type docxViewPara struct {
	Style string
	Text  string
}

// docxViewParagraphs extracts the addressable paragraphs from the DOCX
// package bytes, in the document tools' ¶ order: a paragraph is addressable
// when its parent is the body or a table cell — a text-box or
// content-control paragraph descends from a run or another non-block element
// instead, so it is not addressable. docxgo's API cannot surface paragraph
// styles (its Style() is unimplemented), so the view scans document.xml
// directly with the standard library, pinned to the same enumeration by the
// agreement tests in fs_office_view_test.go.
func docxViewParagraphs(data []byte) ([]docxViewPara, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a readable DOCX file: %w", err)
	}
	var body []byte
	for _, f := range zr.File {
		if f.Name != docxBodyPath {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("read the document body: %w", err)
		}
		body, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read the document body: %w", err)
		}
		break
	}
	if body == nil {
		return nil, fmt.Errorf("not a readable DOCX file: no %s part", docxBodyPath)
	}
	return scanDocxViewParas(body)
}

// scanDocxViewParas walks document.xml capturing each addressable paragraph's
// style and text: the style from the paragraph's own pPr > pStyle val, the
// text from the w:t elements whose innermost open paragraph is the
// addressable one (a text box's nested paragraphs' w:t belongs to the shape,
// not the ¶).
func scanDocxViewParas(body []byte) ([]docxViewPara, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))

	var (
		paras     []docxViewPara
		wmlStack  []string // local names of the open wordprocessingml elements
		paraStack []int    // index into paras per open w:p, -1 when not addressable
		captureT  bool
	)
	parent := func() string {
		if len(wmlStack) == 0 {
			return ""
		}
		return wmlStack[len(wmlStack)-1]
	}
	addressable := func() bool {
		return len(paraStack) > 0 && paraStack[len(paraStack)-1] >= 0
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return paras, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse the document body: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !wmlNamespaces[t.Name.Space] {
				continue // not a wordprocessingml element; never stacked
			}
			switch t.Name.Local {
			case "p":
				index := -1
				if p := parent(); p == "body" || p == "tc" {
					paras = append(paras, docxViewPara{})
					index = len(paras) - 1
				}
				paraStack = append(paraStack, index)
			case "pStyle":
				// A pStyle under pPr under the innermost open paragraph is
				// that paragraph's style; a pStyle deeper in (a text box's
				// own paragraphs) belongs to a non-addressable ¶.
				if parent() == "pPr" && len(wmlStack) >= 2 && wmlStack[len(wmlStack)-2] == "p" && addressable() {
					for _, a := range t.Attr {
						if a.Name.Local == "val" && wmlNamespaces[a.Name.Space] {
							paras[paraStack[len(paraStack)-1]].Style = a.Value
						}
					}
				}
			case "t":
				captureT = addressable()
			}
			wmlStack = append(wmlStack, t.Name.Local)
		case xml.CharData:
			if captureT {
				paras[paraStack[len(paraStack)-1]].Text += string(t)
			}
		case xml.EndElement:
			if !wmlNamespaces[t.Name.Space] {
				continue
			}
			switch t.Name.Local {
			case "p":
				if len(paraStack) > 0 {
					paraStack = paraStack[:len(paraStack)-1]
				}
			case "t":
				captureT = false
			}
			if len(wmlStack) > 0 && wmlStack[len(wmlStack)-1] == t.Name.Local {
				wmlStack = wmlStack[:len(wmlStack)-1]
			}
		}
	}
}

// — PPTX —

// pptxViewDoc is the narrow seam over gopptx for the PPTX view (design D11):
// gopptx is early-stage and pinned to an exact version, so every call into it
// sits behind this interface — one adapter, primitive types — and a swap to a
// successor library is a file-local change. The shape enumeration is the
// document tools' edit engine's: all shapes of a slide in document order,
// text-less shapes included.
type pptxViewDoc interface {
	// SlideIDs returns the slides' internal IDs in presentation order.
	SlideIDs() []int
	// Shapes returns the slide's shapes in document order.
	Shapes(slideID int) ([]pptxViewShape, error)
	// Close releases the file's resources.
	Close() error
}

// pptxViewShape is one shape as the view prints it: kind is the placeholder
// type ("title", "body", …) when the shape is one, "placeholder" for a
// typeless placeholder, and "shape" otherwise.
type pptxViewShape struct {
	ID   int
	Kind string
	Text string
}

// renderPptxView renders the PPTX structured text view: slides in order, one
// line per shape, addressing them the way the edit engines do —
// slide <n>, shape <m>, both 1-based.
func renderPptxView(data []byte) (officeView, error) {
	doc, err := openGopptxView(data)
	if err != nil {
		return officeView{}, fmt.Errorf("not a readable PPTX file: %w", err)
	}
	defer doc.Close()

	slideIDs := doc.SlideIDs()
	lines := make([]string, 0, len(slideIDs))
	shapes := 0
	for pos, slideID := range slideIDs {
		slideShapes, err := doc.Shapes(slideID)
		if err != nil {
			return officeView{}, fmt.Errorf("read slide %d: %w", pos+1, err)
		}
		for m, shape := range slideShapes {
			lines = append(lines, fmt.Sprintf("slide %d: shape %d [%s] \"%s\"", pos+1, m+1, shape.Kind, escapeLine(shape.Text)))
			shapes++
		}
	}
	return officeView{
		summary: fmt.Sprintf("PPTX view: %d %s, %d %s", len(slideIDs), plural(len(slideIDs), "slide"), shapes, plural(shapes, "shape")),
		lines:   lines,
	}, nil
}

// gopptxViewDoc adapts gopptx.File to the pptxViewDoc seam. It is the only
// place the library's types appear (design D11).
type gopptxViewDoc struct{ f *gopptx.File }

// openGopptxView opens a presentation view from its package bytes.
func openGopptxView(data []byte) (pptxViewDoc, error) {
	f, err := gopptx.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return gopptxViewDoc{f: f}, nil
}

func (d gopptxViewDoc) SlideIDs() []int { return d.f.GetSlideList() }

func (d gopptxViewDoc) Shapes(slideID int) ([]pptxViewShape, error) {
	shapes, err := d.f.GetShapes(slideID)
	if err != nil {
		return nil, err
	}
	out := make([]pptxViewShape, 0, len(shapes))
	for _, shape := range shapes {
		id := 0
		kind := "shape"
		if props := shape.NonVisualShapeProperties; props != nil {
			if common := props.CommonNonVisualProperties; common != nil {
				id = common.ID
			}
			if ph := props.NonVisualProperties.Ph; ph != nil {
				if ph.Type != nil {
					kind = *ph.Type
				} else {
					kind = "placeholder"
				}
			}
		}
		out = append(out, pptxViewShape{ID: id, Kind: kind, Text: decodeShapeText(shape.TextBody)})
	}
	return out, nil
}

func (d gopptxViewDoc) Close() error { return d.f.Close() }

// decodeShapeText joins a text body's paragraphs with newlines, matching the
// edit engine's shape text.
func decodeShapeText(body *gopptx.DecodeTextBody) string {
	if body == nil {
		return ""
	}
	joined := make([]string, 0, len(body.Paragraph))
	for _, para := range body.Paragraph {
		var b strings.Builder
		for _, run := range para.Runs {
			b.WriteString(run.Text)
		}
		joined = append(joined, b.String())
	}
	return strings.Join(joined, "\n")
}

// — XLSX —

// renderXlsxView renders the XLSX cell-grid view: per sheet in workbook
// order, its populated cells in row-major order with their addresses — the
// <Sheet>!<Cell> addresses the edit engines' set_cell and clear_cell take.
func renderXlsxView(data []byte) (officeView, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return officeView{}, fmt.Errorf("not a readable XLSX file: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	lines := make([]string, 0, 64)
	cells := 0
	for _, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return officeView{}, fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		for i, row := range rows {
			for col, value := range row {
				if value == "" {
					continue
				}
				addr, err := excelize.CoordinatesToCellName(col+1, i+1)
				if err != nil {
					return officeView{}, fmt.Errorf("address sheet %q cell (%d,%d): %w", sheet, col+1, i+1, err)
				}
				lines = append(lines, fmt.Sprintf("%s!%s = %s", sheet, addr, escapeLine(value)))
				cells++
			}
		}
	}
	return officeView{
		summary: fmt.Sprintf("XLSX view: %d %s, %d populated %s", len(sheets), plural(len(sheets), "sheet"), cells, plural(cells, "cell")),
		lines:   lines,
	}, nil
}
