package documents

import (
	"bytes"
	"context"
	"strings"

	gopptx "github.com/kenny-not-dead/gopptx"
)

// pptxDoc is the narrow seam over gopptx (design D11): gopptx is
// early-stage and pinned to an exact version, so every call into it sits
// behind this interface — one adapter, primitive types — and a swap to a
// successor library or the stdlib OOXML path is package-local. Tests run
// the engine against fakes of this interface, including the panic-recovery
// guarantee (design D9).
type pptxDoc interface {
	// SlideIDs returns the slides' internal IDs in presentation order.
	SlideIDs() []int
	// Shapes returns the slide's text-carrying shapes in document order.
	Shapes(slideID int) ([]pptxShape, error)
	// SetShapeText replaces the shape's text body with text (one entry per
	// paragraph, split on newlines), keeping the body properties and
	// dressing every paragraph in the original first paragraph's
	// properties and first run's formatting.
	SetShapeText(slideID, shapeID int, text string) error
	// Save serializes the edited presentation.
	Save() ([]byte, error)
	// Close releases the file's resources.
	Close() error
}

// pptxShape is one text-carrying shape in document order.
type pptxShape struct {
	ID   int    // the shape's internal cNvPr id, the handle SetShapeText targets
	Text string // the shape's paragraphs joined by newlines
}

// PptxEngine applies targeted operations to a staged PPTX presentation and
// writes the result: text of existing shapes changes, everything else —
// layouts, masters, media, untouched slides' bytes — stays as it was. It
// never creates layouts or slides.
type PptxEngine struct {
	open func(data []byte) (pptxDoc, error) // seam for tests
}

// NewPptxEngine constructs the PPTX edit engine.
func NewPptxEngine() *PptxEngine {
	return &PptxEngine{open: openGopptx}
}

// Edit applies ops to the presentation at src in the order given and writes
// the result to dst. Rejected operations and processing failures come back
// as readable Result values and nothing is written; a Go error is only for
// infrastructure failures.
func (e *PptxEngine) Edit(_ context.Context, src, dst string, ops []Op) (Result, error) {
	return runGuarded(func() (Result, error) { return e.edit(src, dst, ops) })
}

func (e *PptxEngine) edit(src, dst string, ops []Op) (Result, error) {
	data, res, err := gateInput(src)
	if err != nil {
		return Result{}, err
	}
	if !res.OK {
		return res, nil
	}
	doc, err := e.open(data)
	if err != nil {
		return failf("not a readable PPTX file: %v", err), nil
	}
	defer doc.Close()

	for _, op := range ops {
		switch op.Type {
		case OpReplaceShapeText:
			if res := replaceShapeText(doc, op); !res.OK {
				return res, nil
			}
		case OpFillPlaceholders:
			if res := fillPptxPlaceholders(doc, op); !res.OK {
				return res, nil
			}
		default:
			return failf("unknown operation %q for a PPTX — supported: replace_shape_text, fill_placeholders", op.Type), nil
		}
	}
	out, err := doc.Save()
	if err != nil {
		return failf("serialize the edited presentation: %v", err), nil
	}
	return writeResult(dst, out)
}

// resolveSlide maps the 1-based slide position the views print onto the
// library's internal slide ID.
func resolveSlide(doc pptxDoc, slide int) (int, error) {
	if slide < 1 {
		return 0, readablef("the operation needs a 1-based slide number (slide n, shape m as the views print them)")
	}
	ids := doc.SlideIDs()
	if slide > len(ids) {
		return 0, readablef("slide %d not found: the presentation has %d slide(s)", slide, len(ids))
	}
	return ids[slide-1], nil
}

// replaceShapeText sets the addressed shape's text.
func replaceShapeText(doc pptxDoc, op Op) Result {
	slideID, err := resolveSlide(doc, op.Slide)
	if err != nil {
		return failf("%s", err)
	}
	shapes, err := doc.Shapes(slideID)
	if err != nil {
		return failf("read slide %d: %v", op.Slide, err)
	}
	if op.Shape < 1 {
		return failf("replace_shape_text needs a 1-based shape number (slide n, shape m as the views print them)")
	}
	if op.Shape > len(shapes) {
		return failf("shape %d not found on slide %d: it has %d shape(s)", op.Shape, op.Slide, len(shapes))
	}
	if err := doc.SetShapeText(slideID, shapes[op.Shape-1].ID, op.Text); err != nil {
		return failf("replace text of shape %d on slide %d: %v", op.Shape, op.Slide, err)
	}
	return Result{OK: true}
}

