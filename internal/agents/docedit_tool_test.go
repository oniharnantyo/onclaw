package agents

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// The edit_document tool tests (add-document-editing task 5.1, spec
// document-editing "In-place revision of office documents" and "PDF
// structural operations"): targeted operations land on workspace files —
// office files in place, PDFs through structural ops writing outputs — paths
// resolve inside the workspace jail, and rejected operations come back as
// readable results that do not end the turn.

// docxBytes authors the standard three-paragraph DOCX fixture with docxgo: a
// bold title, a plain body line, and an italic footer (the documents
// package's styled fixture, rebuilt here — its helpers are package-internal).
func docxBytes(t *testing.T) []byte {
	t.Helper()
	doc := docx.NewDocument()
	title, err := doc.AddParagraph()
	require.NoError(t, err)
	run, err := title.AddRun()
	require.NoError(t, err)
	require.NoError(t, run.SetText("Quarterly Report"))
	require.NoError(t, run.SetBold(true))

	body, err := doc.AddParagraph()
	require.NoError(t, err)
	run, err = body.AddRun()
	require.NoError(t, err)
	require.NoError(t, run.SetText("Revenue grew in every region."))

	footer, err := doc.AddParagraph()
	require.NoError(t, err)
	run, err = footer.AddRun()
	require.NoError(t, err)
	require.NoError(t, run.SetText("Prepared by finance"))

	path := filepath.Join(t.TempDir(), "fixture.docx")
	require.NoError(t, doc.SaveAs(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

// docxParagraphTexts re-reads a DOCX and returns its ¶-addressable body
// paragraphs' text in document order.
func docxParagraphTexts(t *testing.T, path string) ([]string, error) {
	t.Helper()
	doc, err := docx.OpenDocument(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, block := range doc.Blocks() {
		if block.Paragraph != nil {
			out = append(out, block.Paragraph.Text())
		}
	}
	return out, nil
}

// xlsxBytes authors a one-sheet XLSX fixture with excelize: a header row and
// one data row on the default sheet.
func xlsxBytes(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	require.NoError(t, f.SetCellValue("Sheet1", "A1", "Region"))
	require.NoError(t, f.SetCellValue("Sheet1", "B1", "Revenue"))
	require.NoError(t, f.SetCellValue("Sheet1", "A2", "North"))
	require.NoError(t, f.SetCellValue("Sheet1", "B2", 100))
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	return buf.Bytes()
}

// editFixture roots a workspace with the given files staged into it.
func editFixture(t *testing.T, files map[string][]byte) (tool.BaseTool, string) {
	t.Helper()
	ws := t.TempDir()
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(ws, name), data, 0o644))
	}
	base, err := newEditDocumentTool(ws)
	require.NoError(t, err)
	return base, ws
}

// runEditDocumentTool runs one edit_document call through the InvokableTool
// surface and decodes the result.
func runEditDocumentTool(t *testing.T, base tool.BaseTool, args string) editDocumentOutput {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	require.True(t, ok, "edit_document tool %T does not implement InvokableTool", base)
	out, err := invokable.InvokableRun(t.Context(), args)
	require.NoError(t, err, "rejections must be results, not errors: args %s", args)
	var res editDocumentOutput
	require.NoError(t, json.Unmarshal([]byte(out), &res), "result %q", out)
	return res
}

// TestEditDocumentTool_ReplacesParagraphInPlace covers the spec's replace
// scenario through the tool: the same file carries the new text and every
// other paragraph is unchanged — no output copy appears beside it.
func TestEditDocumentTool_ReplacesParagraphInPlace(t *testing.T) {
	fixture := docxBytes(t)
	base, ws := editFixture(t, map[string][]byte{"report.docx": fixture})

	res := runEditDocumentTool(t, base,
		`{"path":"/workspace/report.docx","ops":[{"type":"replace_paragraph","index":2,"text":"Costs fell in every region."}]}`)
	require.True(t, res.OK, "error field: %s", res.Error)
	require.Equal(t, "/workspace/report.docx", res.Path)

	// The edit is in place: the staged file itself reflects it.
	edited, err := docxParagraphTexts(t, filepath.Join(ws, "report.docx"))
	require.NoError(t, err)
	require.Equal(t, []string{"Quarterly Report", "Costs fell in every region.", "Prepared by finance"}, edited)

	// Bare relative paths address the same file.
	res = runEditDocumentTool(t, base,
		`{"path":"report.docx","ops":[{"type":"replace_paragraph","index":1,"text":"Annual Report"}]}`)
	require.True(t, res.OK, "error field: %s", res.Error)
	edited, err = docxParagraphTexts(t, filepath.Join(ws, "report.docx"))
	require.NoError(t, err)
	require.Equal(t, "Annual Report", edited[0])
}

// TestEditDocumentTool_SetsCellsInPlace covers the XLSX dispatch: a set_cell
// operation writes the value into the addressed cell of the file itself.
func TestEditDocumentTool_SetsCellsInPlace(t *testing.T) {
	base, ws := editFixture(t, map[string][]byte{"sheet.xlsx": xlsxBytes(t)})

	res := runEditDocumentTool(t, base,
		`{"path":"/workspace/sheet.xlsx","ops":[{"type":"set_cell","sheet":"Sheet1","cell":"B2","text":"250"}]}`)
	require.True(t, res.OK, "error field: %s", res.Error)

	f, err := excelize.OpenFile(filepath.Join(ws, "sheet.xlsx"))
	require.NoError(t, err)
	defer f.Close()
	value, err := f.GetCellValue("Sheet1", "B2")
	require.NoError(t, err)
	require.Equal(t, "250", value, "the numeric-looking value was written as a number")
}

// TestEditDocumentTool_OutsideWorkspaceRejected covers the jail: absolute
// host paths, traversal, and empty paths are readable rejections and touch
// nothing.
func TestEditDocumentTool_OutsideWorkspaceRejected(t *testing.T) {
	fixture := docxBytes(t)
	base, ws := editFixture(t, map[string][]byte{"report.docx": fixture})
	before, err := os.ReadFile(filepath.Join(ws, "report.docx"))
	require.NoError(t, err)

	cases := []struct {
		name string
		path string
	}{
		{"absolute host path", "/etc/passwd"},
		{"host path disguised", "/workspace/../../etc/passwd"},
		{"traversal", "../escape.docx"},
		{"deep traversal", "a/../../escape.docx"},
		{"empty", ""},
		{"the mount root", "/workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runEditDocumentTool(t, base,
				`{"path":`+quote(tc.path)+`,"ops":[{"type":"replace_paragraph","index":1,"text":"x"}]}`)
			require.False(t, res.OK)
			require.NotEmpty(t, res.Error, "the rejection names the problem")
		})
	}

	after, err := os.ReadFile(filepath.Join(ws, "report.docx"))
	require.NoError(t, err)
	require.Equal(t, before, after, "the rejections touched nothing")
}

