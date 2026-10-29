package documents

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"github.com/stretchr/testify/require"
)

// runSnapshot captures the visible state of one run for fidelity comparison.
type runSnapshot struct {
	Text   string
	Bold   bool
	Italic bool
	Size   int
	Color  domain.Color
	Font   domain.Font
}

// paraSnapshot captures the visible state of one paragraph.
type paraSnapshot struct {
	Text      string
	StyleID   string
	Alignment domain.Alignment
	Runs      []runSnapshot
}

// paraSnapshots re-reads a DOCX from disk and captures every ¶-addressable
// paragraph's content and styling.
func paraSnapshots(t *testing.T, path string) []paraSnapshot {
	t.Helper()
	doc, err := docx.OpenDocument(path)
	require.NoError(t, err)
	var out []paraSnapshot
	for _, para := range docxParagraphs(doc) {
		snap := paraSnapshot{
			Text:      para.Text(),
			Alignment: para.Alignment(),
		}
		if style := para.Style(); style != nil {
			snap.StyleID = style.ID()
		}
		for _, run := range para.Runs() {
			snap.Runs = append(snap.Runs, runSnapshot{
				Text:   run.Text(),
				Bold:   run.Bold(),
				Italic: run.Italic(),
				Size:   run.Size(),
				Color:  run.Color(),
				Font:   run.Font(),
			})
		}
		out = append(out, snap)
	}
	return out
}

// buildDocx authors a DOCX fixture with docxgo and returns its bytes.
func buildDocx(t *testing.T, build func(doc domain.Document)) []byte {
	t.Helper()
	doc := docx.NewDocument()
	build(doc)
	path := filepath.Join(t.TempDir(), "fixture.docx")
	require.NoError(t, doc.SaveAs(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

// writeFixture writes fixture bytes into the test's input path.
func writeFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// runEdit applies ops through an engine, asserting infrastructure success.
func runEdit(t *testing.T, e interface {
	Edit(ctx context.Context, src, dst string, ops []Op) (Result, error)
}, src string, ops []Op) (string, Result) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out-result")
	res, err := e.Edit(context.Background(), src, dst, ops)
	require.NoError(t, err, "a rejected operation is a Result value, never a Go error")
	return dst, res
}

// docxBodyHas reports whether the saved package's document.xml contains the
// given fragment.
func docxBodyHas(t *testing.T, path, fragment string) bool {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != docxBodyPath {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		raw, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)
		return strings.Contains(string(raw), fragment)
	}
	return false
}

// styledDocx builds the standard three-paragraph fixture: a bold title, a
// plain body line, and an italic footer.
func styledDocx(t *testing.T) []byte {
	return buildDocx(t, func(doc domain.Document) {
		title, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err := title.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("Quarterly Report"))
		require.NoError(t, run.SetBold(true))
		require.NoError(t, run.SetSize(28))

		body, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err = body.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("Revenue grew in every region."))

		footer, err := doc.AddParagraph()
		require.NoError(t, err)
		require.NoError(t, footer.SetAlignment(domain.AlignmentRight))
		run, err = footer.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("Prepared by finance"))
		require.NoError(t, run.SetItalic(true))
	})
}

// TestDocxEngine_ReplacePreservesTheRest covers the spec scenario: replacing
// one paragraph shows the new text there and leaves every other paragraph's
// content and styling unchanged.
func TestDocxEngine_ReplacePreservesTheRest(t *testing.T) {
	fixture := styledDocx(t)
	before := paraSnapshots(t, writeFixture(t, t.TempDir(), "in.docx", fixture))

	src := writeFixture(t, t.TempDir(), "in.docx", fixture)
	dst, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpReplaceParagraph, Index: 2, Text: "Costs fell in every region."},
	})
	require.True(t, res.OK, "replace of an existing paragraph must succeed: %s", res.Error)

	after := paraSnapshots(t, dst)
	require.Len(t, after, len(before))
	require.Equal(t, "Costs fell in every region.", after[1].Text)
	require.False(t, after[1].Runs[0].Bold && after[1].Runs[0].Italic,
		"the plain body paragraph had no formatting to keep")
	after[1] = before[1]
	require.Equal(t, before, after, "every paragraph but ¶2 — text, styles, runs — must be unchanged")
}