// fillPptxPlaceholders fills {{tokens}} found in the shapes' text across
// every slide. Shapes without tokens keep their bytes untouched.
func fillPptxPlaceholders(doc pptxDoc, op Op) Result {
	if len(op.Values) == 0 {
		return failf("fill_placeholders needs a values map of placeholder keys to replacements")
	}
	for _, slideID := range doc.SlideIDs() {
		shapes, err := doc.Shapes(slideID)
		if err != nil {
			return failf("read a slide: %v", err)
		}
		for _, shape := range shapes {
			if !placeholderPattern.MatchString(shape.Text) {
				continue
			}
			filled := placeholderPattern.ReplaceAllStringFunc(shape.Text, func(match string) string {
				if v, ok := op.Values[placeholderKey(match)]; ok {
					return v
				}
				return match
			})
			if filled == shape.Text {
				continue
			}
			if err := doc.SetShapeText(slideID, shape.ID, filled); err != nil {
				return failf("fill a shape on slide %d: %v", slideID, err)
			}
		}
	}
	return Result{OK: true}
}

// gopptxDoc adapts gopptx.File to the pptxDoc seam. It is the only place
// the library's types appear (design D11).
type gopptxDoc struct{ f *gopptx.File }

// openGopptx opens an edited presentation from its package bytes.
func openGopptx(data []byte) (pptxDoc, error) {
	f, err := gopptx.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return gopptxDoc{f: f}, nil
}

func (d gopptxDoc) SlideIDs() []int { return d.f.GetSlideList() }

func (d gopptxDoc) Shapes(slideID int) ([]pptxShape, error) {
	shapes, err := d.f.GetShapes(slideID)
	if err != nil {
		return nil, err
	}
	out := make([]pptxShape, 0, len(shapes))
	for _, shape := range shapes {
		id := 0
		if shape.NonVisualShapeProperties != nil && shape.NonVisualShapeProperties.CommonNonVisualProperties != nil {
			id = shape.NonVisualShapeProperties.CommonNonVisualProperties.ID
		}
		out = append(out, pptxShape{ID: id, Text: decodeShapeText(shape.TextBody)})
	}
	return out, nil
}

func (d gopptxDoc) SetShapeText(slideID, shapeID int, text string) error {
	shapes, err := d.f.GetShapes(slideID)
	if err != nil {
		return err
	}
	var current *gopptx.DecodeTextBody
	for i := range shapes {
		props := shapes[i].NonVisualShapeProperties
		if props == nil || props.CommonNonVisualProperties == nil {
			continue
		}
		if props.CommonNonVisualProperties.ID == shapeID {
			current = shapes[i].TextBody
			break
		}
	}
	return d.f.SetShapeTextBody(slideID, shapeID, newTextBody(current, text))
}

func (d gopptxDoc) Save() ([]byte, error) {
	buf, err := d.f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (d gopptxDoc) Close() error { return d.f.Close() }

// decodeShapeText joins a text body's paragraphs with newlines.
func decodeShapeText(body *gopptx.DecodeTextBody) string {
	if body == nil {
		return ""
	}
	lines := make([]string, 0, len(body.Paragraph))
	for _, para := range body.Paragraph {
		var b strings.Builder
		for _, run := range para.Runs {
			b.WriteString(run.Text)
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

// newTextBody builds the replacement text body for SetShapeText: one
// paragraph per line, each dressed in the original first paragraph's
// properties and first run's formatting so the shape's look survives the
// rewrite. A shape with no text body gets a minimal one.
func newTextBody(current *gopptx.DecodeTextBody, text string) gopptx.DecodeTextBody {
	body := gopptx.DecodeTextBody{}
	var paraTemplate *gopptx.DecodeParagraph
	var runTemplate *gopptx.DecodeRuns
	if current != nil {
		body.BodyProperties = current.BodyProperties
		if len(current.Paragraph) > 0 {
			paraTemplate = &current.Paragraph[0]
			if len(paraTemplate.Runs) > 0 {
				runTemplate = &paraTemplate.Runs[0]
			}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		para := gopptx.DecodeParagraph{}
		if paraTemplate != nil {
			para.ParagraphProperties = paraTemplate.ParagraphProperties
		}
		run := gopptx.DecodeRuns{Text: line}
		if runTemplate != nil {
			run.RunProperties = runTemplate.RunProperties
		}
		para.Runs = []gopptx.DecodeRuns{run}
		body.Paragraph = append(body.Paragraph, para)
	}
	return body
}