// TestEditDocumentTool_Rejections covers the readable rejections: a
// non-office file, an unknown operation target, a malformed op, and an empty
// ops list come back as {ok:false, error} — the turn continues.
func TestEditDocumentTool_Rejections(t *testing.T) {
	fixture := docxBytes(t)
	base, ws := editFixture(t, map[string][]byte{
		"report.docx": fixture,
		"doc.pdf":     []byte("%PDF-1.4"),
		"fake.pptx":   []byte("not a zip"),
		"notes.txt":   []byte("plain notes"),
	})

	cases := []struct {
		name    string
		args    string
		wantSub string
	}{
		{
			name:    "office operation on a PDF",
			args:    `{"path":"/workspace/doc.pdf","ops":[{"type":"replace_paragraph","index":1,"text":"x"}]}`,
			wantSub: `unknown operation "replace_paragraph" for a PDF`,
		},
		{
			name:    "non-editable file",
			args:    `{"path":"/workspace/notes.txt","ops":[{"type":"replace_paragraph","index":1,"text":"x"}]}`,
			wantSub: "not an editable document",
		},
		{
			name:    "missing file",
			args:    `{"path":"/workspace/ghost.docx","ops":[{"type":"replace_paragraph","index":1,"text":"x"}]}`,
			wantSub: "no such file",
		},
		{
			name:    "unknown paragraph target",
			args:    `{"path":"/workspace/report.docx","ops":[{"type":"replace_paragraph","index":99,"text":"x"}]}`,
			wantSub: "¶99 not found",
		},
		{
			name:    "operation for the wrong engine",
			args:    `{"path":"/workspace/report.docx","ops":[{"type":"set_cell","sheet":"Sheet1","cell":"A1","text":"x"}]}`,
			wantSub: "unknown operation",
		},
		{
			name:    "empty ops",
			args:    `{"path":"/workspace/report.docx","ops":[]}`,
			wantSub: "at least one operation is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runEditDocumentTool(t, base, tc.args)
			require.False(t, res.OK)
			require.Contains(t, res.Error, tc.wantSub)
		})
	}

	// The PPTX dispatch is reachable through the tool: an unreadable
	// "presentation" rejects readably from the engine, not from dispatch.
	res := runEditDocumentTool(t, base,
		`{"path":"/workspace/fake.pptx","ops":[{"type":"replace_shape_text","slide":1,"shape":1,"text":"x"}]}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "not a readable")

	// Nothing new landed in the workspace.
	entries, err := os.ReadDir(ws)
	require.NoError(t, err)
	require.Len(t, entries, 4)
}

// pdfFixture builds an n-page PDF in dir (each page displaying its number),
// mirroring the pdfops package's own fixture helper — that one is
// package-internal.
func pdfFixture(t *testing.T, dir, name string, pages int) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`{"pages":{`)
	for i := 1; i <= pages; i++ {
		if i > 1 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"%d":{"content":{"text":[{"value":"fixture page %d","font":{"name":"Courier","size":14},"pos":[50,750]}]}}`, i, i)
	}
	sb.WriteString(`}}`)
	scriptPath := filepath.Join(dir, name+".create.json")
	require.NoError(t, os.WriteFile(scriptPath, []byte(sb.String()), 0o644))
	out := filepath.Join(dir, name)
	require.NoError(t, api.CreateFile(context.Background(), "", scriptPath, out, nil))
	return out
}

