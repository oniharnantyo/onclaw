package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/xuri/excelize/v2"
)

// --- fakes -----------------------------------------------------------------

type fakeDocumentPublisher struct {
	workspaceID string
	name        string
	sourcePath  string
	url         string
	err         error
}

func (f *fakeDocumentPublisher) PublishCreatedDocument(_ context.Context, workspaceID, name, sourcePath string) (string, error) {
	f.workspaceID = workspaceID
	f.name = name
	f.sourcePath = sourcePath
	if f.err != nil {
		return "", f.err
	}
	if f.url == "" {
		f.url = "cap://" + name
	}
	return f.url, nil
}

type fakeHTMLRenderer struct {
	pdf  []byte
	err  error
	html string
}

func (f *fakeHTMLRenderer) RenderHTMLToPDF(_ context.Context, html string) ([]byte, error) {
	f.html = html
	if f.err != nil {
		return nil, f.err
	}
	if f.pdf == nil {
		return []byte("%PDF-1.7 fake-rendered"), nil
	}
	return f.pdf, nil
}

// --- helpers ---------------------------------------------------------------

func newDocumentCreateForDir(t *testing.T, opts ...DocumentCreateOption) (*documentCreateTool, string) {
	t.Helper()
	dir := t.TempDir()
	base, err := NewDocumentCreate(dir, &fakeDocumentPublisher{}, &fakeHTMLRenderer{}, opts...)
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}
	return base.(*documentCreateTool), dir
}

func runCreate(t *testing.T, tool *documentCreateTool, args string) (result string, err error) {
	t.Helper()
	return tool.InvokableRun(context.Background(), args)
}

// runOnBase invokes a tool through the tool.BaseTool returned by the
// constructor, via the InvokableTool surface eino invokable tools implement.
func runOnBase(t *testing.T, base tool.BaseTool, args string) (string, error) {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	if !ok {
		t.Fatalf("constructor result does not implement tool.InvokableTool")
	}
	return invokable.InvokableRun(context.Background(), args)
}

func decodeCreateResult(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	return m
}

// buildMinimalDocxTemplate writes a docx whose document.xml carries the
// placeholder split across separate w:t runs (the known Word split-run
// problem, design.md D3).
func buildMinimalDocxTemplate(t *testing.T, path, documentXML string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	zw := zip.NewWriter(f)
	entries := map[string]string{
		"[Content_Types].xml": docxContentTypes,
		"_rels/.rels":         docxRootRels,
		"word/document.xml":   documentXML,
	}
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip entry: %v", err)
		}
		if _, err := io.WriteString(w, data); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close template: %v", err)
	}
}

// --- xlsx ------------------------------------------------------------------

func TestDocumentCreate_XLSXHappyPath(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)

	out, err := runCreate(t, tool, `{"path":"/workspace/report.xlsx","format":"xlsx","data":{"sheets":[{"name":"Sales","rows":[["Region","Amount",1.5],["EU",42,true]],"bold_first_row":true}]}}`)
	if err != nil {
		t.Fatalf("create xlsx: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["result"] != "Created report.xlsx (xlsx)" || res["format"] != "xlsx" || res["name"] != "report.xlsx" {
		t.Errorf("unexpected result payload: %v", res)
	}
	if res["path"] != "/workspace/report.xlsx" {
		t.Errorf("expected mount-scoped path, got %v", res["path"])
	}

	f, err := excelize.OpenFile(filepath.Join(dir, "report.xlsx"))
	if err != nil {
		t.Fatalf("reopen xlsx: %v", err)
	}
	defer f.Close()
	if got, _ := f.GetCellValue("Sales", "A1"); got != "Region" {
		t.Errorf("A1 = %q", got)
	}
	if got, _ := f.GetCellValue("Sales", "C1"); got != "1.5" {
		t.Errorf("numeric cell C1 = %q", got)
	}
	if got, _ := f.GetCellValue("Sales", "C2"); got != "TRUE" {
		t.Errorf("boolean cell C2 = %q", got)
	}
	styleID, err := f.GetCellStyle("Sales", "A1")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	if styleID == 0 {
		t.Error("expected bold style applied to header row")
	}
	bodyStyle, _ := f.GetCellStyle("Sales", "A2")
	if bodyStyle == styleID {
		t.Error("body row should not carry the header style")
	}
}

