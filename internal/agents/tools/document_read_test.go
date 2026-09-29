package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
)

// documentReadResult is the decoded result JSON of a document.read invocation.
type documentReadResult struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
	Error    string `json:"error"`
	Note     string `json:"note"`
	Pages    string `json:"pages"`
	Section  string `json:"section"`
}

// newTestDocumentRead builds the tool over a fresh temp agent workspace.
func newTestDocumentRead(t *testing.T, opts ...DocumentReadOption) (tool.BaseTool, string) {
	t.Helper()
	agentDir := t.TempDir()
	tr, err := NewDocumentRead(agentDir, opts...)
	if err != nil {
		t.Fatalf("NewDocumentRead: %v", err)
	}
	return tr, agentDir
}

// writeAgentFile writes a fixture into the agent workspace and returns its path.
func writeAgentFile(t *testing.T, agentDir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(agentDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
	return path
}

// invokableTool is the slice of the eino tool surface document.read tests call.
type invokableTool interface {
	InvokableRun(context.Context, string, ...tool.Option) (string, error)
}

// runDocumentRead invokes the tool and decodes its JSON result payload.
func runDocumentRead(t *testing.T, tr tool.BaseTool, userPath string) (string, error, documentReadResult) {
	t.Helper()
	return runDocumentReadArgs(t, tr, map[string]string{"path": userPath})
}

// runDocumentReadArgs invokes the tool with a full argument map (the scoped
// reads' entry point) and decodes its JSON result payload.
func runDocumentReadArgs(t *testing.T, tr tool.BaseTool, args map[string]string) (string, error, documentReadResult) {
	t.Helper()
	inv, ok := tr.(invokableTool)
	if !ok {
		t.Fatalf("tool %T does not implement InvokableRun", tr)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), string(raw))
	var result documentReadResult
	if err == nil {
		// Parameter-error paths return no payload; only decode real results.
		if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
			t.Fatalf("decode result JSON %q: %v", out, jsonErr)
		}
	}
	return out, err, result
}

// --- Fixtures ---------------------------------------------------------------

// writeTestPDF builds a one-page PDF with fpdf containing text (or no text at
// all for the scanned-PDF fixture).
func writeTestPDF(t *testing.T, agentDir, name, text string) string {
	t.Helper()
	return writeTestMultiPagePDF(t, agentDir, name, text)
}

// writeTestMultiPagePDF builds a multi-page PDF with fpdf, one optional text
// line per page.
func writeTestMultiPagePDF(t *testing.T, agentDir, name string, pages ...string) string {
	t.Helper()
	doc := fpdf.New("P", "mm", "A4", "")
	for _, text := range pages {
		doc.AddPage()
		if text != "" {
			doc.SetFont("helvetica", "", 14)
			doc.Text(10, 20, text)
		}
	}
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatalf("build pdf fixture: %v", err)
	}
	return writeAgentFile(t, agentDir, name, buf.Bytes())
}