// TestDocxEngine_ReplaceKeepsRunFormatting covers the styling rider: the
// replaced text inherits the paragraph's dominant character formatting.
func TestDocxEngine_ReplaceKeepsRunFormatting(t *testing.T) {
	fixture := styledDocx(t)
	src := writeFixture(t, t.TempDir(), "in.docx", fixture)

	dst, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpReplaceParagraph, Index: 1, Text: "Annual Report"},
		{Type: OpReplaceParagraph, Index: 3, Text: "Prepared by accounting"},
	})
	require.True(t, res.OK, "%s", res.Error)

	after := paraSnapshots(t, dst)
	require.Len(t, after, 3)
	require.Len(t, after[0].Runs, 1)
	require.Equal(t, "Annual Report", after[0].Runs[0].Text)
	require.True(t, after[0].Runs[0].Bold, "the title's bold formatting carries onto the replacement")
	require.Equal(t, 28, after[0].Runs[0].Size)
	require.True(t, after[2].Runs[0].Italic, "the footer's italic formatting carries onto the replacement")
	require.Equal(t, domain.AlignmentRight, after[2].Alignment, "paragraph-level alignment survives")
}

// TestDocxEngine_NoOpsRoundTrip covers the fidelity baseline: saving with no
// operations reads back exactly equal.
func TestDocxEngine_NoOpsRoundTrip(t *testing.T) {
	fixture := styledDocx(t)
	before := paraSnapshots(t, writeFixture(t, t.TempDir(), "in.docx", fixture))

	src := writeFixture(t, t.TempDir(), "in.docx", fixture)
	dst, res := runEdit(t, NewDocxEngine(), src, nil)
	require.True(t, res.OK)

	afterRaw, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, fixture, afterRaw, "no operations must leave the file byte-for-byte unchanged")

	after := paraSnapshots(t, dst)
	require.Equal(t, before, after)
}

// TestDocxEngine_FillPlaceholders covers the spec scenario: each placeholder
// is replaced by the supplied value and the template's styling is unchanged.
func TestDocxEngine_FillPlaceholders(t *testing.T) {
	fixture := buildDocx(t, func(doc domain.Document) {
		line, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err := line.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("Dear {{name}}, your invoice {{number}} is ready."))
		require.NoError(t, run.SetBold(true))
		untouched, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err = untouched.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("No placeholders here."))
	})
	src := writeFixture(t, t.TempDir(), "in.docx", fixture)
	before := paraSnapshots(t, writeFixture(t, t.TempDir(), "in.docx", fixture))

	dst, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpFillPlaceholders, Values: map[string]string{"name": "Ada", "number": "INV-7"}},
	})
	require.True(t, res.OK, "%s", res.Error)

	after := paraSnapshots(t, dst)
	require.Len(t, after, 2)
	require.Equal(t, "Dear Ada, your invoice INV-7 is ready.", after[0].Text)
	require.NotContains(t, after[0].Text, "{{")
	require.True(t, after[0].Runs[0].Bold, "the template's styling is unchanged")
	require.Equal(t, before[1], after[1], "the paragraph without placeholders is untouched")
}

// TestDocxEngine_FillSplitToken covers Word's run-splitting of {{tokens}}:
// a token Word split across two adjacent runs still fills.
func TestDocxEngine_FillSplitToken(t *testing.T) {
	fixture := buildDocx(t, func(doc domain.Document) {
		line, err := doc.AddParagraph()
		require.NoError(t, err)
		first, err := line.AddRun()
		require.NoError(t, err)
		require.NoError(t, first.SetText("Hello {{spl"))
		second, err := line.AddRun()
		require.NoError(t, err)
		require.NoError(t, second.SetText("it}}, welcome."))
	})
	src := writeFixture(t, t.TempDir(), "in.docx", fixture)

	dst, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpFillPlaceholders, Values: map[string]string{"split": "World"}},
	})
	require.True(t, res.OK, "%s", res.Error)

	after := paraSnapshots(t, dst)
	require.Equal(t, "Hello World, welcome.", after[0].Text)
}

