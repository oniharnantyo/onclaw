package docgen

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	gopptx "github.com/kenny-not-dead/gopptx"
)

// The PPTX generation engine (add-document-editing design D6/D11, task
// 4.2): template-document presets only. The skeleton is copied and its
// existing {{tokens}} in shape text are filled with the content, in
// document order; content beyond the tokens is dropped and no slide or
// shape is ever created — the pptxFile seam below exposes no way to, so
// "fill existing layouts only" is structural, not a discipline. All
// gopptx calls live in the gopptxLibrary adapter at the bottom: the early
// stage dependency is swapped (or replaced by the stdlib OOXML path) as a
// package-local change.

// pptxTokenPattern matches a {{token}} in shape text: word characters
// inside double curly braces, the same vocabulary the DOCX templates and
// the edit tool's placeholder fills speak.
var pptxTokenPattern = regexp.MustCompile(`\{\{\.?(\w+)\}\}`)

// pptxLibrary opens skeleton bytes as a presentation through the seam.
type pptxLibrary interface {
	Open(data []byte) (pptxFile, error)
}

// pptxFile is the narrow slice of an open presentation the engine drives
// (design D11). It deliberately has no create-slide or create-shape
// operation: a PPTX generation fills the skeleton's placeholders and never
// invents layouts (spec document-generation "PPTX presets fill existing
// layouts only").
type pptxFile interface {
	// SlideIDs returns the presentation's slide IDs in document order.
	SlideIDs() []int
	// Shapes returns the slide's text-bearing shapes in document order.
	Shapes(slideID int) ([]pptxShape, error)
	// SetShapeParagraphs replaces the shape's text with one paragraph per
	// given line, preserving the shape and its look.
	SetShapeParagraphs(slideID, shapeID int, paragraphs []string) error
	// Save serializes the presentation.
	Save(w io.Writer) error
	// Close releases the presentation.
	Close() error
}

// pptxShape is one text-bearing shape of a slide.
type pptxShape struct {
	ID         int
	Paragraphs []string // the shape's paragraph texts, in order
}

