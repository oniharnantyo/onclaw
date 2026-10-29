package docgen

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	gopptx "github.com/kenny-not-dead/gopptx"
	"github.com/stretchr/testify/require"
)

// The PPTX engine tests (add-document-editing task 4.2/4.5): the seam unit
// tests run against a hand-written fake; the spec's fill-existing-layouts
// scenario runs against a skeleton authored by gopptx itself — no binary
// fixtures, no network.

// fakePPTXFile records the seam's write calls; its shape tree is fixed at
// construction — the seam has no way to grow it, which is the point.
type fakePPTXFile struct {
	slides  []int
	shapes  map[int][]pptxShape
	written []byte
	sets    []fakePPTXSet
	err     error
}

type fakePPTXSet struct {
	slideID    int
	shapeID    int
	paragraphs []string
}

func (f *fakePPTXFile) SlideIDs() []int { return f.slides }

func (f *fakePPTXFile) Shapes(slideID int) ([]pptxShape, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.shapes[slideID], nil
}

func (f *fakePPTXFile) SetShapeParagraphs(slideID, shapeID int, paragraphs []string) error {
	f.sets = append(f.sets, fakePPTXSet{slideID: slideID, shapeID: shapeID, paragraphs: paragraphs})
	return nil
}

func (f *fakePPTXFile) Save(w io.Writer) error {
	_, err := w.Write(f.written)
	return err
}

func (f *fakePPTXFile) Close() error { return nil }

type fakePPTXLibrary struct {
	file *fakePPTXFile
}

func (l fakePPTXLibrary) Open([]byte) (pptxFile, error) { return l.file, nil }

// TestRenderPPTX_FillsTokensInDocumentOrder pins the content mapping: the
// markdown's blocks fill the skeleton's tokens in document order across
// slides and shapes; shapes without tokens are never written.
func TestRenderPPTX_FillsTokensInDocumentOrder(t *testing.T) {
	file := &fakePPTXFile{
		slides: []int{256, 257},
		shapes: map[int][]pptxShape{
			256: {
				{ID: 7, Paragraphs: []string{"{{title}}", "static subtitle"}},
				{ID: 8, Paragraphs: []string{"no tokens here"}},
			},
			257: {
				{ID: 9, Paragraphs: []string{"{{point_a}}", "{{point_b}}"}},
			},
		},
	}
	lib := fakePPTXLibrary{file: file}
	file.written = []byte("PPTX-BYTES")

	data, err := renderPPTX(lib, []byte("skeleton"), "# Q3 Review\n\nIntro paragraph.\n- Alpha\n- Beta\n- Dropped")
	require.NoError(t, err)
	require.Equal(t, []byte("PPTX-BYTES"), data)

	// Slide 256's token-bearing shape got the first block and kept its
	// static line; the shape without tokens was never written; slide 257's
	// shape got the next two blocks.
	require.Equal(t, []fakePPTXSet{
		{slideID: 256, shapeID: 7, paragraphs: []string{"Q3 Review", "static subtitle"}},
		{slideID: 257, shapeID: 9, paragraphs: []string{"Intro paragraph.", "Alpha"}},
	}, file.sets)
}

// TestRenderPPTX_UnderflowClearsTokens pins the missing-content half: when
// the content runs out, the remaining tokens are cleared — a generated deck
// never shows template artifacts.
func TestRenderPPTX_UnderflowClearsTokens(t *testing.T) {
	file := &fakePPTXFile{
		slides: []int{256},
		shapes: map[int][]pptxShape{
			256: {{ID: 7, Paragraphs: []string{"{{a}}", "{{b}}", "{{c}}"}}},
		},
	}
	_, err := renderPPTX(fakePPTXLibrary{file: file}, []byte("skeleton"), "# Only One")
	require.NoError(t, err)
	require.Equal(t, []string{"Only One", "", ""}, file.sets[0].paragraphs)
}

