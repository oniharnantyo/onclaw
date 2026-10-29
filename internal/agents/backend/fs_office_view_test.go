package backend

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"

	gopptx "github.com/kenny-not-dead/gopptx"
	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xuri/excelize/v2"

	"github.com/oniharnantyo/buddies/internal/agents/tools/documents"
)

// — fixture authors: each office format is authored in-test by the same
// library the edit engines use, so fixtures carry no binaries —

// authorDocx authors a DOCX fixture with docxgo and returns its bytes.
func authorDocx(t *testing.T, build func(doc domain.Document)) []byte {
	t.Helper()
	doc := docx.NewDocument()
	build(doc)
	var buf strings.Builder
	if _, err := doc.WriteTo(&buf); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}
	return []byte(buf.String())
}

// authorXlsx authors an XLSX fixture with excelize and returns its bytes.
func authorXlsx(t *testing.T, build func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	build(f)
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return buf.Bytes()
}

// authorPptx authors a PPTX fixture with gopptx and returns its bytes.
func authorPptx(t *testing.T, build func(f *gopptx.File)) []byte {
	t.Helper()
	f := gopptx.NewFile()
	build(f)
	var buf strings.Builder
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write pptx fixture: %v", err)
	}
	require.NoError(t, f.Close())
	return []byte(buf.String())
}

// writeInJail writes fixture bytes into the jail root and returns the
// jail-relative path to read it by.
func writeInJail(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, name), data, 0o644))
	return name
}

// readInJail reads a jail-relative path through the jail and returns the view
// content, asserting the file on disk is byte-for-byte unchanged by the read
// (the spec's read-does-not-modify requirement).
func readInJail(t *testing.T, root, name string, offset, limit int) string {
	t.Helper()
	jail, err := NewFilesystemJail(root)
	require.NoError(t, err)
	ctx := context.Background()

	before, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)

	res, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: name, Offset: offset, Limit: limit})
	require.NoError(t, err)

	after, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)
	require.Equal(t, before, after, "reading must leave the file byte-for-byte unchanged")
	return res.Content
}

// viewSummary splits a view into its header line and element lines.
func viewSummary(content string) (string, []string) {
	parts := strings.SplitN(content, "\n", 2)
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts[0], strings.Split(parts[1], "\n")
}

// parseDocxViewLine splits a ¶ view line into its index, style, and text.
func parseDocxViewLine(t *testing.T, line string) (index int, style, text string) {
	t.Helper()
	rest, ok := strings.CutPrefix(line, "¶")
	require.True(t, ok, "view line %q must address a paragraph", line)
	numeral, rest, _ := strings.Cut(rest, " ")
	n, err := strconv.Atoi(numeral)
	require.NoError(t, err, "view line %q must carry a ¶ number", line)
	if styled, tail, found := strings.Cut(rest, "]"); found {
		style = strings.TrimPrefix(styled, " [")
		text = strings.TrimPrefix(tail, " ")
	} else {
		text = strings.TrimPrefix(rest, " ")
	}
	return n, style, text
}

// docxViewFixture is the standard DOCX fixture: a styled heading, a plain
// body paragraph, a table whose cell holds two paragraphs, and a paragraph
// after the table — the addressing cases the engines and views must agree on.
func docxViewFixture(t *testing.T) []byte {
	t.Helper()
	return authorDocx(t, func(doc domain.Document) {
		heading, err := doc.AddParagraph()
		require.NoError(t, err)
		require.NoError(t, heading.SetStyle("Heading1"))
		run, err := heading.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("Quarterly Report"))

		body, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err = body.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("Revenue grew in every region."))

		table, err := doc.AddTable(1, 1)
		require.NoError(t, err)
		cell, err := table.Rows()[0].Cell(0)
		require.NoError(t, err)
		first, err := cell.AddParagraph()
		require.NoError(t, err)
		run, err = first.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("cell one"))
		second, err := cell.AddParagraph()
		require.NoError(t, err)
		run, err = second.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("cell two"))

		after, err := doc.AddParagraph()
		require.NoError(t, err)
		run, err = after.AddRun()
		require.NoError(t, err)
		require.NoError(t, run.SetText("after table"))
	})
}