// writeTestOOXML writes a minimal OOXML zip: an archive of name→XML entries.
func writeTestOOXML(t *testing.T, agentDir, name string, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for zipPath, xmlBody := range entries {
		w, err := zw.Create(zipPath)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", zipPath, err)
		}
		if _, err := w.Write([]byte(xmlBody)); err != nil {
			t.Fatalf("write zip entry %s: %v", zipPath, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return writeAgentFile(t, agentDir, name, buf.Bytes())
}

// writeTestXLSX builds a two-sheet workbook with excelize.
func writeTestXLSX(t *testing.T, agentDir, name string) string {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetCellValue("Sheet1", "A1", "region"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "sales"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "A2", "north"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B2", "42"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	if _, err := f.NewSheet("Notes"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	if err := f.SetCellValue("Notes", "A1", "remember"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return writeAgentFile(t, agentDir, name, buf.Bytes())
}

// --- Happy paths -------------------------------------------------------------

func TestDocumentReadPDF(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestPDF(t, agentDir, "report.pdf", "Hello OnClaw PDF extraction")

	_, err, result := runDocumentRead(t, tr, path)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if result.Name != "report.pdf" {
		t.Errorf("Name = %q, want report.pdf", result.Name)
	}
	if !strings.Contains(result.Markdown, "Hello OnClaw") {
		t.Errorf("Markdown = %q, want it to contain extracted text", result.Markdown)
	}
}

func TestDocumentReadScannedPDFIsEmptyResult(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeTestPDF(t, agentDir, "scan.pdf", "")

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "scan.pdf"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	want := "No extractable text found in scan.pdf (scanned or image-only document)."
	if result.Markdown != want {
		t.Errorf("Markdown = %q, want %q", result.Markdown, want)
	}
}

func TestDocumentReadDOCX(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	const documentXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Quarterly Report</w:t></w:r></w:p>
    <w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Bold intro</w:t></w:r><w:r><w:rPr><w:i/></w:rPr><w:t>Italic tail</w:t></w:r></w:p>
    <w:p><w:r><w:t>Plain paragraph</w:t></w:r></w:p>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>Name</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Value</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>alpha</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>pipe | cell</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:body>
</w:document>`
	writeTestOOXML(t, agentDir, "spec.docx", map[string]string{"word/document.xml": documentXML})

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "spec.docx"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	md := result.Markdown
	for _, want := range []string{
		"# Quarterly Report",
		"**Bold intro**",
		"*Italic tail*",
		"Plain paragraph",
		"| Name | Value |",
		"| --- | --- |",
		"| alpha | pipe \\| cell |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
}

func TestDocumentReadXLSX(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeTestXLSX(t, agentDir, "book.xlsx")

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "book.xlsx"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	md := result.Markdown
	for _, want := range []string{
		"## Sheet1",
		"| region | sales |",
		"| --- | --- |",
		"| north | 42 |",
		"## Notes",
		"| remember |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Index(md, "## Notes") < strings.Index(md, "## Sheet1") {
		t.Errorf("Notes sheet should follow Sheet1:\n%s", md)
	}
}

func TestDocumentReadPPTX(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	slide := func(lines ...string) string {
		var b strings.Builder
		b.WriteString(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree>`)
		for _, line := range lines {
			b.WriteString(`<p:sp><p:txBody><a:p><a:r><a:t>` + line + `</a:t></a:r></a:p></p:txBody></p:sp>`)
		}
		b.WriteString(`</p:spTree></p:cSld></p:sld>`)
		return b.String()
	}
	writeTestOOXML(t, agentDir, "deck.pptx", map[string]string{
		"ppt/slides/slide2.xml": slide("Closing thoughts"),
		"ppt/slides/slide1.xml": slide("Welcome to the deck", "Second bullet line"),
		"[Content_Types].xml":   `<Types/>`,
	})

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "deck.pptx"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	md := result.Markdown
	slide1 := strings.Index(md, "## Slide 1")
	slide2 := strings.Index(md, "## Slide 2")
	if slide1 < 0 || slide2 < 0 {
		t.Fatalf("Markdown missing slide headings:\n%s", md)
	}
	if slide2 < slide1 {
		t.Errorf("slides not sorted numerically:\n%s", md)
	}
	if !strings.Contains(md, "Welcome to the deck") || !strings.Contains(md, "Second bullet line") || !strings.Contains(md, "Closing thoughts") {
		t.Errorf("Markdown missing slide text:\n%s", md)
	}
}

func TestDocumentReadHTML(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	htmlDoc := `<!DOCTYPE html>
<html><head><title>Ignored</title><script>var x = "stripped";</script></head>
<body>
<h1>Main Title</h1>
<p>Intro with <strong>bold</strong> and <em>emphasis</em> text.</p>
<ul><li>first item</li><li>second item</li></ul>
<table><tr><th>Col A</th><th>Col B</th></tr><tr><td>1</td><td><strong>two</strong></td></tr></table>
<h2>Section</h2>
<p>Trailing paragraph.</p>
</body></html>`
	writeAgentFile(t, agentDir, "page.html", []byte(htmlDoc))

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "page.html"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	md := result.Markdown
	for _, want := range []string{
		"# Main Title",
		"**bold**",
		"*emphasis*",
		"- first item",
		"- second item",
		"| Col A | Col B |",
		"| --- | --- |",
		"| 1 | **two** |",
		"## Section",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
	for _, stripped := range []string{"<script>", "var x", "<title>"} {
		if strings.Contains(md, stripped) {
			t.Errorf("Markdown should not contain script/title content %q:\n%s", stripped, md)
		}
	}
}

func TestDocumentReadCSV(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	csvData := "city,population\n\"Portland, OR\",650000\nAustin|Texas,960000\n"
	writeAgentFile(t, agentDir, "cities.csv", []byte(csvData))

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "cities.csv"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	md := result.Markdown
	for _, want := range []string{
		"| city | population |",
		"| --- | --- |",
		"| Portland, OR | 650000 |",
		"| Austin\\|Texas | 960000 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
}

func TestDocumentReadTSV(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	tsvData := "key\tvalue\nalpha\tone\nbeta\ttwo\n"
	writeAgentFile(t, agentDir, "pairs.tsv", []byte(tsvData))

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "pairs.tsv"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	md := result.Markdown
	for _, want := range []string{"| key | value |", "| alpha | one |", "| beta | two |"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
}

// --- Path jail ----------------------------------------------------------------

func TestDocumentReadPathTraversalRejected(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeAgentFile(t, agentDir, "inside.txt", []byte("x"))

	for _, tc := range []struct {
		name string
		path string
	}{
		{"dotdot-relative", "../outside.txt"},
		{"dotdot-in-mount", "/workspace/../secrets.txt"},
		{"dotdot-buried", "docs/../../secrets.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"path": tc.path})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			_, runErr := tr.(invokableTool).InvokableRun(context.Background(), string(args))
			if runErr == nil {
				t.Fatalf("expected error for %q, got none", tc.path)
			}
			if !strings.Contains(runErr.Error(), "..") {
				t.Errorf("error %q should name the escape attempt", runErr)
			}
		})
	}
}

func TestDocumentReadOutsideJailRejected(t *testing.T) {
	tr, _ := newTestDocumentRead(t)
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outside, []byte("pdf bytes"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	args, err := json.Marshal(map[string]string{"path": outside})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, runErr := tr.(invokableTool).InvokableRun(context.Background(), string(args))
	if runErr == nil {
		t.Fatalf("expected rejection of host path outside jail")
	}
	if !strings.Contains(runErr.Error(), "outside allowed directories") {
		t.Errorf("error = %q, want outside-allowed-directories rejection", runErr)
	}
}

func TestDocumentReadSymlinkEscapeRejected(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "leaked.csv")
	if err := os.WriteFile(outsideFile, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(agentDir, "innocent.csv")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	args, err := json.Marshal(map[string]string{"path": link})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, runErr := tr.(invokableTool).InvokableRun(context.Background(), string(args))
	if runErr == nil {
		t.Fatalf("expected symlink escape rejection")
	}
	if !strings.Contains(runErr.Error(), "outside allowed directories") {
		t.Errorf("error = %q, want outside-allowed-directories rejection", runErr)
	}
}

func TestDocumentReadReadOnlyRoots(t *testing.T) {
	agentDir := t.TempDir()
	root := t.TempDir()
	inRoot := filepath.Join(root, "drop.csv")
	if err := os.WriteFile(inRoot, []byte("id,label\n1,ok\n"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}

	tr, err := NewDocumentRead(agentDir, WithReadOnlyRoots(root))
	if err != nil {
		t.Fatalf("NewDocumentRead: %v", err)
	}

	_, _, result := runDocumentRead(t, tr, inRoot)
	if result.Error != "" {
		t.Fatalf("read-only root file rejected: %s", result.Error)
	}
	if !strings.Contains(result.Markdown, "| id | label |") {
		t.Errorf("Markdown = %q, want the csv table", result.Markdown)
	}
}

// --- references mount resolution (the references/<name> contract) ------------

// newReferencesRoot builds a read-only root whose base directory is named
// "references" (the run's references mount shape) with the given files.
func newReferencesRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "references")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir references root: %v", err)
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// invokeDocumentReadRaw invokes the tool with a raw path argument and returns
// the Go error (resolve failures are errors, not structured results).
func invokeDocumentReadRaw(t *testing.T, tr tool.BaseTool, userPath string) error {
	t.Helper()
	args, err := json.Marshal(map[string]string{"path": userPath})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, runErr := tr.(invokableTool).InvokableRun(context.Background(), string(args))
	return runErr
}

// TestDocumentReadReferencesMountResolvesMd reads a mounted markdown document
// through the documented references/<document name> form: the single read
// passes the md bytes through as the markdown payload, and the section
// parameter slices an ATX heading's section (add-reference-documents D5).
func TestDocumentReadReferencesMountResolvesMd(t *testing.T) {
	root := newReferencesRoot(t, map[string]string{
		"manual.md": "# Smoke Manual\n\nIntro text.\n\n## Signing Keys\n\nRotate the signing keys monthly.\n",
	})
	tr, _ := newTestDocumentRead(t, WithReadOnlyRoots(root))

	_, err, result := runDocumentRead(t, tr, "references/manual.md")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if want := "# Smoke Manual\n\nIntro text.\n\n## Signing Keys\n\nRotate the signing keys monthly."; result.Markdown != want {
		t.Errorf("Markdown = %q, want the md bytes verbatim", result.Markdown)
	}

	_, err, result = runDocumentReadArgs(t, tr, map[string]string{"path": "references/manual.md", "section": "Signing Keys"})
	if err != nil {
		t.Fatalf("section InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected section error: %s", result.Error)
	}
	if want := "## Signing Keys\n\nRotate the signing keys monthly."; result.Markdown != want {
		t.Errorf("section Markdown = %q, want %q", result.Markdown, want)
	}
	if result.Section != "Signing Keys" {
		t.Errorf("Section echo = %q, want \"Signing Keys\"", result.Section)
	}
}

// TestDocumentReadReferencesMountResolvesTxt reads a mounted plain-text
// document through the references/ path form.
func TestDocumentReadReferencesMountResolvesTxt(t *testing.T) {
	root := newReferencesRoot(t, map[string]string{
		"notes.txt": "plain runbook text\nline two\n",
	})
	tr, _ := newTestDocumentRead(t, WithReadOnlyRoots(root))

	_, err, result := runDocumentRead(t, tr, "references/notes.txt")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if want := "plain runbook text\nline two"; result.Markdown != want {
		t.Errorf("Markdown = %q, want %q", result.Markdown, want)
	}
}

// TestDocumentReadReferencesMountIsComponentExact pins the resolution rules:
// a root whose name merely STARTS with "references" never captures the
// references/ path form, and a missing file inside the real references root
// is a no-such-file rather than a silent fallback into the agent workspace.
func TestDocumentReadReferencesMountIsComponentExact(t *testing.T) {
	decoy := filepath.Join(t.TempDir(), "references-archive")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatalf("mkdir decoy root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "decoy.md"), []byte("# Decoy\n"), 0o644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}
	root := newReferencesRoot(t, nil)
	tr, _ := newTestDocumentRead(t, WithReadOnlyRoots(decoy, root))

	// With only the prefix-named decoy present, the path stays agent-relative
	// (where nothing exists) — the decoy must not be consulted.
	if runErr := invokeDocumentReadRaw(t, tr, "references/decoy.md"); runErr == nil || !strings.Contains(runErr.Error(), "no such file") {
		t.Errorf("error = %v, want no-such-file without consulting the decoy root", runErr)
	}

	// Adding the real references root does not conjure absent files.
	tr2, _ := newTestDocumentRead(t, WithReadOnlyRoots(decoy, root))
	if runErr := invokeDocumentReadRaw(t, tr2, "references/absent.md"); runErr == nil || !strings.Contains(runErr.Error(), "no such file") {
		t.Errorf("error = %v, want no-such-file for an absent references file", runErr)
	}
}

// TestDocumentReadReferencesMountJail keeps the jail intact over the new path
// form: traversal attempts are rejected up front and symlinks inside the
// references root cannot leak outside the allowed roots.
func TestDocumentReadReferencesMountJail(t *testing.T) {
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "escape.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	root := newReferencesRoot(t, nil)
	if err := os.Symlink(outsideFile, filepath.Join(root, "leaked.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	tr, _ := newTestDocumentRead(t, WithReadOnlyRoots(root))

	if runErr := invokeDocumentReadRaw(t, tr, "references/../../escape.txt"); runErr == nil {
		t.Fatal("expected traversal through the references form to be rejected")
	} else if !strings.Contains(runErr.Error(), "..") {
		t.Errorf("error %q should name the escape attempt", runErr)
	}

	if runErr := invokeDocumentReadRaw(t, tr, "references/leaked.txt"); runErr == nil {
		t.Fatal("expected symlink escape through the references form to be rejected")
	} else if !strings.Contains(runErr.Error(), "outside allowed directories") {
		t.Errorf("error = %q, want outside-allowed-directories rejection", runErr)
	}
}

// --- Output contract ------------------------------------------------------------

func TestDocumentReadTruncatesOver200KB(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	big := strings.Repeat("<p>word </p>", 60000) // ~420 KB of paragraph text
	writeAgentFile(t, agentDir, "big.html", []byte("<html><body>"+big+"</body></html>"))

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "big.html"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if !strings.HasSuffix(result.Markdown, truncationDocumentReadNotice) {
		t.Errorf("Markdown should end with the truncation notice")
	}
	if len(result.Markdown) > MaxDocumentReadOutputBytes+len(truncationDocumentReadNotice) {
		t.Errorf("Markdown length = %d, exceeds the cap", len(result.Markdown))
	}
}

// --- Structured failure results (design.md D3) -----------------------------------

func TestDocumentReadCorruptDocumentStructuredFailure(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeAgentFile(t, agentDir, "broken.docx", []byte("this is definitely not a zip archive"))

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "broken.docx"))
	if err != nil {
		t.Fatalf("corrupt document must be a structured result, not a Go error: %v", err)
	}
	if result.Error == "" {
		t.Fatalf("expected an error field in the result: %+v", result)
	}
	if !strings.Contains(result.Error, "broken.docx") {
		t.Errorf("error %q should name the document", result.Error)
	}
	if result.Name != "broken.docx" {
		t.Errorf("Name = %q, want broken.docx", result.Name)
	}
}

func TestDocumentReadUnsupportedExtensionStructuredFailure(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeAgentFile(t, agentDir, "blob.xyz", []byte("opaque bytes"))

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "blob.xyz"))
	if err != nil {
		t.Fatalf("unsupported format must be a structured result, not a Go error: %v", err)
	}
	if result.Error == "" {
		t.Fatalf("expected an error field in the result: %+v", result)
	}
	if !strings.Contains(result.Error, "blob.xyz") || !strings.Contains(result.Error, ".xyz") {
		t.Errorf("error %q should name the document and format", result.Error)
	}
	for _, format := range supportedDocumentFormats() {
		if !strings.Contains(result.Error, format) {
			t.Errorf("error %q should suggest supported format %q", result.Error, format)
		}
	}
}

func TestDocumentReadConversionDeadline(t *testing.T) {
	agentDir := t.TempDir()
	tr, err := NewDocumentRead(agentDir, WithDocumentReadTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewDocumentRead: %v", err)
	}
	writeAgentFile(t, agentDir, "slow.txt", []byte("never converted"))

	// Inject a converter for .txt that sleeps past the deadline and watch the
	// structured timeout result come back instead of a Go error.
	toolImpl := tr.(*documentReadTool)
	toolImpl.converters[".txt"] = func(ctx context.Context, path string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
			return "too late", nil
		}
	}

	_, runErr, result := runDocumentRead(t, tr, filepath.Join(agentDir, "slow.txt"))
	if runErr != nil {
		t.Fatalf("deadline must be a structured result, not a Go error: %v", runErr)
	}
	if !strings.Contains(result.Error, "timed out") || !strings.Contains(result.Error, "slow.txt") {
		t.Errorf("error = %q, want a timeout naming the document", result.Error)
	}
}

func TestDocumentReadParentCancellationPropagates(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeAgentFile(t, agentDir, "gone.txt", []byte("data"))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	args, err := json.Marshal(map[string]string{"path": filepath.Join(agentDir, "gone.txt")})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	tr.(*documentReadTool).converters[".txt"] = func(ctx context.Context, path string) (string, error) {
		return "should not be reached", nil
	}
	_, runErr := tr.(invokableTool).InvokableRun(cancelled, string(args))
	if runErr == nil {
		t.Fatalf("parent cancellation should propagate as a Go error")
	}
}

func TestNewDocumentRead_Construction(t *testing.T) {
	if _, err := NewDocumentRead(""); err == nil {
		t.Error("expected empty-dir construction error")
	}
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := NewDocumentRead(missing); err != nil {
		t.Errorf("missing dir should self-heal at construction, got %v", err)
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Errorf("missing dir should be created at construction, stat err: %v", err)
	}
	file := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewDocumentRead(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected not-a-directory construction error, got %v", err)
	}
}

// --- PDF page anchors (add-reference-documents tasks 3.1) ----------------------

func TestConvertPDFEmitsPageAnchors(t *testing.T) {
	_, agentDir := newTestDocumentRead(t)
	path := writeTestMultiPagePDF(t, agentDir, "manual.pdf",
		"Alpha page body", "Beta page body", "Gamma page body")

	md, err := convertPDF(context.Background(), path)
	if err != nil {
		t.Fatalf("convertPDF: %v", err)
	}

	// One anchor per page, each on its own line, numbering 1-based in order.
	if got := strings.Count(md, "<!-- onclaw:page "); got != 3 {
		t.Fatalf("markdown has %d anchors, want 3:\n%s", got, md)
	}
	prev := -1
	for i, want := range []string{
		"<!-- onclaw:page 1 -->",
		"<!-- onclaw:page 2 -->",
		"<!-- onclaw:page 3 -->",
	} {
		at := strings.Index(md, want)
		if at < 0 {
			t.Fatalf("markdown missing anchor %q:\n%s", want, md)
		}
		if at <= prev {
			t.Errorf("anchor %d out of order:\n%s", i+1, md)
		}
		if at > 0 && md[at-1] != '\n' {
			t.Errorf("anchor %d is not at the start of its own line:\n%s", i+1, md)
		}
		if end := at + len(want); end >= len(md) || md[end] != '\n' {
			t.Errorf("anchor %d is not on its own line:\n%s", i+1, md)
		}
		prev = at
	}

	// Anchors precede every page's text, including page 1.
	firstText := strings.Index(md, "Alpha page body")
	firstAnchor := strings.Index(md, "<!-- onclaw:page 1 -->")
	if firstAnchor != 0 || firstText < firstAnchor {
		t.Errorf("page 1 text must follow its anchor:\n%s", md)
	}

	// Stripping restores the plain page-per-paragraph output.
	stripped := strings.TrimSpace(StripPageMarkers(md))
	if want := "Alpha page body\n\nBeta page body\n\nGamma page body"; stripped != want {
		t.Errorf("StripPageMarkers = %q, want %q", stripped, want)
	}
}

func TestDocumentReadPDFStripsPageAnchorsFromOutput(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeTestMultiPagePDF(t, agentDir, "book.pdf",
		"One body", "Two body")

	_, err, result := runDocumentRead(t, tr, filepath.Join(agentDir, "book.pdf"))
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if strings.Contains(result.Markdown, "onclaw:page") || strings.Contains(result.Markdown, "<!--") {
		t.Errorf("tool output must stay anchor-free:\n%s", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "One body") || !strings.Contains(result.Markdown, "Two body") {
		t.Errorf("Markdown = %q, want both pages' text", result.Markdown)
	}
}

func TestStripPageMarkers(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"anchors-with-text", "<!-- onclaw:page 1 -->\nA\n\n<!-- onclaw:page 2 -->\nB", "A\n\nB"},
		{"anchors-only", "<!-- onclaw:page 1 -->\n<!-- onclaw:page 2 -->", ""},
		{"no-anchors", "plain\n\ntext", "plain\n\ntext"},
		{"indented-anchor", "  <!-- onclaw:page 3 -->\nx", "x"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripPageMarkers(tc.in); got != tc.want {
				t.Errorf("StripPageMarkers(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParsePageSegments(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []PageSegment
	}{
		{"empty", "", nil},
		{"per-page-bodies", "<!-- onclaw:page 1 -->\nA\n\n<!-- onclaw:page 2 -->\nB\n",
			[]PageSegment{{Page: 1, Body: "A"}, {Page: 2, Body: "B"}}},
		{"leading-content-joins-first-page", "lead\n<!-- onclaw:page 3 -->\nbody3",
			[]PageSegment{{Page: 3, Body: "lead\nbody3"}}},
		{"no-anchors-single-page-one", "whole text",
			[]PageSegment{{Page: 1, Body: "whole text"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePageSegments(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("ParsePageSegments(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("segment %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- Shared conversion entry point (references upload pipeline) ---------------

func TestConvertDocumentByExtension(t *testing.T) {
	t.Run("pdf bytes convert with anchors intact", func(t *testing.T) {
		pdfPath := writeTestMultiPagePDF(t, t.TempDir(), "unused.pdf", "Solo page body")
		data, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatalf("read pdf fixture: %v", err)
		}
		md, err := ConvertDocument("manual.pdf", data)
		if err != nil {
			t.Fatalf("ConvertDocument: %v", err)
		}
		if !strings.Contains(md, "<!-- onclaw:page 1 -->") || !strings.Contains(md, "Solo page body") {
			t.Errorf("markdown = %q, want anchor + page text", md)
		}
	})

	t.Run("extension match is case-insensitive", func(t *testing.T) {
		pdfPath := writeTestMultiPagePDF(t, t.TempDir(), "unused.pdf", "Case page")
		data, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatalf("read pdf fixture: %v", err)
		}
		if _, err := ConvertDocument("Manual.PDF", data); err != nil {
			t.Errorf("ConvertDocument(Manual.PDF): %v", err)
		}
	})

	t.Run("unsupported extension errors naming supported formats", func(t *testing.T) {
		_, err := ConvertDocument("blob.xyz", []byte("opaque bytes"))
		if err == nil {
			t.Fatal("expected error for xyz (no converter registered)")
		}
		for _, format := range DocumentConverterExtensions() {
			if !strings.Contains(err.Error(), format) {
				t.Errorf("error %q should name supported format %q", err.Error(), format)
			}
		}
	})

	t.Run("corrupt document errors", func(t *testing.T) {
		if _, err := ConvertDocument("broken.pdf", []byte("definitely not a pdf")); err == nil {
			t.Fatal("expected error for corrupt pdf bytes")
		}
	})
}

func TestDocumentConverterExtensions(t *testing.T) {
	exts := DocumentConverterExtensions()
	want := []string{"csv", "docx", "htm", "html", "md", "pdf", "pptx", "tsv", "txt", "xlsx"}
	if len(exts) != len(want) {
		t.Fatalf("extensions = %v, want %v", exts, want)
	}
	for i := range want {
		if exts[i] != want[i] {
			t.Errorf("extensions[%d] = %q, want %q (sorted, dotless)", i, exts[i], want[i])
		}
	}
}

// --- Scoped reads: pages (add-reference-documents D5 / delta scenarios) -----

func TestDocumentReadPagesSlice(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestMultiPagePDF(t, agentDir, "manual.pdf",
		"Alpha page body", "Beta page body", "Gamma page body")

	out, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "2"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if want := "## Page 2\n\nBeta page body"; result.Markdown != want {
		t.Errorf("Markdown = %q, want %q", result.Markdown, want)
	}
	if result.Pages != "2" {
		t.Errorf("Pages echo = %q, want \"2\"", result.Pages)
	}
	if strings.Contains(result.Markdown, "Alpha") || strings.Contains(result.Markdown, "Gamma") {
		t.Errorf("pages read leaked unrequested pages:\n%s", result.Markdown)
	}
	if strings.Contains(out, "<!--") {
		t.Errorf("pages output must stay anchor-free")
	}
}

func TestDocumentReadPagesRangeAndCombination(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	pages := []string{"one", "two", "three", "four", "five", "six", "seven"}
	path := writeTestMultiPagePDF(t, agentDir, "book.pdf", pages...)

	out, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "1,3,5-7"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	for _, want := range []string{
		"## Page 1", "one",
		"## Page 3", "three",
		"## Page 5", "five",
		"## Page 6", "six",
		"## Page 7", "seven",
	} {
		if !strings.Contains(result.Markdown, want) {
			t.Errorf("Markdown missing %q:\n%s", want, result.Markdown)
		}
	}
	for _, absent := range []string{"## Page 2", "two", "## Page 4", "four"} {
		if strings.Contains(result.Markdown, absent) {
			t.Errorf("Markdown must not contain unrequested %q:\n%s", absent, result.Markdown)
		}
	}
	if result.Pages != "1,3,5-7" {
		t.Errorf("Pages echo = %q, want \"1,3,5-7\"", result.Pages)
	}
	if strings.Contains(out, "<!--") {
		t.Errorf("pages output must stay anchor-free")
	}

	// An inclusive range renders every page in it, echo normalized.
	_, err, result = runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "2-4"})
	if err != nil {
		t.Fatalf("range InvokableRun: %v", err)
	}
	if result.Pages != "2-4" {
		t.Errorf("range Pages echo = %q, want \"2-4\"", result.Pages)
	}
	if !strings.Contains(result.Markdown, "## Page 2") || !strings.Contains(result.Markdown, "## Page 4") {
		t.Errorf("range Markdown missing pages:\n%s", result.Markdown)
	}
}

func TestDocumentReadPagesOutOfRangeDropped(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestMultiPagePDF(t, agentDir, "short.pdf", "Alpha", "Beta", "Gamma")

	// Partially out of range: out-of-range pages drop, present ones render.
	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "2-10"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if result.Pages != "2-3" {
		t.Errorf("Pages echo = %q, want the effective \"2-3\"", result.Pages)
	}
	if !strings.Contains(result.Markdown, "## Page 2") || !strings.Contains(result.Markdown, "## Page 3") {
		t.Errorf("Markdown missing pages 2-3:\n%s", result.Markdown)
	}

	// Entirely out of range: structured not-found naming the document.
	_, err, result = runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "10-12"})
	if err != nil {
		t.Fatalf("not-found must be a structured result, not a Go error: %v", err)
	}
	if result.Error == "" {
		t.Fatalf("expected a structured not-found error: %+v", result)
	}
	if !strings.Contains(result.Error, "short.pdf") {
		t.Errorf("error %q should name the document", result.Error)
	}
}

func TestDocumentReadPagesOnDocxNote(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestOOXML(t, agentDir, "spec.docx", map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Plain</w:t></w:r></w:p></w:body></w:document>`,
	})

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "1-3"})
	if err != nil {
		t.Fatalf("pages on a docx must be a structured note, not a Go error: %v", err)
	}
	if result.Note == "" {
		t.Fatalf("expected a note field: %+v", result)
	}
	if !strings.Contains(result.Note, "PDFs only") || !strings.Contains(result.Note, "section") {
		t.Errorf("note %q should name the limitation and the section alternative", result.Note)
	}
	if result.Markdown != "" {
		t.Errorf("note result carries no markdown, got %q", result.Markdown)
	}
}

// TestDocumentReadPagesOnUnsupportedFormatNotes pins the check order: the
// page-scope mismatch is a request-shape matter, answered with the note before
// the converter registry is consulted.
func TestDocumentReadPagesOnUnsupportedFormatNotes(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeAgentFile(t, agentDir, "blob.xyz", []byte("opaque bytes"))

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "1"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Note == "" {
		t.Fatalf("expected the pages note for a non-PDF, got: %+v", result)
	}
}

func TestDocumentReadPagesParameterValidation(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestMultiPagePDF(t, agentDir, "manual.pdf", "Alpha", "Beta")

	for name, spec := range map[string]string{
		"zero":           "0",
		"negative":       "-3",
		"garbage":        "abc",
		"reversed-range": "5-2",
		"empty-entry":    "1,,2",
		"mixed-garbage":  "1,x",
		"negative-range": "1--2",
		"trailing-comma": "1,",
	} {
		t.Run(name, func(t *testing.T) {
			_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": spec})
			if err == nil {
				t.Fatalf("expected a parameter error for pages=%q, got %+v", spec, result)
			}
			if !strings.HasPrefix(err.Error(), "document.read:") {
				t.Errorf("error %q should be namespaced to the tool", err)
			}
		})
	}

	// Whitespace-only counts as absent: the read proceeds full-document.
	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "   "})
	if err != nil {
		t.Fatalf("whitespace pages must read as absent: %v", err)
	}
	if result.Pages != "" || !strings.Contains(result.Markdown, "Alpha") {
		t.Errorf("whitespace pages should behave like a full read: %+v", result)
	}
}

func TestDocumentReadPagesAndSectionMutuallyExclusive(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestMultiPagePDF(t, agentDir, "manual.pdf", "Alpha", "Beta")

	_, err, _ := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "1", "section": "Page 2"})
	if err == nil {
		t.Fatal("expected a parameter error when both scope parameters are set")
	}
	if !strings.Contains(err.Error(), "either pages or section") {
		t.Errorf("error = %q, want the both-parameters guidance", err)
	}
}

// --- Scoped reads: section (add-reference-documents D5 / delta scenarios) ----

func TestDocumentReadSectionXLSXSheet(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestXLSX(t, agentDir, "rates.xlsx")

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "section": "Notes"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if !strings.Contains(result.Markdown, "## Notes") || !strings.Contains(result.Markdown, "| remember |") {
		t.Errorf("Markdown = %q, want the Notes sheet", result.Markdown)
	}
	if strings.Contains(result.Markdown, "## Sheet1") || strings.Contains(result.Markdown, "north") {
		t.Errorf("section read leaked other sheets:\n%s", result.Markdown)
	}
	if result.Section != "Notes" {
		t.Errorf("Section echo = %q, want \"Notes\"", result.Section)
	}
}

func TestDocumentReadSectionPPTXSlide(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	slide := func(lines ...string) string {
		var b strings.Builder
		b.WriteString(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree>`)
		for _, line := range lines {
			b.WriteString(`<p:sp><p:txBody><a:p><a:r><a:t>` + line + `</a:t></a:r></a:p></p:txBody></p:sp>`)
		}
		b.WriteString(`</p:spTree></p:cSld></p:sld>`)
		return b.String()
	}
	path := writeTestOOXML(t, agentDir, "deck.pptx", map[string]string{
		"ppt/slides/slide1.xml": slide("Welcome to the deck", "Second bullet line"),
		"ppt/slides/slide2.xml": slide("Closing thoughts"),
	})

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "section": "Slide 2"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if !strings.Contains(result.Markdown, "## Slide 2") || !strings.Contains(result.Markdown, "Closing thoughts") {
		t.Errorf("Markdown = %q, want slide 2's content", result.Markdown)
	}
	if strings.Contains(result.Markdown, "Welcome to the deck") {
		t.Errorf("section read leaked slide 1:\n%s", result.Markdown)
	}
}

func TestDocumentReadSectionDocxHeading(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	const documentXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Quarterly Report</w:t></w:r></w:p>
    <w:p><w:r><w:t>Revenue grew in every region.</w:t></w:r></w:p>
    <w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Outlook</w:t></w:r></w:p>
    <w:p><w:r><w:t>Margins hold steady.</w:t></w:r></w:p>
  </w:body>
</w:document>`
	path := writeTestOOXML(t, agentDir, "spec.docx", map[string]string{"word/document.xml": documentXML})

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "section": "Quarterly Report"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if !strings.Contains(result.Markdown, "# Quarterly Report") || !strings.Contains(result.Markdown, "Revenue grew in every region.") {
		t.Errorf("Markdown = %q, want the matched section's heading and body", result.Markdown)
	}
	if strings.Contains(result.Markdown, "Margins hold steady.") {
		t.Errorf("section body must end at the next heading:\n%s", result.Markdown)
	}
	if result.Section != "Quarterly Report" {
		t.Errorf("Section echo = %q", result.Section)
	}
}

func TestDocumentReadSectionCaseInsensitive(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestXLSX(t, agentDir, "rates.xlsx")

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "section": "notes"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if !strings.Contains(result.Markdown, "## Notes") {
		t.Errorf("Markdown = %q, want the case-insensitive match on Notes", result.Markdown)
	}
}

func TestDocumentReadUnknownSectionStructuredNotFound(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	path := writeTestXLSX(t, agentDir, "rates.xlsx")

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "section": "Missing Sheet"})
	if err != nil {
		t.Fatalf("unknown section must be a structured result, not a Go error: %v", err)
	}
	if want := "section not found: Missing Sheet"; result.Error != want {
		t.Errorf("Error = %q, want %q", result.Error, want)
	}
	if result.Name != "rates.xlsx" {
		t.Errorf("Name = %q, want rates.xlsx", result.Name)
	}
	if result.Section != "" {
		t.Errorf("a miss carries no section echo, got %q", result.Section)
	}
}

// --- Scoped reads: parsing and slicing units ---------------------------------

func TestParsePageSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []int
		err  bool
	}{
		{"single", "3", []int{3}, false},
		{"range", "2-4", []int{2, 3, 4}, false},
		{"combination", "1,3,5-7", []int{1, 3, 5, 6, 7}, false},
		{"spaces", " 1 , 3 - 4 ", []int{1, 3, 4}, false},
		{"dedupe", "2,2-3,3", []int{2, 3}, false},
		{"zero", "0", nil, true},
		{"negative", "-3", nil, true},
		{"reversed", "5-2", nil, true},
		{"garbage", "abc", nil, true},
		{"mixed", "1,x", nil, true},
		{"empty-entry", "1,,2", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePageSelection(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("parsePageSelection(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePageSelection(%q): %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parsePageSelection(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("parsePageSelection(%q)[%d] = %d, want %d", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}

	// Runaway selections are parameter errors, never expansions.
	if _, err := parsePageSelection("1-99999999"); err == nil {
		t.Error("expected an error for a selection beyond the page cap")
	}
}

func TestRenderPageSelection(t *testing.T) {
	for _, tc := range []struct {
		in   []int
		want string
	}{
		{[]int{30, 31, 32, 33, 34}, "30-34"},
		{[]int{1, 3, 5, 6, 7}, "1,3,5-7"},
		{[]int{2}, "2"},
		{[]int{1, 2, 3, 10}, "1-3,10"},
		{nil, ""},
	} {
		if got := renderPageSelection(tc.in); got != tc.want {
			t.Errorf("renderPageSelection(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSliceMarkdownSection(t *testing.T) {
	doc := "preamble line\n" +
		"# Top\n" +
		"top body\n" +
		"## Webhooks\n" +
		"sign every request\n" +
		"### Signing\n" +
		"hmac only\n" +
		"## Closing ##\n" +
		"the end\n"
	for _, tc := range []struct {
		name     string
		title    string
		wantOK   bool
		wantMd   string
		wantBody string
	}{
		{"level-two-heading", "Webhooks", true, "## Webhooks", "sign every request"},
		{"nested-heading-ends-at-next-of-any-level", "Signing", true, "### Signing", "hmac only"},
		{"case-insensitive", "webhooks", true, "## Webhooks", "sign every request"},
		{"closing-hash-trimmed", "Closing", true, "## Closing ##", "the end"},
		{"preamble-never-matches", "preamble line", false, "", ""},
		{"absent", "Absent", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			heading, body, ok := sliceMarkdownSection(doc, tc.title)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if heading != tc.wantMd {
				t.Errorf("heading = %q, want %q", heading, tc.wantMd)
			}
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}

	// Inline hashes are content, not closing sequences.
	heading, _, ok := sliceMarkdownSection("## C# basics\nbody", "C# basics")
	if !ok || heading != "## C# basics" {
		t.Errorf("hash-in-title handling: ok=%v heading=%q", ok, heading)
	}
}

// --- Scoped reads: jail and cap ----------------------------------------------

func TestDocumentReadScopedReadsStillJailed(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	writeTestMultiPagePDF(t, agentDir, "inside.pdf", "Alpha", "Beta")

	// Path traversal stays a parameter error under both scope parameters.
	for _, args := range []map[string]string{
		{"path": "../inside.pdf", "pages": "1"},
		{"path": "/workspace/../secrets.pdf", "section": "X"},
	} {
		_, err, _ := runDocumentReadArgs(t, tr, args)
		if err == nil {
			t.Fatalf("expected rejection for %+v", args)
		}
	}

	// A symlink escaping the jail is rejected the same way with a scope
	// parameter set.
	outside := filepath.Join(t.TempDir(), "leaked.pdf")
	if err := os.WriteFile(outside, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(agentDir, "innocent.pdf")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err, _ := runDocumentReadArgs(t, tr, map[string]string{"path": link, "section": "X"}); err == nil {
		t.Fatal("expected symlink escape rejection under a scoped read")
	}
}

func TestDocumentReadScopedReadInReadOnlyRoot(t *testing.T) {
	agentDir := t.TempDir()
	root := t.TempDir()
	inRoot := writeAgentFile(t, root, "drop.html", []byte("<html><body><h2>Drop Notes</h2><p>attached payload</p></body></html>"))

	tr, err := NewDocumentRead(agentDir, WithReadOnlyRoots(root))
	if err != nil {
		t.Fatalf("NewDocumentRead: %v", err)
	}

	_, _, result := runDocumentReadArgs(t, tr, map[string]string{"path": inRoot, "section": "Drop Notes"})
	if result.Error != "" {
		t.Fatalf("scoped read of a read-only-root file rejected: %s", result.Error)
	}
	if !strings.Contains(result.Markdown, "attached payload") {
		t.Errorf("Markdown = %q, want the matched section", result.Markdown)
	}
}

func TestDocumentReadPagesOutputCap(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	big := strings.Repeat("lorem ipsum dolor sit ", 4000) // ~88 KB per page
	path := writeTestMultiPagePDF(t, agentDir, "big.pdf", big, big, big)

	_, err, result := runDocumentReadArgs(t, tr, map[string]string{"path": path, "pages": "1-3"})
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected structured error: %s", result.Error)
	}
	if !strings.HasSuffix(result.Markdown, truncationDocumentReadNotice) {
		t.Error("the cap must bind after slicing: a large slice truncates")
	}
	if len(result.Markdown) > MaxDocumentReadOutputBytes+len(truncationDocumentReadNotice) {
		t.Errorf("Markdown length = %d, exceeds the cap", len(result.Markdown))
	}
}

// --- Document read path handling (fix-reference-document-retrieval 3.1/3.2) ---

// TestCanonicalizePath pins the firmlink canonicalization: only the exact
// leading /System/Volumes/Data component is stripped, and non-firmlink paths
// — plain, relative, similar-prefix, the mount root itself — pass through
// byte-identical.
func TestCanonicalizePath(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain-absolute", "/Users/me/.onclaw/data/doc.md", "/Users/me/.onclaw/data/doc.md"},
		{"firmlink-alias", "/System/Volumes/Data/Users/me/.onclaw/data/doc.md", "/Users/me/.onclaw/data/doc.md"},
		{"mount-root-itself", "/System/Volumes/Data", "/System/Volumes/Data"},
		{"mount-root-trailing-sep", "/System/Volumes/Data/", "/System/Volumes/Data/"},
		{"similar-prefix-not-a-firmlink", "/System/Volumes/DataExtra/doc.md", "/System/Volumes/DataExtra/doc.md"},
		{"relative", "references/doc.md", "references/doc.md"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalizePath(tc.in); got != tc.want {
				t.Errorf("canonicalizePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDocumentReadFirmlinkSpellingResolvesSameFile (3.1): on darwin the data
// volume firmlink gives one file two absolute spellings; the tool must
// resolve and read the same bytes under both — the live run's 0-ms dead end.
// Every other jail test exercises the plain spelling, so non-firmlink
// behavior stays covered by the existing suite.
func TestDocumentReadFirmlinkSpellingResolvesSameFile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("data-volume firmlink spellings only exist on darwin")
	}
	root := newReferencesRoot(t, map[string]string{
		"manual.md": "# Firmlink Manual\n\nSame bytes under both spellings.\n",
	})
	tr, agentDir := newTestDocumentRead(t, WithReadOnlyRoots(root))

	// Plain spelling (control): the documented references/<name> form.
	_, err, plain := runDocumentRead(t, tr, "references/manual.md")
	if err != nil {
		t.Fatalf("plain InvokableRun: %v", err)
	}
	if plain.Error != "" {
		t.Fatalf("plain read rejected: %s", plain.Error)
	}

	// Firmlink spelling of the same file resolves and reads identically. The
	// alias is built from the resolved root spelling: EvalSymlinks keeps the
	// /System/Volumes/Data prefix (firmlink components are not symlinks), so
	// canonicalizePath must map it back onto the stored root.
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	firmlinkPath := darwinDataVolumePrefix + resolvedRoot + "/manual.md"
	if _, err := os.Stat(firmlinkPath); err != nil {
		t.Fatalf("firmlink spelling did not resolve on this darwin host: %v", err)
	}
	_, err, fir := runDocumentRead(t, tr, firmlinkPath)
	if err != nil {
		t.Fatalf("firmlink InvokableRun: %v", err)
	}
	if fir.Error != "" {
		t.Fatalf("firmlink-spelled read rejected: %s", fir.Error)
	}
	if fir.Markdown != plain.Markdown {
		t.Errorf("firmlink Markdown = %q, want the plain spelling's bytes %q", fir.Markdown, plain.Markdown)
	}

	// The agent workspace root accepts its firmlink spelling too.
	writeAgentFile(t, agentDir, "notes.md", []byte("workspace note\n"))
	resolvedAgentDir, err := filepath.EvalSymlinks(agentDir)
	if err != nil {
		t.Fatalf("resolve agent dir: %v", err)
	}
	_, err, ws := runDocumentRead(t, tr, darwinDataVolumePrefix+resolvedAgentDir+"/notes.md")
	if err != nil {
		t.Fatalf("firmlink workspace InvokableRun: %v", err)
	}
	if ws.Error != "" {
		t.Fatalf("firmlink workspace read rejected: %s", ws.Error)
	}
	if ws.Markdown != "workspace note" {
		t.Errorf("workspace Markdown = %q, want %q", ws.Markdown, "workspace note")
	}
}

// TestDocumentReadOutsideErrorTeachesAcceptedForms (3.2): the rejection names
// the accepted path forms instead of a bare refusal, and the same document
// staged where the error points reads on the very next call — the one-retry
// self-correction.
func TestDocumentReadOutsideErrorTeachesAcceptedForms(t *testing.T) {
	tr, agentDir := newTestDocumentRead(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("# Outside\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	runErr := invokeDocumentReadRaw(t, tr, outside)
	if runErr == nil {
		t.Fatal("expected rejection of a host path outside the jail")
	}
	msg := runErr.Error()
	for _, want := range []string{
		"outside allowed directories",
		"references/<document name>",
		"workspace-relative",
		"/workspace/...",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should name the accepted form %q", msg, want)
		}
	}

	// One-retry recovery: the same document inside the workspace reads via the
	// workspace-relative form the error taught.
	writeAgentFile(t, agentDir, "elsewhere.md", []byte("# Outside\n"))
	_, err, result := runDocumentRead(t, tr, "elsewhere.md")
	if err != nil {
		t.Fatalf("workspace-relative retry: %v", err)
	}
	if result.Error != "" || result.Markdown != "# Outside" {
		t.Errorf("retry result = %+v, want the document's markdown", result)
	}
}

// TestDocumentReadOutsideErrorHintsReferencesForm (3.2): when the rejected
// spelling is a spelling of a file a references mount serves — its canonical
// path sits directly inside a directory named "references", e.g. a stale
// session's mount path — the error says so and names the references/<name>
// form, and the promised retry reads the mounted document in one step.
func TestDocumentReadOutsideErrorHintsReferencesForm(t *testing.T) {
	root := newReferencesRoot(t, map[string]string{"manual.md": "# Hint Manual\n\nMounted copy.\n"})
	tr, _ := newTestDocumentRead(t, WithReadOnlyRoots(root))

	// A references file addressed through a path outside THIS run's roots.
	foreignDir := filepath.Join(t.TempDir(), "references")
	if err := os.MkdirAll(foreignDir, 0o755); err != nil {
		t.Fatalf("mkdir foreign references dir: %v", err)
	}
	foreign := filepath.Join(foreignDir, "manual.md")
	if err := os.WriteFile(foreign, []byte("# Hint Manual\n\nMounted copy.\n"), 0o644); err != nil {
		t.Fatalf("write foreign references file: %v", err)
	}

	runErr := invokeDocumentReadRaw(t, tr, foreign)
	if runErr == nil {
		t.Fatal("expected rejection of a references path outside this run's roots")
	}
	msg := runErr.Error()
	if !strings.Contains(msg, "outside allowed directories") {
		t.Errorf("error %q should remain a jail rejection", msg)
	}
	if !strings.Contains(msg, "same file as") || !strings.Contains(msg, "references/manual.md") {
		t.Errorf("error %q should hint the same file's references/<name> form", msg)
	}

	// The hinted form reads the mounted document on the retry.
	_, err, result := runDocumentRead(t, tr, "references/manual.md")
	if err != nil {
		t.Fatalf("references retry: %v", err)
	}
	if result.Error != "" || !strings.Contains(result.Markdown, "Mounted copy.") {
		t.Errorf("retry result = %+v, want the mounted document", result)
	}
}

// TestDocumentReadSymlinkEscapeGetsNoSameFileHint pins the hint's honesty: a
// genuine escape (an in-root symlink resolving outside) is a real jail
// rejection, not a spelling artifact, and must not be dressed up as the same
// file as a references document.
func TestDocumentReadSymlinkEscapeGetsNoSameFileHint(t *testing.T) {
	root := newReferencesRoot(t, nil)
	outsideFile := filepath.Join(t.TempDir(), "leaked.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "leaked.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	tr, _ := newTestDocumentRead(t, WithReadOnlyRoots(root))

	runErr := invokeDocumentReadRaw(t, tr, "references/leaked.txt")
	if runErr == nil {
		t.Fatal("expected symlink escape rejection")
	}
	if !strings.Contains(runErr.Error(), "outside allowed directories") {
		t.Errorf("error = %q, want outside-allowed-directories rejection", runErr)
	}
	if strings.Contains(runErr.Error(), "same file as") {
		t.Errorf("error %q must not claim a genuine escape is a references file", runErr)
	}
}