// TestDocxEngine_DeleteAndInsertParagraph covers the structural operations:
// delete removes the addressed ¶; insert adds one after the addressed ¶,
// with its optional style.
func TestDocxEngine_DeleteAndInsertParagraph(t *testing.T) {
	fixture := styledDocx(t)
	src := writeFixture(t, t.TempDir(), "in.docx", fixture)

	dst, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpDeleteParagraph, Index: 2},
	})
	require.True(t, res.OK, "%s", res.Error)
	after := paraSnapshots(t, dst)
	require.Len(t, after, 2)
	require.Equal(t, "Quarterly Report", after[0].Text)
	require.Equal(t, "Prepared by finance", after[1].Text)

	dst, res = runEdit(t, NewDocxEngine(), dst, []Op{
		{Type: OpInsertParagraph, AfterIndex: 1, Text: "A new middle line.", Style: "Heading1"},
	})
	require.True(t, res.OK, "%s", res.Error)
	after = paraSnapshots(t, dst)
	require.Len(t, after, 3)
	require.Equal(t, "A new middle line.", after[1].Text)
	require.True(t, docxBodyHas(t, dst, `<w:pStyle w:val="Heading1"/>`),
		"the inserted paragraph carries the requested style")
	require.Equal(t, "Prepared by finance", after[2].Text, "the insert lands after ¶1")
}

// TestDocxEngine_TableCellParagraphsCountAsBlocks covers the addressing
// contract: table-cell paragraphs carry their own ¶ numbers in body order.
func TestDocxEngine_TableCellParagraphsCountAsBlocks(t *testing.T) {
	fixture := buildDocx(t, func(doc domain.Document) {
		para, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err := para.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("before table"))
		table, err := doc.AddTable(1, 1)
		require.NoError(t, err)
		cell, err := table.Rows()[0].Cell(0)
		require.NoError(t, err)
		cellPara, err := cell.AddParagraph()
		require.NoError(t, err)
		run, err = cellPara.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("cell one"))
		cellPara, err = cell.AddParagraph()
		require.NoError(t, err)
		run, err = cellPara.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("cell two"))
		after, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err = after.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("after table"))
	})
	src := writeFixture(t, t.TempDir(), "in.docx", fixture)
	before := paraSnapshots(t, writeFixture(t, t.TempDir(), "in.docx", fixture))
	require.Len(t, before, 4, "¶ order: body, cell, cell, body")
	require.Equal(t, []string{"before table", "cell one", "cell two", "after table"},
		[]string{before[0].Text, before[1].Text, before[2].Text, before[3].Text})

	dst, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpReplaceParagraph, Index: 3, Text: "cell two edited"},
	})
	require.True(t, res.OK, "%s", res.Error)
	after := paraSnapshots(t, dst)
	require.Equal(t, "cell two edited", after[2].Text, "¶3 addresses the cell's second paragraph")
	after[2] = before[2]
	require.Equal(t, before, after, "nothing else may move")

	// Deleting one of a cell's two paragraphs is allowed; deleting the last
	// remaining paragraph of a cell is rejected readably.
	dst, res = runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpDeleteParagraph, Index: 3},
	})
	require.True(t, res.OK, "%s", res.Error)
	after = paraSnapshots(t, dst)
	require.Len(t, after, 3)
	require.Equal(t, []string{"before table", "cell one", "after table"},
		[]string{after[0].Text, after[1].Text, after[2].Text})

	_, res = runEdit(t, NewDocxEngine(), dst, []Op{
		{Type: OpDeleteParagraph, Index: 2},
	})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "only paragraph of its table cell")
}