// pdfPageCount returns the page count of a PDF file.
func pdfPageCount(t *testing.T, path string) int {
	t.Helper()
	n, err := api.PageCountFile(context.Background(), path)
	require.NoError(t, err)
	return n
}

// pdfDigest is the SHA-256 of a file, for the unchanged-input assertions.
func pdfDigest(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	return sum[:]
}

// pdfToolFixture roots a workspace holding PDF fixtures, plus the merged
// edit_document tool over it.
func pdfToolFixture(t *testing.T, files map[string]int) (tool.BaseTool, string) {
	t.Helper()
	ws := t.TempDir()
	for name, pages := range files {
		pdfFixture(t, ws, name, pages)
	}
	base, err := newEditDocumentTool(ws)
	require.NoError(t, err)
	return base, ws
}

// pdfEditArgs renders one PDF operation as the tool's path+ops input shape.
func pdfEditArgs(path, opJSON string) string {
	return `{"path":"` + path + `","ops":[` + opJSON + `]}`
}

// TestEditDocumentTool_PDFMerge covers the spec's merge scenario through the
// merged tool: the output PDF contains the pages of both inputs in order and
// the inputs are unchanged. The output lands under artifacts/, the address
// the tool description recommends.
func TestEditDocumentTool_PDFMerge(t *testing.T) {
	base, ws := pdfToolFixture(t, map[string]int{"a.pdf": 2, "b.pdf": 3})
	digestA := pdfDigest(t, filepath.Join(ws, "a.pdf"))
	digestB := pdfDigest(t, filepath.Join(ws, "b.pdf"))

	res := runEditDocumentTool(t, base, pdfEditArgs("/workspace/a.pdf",
		`{"type":"merge","inputs":["/workspace/a.pdf","/workspace/b.pdf"],"output":"/workspace/artifacts/merged.pdf"}`))
	require.True(t, res.OK, "error field: %s", res.Error)
	require.Equal(t, "merge", res.Op)
	require.Equal(t, "/workspace/artifacts/merged.pdf", res.OutputPath)
	require.Equal(t, 5, res.PageCount, "the merged PDF carries both inputs' pages in order")
	require.NotEmpty(t, res.Message)

	merged := pdfPageCount(t, filepath.Join(ws, "artifacts", "merged.pdf"))
	require.Equal(t, 5, merged)
	require.Equal(t, digestA, pdfDigest(t, filepath.Join(ws, "a.pdf")), "the inputs are unchanged")
	require.Equal(t, digestB, pdfDigest(t, filepath.Join(ws, "b.pdf")), "the inputs are unchanged")
}