// xlsxViewFixture is the standard XLSX fixture: a sparse sheet (a row gap
// between populated cells) and a second sheet holding a number and a boolean.
func xlsxViewFixture(t *testing.T) []byte {
	t.Helper()
	return authorXlsx(t, func(f *excelize.File) {
		require.NoError(t, f.SetCellValue("Sheet1", "A1", "one"))
		require.NoError(t, f.SetCellValue("Sheet1", "A3", "three"))
		_, err := f.NewSheet("Data")
		require.NoError(t, err)
		require.NoError(t, f.SetCellValue("Data", "C1", true))
		require.NoError(t, f.SetCellValue("Data", "B2", 42.5))
	})
}

// pptxViewFixture is the standard PPTX fixture: a shape with text on the
// seeded first slide and a second slide whose shape carries two paragraphs.
func pptxViewFixture(t *testing.T) []byte {
	t.Helper()
	return authorPptx(t, func(f *gopptx.File) {
		slideIDs := f.GetSlideList()
		require.NotEmpty(t, slideIDs, "a new presentation seeds its first slide")
		seeded := slideIDs[0]
		_, err := f.CreateShape(seeded, gopptx.DecodeShapeProperties{}, gopptx.DecodeTextBody{
			Paragraph: []gopptx.DecodeParagraph{{Runs: []gopptx.DecodeRuns{{Text: "Title text"}}}},
		})
		require.NoError(t, err)

		second, err := f.NewSlide()
		require.NoError(t, err)
		_, err = f.CreateShape(second, gopptx.DecodeShapeProperties{}, gopptx.DecodeTextBody{
			Paragraph: []gopptx.DecodeParagraph{
				{Runs: []gopptx.DecodeRuns{{Text: "line one"}}},
				{Runs: []gopptx.DecodeRuns{{Text: "line two"}}},
			},
		})
		require.NoError(t, err)
	})
}

// docxgoParagraphTexts walks a DOCX with docxgo the way the document tools'
// engines enumerate the ¶-addressable paragraphs (body blocks, then each
// table cell's paragraphs before its nested tables') — the enumeration the
// view must agree with.
func docxgoParagraphTexts(t *testing.T, data []byte) []string {
	t.Helper()
	doc, err := docx.OpenDocumentFromBytes(data)
	require.NoError(t, err)
	var out []string
	for _, block := range doc.Blocks() {
		switch {
		case block.Paragraph != nil:
			out = append(out, block.Paragraph.Text())
		case block.Table != nil:
			out = appendDocxTableTexts(out, block.Table)
		}
	}
	return out
}

func appendDocxTableTexts(out []string, table domain.Table) []string {
	for _, row := range table.Rows() {
		for _, cell := range row.Cells() {
			for _, para := range cell.Paragraphs() {
				out = append(out, para.Text())
			}
			for _, nested := range cell.Tables() {
				out = appendDocxTableTexts(out, nested)
			}
		}
	}
	return out
}

// TestReadOfficeView_DOCX covers the spec scenario: a DOCX read through the
// jailed read path presents the paragraphs and headings in order, with the
// table-cell paragraphs carrying their own ¶ numbers, and leaves the file
// byte-for-byte unchanged.
func TestReadOfficeView_DOCX(t *testing.T) {
	root := t.TempDir()
	name := writeInJail(t, root, "report.docx", docxViewFixture(t))

	content := readInJail(t, root, name, 0, 0)
	summary, lines := viewSummary(content)

	assert.Equal(t, "DOCX view: 5 paragraphs", summary)
	assert.Equal(t, []string{
		"¶1 [Heading1] Quarterly Report",
		"¶2 Revenue grew in every region.",
		"¶3 cell one",
		"¶4 cell two",
		"¶5 after table",
	}, lines)
}

