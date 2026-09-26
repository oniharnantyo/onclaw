package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	inv, ok := tr.(invokableTool)
	if !ok {
		t.Fatalf("tool %T does not implement InvokableRun", tr)
	}
	args, err := json.Marshal(map[string]string{"path": userPath})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), string(args))
	var result documentReadResult
	if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
		t.Fatalf("decode result JSON %q: %v", out, jsonErr)
	}
	return out, err, result
}

// --- Fixtures ---------------------------------------------------------------

// writeTestPDF builds a one-page PDF with fpdf containing text (or no text at
// all for the scanned-PDF fixture).
func writeTestPDF(t *testing.T, agentDir, name, text string) string {
	t.Helper()
	doc := fpdf.New("P", "mm", "A4", "")
	doc.AddPage()
	if text != "" {
		doc.SetFont("helvetica", "", 14)
		doc.Text(10, 20, text)
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
