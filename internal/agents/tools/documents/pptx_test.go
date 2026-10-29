package documents

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopptx "github.com/kenny-not-dead/gopptx"
	"github.com/stretchr/testify/require"
)

// fakePptxDoc stands in for the gopptx seam so engine tests run without the
// library, including the panic-injection case for the recover guarantee.
type fakePptxDoc struct {
	ids     []int
	shapes  map[int][]pptxShape
	saved   bool
	closed  bool
	setText func(slideID, shapeID int, text string) error // optional override
}

func newFakePptxDoc() *fakePptxDoc {
	return &fakePptxDoc{
		ids: []int{256, 257},
		shapes: map[int][]pptxShape{
			256: {
				{ID: 2, Text: "Title: {{title}}"},
				{ID: 3, Text: "no tokens here"},
			},
			257: {
				{ID: 5, Text: "Dear {{name}},"},
			},
		},
	}
}

func (f *fakePptxDoc) SlideIDs() []int { return f.ids }

func (f *fakePptxDoc) Shapes(slideID int) ([]pptxShape, error) {
	return f.shapes[slideID], nil
}

func (f *fakePptxDoc) SetShapeText(slideID, shapeID int, text string) error {
	if f.setText != nil {
		return f.setText(slideID, shapeID, text)
	}
	shapes := f.shapes[slideID]
	for i := range shapes {
		if shapes[i].ID == shapeID {
			shapes[i].Text = text
			return nil
		}
	}
	return nil
}

func (f *fakePptxDoc) Save() ([]byte, error) {
	f.saved = true
	return []byte("saved-bytes"), nil
}

func (f *fakePptxDoc) Close() error {
	f.closed = true
	return nil
}

// fakePptxEngine builds a PptxEngine over a fake document; the source file
// only has to clear the input gates.
func fakePptxEngine(t *testing.T, doc pptxDoc) (engine *PptxEngine, src string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.Create("ppt/presentation.xml")
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	src = writeFixture(t, t.TempDir(), "in.pptx", buf.Bytes())
	return &PptxEngine{open: func([]byte) (pptxDoc, error) { return doc, nil }}, src
}

// TestPptxEngine_ReplaceShapeText covers replace_shape_text through the
// seam: the addressed shape's text is set, nothing else is touched.
func TestPptxEngine_ReplaceShapeText(t *testing.T) {
	doc := newFakePptxDoc()
	engine, src := fakePptxEngine(t, doc)

	dst, res := runEdit(t, engine, src, []Op{
		{Type: OpReplaceShapeText, Slide: 1, Shape: 1, Text: "Board Deck"},
	})
	require.True(t, res.OK, "%s", res.Error)
	require.Equal(t, "Board Deck", doc.shapes[256][0].Text)
	require.Equal(t, "no tokens here", doc.shapes[256][1].Text, "the neighboring shape is untouched")
	require.Equal(t, "Dear {{name}},", doc.shapes[257][0].Text, "other slides are untouched")

	raw, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "saved-bytes", string(raw))
	require.True(t, doc.closed, "the seam's file is closed after the edit")
}

// TestPptxEngine_FillPlaceholders covers fill_placeholders: tokens across
// every slide fill; shapes without tokens are never rewritten.
func TestPptxEngine_FillPlaceholders(t *testing.T) {
	doc := newFakePptxDoc()
	engine, src := fakePptxEngine(t, doc)

	_, res := runEdit(t, engine, src, []Op{
		{Type: OpFillPlaceholders, Values: map[string]string{"title": "Q3 Review", "name": "Ada"}},
	})
	require.True(t, res.OK, "%s", res.Error)
	require.Equal(t, "Title: Q3 Review", doc.shapes[256][0].Text)
	require.Equal(t, "no tokens here", doc.shapes[256][1].Text)
	require.Equal(t, "Dear Ada,", doc.shapes[257][0].Text)
}