// TestDocxEngine_RejectionsAreReadableResults covers the spec scenario: a
// nonexistent target, a malformed operation, and an unknown operation come
// back as readable results that do not end the turn — and write nothing.
func TestDocxEngine_RejectionsAreReadableResults(t *testing.T) {
	fixture := styledDocx(t)
	src := writeFixture(t, t.TempDir(), "in.docx", fixture)

	t.Run("nonexistent paragraph", func(t *testing.T) {
		dst, res := runEdit(t, NewDocxEngine(), src, []Op{
			{Type: OpReplaceParagraph, Index: 9, Text: "nope"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "¶9 not found")
		require.Contains(t, res.Error, "3 paragraphs")
		_, err := os.Stat(dst)
		require.True(t, os.IsNotExist(err), "a rejected batch must not write a result")
	})

	t.Run("missing index", func(t *testing.T) {
		_, res := runEdit(t, NewDocxEngine(), src, []Op{
			{Type: OpReplaceParagraph, Text: "no index"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "1-based paragraph index")
	})

	t.Run("unknown operation", func(t *testing.T) {
		_, res := runEdit(t, NewDocxEngine(), src, []Op{
			{Type: "make_it_nicer"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "unknown operation")
		require.Contains(t, res.Error, "make_it_nicer")
	})

	t.Run("empty placeholder values", func(t *testing.T) {
		_, res := runEdit(t, NewDocxEngine(), src, []Op{
			{Type: OpFillPlaceholders},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "values map")
	})
}

// TestDocxEngine_NotADocxIsReadable covers the processing-failure lane: a
// file that is not a DOCX is a readable result, not a Go error.
func TestDocxEngine_NotADocxIsReadable(t *testing.T) {
	src := writeFixture(t, t.TempDir(), "in.docx", []byte("this is not a zip archive, let alone a DOCX"))
	_, res := runEdit(t, NewDocxEngine(), src, []Op{
		{Type: OpReplaceParagraph, Index: 1, Text: "x"},
	})
	require.False(t, res.OK)
	require.Contains(t, res.Error, "not a readable office document")
}

// TestDocxEngine_RejectsOversizeInput covers the 20 MiB input cap (D9).
func TestDocxEngine_RejectsOversizeInput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.docx")
	f, err := os.Create(src)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(maxInputBytes+1))
	require.NoError(t, f.Close())

	dst := filepath.Join(dir, "out.docx")
	res, err := NewDocxEngine().Edit(context.Background(), src, dst, nil)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "over the 20 MiB limit")
	_, err = os.Stat(dst)
	require.True(t, os.IsNotExist(err))
}

// TestScanDocxBody pins the body scanner against hand-written document.xml:
// self-closing paragraphs count, text-box paragraphs do not, and table-cell
// paragraphs carry their own addresses — matching what docxgo reconstructs.
func TestScanDocxBody(t *testing.T) {
	const wml = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	tests := []struct {
		name string
		body string
		want []string // text content of each addressable ¶, in order
	}{
		{
			name: "plain body paragraphs",
			body: `<w:document ` + wml + `><w:body><w:p><w:r><w:t>one</w:t></w:r></w:p>` +
				`<w:p><w:r><w:t>two</w:t></w:r></w:p></w:body></w:document>`,
			want: []string{"one", "two"},
		},
		{
			name: "self-closing paragraph counts",
			body: `<w:document ` + wml + `><w:body><w:p/>` +
				`<w:p><w:r><w:t>after empty</w:t></w:r></w:p></w:body></w:document>`,
			want: []string{"", "after empty"},
		},
		{
			name: "cell paragraphs count, inner textbox paragraphs do not",
			body: `<w:document ` + wml + `><w:body>` +
				`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>cell</w:t></w:r></w:p>` +
				`<w:p><w:r><w:t>holder</w:t><w:drawing><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` +
				`<w:txbxContent><w:p><w:r><w:t>inner</w:t></w:r></w:p></w:txbxContent>` +
				`</a:graphic></w:drawing></w:r></w:p></w:tc></w:tr></w:tbl>` +
				`<w:p><w:r><w:t>body</w:t></w:r></w:p></w:body></w:document>`,
			// Three addressable ¶s: the cell paragraph, the paragraph that
			// carries the text box (its raw span contains the box's text),
			// and the body paragraph. The p inside txbxContent is not
			// counted — docxgo reconstructs no block for it.
			want: []string{"cell", "holderinner", "body"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scan, err := scanDocxBody([]byte(tt.body))
			require.NoError(t, err)
			require.Len(t, scan.paragraphs, len(tt.want))
			for i, want := range tt.want {
				span := scan.paragraphs[i]
				require.Greater(t, span.end, span.start, "¶%d must have a non-empty span", i+1)
				require.Equal(t, want, fragmentText(tt.body[span.start:span.end]))
			}
		})
	}
}

// fragmentText extracts the <w:t> contents of a paragraph fragment, standing
// in for re-reading the edited file in the scanner tests.
func fragmentText(fragment string) string {
	var b strings.Builder
	rest := fragment
	for {
		open := strings.Index(rest, "<w:t>")
		if open < 0 {
			break
		}
		rest = rest[open:]
		gt := strings.Index(rest, ">")
		if gt < 0 {
			break
		}
		rest = rest[gt+1:]
		end := strings.Index(rest, "</w:t>")
		if end < 0 {
			break
		}
		b.WriteString(rest[:end])
		rest = rest[end+len("</w:t>"):]
	}
	return b.String()
}