func TestDocumentCreate_XLSXTextFallback(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	_, err := runCreate(t, tool, `{"path":"/workspace/t.csv.xlsx","format":"xlsx","data":{"text":"| A | B |\n| --- | --- |\n| one \\| two | 3 |"}}`)
	if err != nil {
		t.Fatalf("create xlsx from text: %v", err)
	}
	f, err := excelize.OpenFile(filepath.Join(dir, "t.csv.xlsx"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer f.Close()
	if got, _ := f.GetCellValue("Sheet1", "A2"); got != "one | two" {
		t.Errorf("escaped pipe cell = %q", got)
	}
	if got, _ := f.GetCellValue("Sheet1", "B2"); got != "3" {
		t.Errorf("B2 = %q", got)
	}
}

func TestDocumentCreate_XLSXTemplateFill(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)

	tpl := excelize.NewFile()
	if err := tpl.SetCellValue("Sheet1", "A1", "Total"); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	if err := tpl.SetCellValue("Sheet1", "A2", "Items"); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	templatePath := filepath.Join(dir, "invoice-template.xlsx")
	if err := tpl.SaveAs(templatePath); err != nil {
		t.Fatalf("save template: %v", err)
	}
	tpl.Close()

	args := fmt.Sprintf(`{"path":"/workspace/filled.xlsx","format":"xlsx","template":%s,"data":{"cells":{"Sheet1!B1":"Acme report","Sheet1!B2":42},"append_rows":{"Sheet1":[["a","b"],["c","d"]]}}}`, quoteJSON("/workspace/invoice-template.xlsx"))
	if _, err := runCreate(t, tool, args); err != nil {
		t.Fatalf("template fill: %v", err)
	}

	f, err := excelize.OpenFile(filepath.Join(dir, "filled.xlsx"))
	if err != nil {
		t.Fatalf("reopen filled: %v", err)
	}
	defer f.Close()
	if got, _ := f.GetCellValue("Sheet1", "B1"); got != "Acme report" {
		t.Errorf("B1 = %q", got)
	}
	if got, _ := f.GetCellValue("Sheet1", "B2"); got != "42" {
		t.Errorf("B2 = %q", got)
	}
	if got, _ := f.GetCellValue("Sheet1", "A3"); got != "a" {
		t.Errorf("append row A3 = %q", got)
	}
	if got, _ := f.GetCellValue("Sheet1", "B4"); got != "d" {
		t.Errorf("append row B4 = %q", got)
	}
	// The template file itself is untouched.
	if got, _ := func() (string, error) {
		tf, err := excelize.OpenFile(templatePath)
		if err != nil {
			return "", err
		}
		defer tf.Close()
		return tf.GetCellValue("Sheet1", "B1")
	}(); got != "" {
		t.Errorf("template B1 should be empty, got %q", got)
	}
}

func TestDocumentCreate_TemplateFromReadOnlyRoot(t *testing.T) {
	dir := t.TempDir()
	dropLane := t.TempDir() // chat-attached template mount outside the jail

	tpl := excelize.NewFile()
	templatePath := filepath.Join(dropLane, "attached-template.xlsx")
	if err := tpl.SetCellValue("Sheet1", "A1", "Hello"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := tpl.SaveAs(templatePath); err != nil {
		t.Fatalf("save: %v", err)
	}
	tpl.Close()

	tool, err := NewDocumentCreate(dir, &fakeDocumentPublisher{}, &fakeHTMLRenderer{},
		WithDocumentCreateReadOnlyRoots(dropLane), WithWorkspaceID("ws-1"))
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}
	args := fmt.Sprintf(`{"path":"/workspace/out.xlsx","format":"xlsx","template":%s,"data":{"cells":{"Sheet1!B1":"filled"}}}`, quoteJSON(templatePath))
	if _, err := runOnBase(t, tool, args); err != nil {
		t.Fatalf("template from read-only root: %v", err)
	}

	f, err := excelize.OpenFile(filepath.Join(dir, "out.xlsx"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer f.Close()
	if got, _ := f.GetCellValue("Sheet1", "B1"); got != "filled" {
		t.Errorf("B1 = %q", got)
	}
}

func TestDocumentCreate_TemplateOutsideRootsRejected(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	outside := filepath.Join(t.TempDir(), "secret.xlsx")
	if err := os.WriteFile(outside, []byte("not a zip"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	args := fmt.Sprintf(`{"path":"/workspace/out.xlsx","format":"xlsx","template":%s,"data":{"cells":{}}}`, quoteJSON(outside))
	_, err := runCreate(t, tool, args)
	if err == nil || !strings.Contains(err.Error(), "outside allowed directories") {
		t.Fatalf("expected outside-roots rejection, got %v", err)
	}
}

func TestDocumentCreate_TemplateFormatMismatchIsStructuredError(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	if err := os.WriteFile(filepath.Join(dir, "template.docx"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	args := `{"path":"/workspace/out.xlsx","format":"xlsx","template":"/workspace/template.docx","data":{"cells":{"Sheet1!A1":1}}}`
	out, err := runCreate(t, tool, args)
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["error"] == nil || !strings.Contains(res["error"].(string), "docx") {
		t.Errorf("expected format-mismatch error naming docx, got %v", res)
	}
}

// --- docx ------------------------------------------------------------------

func TestDocumentCreate_DOCXHappyPath(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	markdown := "# Quarterly Report\n\nIntro with **bold** and *italic* text.\n\n## Details\n\n- first bullet\n- second bullet\n\n| Item | Qty |\n| --- | --- |\n| Widget | 3 |\n\n### Wrap up\n\nClosing paragraph.\n"
	args := fmt.Sprintf(`{"path":"/workspace/report.docx","format":"docx","data":{"markdown":%s}}`, quoteJSON(markdown))
	out, err := runCreate(t, tool, args)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	if res := decodeCreateResult(t, out); res["format"] != "docx" {
		t.Errorf("unexpected result: %v", res)
	}

	// Zip structure: the minimal OOXML parts must exist.
	zr, err := zip.OpenReader(filepath.Join(dir, "report.docx"))
	if err != nil {
		t.Fatalf("open docx zip: %v", err)
	}
	defer zr.Close()
	want := map[string]bool{"[Content_Types].xml": false, "_rels/.rels": false, "word/document.xml": false}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("docx missing part %s", name)
		}
	}

	// Round-trip through the read tool's converter: structure must survive.
	markdownOut, err := convertDocument(context.Background(), convertDOCX, filepath.Join(dir, "report.docx"))
	if err != nil {
		t.Fatalf("convertDOCX round trip: %v", err)
	}
	for _, want := range []string{
		"# Quarterly Report", "## Details", "### Wrap up",
		"**bold**", "*italic*", "first bullet", "second bullet",
		"| Item | Qty |", "| Widget | 3 |", "Closing paragraph.",
	} {
		if !strings.Contains(markdownOut, want) {
			t.Errorf("round trip missing %q in:\n%s", want, markdownOut)
		}
	}
}

func TestDocumentCreate_DOCXTemplateSplitRunMerge(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)

	templateXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>Hello </w:t></w:r><w:r><w:t>{{na</w:t></w:r><w:r><w:t>me}}</w:t></w:r><w:r><w:t>, your total is {{total}}.</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>{{title}}</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	templatePath := filepath.Join(dir, "letter-template.docx")
	buildMinimalDocxTemplate(t, templatePath, templateXML)

	args := fmt.Sprintf(`{"path":"/workspace/letter.docx","format":"docx","template":%s,"data":{"name":"Alice","title":"Invoice Summary","total":"1,240.50"}}`, quoteJSON("/workspace/letter-template.docx"))
	if _, err := runCreate(t, tool, args); err != nil {
		t.Fatalf("docx template fill: %v", err)
	}

	markdownOut, err := convertDocument(context.Background(), convertDOCX, filepath.Join(dir, "letter.docx"))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	for _, want := range []string{"Hello Alice, your total is 1,240.50.", "# Invoice Summary"} {
		if !strings.Contains(markdownOut, want) {
			t.Errorf("merged output missing %q in:\n%s", want, markdownOut)
		}
	}
	if strings.Contains(markdownOut, "{{") {
		t.Errorf("unfilled placeholder remains: %s", markdownOut)
	}
}

func TestDocumentCreate_DOCXUnknownPlaceholderLeftVisible(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	templateXML := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>Value: {{known}} {{unknown}}</w:t></w:r></w:p></w:body></w:document>`
	templatePath := filepath.Join(dir, "t.docx")
	buildMinimalDocxTemplate(t, templatePath, templateXML)

	args := `{"path":"/workspace/out.docx","format":"docx","template":"/workspace/t.docx","data":{"known":"42"}}`
	if _, err := runCreate(t, tool, args); err != nil {
		t.Fatalf("fill: %v", err)
	}
	out, err := convertDocument(context.Background(), convertDOCX, filepath.Join(dir, "out.docx"))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if !strings.Contains(out, "Value: 42 {{unknown}}") {
		t.Errorf("expected known filled and unknown visible, got %q", out)
	}
}

func TestDocumentCreate_DOCXMissingMarkdownIsStructuredError(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	out, err := runCreate(t, tool, `{"path":"/workspace/empty.docx","format":"docx","data":{}}`)
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["error"] == nil || !strings.Contains(res["error"].(string), "empty.docx") {
		t.Errorf("expected error naming the document, got %v", res)
	}
}

// --- pptx ------------------------------------------------------------------

func TestDocumentCreate_PPTXHappyPath(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	args := `{"path":"/workspace/deck.pptx","format":"pptx","data":{"slides":[{"title":"Overview","bullets":["Point one","Point two"]},{"title":"Details","bullets":["Deep dive"],"notes":"skipped in v1"}]}}`
	out, err := runCreate(t, tool, args)
	if err != nil {
		t.Fatalf("create pptx: %v", err)
	}
	if res := decodeCreateResult(t, out); res["format"] != "pptx" {
		t.Errorf("unexpected result: %v", res)
	}

	path := filepath.Join(dir, "deck.pptx")
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open pptx zip: %v", err)
	}
	defer zr.Close()

	wantParts := map[string]bool{
		"ppt/presentation.xml":              false,
		"ppt/slideMasters/slideMaster1.xml": false,
		"ppt/slideLayouts/slideLayout1.xml": false,
		"ppt/theme/theme1.xml":              false,
		"ppt/slides/slide1.xml":             false,
		"ppt/slides/slide2.xml":             false,
		"ppt/slides/_rels/slide1.xml.rels":  false,
		"ppt/_rels/presentation.xml.rels":   false,
	}
	slideCount := 0
	for _, f := range zr.File {
		if seen, ok := wantParts[f.Name]; ok {
			wantParts[f.Name] = !seen
		}
		if _, ok := pptxSlideNumber(f.Name); ok {
			slideCount++
		}
	}
	if slideCount != 2 {
		t.Errorf("expected 2 slide parts, got %d", slideCount)
	}
	for name, seen := range wantParts {
		if !seen {
			t.Errorf("pptx missing part %s", name)
		}
	}
}

func TestDocumentCreate_PPTXRoundTripText(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	args := `{"path":"/workspace/deck.pptx","format":"pptx","data":{"slides":[{"title":"Q3 Review","bullets":["Revenue up","Churn down"]}]}}`
	if _, err := runCreate(t, tool, args); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := convertDocument(context.Background(), convertPPTX, filepath.Join(dir, "deck.pptx"))
	if err != nil {
		t.Fatalf("convertPPTX round trip: %v", err)
	}
	for _, want := range []string{"## Slide 1", "Q3 Review", "Revenue up", "Churn down"} {
		if !strings.Contains(out, want) {
			t.Errorf("round trip missing %q in:\n%s", want, out)
		}
	}
}

func TestDocumentCreate_PPTXTemplateFill(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)

	// Build the template deck through the generator itself.
	templateArgs := `{"path":"/workspace/tpl.pptx","format":"pptx","data":{"slides":[{"title":"{{title}}","bullets":["{{point}}"]}]}}`
	if _, err := runCreate(t, tool, templateArgs); err != nil {
		t.Fatalf("build template: %v", err)
	}

	fillArgs := `{"path":"/workspace/filled.pptx","format":"pptx","template":"/workspace/tpl.pptx","data":{"title":"Board Meeting","point":"Revenue up 20%"}}`
	if _, err := runCreate(t, tool, fillArgs); err != nil {
		t.Fatalf("pptx template fill: %v", err)
	}

	out, err := convertDocument(context.Background(), convertPPTX, filepath.Join(dir, "filled.pptx"))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	for _, want := range []string{"Board Meeting", "Revenue up 20%"} {
		if !strings.Contains(out, want) {
			t.Errorf("filled deck missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{{") {
		t.Errorf("unfilled placeholder remains: %s", out)
	}
}

func TestDocumentCreate_PPTXNoSlidesIsStructuredError(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	out, err := runCreate(t, tool, `{"path":"/workspace/deck.pptx","format":"pptx","data":{"slides":[]}}`)
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["error"] == nil || !strings.Contains(res["error"].(string), "deck.pptx") {
		t.Errorf("expected error naming the document, got %v", res)
	}
}

// --- pdf -------------------------------------------------------------------

func TestDocumentCreate_PDFStructuredInvoice(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	args := `{"path":"/workspace/invoice.pdf","format":"pdf","data":{"source":"structured","invoice":{"seller":"OnClaw Inc\n1 Market St","buyer":"Acme Corp","number":"INV-2026-001","date":"2026-09-12","line_items":[{"description":"Agent workspace license","qty":3,"unit_price":250},{"description":"Support plan","qty":1,"unit_price":490}],"tax_rate":10,"notes":"Payment due in 30 days."}}}`
	out, err := runCreate(t, tool, args)
	if err != nil {
		t.Fatalf("create pdf: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["result"] != "Created invoice.pdf (pdf)" {
		t.Errorf("unexpected result: %v", res)
	}

	path := filepath.Join(dir, "invoice.pdf")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Errorf("not a PDF: %q", raw[:min(8, len(raw))])
	}

	text, err := convertDocument(context.Background(), convertPDF, path)
	if err != nil {
		t.Fatalf("convertPDF round trip: %v", err)
	}
		for _, want := range []string{"INVOICE", "INV-2026-001", "OnClaw Inc", "Acme Corp", "Agent workspace license", "1,240.00", "124.00", "1,364.00", "Payment due in 30 days"} {
			if !strings.Contains(text, want) {
				t.Errorf("pdf text missing %q in:\n%s", want, text)
			}
		}
	}

func TestDocumentCreate_PDFMarkdownDocument(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	args := `{"path":"/workspace/article.pdf","format":"pdf","data":{"markdown":"# Harness Engineering for Self-Improvement\n\nBy Lilian Weng\n\n## Overview\n\n- Recursive self-improvement loops\n- Frontier research velocity\n\nThis is a full article summary rendered cleanly to PDF."}}`
	out, err := runCreate(t, tool, args)
	if err != nil {
		t.Fatalf("create pdf markdown: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["result"] != "Created article.pdf (pdf)" {
		t.Errorf("unexpected result: %v", res)
	}
	path := filepath.Join(dir, "article.pdf")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read article.pdf: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Errorf("expected PDF header, got %q", raw[:20])
	}
	text, err := convertDocument(context.Background(), convertPDF, path)
	if err != nil {
		t.Fatalf("convertPDF: %v", err)
	}
	for _, want := range []string{"Harness Engineering", "Self-Improvement", "Lilian Weng", "Recursive self-improvement", "Frontier research velocity"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in converted text:\n%s", want, text)
		}
	}
}

func TestDocumentCreate_PDFHTMLRoute(t *testing.T) {
	publisher := &fakeDocumentPublisher{}
	renderer := &fakeHTMLRenderer{pdf: []byte("%PDF-1.7 rendered-html")}
	dir := t.TempDir()
	base, err := NewDocumentCreate(dir, publisher, renderer)
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}

	args := `{"path":"/workspace/page.pdf","format":"pdf","data":{"source":"html","html":"<!DOCTYPE html><html><body><h1>Bespoke invoice</h1></body></html>"}}`
	out, err := runOnBase(t, base, args)
	if err != nil {
		t.Fatalf("html route: %v", err)
	}
	if !strings.Contains(renderer.html, "Bespoke invoice") {
		t.Errorf("renderer did not receive the HTML: %q", renderer.html)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "page.pdf"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Errorf("output is not a PDF: %q", raw)
	}
	res := decodeCreateResult(t, out)
	if res["url"] == "" {
		t.Errorf("expected capability URL on result, got %v", res)
	}
	if publisher.name != "page.pdf" || publisher.workspaceID != "" {
		t.Errorf("publisher got name=%q workspaceID=%q", publisher.name, publisher.workspaceID)
	}
}

func TestDocumentCreate_PDFHTMLRendererErrorIsStructured(t *testing.T) {
	renderer := &fakeHTMLRenderer{err: fmt.Errorf("chrome binary not found")}
	dir := t.TempDir()
	base, err := NewDocumentCreate(dir, &fakeDocumentPublisher{}, renderer)
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}
	args := `{"path":"/workspace/broken.pdf","format":"pdf","data":{"source":"html","html":"<html></html>"}}`
	out, err := runOnBase(t, base, args)
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	errMsg, _ := res["error"].(string)
	if errMsg == "" || !strings.Contains(errMsg, "structured") {
		t.Errorf("expected structured-route guidance in error, got %v", res)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "broken.pdf")); !os.IsNotExist(statErr) {
		t.Error("no output file should exist on render failure")
	}
}

func TestDocumentCreate_PDFHTMLRendererUnavailableIsStructured(t *testing.T) {
	// nil renderer encodes a deployment without headless Chrome.
	dir := t.TempDir()
	base, err := NewDocumentCreate(dir, &fakeDocumentPublisher{}, nil)
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}
	out, err := runOnBase(t, base, `{"path":"/workspace/page.pdf","format":"pdf","data":{"source":"html","html":"<html></html>"}}`)
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	errMsg, _ := res["error"].(string)
	if errMsg == "" || !strings.Contains(errMsg, "structured") {
		t.Errorf("expected Chrome-absent degradation pointing at structured route, got %v", res)
	}
}

func TestDocumentCreate_PDFTemplateRejected(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	if err := os.WriteFile(filepath.Join(dir, "invoice.pdf"), []byte("%PDF fake template"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out, err := runCreate(t, tool, `{"path":"/workspace/out.pdf","format":"pdf","template":"/workspace/invoice.pdf","data":{"source":"structured","invoice":{"line_items":[{"description":"x","qty":1,"unit_price":1}]}}}`)
	if err != nil {
		t.Fatalf("expected structured rejection, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	errMsg, _ := res["error"].(string)
	if errMsg == "" || !strings.Contains(errMsg, "xlsx or docx") {
		t.Errorf("expected rejection suggesting xlsx or docx templates, got %v", res)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out.pdf")); !os.IsNotExist(statErr) {
		t.Error("no output file should exist for a rejected pdf template request")
	}
}

func TestDocumentCreate_PDFMissingInvoiceIsStructuredError(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	out, err := runCreate(t, tool, `{"path":"/workspace/inv.pdf","format":"pdf","data":{"source":"structured"}}`)
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["error"] == nil {
		t.Errorf("expected missing-invoice error, got %v", res)
	}
}

// --- shell contract --------------------------------------------------------

func TestDocumentCreate_UnknownFormatIsParameterError(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	_, err := runCreate(t, tool, `{"path":"/workspace/x.pages","format":"pages","data":{}}`)
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected unsupported-format error, got %v", err)
	}
}

func TestDocumentCreate_OutputEscapeRejected(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	for _, path := range []string{"/workspace/../escape.xlsx", "../escape.xlsx", "/etc/passwd"} {
		_, err := runCreate(t, tool, fmt.Sprintf(`{"path":%s,"format":"xlsx","data":{"sheets":[{"rows":[["a"]]}]}}`, quoteJSON(path)))
		if err == nil {
			t.Errorf("%q: expected escape rejection", path)
			continue
		}
		if !strings.Contains(err.Error(), "escape attempt") && !strings.Contains(err.Error(), "absolute paths are not allowed") {
			t.Errorf("%q: unexpected rejection copy: %v", path, err)
		}
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) != 0 {
		t.Errorf("jail must stay empty after rejections, got %v", entries)
	}
}

func TestDocumentCreate_OutputSymlinkEscapeRejected(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	outside := filepath.Join(t.TempDir(), "victim.xlsx")
	link := filepath.Join(dir, "escape.xlsx")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := runCreate(t, tool, `{"path":"/workspace/escape.xlsx","format":"xlsx","data":{"sheets":[{"rows":[["a"]]}]}}`)
	if err == nil || !strings.Contains(err.Error(), "escapes jail") {
		t.Fatalf("expected symlink-escape rejection, got %v", err)
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Error("symlink target must not be written")
	}
}

func TestDocumentCreate_OutputDirectoryMissingIsError(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	_, err := runCreate(t, tool, `{"path":"/workspace/absent/dir/out.xlsx","format":"xlsx","data":{"sheets":[{"rows":[["a"]]}]}}`)
	if err == nil || !strings.Contains(err.Error(), "output directory does not exist") {
		t.Fatalf("expected missing-directory error, got %v", err)
	}
}

func TestDocumentCreate_OutputDirectoryRejected(t *testing.T) {
	tool, dir := newDocumentCreateForDir(t)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := runCreate(t, tool, `{"path":"/workspace/subdir","format":"xlsx","data":{"sheets":[{"rows":[["a"]]}]}}`)
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected directory rejection, got %v", err)
	}
}

func TestDocumentCreate_WorkspaceRootRejected(t *testing.T) {
	tool, _ := newDocumentCreateForDir(t)
	_, err := runCreate(t, tool, `{"path":"/workspace","format":"xlsx","data":{}}`)
	if err == nil || !strings.Contains(err.Error(), "cannot write the workspace root") {
		t.Fatalf("expected workspace-root rejection, got %v", err)
	}
}

func TestDocumentCreate_PublisherBindsWorkspaceIDAndPath(t *testing.T) {
	publisher := &fakeDocumentPublisher{}
	dir := t.TempDir()
	base, err := NewDocumentCreate(dir, publisher, &fakeHTMLRenderer{}, WithWorkspaceID("ws-42"))
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}
	out, err := runOnBase(t, base, `{"path":"/workspace/deck.pptx","format":"pptx","data":{"slides":[{"title":"T","bullets":["b"]}]}}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if publisher.workspaceID != "ws-42" || publisher.name != "deck.pptx" {
		t.Errorf("publisher got workspaceID=%q name=%q", publisher.workspaceID, publisher.name)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve dir: %v", err)
	}
	if !strings.HasSuffix(publisher.sourcePath, "deck.pptx") || !strings.HasPrefix(publisher.sourcePath, resolvedDir+string(filepath.Separator)) {
		t.Errorf("publisher sourcePath should be the resolved jail path, got %q", publisher.sourcePath)
	}
	res := decodeCreateResult(t, out)
	if res["url"] == "" {
		t.Errorf("expected capability URL, got %v", res)
	}
}

func TestDocumentCreate_PublisherErrorStillReturnsCreatedResult(t *testing.T) {
	publisher := &fakeDocumentPublisher{err: fmt.Errorf("storage unavailable")}
	dir := t.TempDir()
	base, err := NewDocumentCreate(dir, publisher, &fakeHTMLRenderer{})
	if err != nil {
		t.Fatalf("NewDocumentCreate: %v", err)
	}
	out, err := runOnBase(t, base, `{"path":"/workspace/doc.docx","format":"docx","data":{"markdown":"# Hi"}}`)
	if err != nil {
		t.Fatalf("create must succeed even when delivery fails: %v", err)
	}
	res := decodeCreateResult(t, out)
	if res["result"] != "Created doc.docx (docx)" || res["url"] != "" {
		t.Errorf("expected created result without url, got %v", res)
	}
}

func TestNewDocumentCreate_Construction(t *testing.T) {
	if _, err := NewDocumentCreate("", nil, nil); err == nil {
		t.Error("expected empty-dir construction error")
	}
	if _, err := NewDocumentCreate(filepath.Join(t.TempDir(), "absent"), nil, nil); err == nil {
		t.Error("expected missing-dir construction error")
	}
	file := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewDocumentCreate(file, nil, nil); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected not-a-directory construction error, got %v", err)
	}
}