// TestEditDocumentTool_PDFExtract covers the extract op through the merged
// tool, including a bare relative input path.
func TestEditDocumentTool_PDFExtract(t *testing.T) {
	base, ws := pdfToolFixture(t, map[string]int{"doc.pdf": 4})

	res := runEditDocumentTool(t, base, pdfEditArgs("doc.pdf",
		`{"type":"extract","input":"doc.pdf","pages":"2-3","output":"/workspace/artifacts/pages.pdf"}`))
	require.True(t, res.OK, "error field: %s", res.Error)
	require.Equal(t, "extract", res.Op)
	require.Equal(t, 2, res.PageCount)
	require.Equal(t, 4, pdfPageCount(t, filepath.Join(ws, "doc.pdf")), "the input is unchanged")
}

// TestEditDocumentTool_PDFRotate covers the rotate op through the merged
// tool.
func TestEditDocumentTool_PDFRotate(t *testing.T) {
	base, ws := pdfToolFixture(t, map[string]int{"doc.pdf": 3})

	res := runEditDocumentTool(t, base, pdfEditArgs("/workspace/doc.pdf",
		`{"type":"rotate","input":"/workspace/doc.pdf","pages":"1","angle":90,"output":"/workspace/artifacts/rotated.pdf"}`))
	require.True(t, res.OK, "error field: %s", res.Error)
	require.Equal(t, "rotate", res.Op)
	require.Equal(t, 3, res.PageCount, "rotation preserves the page count")
	require.Equal(t, 3, pdfPageCount(t, filepath.Join(ws, "doc.pdf")), "the input is unchanged")
}

// TestEditDocumentTool_PDFRewriteTextRedirect covers the spec's redirect
// scenario through the merged tool: the rewrite_text request returns a
// readable result explaining PDF text is changed by regenerating and naming
// the create_document path, and the turn continues.
func TestEditDocumentTool_PDFRewriteTextRedirect(t *testing.T) {
	base, _ := pdfToolFixture(t, map[string]int{"doc.pdf": 1})

	res := runEditDocumentTool(t, base, pdfEditArgs("/workspace/doc.pdf", `{"type":"rewrite_text","input":"/workspace/doc.pdf"}`))
	require.False(t, res.OK, "the redirect is a readable failure")
	require.Equal(t, "rewrite_text", res.Op)
	require.Contains(t, res.Error, "regenerating the document")
	require.Contains(t, res.Error, "create_document")
}