// renderPPTX fills the skeleton's {{tokens}} with the markdown's content
// blocks, in document order: the first block's text fills the first token,
// and so on. Content beyond the tokens is dropped (the skeleton's layouts
// bound the deck); tokens beyond the content are cleared — a generated
// deck never shows template artifacts. A library panic — a hostile
// skeleton must not take the gateway down (design D9) — comes back as an
// error.
func renderPPTX(lib pptxLibrary, skeleton []byte, markdown string) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("docgen: PPTX rendering failed: %v", r)
		}
	}()
	f, err := lib.Open(skeleton)
	if err != nil {
		return nil, fmt.Errorf("docgen: open PPTX template: %w", err)
	}
	defer f.Close()

	next := 0
	blocks := parseBlocks(markdown)
	for _, slideID := range f.SlideIDs() {
		shapes, err := f.Shapes(slideID)
		if err != nil {
			return nil, fmt.Errorf("docgen: read PPTX slide %d: %w", slideID, err)
		}
		for _, shape := range shapes {
			if !paragraphsHaveToken(shape.Paragraphs) {
				continue
			}
			filled, consumed := fillPPTXParagraphs(shape.Paragraphs, blocks, next)
			next += consumed
			if err := f.SetShapeParagraphs(slideID, shape.ID, filled); err != nil {
				return nil, fmt.Errorf("docgen: fill PPTX shape text: %w", err)
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Save(&buf); err != nil {
		return nil, fmt.Errorf("docgen: write PPTX: %w", err)
	}
	return buf.Bytes(), nil
}

// paragraphsHaveToken reports whether any of the shape's paragraphs carry
// a {{token}} — only such shapes are touched, the rest of the skeleton's
// decoration is left byte-identical.
func paragraphsHaveToken(paragraphs []string) bool {
	for _, p := range paragraphs {
		if pptxTokenPattern.MatchString(p) {
			return true
		}
	}
	return false
}

// fillPPTXParagraphs replaces the tokens of one shape's paragraphs with
// the content blocks starting at next, returning the new paragraph texts
// and how many blocks were consumed. Each token takes one block's text;
// with no block left, the token is cleared. A paragraph keeps its place —
// only its token spans change.
func fillPPTXParagraphs(paragraphs []string, blocks []block, next int) ([]string, int) {
	filled := make([]string, len(paragraphs))
	consumed := 0
	for i, p := range paragraphs {
		if !pptxTokenPattern.MatchString(p) {
			filled[i] = p
			continue
		}
		filled[i] = pptxTokenPattern.ReplaceAllStringFunc(p, func(match string) string {
			idx := next + consumed
			consumed++
			if idx < len(blocks) {
				return blocks[idx].Text
			}
			return "" // content exhausted: the token is cleared, not leaked
		})
	}
	return filled, consumed
}

// gopptxLibrary is the pptxLibrary backed by gopptx (design D11) — the
// only place in the package that names the library.
type gopptxLibrary struct{}

// NewGopptxLibrary returns the real PPTX library seam.
func NewGopptxLibrary() pptxLibrary { return gopptxLibrary{} }

// Open implements pptxLibrary.
func (gopptxLibrary) Open(data []byte) (pptxFile, error) {
	f, err := gopptx.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return gopptxFile{f: f}, nil
}

// gopptxFile adapts one gopptx.File onto the pptxFile seam.
type gopptxFile struct {
	f *gopptx.File
}

// SlideIDs implements pptxFile.
func (g gopptxFile) SlideIDs() []int { return g.f.GetSlideList() }

// Shapes implements pptxFile: the slide's text-bearing shapes in document
// order, each carrying its paragraph texts.
func (g gopptxFile) Shapes(slideID int) ([]pptxShape, error) {
	raw, err := g.f.GetShapes(slideID)
	if err != nil {
		return nil, err
	}
	shapes := make([]pptxShape, 0, len(raw))
	for _, s := range raw {
		var id int
		if s.NonVisualShapeProperties != nil && s.NonVisualShapeProperties.CommonNonVisualProperties != nil {
			id = s.NonVisualShapeProperties.CommonNonVisualProperties.ID
		}
		if s.TextBody == nil {
			continue
		}
		paragraphs := make([]string, 0, len(s.TextBody.Paragraph))
		for _, p := range s.TextBody.Paragraph {
			var sb strings.Builder
			for _, r := range p.Runs {
				sb.WriteString(r.Text)
			}
			paragraphs = append(paragraphs, sb.String())
		}
		shapes = append(shapes, pptxShape{ID: id, Paragraphs: paragraphs})
	}
	return shapes, nil
}

// SetShapeParagraphs implements pptxFile: the shape's text body is
// rewritten with one paragraph per line, keeping the shape's body
// properties and copying the replaced paragraph's run properties (its
// typeface, size, color) so a filled title placeholder keeps the theme's
// look. Paragraphs beyond the originals inherit the last original's run
// properties.
func (g gopptxFile) SetShapeParagraphs(slideID, shapeID int, paragraphs []string) error {
	original, err := g.f.GetShapes(slideID)
	if err != nil {
		return err
	}
	var body *gopptx.DecodeTextBody
	for _, s := range original {
		if s.NonVisualShapeProperties == nil || s.NonVisualShapeProperties.CommonNonVisualProperties == nil ||
			s.NonVisualShapeProperties.CommonNonVisualProperties.ID != shapeID {
			continue
		}
		body = s.TextBody
		break
	}
	if body == nil {
		return gopptx.ErrShapeNotExist{ShapeID: shapeID}
	}
	newBody := gopptx.DecodeTextBody{BodyProperties: body.BodyProperties}
	for i, text := range paragraphs {
		para := gopptx.DecodeParagraph{
			Runs: []gopptx.DecodeRuns{{RunProperties: originalRunProperties(body, i), Text: text}},
		}
		newBody.Paragraph = append(newBody.Paragraph, para)
	}
	return g.f.SetShapeTextBody(slideID, shapeID, newBody)
}

// originalRunProperties picks the run properties to fill paragraph i with:
// the same paragraph's first run when the shape still has one there, the
// shape's last run otherwise — the closest stand-in for the placeholder's
// own look.
func originalRunProperties(body *gopptx.DecodeTextBody, i int) *gopptx.DecodeRunProperties {
	if i < len(body.Paragraph) {
		for _, r := range body.Paragraph[i].Runs {
			if r.RunProperties != nil {
				return r.RunProperties
			}
		}
	}
	for p := len(body.Paragraph) - 1; p >= 0; p-- {
		for _, r := range body.Paragraph[p].Runs {
			if r.RunProperties != nil {
				return r.RunProperties
			}
		}
	}
	return nil
}

// Save implements pptxFile.
func (g gopptxFile) Save(w io.Writer) error { return g.f.Write(w) }

// Close implements pptxFile.
func (g gopptxFile) Close() error { return g.f.Close() }