// TestPptxEngine_FillLeavesUnknownTokens covers the non-strict merge: a
// token without a supplied value stays in place.
func TestPptxEngine_FillLeavesUnknownTokens(t *testing.T) {
	doc := newFakePptxDoc()
	engine, src := fakePptxEngine(t, doc)

	_, res := runEdit(t, engine, src, []Op{
		{Type: OpFillPlaceholders, Values: map[string]string{"name": "Ada"}},
	})
	require.True(t, res.OK, "%s", res.Error)
	require.Equal(t, "Title: {{title}}", doc.shapes[256][0].Text)
	require.Equal(t, "Dear Ada,", doc.shapes[257][0].Text)
}

// TestPptxEngine_RejectionsAreReadableResults covers unknown slides and
// shapes, malformed operations, and empty values as readable results — and
// that a rejected batch writes nothing.
func TestPptxEngine_RejectionsAreReadableResults(t *testing.T) {
	doc := newFakePptxDoc()
	engine, src := fakePptxEngine(t, doc)

	t.Run("unknown slide", func(t *testing.T) {
		dst, res := runEdit(t, engine, src, []Op{
			{Type: OpReplaceShapeText, Slide: 5, Shape: 1, Text: "x"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "slide 5 not found")
		require.Contains(t, res.Error, "2 slide(s)")
		_, err := os.Stat(dst)
		require.True(t, os.IsNotExist(err), "a rejected batch must not write a result")
	})

	t.Run("unknown shape", func(t *testing.T) {
		_, res := runEdit(t, engine, src, []Op{
			{Type: OpReplaceShapeText, Slide: 1, Shape: 9, Text: "x"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "shape 9 not found on slide 1")
	})

	t.Run("missing shape number", func(t *testing.T) {
		_, res := runEdit(t, engine, src, []Op{
			{Type: OpReplaceShapeText, Slide: 1, Text: "x"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "1-based shape number")
	})

	t.Run("unknown operation", func(t *testing.T) {
		_, res := runEdit(t, engine, src, []Op{
			{Type: "add_slide"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "unknown operation")
		require.Contains(t, res.Error, "add_slide")
	})

	t.Run("empty values", func(t *testing.T) {
		_, res := runEdit(t, engine, src, []Op{
			{Type: OpFillPlaceholders},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "values map")
	})
}

// TestPptxEngine_PanicBecomesReadableResult pins the design D9 recover
// guarantee end to end: a library panic mid-operation is a readable failure
// Result, never a gateway crash, and nothing is written.
func TestPptxEngine_PanicBecomesReadableResult(t *testing.T) {
	doc := newFakePptxDoc()
	doc.setText = func(int, int, string) error {
		panic("shape tree corrupted")
	}
	engine, src := fakePptxEngine(t, doc)

	dst := filepath.Join(t.TempDir(), "out.pptx")
	res, err := engine.Edit(context.Background(), src, dst, []Op{
		{Type: OpReplaceShapeText, Slide: 1, Shape: 1, Text: "x"},
	})
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "document processing failed")
	require.Contains(t, res.Error, "shape tree corrupted")
	_, err = os.Stat(dst)
	require.True(t, os.IsNotExist(err), "a panicked batch must not write a result")
}

// buildPptx authors a PPTX fixture with gopptx: two slides, each with one
// text shape, the first slide's shape carrying run formatting.
func buildPptx(t *testing.T) []byte {
	t.Helper()
	f := gopptx.NewFile()
	defer f.Close()

	textBody := func(text string, size int, bold bool) gopptx.DecodeTextBody {
		body := gopptx.DecodeTextBody{BodyProperties: &gopptx.DecodeBodyProperties{}}
		rpr := &gopptx.DecodeRunProperties{Size: &size}
		if bold {
			one := 1
			rpr.Bold = &one
		}
		body.Paragraph = []gopptx.DecodeParagraph{
			{Runs: []gopptx.DecodeRuns{{RunProperties: rpr, Text: text}}},
		}
		return body
	}
	props := gopptx.DecodeShapeProperties{
		Xfrm: &gopptx.DecodeXfrm{
			Offset:  &gopptx.Offset{X: 100, Y: 100},
			Extents: &gopptx.Extents{CX: 4000, CY: 1000},
		},
	}
	_, err := f.CreateShape(256, props, textBody("Hello {{who}}", 2400, true))
	require.NoError(t, err)
	slide2, err := f.NewSlide()
	require.NoError(t, err)
	require.Equal(t, 257, slide2)
	_, err = f.CreateShape(slide2, props, textBody("Second slide text", 1800, false))
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "fixture.pptx")
	require.NoError(t, f.SaveAs(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

// pptxShapeTexts re-reads a PPTX and captures each slide's shape texts and
// first-run formatting.
func pptxShapeTexts(t *testing.T, path string) map[int][]string {
	t.Helper()
	f, err := gopptx.OpenFile(path)
	require.NoError(t, err)
	defer f.Close()
	out := make(map[int][]string)
	for _, slideID := range f.GetSlideList() {
		shapes, err := f.GetShapes(slideID)
		require.NoError(t, err)
		var texts []string
		for _, shape := range shapes {
			texts = append(texts, decodeShapeText(shape.TextBody))
		}
		out[slideID] = texts
	}
	return out
}

// TestPptxEngine_AdapterRoundTrip drives the real gopptx adapter: replace
// one shape's text, fill another, and verify the untouched content and the
// run formatting survive.
func TestPptxEngine_AdapterRoundTrip(t *testing.T) {
	fixture := buildPptx(t)
	before := pptxShapeTexts(t, writeFixture(t, t.TempDir(), "in.pptx", fixture))
	// gopptx's slide template ships two empty placeholder shapes before any
	// created shape, so the authored shape is shape 3 in document order.
	require.Equal(t, map[int][]string{256: {"", "", "Hello {{who}}"}, 257: {"", "", "Second slide text"}}, before)

	src := writeFixture(t, t.TempDir(), "in.pptx", fixture)
	dst, res := runEdit(t, NewPptxEngine(), src, []Op{
		{Type: OpReplaceShapeText, Slide: 2, Shape: 3, Text: "Second slide, revised"},
		{Type: OpFillPlaceholders, Values: map[string]string{"who": "World"}},
	})
	require.True(t, res.OK, "%s", res.Error)

	f, err := gopptx.OpenFile(dst)
	require.NoError(t, err)
	defer f.Close()

	shapes, err := f.GetShapes(256)
	require.NoError(t, err)
	require.Len(t, shapes, 3)
	require.Equal(t, "Hello World", decodeShapeText(shapes[2].TextBody))
	require.NotNil(t, shapes[2].TextBody.Paragraph[0].Runs[0].RunProperties)
	size := shapes[2].TextBody.Paragraph[0].Runs[0].RunProperties.Size
	require.NotNil(t, size)
	require.Equal(t, 2400, *size, "the filled shape keeps its run formatting")
	bold := shapes[2].TextBody.Paragraph[0].Runs[0].RunProperties.Bold
	require.NotNil(t, bold)
	require.Equal(t, 1, *bold, "the filled shape keeps its bold run formatting")

	second, err := f.GetShapes(257)
	require.NoError(t, err)
	require.Len(t, second, 3)
	require.Equal(t, "Second slide, revised", decodeShapeText(second[2].TextBody))
}

// TestPptxEngine_AdapterNotAPPTXIsReadable covers the real adapter's
// processing-failure lane.
func TestPptxEngine_AdapterNotAPPTXIsReadable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.Create("not-a-presentation.txt")
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	src := writeFixture(t, t.TempDir(), "in.pptx", buf.Bytes())

	_, res := runEdit(t, NewPptxEngine(), src, []Op{
		{Type: OpReplaceShapeText, Slide: 1, Shape: 1, Text: "x"},
	})
	require.False(t, res.OK)
	require.True(t, strings.Contains(res.Error, "not a readable PPTX") || strings.Contains(res.Error, "read a slide"),
		"a non-presentation archive must fail readably, got: %s", res.Error)
}