// TestReadOfficeView_DOCXAgreesWithEditEngines pins the cross-task contract:
// the view's ¶ enumeration is exactly the document tools' engines' — the ¶n
// an edit targets is the line the view printed.
func TestReadOfficeView_DOCXAgreesWithEditEngines(t *testing.T) {
	fixture := docxViewFixture(t)
	root := t.TempDir()
	src := filepath.Join(root, "report.docx")
	require.NoError(t, os.WriteFile(src, fixture, 0o644))

	// The view's ¶ texts equal docxgo's own reconstruction of the document —
	// the walk the engines' content operations enumerate over.
	_, lines := viewSummary(readInJail(t, root, "report.docx", 0, 0))
	texts := make([]string, len(lines))
	for i, line := range lines {
		_, _, texts[i] = parseDocxViewLine(t, line)
	}
	assert.Equal(t, docxgoParagraphTexts(t, fixture), texts)

	// The engines' paragraph count is the view's: replacing one past the last
	// ¶ is rejected with the same count the header printed.
	_, res := runDocxEdit(t, src, documents.Op{Type: documents.OpReplaceParagraph, Index: len(lines) + 1, Text: "nope"})
	require.False(t, res.OK)
	assert.Contains(t, res.Error, "the document has 5 paragraphs")

	// Replacing ¶4 — the table cell's second paragraph — changes exactly that
	// view line.
	dst, res := runDocxEdit(t, src, documents.Op{Type: documents.OpReplaceParagraph, Index: 4, Text: "cell two edited"})
	require.True(t, res.OK, "%s", res.Error)
	_, edited := viewSummary(renderFile(t, dst))
	assert.Equal(t, []string{
		"¶1 [Heading1] Quarterly Report",
		"¶2 Revenue grew in every region.",
		"¶3 cell one",
		"¶4 cell two edited",
		"¶5 after table",
	}, edited)
}

// TestReadOfficeView_XLSX covers the spec scenario: an XLSX read presents
// each sheet's populated cells with their addresses, row-major per sheet,
// sparse rows keeping their true row numbers.
func TestReadOfficeView_XLSX(t *testing.T) {
	root := t.TempDir()
	name := writeInJail(t, root, "book.xlsx", xlsxViewFixture(t))

	content := readInJail(t, root, name, 0, 0)
	summary, lines := viewSummary(content)

	assert.Equal(t, "XLSX view: 2 sheets, 4 populated cells", summary)
	assert.Equal(t, []string{
		"Sheet1!A1 = one",
		"Sheet1!A3 = three",
		"Data!C1 = TRUE",
		"Data!B2 = 42.5",
	}, lines)
}

// TestReadOfficeView_XLSXAgreesWithEditEngines pins the grid contract: a
// set_cell at an address the view printed lands on that cell, and the
// re-rendered view shows only that cell changed.
func TestReadOfficeView_XLSXAgreesWithEditEngines(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "book.xlsx")
	require.NoError(t, os.WriteFile(src, xlsxViewFixture(t), 0o644))

	engine := documents.NewXlsxEngine()
	dst := filepath.Join(t.TempDir(), "out.xlsx")
	res, err := engine.Edit(context.Background(), src, dst, []documents.Op{
		{Type: documents.OpSetCell, Sheet: "Data", Cell: "B2", Text: "7"},
	})
	require.NoError(t, err)
	require.True(t, res.OK, "%s", res.Error)

	_, edited := viewSummary(renderFile(t, dst))
	assert.Equal(t, []string{
		"Sheet1!A1 = one",
		"Sheet1!A3 = three",
		"Data!C1 = TRUE",
		"Data!B2 = 7",
	}, edited)
}