// TestEditDocumentTool_PDFRejections covers the readable rejections: unknown
// operations, missing inputs, jail escapes, and overwriting inputs — the
// turn continues every time.
func TestEditDocumentTool_PDFRejections(t *testing.T) {
	base, ws := pdfToolFixture(t, map[string]int{"doc.pdf": 1})

	cases := []struct {
		name    string
		args    string
		wantSub string
	}{
		{
			name:    "unknown operation",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"shrink","input":"/workspace/doc.pdf"}`),
			wantSub: "unknown operation",
		},
		{
			name:    "missing input",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"extract","input":"/workspace/ghost.pdf","pages":"1","output":"/workspace/artifacts/x.pdf"}`),
			wantSub: "no such file",
		},
		{
			name:    "input outside the workspace",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"extract","input":"/etc/hosts","pages":"1","output":"/workspace/artifacts/x.pdf"}`),
			wantSub: "absolute paths are not allowed",
		},
		{
			name:    "traversal output",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"extract","input":"/workspace/doc.pdf","pages":"1","output":"/workspace/../escape.pdf"}`),
			wantSub: "escape attempt",
		},
		{
			name:    "output overwrites the input",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"rotate","input":"/workspace/doc.pdf","angle":90,"output":"/workspace/doc.pdf"}`),
			wantSub: "would overwrite input",
		},
		{
			name:    "merge without an output",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"merge","inputs":["/workspace/doc.pdf","/workspace/doc.pdf"]}`),
			wantSub: "needs an output path",
		},
		{
			name:    "extract without a page selection",
			args:    pdfEditArgs("/workspace/doc.pdf", `{"type":"extract","input":"/workspace/doc.pdf","output":"/workspace/artifacts/x.pdf"}`),
			wantSub: "needs a page selection",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runEditDocumentTool(t, base, tc.args)
			require.False(t, res.OK)
			require.Contains(t, res.Error, tc.wantSub)
		})
	}

	// The rejections wrote nothing: the artifacts directory may have been
	// created on demand (the resolve step brings it into being, like
	// create_document), but no PDF landed in it.
	entries, err := os.ReadDir(filepath.Join(ws, "artifacts"))
	require.NoError(t, err)
	require.Empty(t, entries, "rejected operations leave no files behind")
}

// TestEditDocumentTool_PDFStampAndFillForm cover the remaining structural
// ops through the merged tool: a stamp writes an output and a form fill sets
// fields, both leaving the inputs unchanged.
func TestEditDocumentTool_PDFStampAndFillForm(t *testing.T) {
	t.Run("stamp", func(t *testing.T) {
		base, ws := pdfToolFixture(t, map[string]int{"doc.pdf": 2})
		res := runEditDocumentTool(t, base, pdfEditArgs("/workspace/doc.pdf",
			`{"type":"stamp","input":"/workspace/doc.pdf","text":"CONFIDENTIAL","position":"c","output":"/workspace/artifacts/stamped.pdf"}`))
		require.True(t, res.OK, "error field: %s", res.Error)
		require.Equal(t, "stamp", res.Op)
		require.Equal(t, 2, res.PageCount)
		require.Equal(t, 2, pdfPageCount(t, filepath.Join(ws, "doc.pdf")), "the input is unchanged")
	})

	t.Run("fill_form", func(t *testing.T) {
		ws := t.TempDir()
		scriptPath := filepath.Join(ws, "form.create.json")
		require.NoError(t, os.WriteFile(scriptPath, []byte(
			`{"fonts":{"input":{"name":"Courier","size":12}},"pages":{"1":{"content":{`+
				`"textfield":[{"id":"full_name","value":"","pos":[50,600],"width":200,"height":24}]}}}}`), 0o644))
		form := filepath.Join(ws, "form.pdf")
		require.NoError(t, api.CreateFile(t.Context(), "", scriptPath, form, nil))

		base, err := newEditDocumentTool(ws)
		require.NoError(t, err)
		res := runEditDocumentTool(t, base, pdfEditArgs("/workspace/form.pdf",
			`{"type":"fill_form","input":"/workspace/form.pdf","fields":{"full_name":"Ada Lovelace"},"output":"/workspace/artifacts/filled.pdf"}`))
		require.True(t, res.OK, "error field: %s", res.Error)
		require.Equal(t, "fill_form", res.Op)
		require.FileExists(t, filepath.Join(ws, "artifacts", "filled.pdf"))
	})
}