// TestRenderPPTX_OpenFailureIsReadable pins the error wrap: a skeleton that
// is not a presentation fails with a readable message, and the engine's
// own empty-template guard holds.
func TestRenderPPTX_OpenFailureIsReadable(t *testing.T) {
	lib := failedOpenLib{err: errors.New("not a zip")}
	_, err := renderPPTX(lib, []byte("junk"), "# H")
	require.ErrorContains(t, err, "open PPTX template")
	require.ErrorContains(t, err, "not a zip")
}

type failedOpenLib struct{ err error }

func (l failedOpenLib) Open([]byte) (pptxFile, error) { return nil, l.err }

// TestRenderPPTX_HostileSkeletonDoesNotPanic pins the D9 boundary: a
// panicking library comes back as a readable error, not a gateway crash.
func TestRenderPPTX_HostileSkeletonDoesNotPanic(t *testing.T) {
	_, err := renderPPTX(panickingLib{}, []byte("skeleton"), "# H")
	require.ErrorContains(t, err, "PPTX rendering failed")
}

type panickingLib struct{}

func (panickingLib) Open([]byte) (pptxFile, error) { panic("hostile skeleton") }

// TestConvertPPTX_RequiresTemplate pins the engine-level guard behind the
// tool's readable rejection: no template bytes, no deck.
func TestConvertPPTX_RequiresTemplate(t *testing.T) {
	c := NewConverter()
	t.Cleanup(func() { _ = c.Close() })
	_, err := c.ConvertPPTX(context.Background(), "# H", "/tmp/never.pptx", nil, nil)
	require.ErrorContains(t, err, "needs a template preset")
}

// TestRenderPPTX_GopptxFillsExistingLayoutsOnly covers the spec's scenario
// (document-generation "PPTX presets fill existing layouts only"): a deck
// generated from a gopptx-authored skeleton with more content than the
// slides hold placeholders for fills the tokens, drops the overflow, and
// leaves the slide and shape inventory untouched.
func TestRenderPPTX_GopptxFillsExistingLayoutsOnly(t *testing.T) {
	// The skeleton: one slide (gopptx's NewFile default) carrying two
	// shapes — a title token and a two-line content token block.
	skeletonDoc := gopptx.NewFile()
	body := func(lines ...string) gopptx.DecodeTextBody {
		tb := gopptx.DecodeTextBody{BodyProperties: &gopptx.DecodeBodyProperties{}}
		for _, line := range lines {
			tb.Paragraph = append(tb.Paragraph, gopptx.DecodeParagraph{
				Runs: []gopptx.DecodeRuns{{Text: line}},
			})
		}
		return tb
	}
	titleID, err := skeletonDoc.CreateShape(256, gopptx.DecodeShapeProperties{}, body("{{title}}"))
	require.NoError(t, err)
	pointsID, err := skeletonDoc.CreateShape(256, gopptx.DecodeShapeProperties{}, body("{{point_a}}", "{{point_b}}"))
	require.NoError(t, err)
	var skeleton bytes.Buffer
	_, err = skeletonDoc.WriteTo(&skeleton)
	require.NoError(t, err)

	// Five blocks for three tokens: the last two are dropped.
	data, err := renderPPTX(NewGopptxLibrary(), skeleton.Bytes(),
		"# Q3 Review\n\nIntro paragraph.\n- Alpha\n- Beta\n- Dropped")
	require.NoError(t, err)

	deck, err := gopptx.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = deck.Close() })

	require.Equal(t, []int{256}, deck.GetSlideList(), "no slide was invented")
	shapes, err := deck.GetShapes(256)
	require.NoError(t, err)
	require.Len(t, shapes, 4, "the template's two shapes plus the two authored ones — nothing invented")

	textOf := func(shapeID int) []string {
		var lines []string
		for _, s := range shapes {
			if s.NonVisualShapeProperties.CommonNonVisualProperties.ID == shapeID {
				for _, p := range s.TextBody.Paragraph {
					for _, r := range p.Runs {
						lines = append(lines, r.Text)
					}
				}
			}
		}
		return lines
	}
	require.Equal(t, []string{"Q3 Review"}, textOf(titleID), "the first shape's token took the first block")
	require.Equal(t, []string{"Intro paragraph.", "Alpha"}, textOf(pointsID), "the second shape filled in order; Beta and Dropped were dropped")
}