// TestReadOfficeView_PPTX covers the structured text view: slides in order,
// each shape addressed as slide n, shape m, a shape's paragraphs joined with
// escaped newlines, and text-less shapes still addressable.
func TestReadOfficeView_PPTX(t *testing.T) {
	root := t.TempDir()
	name := writeInJail(t, root, "deck.pptx", pptxViewFixture(t))

	content := readInJail(t, root, name, 0, 0)
	summary, lines := viewSummary(content)

	// gopptx's new file seeds slide 1 with title and subtitle placeholder shapes.
	assert.Equal(t, "PPTX view: 2 slides, 6 shapes", summary)
	assert.Equal(t, []string{
		"slide 1: shape 1 [title] \"\"",
		"slide 1: shape 2 [subTitle] \"\"",
		"slide 1: shape 3 [shape] \"Title text\"",
		"slide 2: shape 1 [title] \"\"",
		"slide 2: shape 2 [subTitle] \"\"",
		"slide 2: shape 3 [shape] \"line one\\nline two\"",
	}, lines)
}

// TestReadOfficeView_PPTXAgreesWithEditEngines pins the shape contract: a
// replace_shape_text at slide n, shape m lands on the shape the view printed.
func TestReadOfficeView_PPTXAgreesWithEditEngines(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "deck.pptx")
	require.NoError(t, os.WriteFile(src, pptxViewFixture(t), 0o644))

	engine := documents.NewPptxEngine()
	dst := filepath.Join(t.TempDir(), "out.pptx")
	res, err := engine.Edit(context.Background(), src, dst, []documents.Op{
		{Type: documents.OpReplaceShapeText, Slide: 2, Shape: 3, Text: "Replaced lines"},
	})
	require.NoError(t, err)
	require.True(t, res.OK, "%s", res.Error)

	_, edited := viewSummary(renderFile(t, dst))
	assert.Equal(t, []string{
		"slide 1: shape 1 [title] \"\"",
		"slide 1: shape 2 [subTitle] \"\"",
		"slide 1: shape 3 [shape] \"Title text\"",
		"slide 2: shape 1 [title] \"\"",
		"slide 2: shape 2 [subTitle] \"\"",
		"slide 2: shape 3 [shape] \"Replaced lines\"",
	}, edited)
}

// TestReadOfficeView_Pagination mirrors the text read path's offset/limit
// mechanics over the view's element lines, header always present.
func TestReadOfficeView_Pagination(t *testing.T) {
	fixture := authorDocx(t, func(doc domain.Document) {
		for _, text := range []string{"p1", "p2", "p3", "p4", "p5", "p6"} {
			para, err := doc.AddParagraph()
			require.NoError(t, err)
			run, err := para.AddRun()
			require.NoError(t, err)
			require.NoError(t, run.SetText(text))
		}
	})
	path := filepath.Join(t.TempDir(), "doc.docx")
	require.NoError(t, os.WriteFile(path, fixture, 0o644))

	t.Run("no window renders everything", func(t *testing.T) {
		content, ok, err := renderOfficeView(path, 0, 0)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Len(t, strings.Split(content, "\n"), 7)
	})

	t.Run("offset and limit window the element lines", func(t *testing.T) {
		content, _, err := renderOfficeView(path, 2, 2)
		require.NoError(t, err)
		summary, lines := viewSummary(content)
		assert.Equal(t, "DOCX view: 6 paragraphs", summary, "the header stays on every page")
		assert.Equal(t, []string{"¶2 p2", "¶3 p3"}, lines)
	})

	t.Run("offset past the end renders the header only", func(t *testing.T) {
		content, _, err := renderOfficeView(path, 99, 0)
		require.NoError(t, err)
		assert.Equal(t, "DOCX view: 6 paragraphs", content)
	})

	t.Run("MultiModalRead paginates the view too", func(t *testing.T) {
		jail, err := NewFilesystemJail(filepath.Dir(path))
		require.NoError(t, err)
		res, err := jail.(einofs.MultiModalReader).MultiModalRead(context.Background(),
			&einofs.MultiModalReadRequest{ReadRequest: einofs.ReadRequest{FilePath: "doc.docx", Offset: 3, Limit: 2}})
		require.NoError(t, err)
		require.Nil(t, res.Parts, "an office view is text, not an inline part")
		assert.Equal(t, "DOCX view: 6 paragraphs\n¶3 p3\n¶4 p4", res.FileContent.Content)
	})
}

// TestReadOfficeView_TextAndPDFUnaffected covers the read path's other lanes:
// plain text still reads raw with its windowing, PDFs still ride as inline
// parts, and the office view rides MultiModalRead as text.
func TestReadOfficeView_TextAndPDFUnaffected(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	jail, err := NewFilesystemJail(root)
	require.NoError(t, err)
	mmReader := jail.(einofs.MultiModalReader)

	t.Run("text file reads raw", func(t *testing.T) {
		writeInJail(t, root, "notes.txt", []byte("l1\nl2\nl3"))
		res, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: "notes.txt", Offset: 2, Limit: 1})
		require.NoError(t, err)
		assert.Equal(t, "l2", res.Content)
	})

	t.Run("pdf still reads as an inline part", func(t *testing.T) {
		writeInJail(t, root, "paper.pdf", []byte("%PDF-1.4 minimal"))
		res, err := mmReader.MultiModalRead(ctx, &einofs.MultiModalReadRequest{ReadRequest: einofs.ReadRequest{FilePath: "paper.pdf"}})
		require.NoError(t, err)
		require.Len(t, res.Parts, 1)
		assert.Equal(t, einofs.FileContentPartTypePDF, res.Parts[0].Type)
	})

	t.Run("docx reads as a view through MultiModalRead", func(t *testing.T) {
		writeInJail(t, root, "upper.DOCX", docxViewFixture(t))
		res, err := mmReader.MultiModalRead(ctx, &einofs.MultiModalReadRequest{ReadRequest: einofs.ReadRequest{FilePath: "upper.DOCX"}})
		require.NoError(t, err)
		require.Nil(t, res.Parts)
		summary, lines := viewSummary(res.FileContent.Content)
		assert.Equal(t, "DOCX view: 5 paragraphs", summary)
		assert.Equal(t, "¶1 [Heading1] Quarterly Report", lines[0])
	})
}

// TestReadOfficeView_Guards covers design D9's safety lane and the readable
// failure for files that do not parse.
func TestReadOfficeView_Guards(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	jail, err := NewFilesystemJail(root)
	require.NoError(t, err)

	t.Run("junk with a docx extension is a readable error", func(t *testing.T) {
		writeInJail(t, root, "junk.docx", []byte("this is not a zip archive, let alone a DOCX"))
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: "junk.docx", Offset: 0, Limit: 0})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a readable DOCX file")
	})

	t.Run("oversize file is refused", func(t *testing.T) {
		path := filepath.Join(root, "big.docx")
		f, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(officeMaxViewBytes+1))
		require.NoError(t, f.Close())
		_, err = jail.Read(ctx, &einofs.ReadRequest{FilePath: "big.docx", Offset: 0, Limit: 0})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "views are capped at")
	})

	t.Run("archive beyond the decompression budget is refused", func(t *testing.T) {
		previous := officeMaxDecompressedBytes
		officeMaxDecompressedBytes = 64
		t.Cleanup(func() { officeMaxDecompressedBytes = previous })

		writeInJail(t, root, "book.xlsx", xlsxViewFixture(t))
		_, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: "book.xlsx", Offset: 0, Limit: 0})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "safety cap")
	})
}

// — small helpers keeping the test file readable —

func runDocxEdit(t *testing.T, src string, op documents.Op) (string, documents.Result) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out.docx")
	res, err := documents.NewDocxEngine().Edit(context.Background(), src, dst, []documents.Op{op})
	require.NoError(t, err, "a rejected operation is a Result value, never a Go error")
	return dst, res
}

func renderFile(t *testing.T, path string) string {
	t.Helper()
	content, ok, err := renderOfficeView(path, 0, 0)
	require.NoError(t, err)
	require.True(t, ok, "%s must render as an office view", path)
	return content
}
