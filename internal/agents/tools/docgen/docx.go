package docgen

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"github.com/mmonterroca/docxgo/v2/pkg/template"
)

// The DOCX generation engine (add-document-editing design D6, task 4.2).
// The two preset flavors share the content injection and differ in where
// the look comes from: a template preset copies the uploaded skeleton and
// appends the content into its styled body region (filling its standard
// placeholders), a config preset builds the built-in default template at
// runtime with docxgo and applies the preset's parameters as overrides.
// docxgo sits directly behind these calls — only gopptx needs the narrow
// seam (design D11).

// docxTitleKey is the standard placeholder a template skeleton may fill
// from the generated content: the markdown's first heading. Generation
// knows nothing else about the user's document, so other {{tokens}} are
// left for the user's own mail-merge.
const docxTitleKey = "title"

// renderDOCX renders markdown into DOCX bytes for the preset. A non-empty
// skeleton selects the template flavor; a nil preset (no DOCX preset
// resolved) and an empty preset both mean the built-in default template
// untouched. Library panics — a hostile skeleton must not take the gateway
// down (add-document-editing design D9) — come back as errors.
func renderDOCX(preset *DOCXPreset, skeleton []byte, markdown string) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("docgen: DOCX rendering failed: %v", r)
		}
	}()
	blocks := parseBlocks(markdown)
	var doc domain.Document
	if len(skeleton) > 0 {
		if doc, err = openDOCXTemplate(skeleton, blocks); err != nil {
			return nil, err
		}
	} else {
		if doc, err = buildDefaultDOCX(preset); err != nil {
			return nil, err
		}
	}
	if err := appendDOCXBlocks(doc, blocks); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("docgen: write DOCX: %w", err)
	}
	return buf.Bytes(), nil
}

// openDOCXTemplate opens the uploaded skeleton and fills its standard
// placeholders before any content lands: {{title}} takes the markdown's
// first heading (empty when there is none), so a letterhead's cover line
// tracks the document. The fill rides docxgo's merge machinery, which
// consolidates Word's run-split tokens; other keys are left as-is (the
// non-strict merge), mirroring the edit tool's placeholder semantics.
func openDOCXTemplate(skeleton []byte, blocks []block) (domain.Document, error) {
	doc, err := docx.OpenDocumentFromBytes(skeleton)
	if err != nil {
		return nil, fmt.Errorf("docgen: open DOCX template: %w", err)
	}
	data := template.MergeData{docxTitleKey: firstHeading(blocks)}
	if err := template.MergeTemplate(doc, data); err != nil {
		return nil, fmt.Errorf("docgen: fill DOCX template placeholders: %w", err)
	}
	return doc, nil
}

// buildDefaultDOCX builds the application's built-in default template at
// runtime (design D6: config presets parametrize one shipped default, they
// are not a styling engine): docxgo's fresh document — Title, Heading 1–3,
// and Normal styles among its built-ins — with the preset's page setup,
// fonts, heading treatment, and footer applied as overrides. Zero
// parameters keep the built-in values. The content lands afterwards, so
// the document is never empty when it is written.
func buildDefaultDOCX(preset *DOCXPreset) (domain.Document, error) {
	doc := docx.NewDocument()
	if preset != nil {
		if err := applyDOCXPageSetup(doc, preset.Page); err != nil {
			return nil, err
		}
		if err := applyDOCXStyleOverrides(doc, preset); err != nil {
			return nil, err
		}
		if err := applyDOCXFooter(doc, preset.Footer); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// applyDOCXPageSetup applies the config preset's page geometry: size,
// margins, and orientation. docxgo stores orientation without touching the
// page dimensions, so landscape swaps them here — the one place width and
// height are decided together.
func applyDOCXPageSetup(doc domain.Document, page *DOCXPageSetup) error {
	if page == nil {
		return nil
	}
	sec, err := doc.DefaultSection()
	if err != nil {
		return fmt.Errorf("docgen: page setup: %w", err)
	}
	if size, ok := docxPageSize(page.Size); ok {
		if err := sec.SetPageSize(domain.PageSize(size)); err != nil {
			return fmt.Errorf("docgen: page setup: %w", err)
		}
	}
	if page.MarginInches > 0 {
		// Start from the section's current margins: docxgo's Margins also
		// carries header/footer distances the preset does not touch.
		margins := sec.Margins()
		twips := int(page.MarginInches * 1440)
		margins.Top, margins.Bottom, margins.Left, margins.Right = twips, twips, twips, twips
		if err := sec.SetMargins(margins); err != nil {
			return fmt.Errorf("docgen: page setup: %w", err)
		}
	}
	if page.Orientation == "landscape" {
		size := sec.PageSize()
		if err := sec.SetPageSize(domain.PageSize{Width: size.Height, Height: size.Width}); err != nil {
			return fmt.Errorf("docgen: page setup: %w", err)
		}
		if err := sec.SetOrientation(domain.OrientationLandscape); err != nil {
			return fmt.Errorf("docgen: page setup: %w", err)
		}
	}
	return nil
}

// applyDOCXStyleOverrides writes the config preset's font and heading
// parameters over the built-in styles (the docxgo theme mechanism's
// route: fetch the style, assert it, set what the preset carries).
func applyDOCXStyleOverrides(doc domain.Document, preset *DOCXPreset) error {
	sm := doc.StyleManager()
	if f := preset.Fonts; f != nil {
		style, err := sm.GetStyle(domain.StyleIDNormal)
		if err != nil {
			return fmt.Errorf("docgen: fonts: %w", err)
		}
		if para, ok := style.(domain.ParagraphStyle); ok {
			if f.BodyFamily != "" {
				if err := para.SetFont(domain.Font{Name: f.BodyFamily}); err != nil {
					return fmt.Errorf("docgen: fonts: %w", err)
				}
			}
			if f.BodySize != 0 {
				if err := para.SetSize(sizeHalfPoints(f.BodySize)); err != nil {
					return fmt.Errorf("docgen: fonts: %w", err)
				}
			}
		}
	}
	h := preset.Heading
	if h == nil {
		return nil
	}
	color := domain.Color{}
	hasColor := false
	if h.Color != "" {
		if c, err := parseHexColor(h.Color); err == nil {
			color, hasColor = c, true
		}
	}
	for _, level := range []struct {
		id   string
		size float64 // the preset's HeadingSize, stepped down per level
	}{{domain.StyleIDHeading1, 0}, {domain.StyleIDHeading2, -2}, {domain.StyleIDHeading3, -4}} {
		style, err := sm.GetStyle(level.id)
		if err != nil {
			return fmt.Errorf("docgen: heading: %w", err)
		}
		para, ok := style.(domain.ParagraphStyle)
		if !ok {
			continue
		}
		if hf := preset.Fonts; hf != nil && hf.HeadingFamily != "" {
			if err := para.SetFont(domain.Font{Name: hf.HeadingFamily}); err != nil {
				return fmt.Errorf("docgen: heading: %w", err)
			}
		}
		if hs := preset.Fonts; hs != nil && hs.HeadingSize != 0 {
			size := hs.HeadingSize + level.size
			if size < 9 {
				size = 9
			}
			if err := para.SetSize(sizeHalfPoints(size)); err != nil {
				return fmt.Errorf("docgen: heading: %w", err)
			}
		}
		if h.Bold != nil {
			if err := para.SetBold(*h.Bold); err != nil {
				return fmt.Errorf("docgen: heading: %w", err)
			}
		}
		if hasColor {
			if err := para.SetColor(color); err != nil {
				return fmt.Errorf("docgen: heading: %w", err)
			}
		}
	}
	return nil
}

// applyDOCXFooter builds the footer the config preset asks for: literal
// text and/or a page-number field. No footer configuration, no footer part.
func applyDOCXFooter(doc domain.Document, footer *DOCXFooterStyle) error {
	if footer == nil || (footer.Text == "" && !footer.ShowPageNumber) {
		return nil
	}
	sec, err := doc.DefaultSection()
	if err != nil {
		return fmt.Errorf("docgen: footer: %w", err)
	}
	f, err := sec.Footer(domain.FooterDefault)
	if err != nil {
		return fmt.Errorf("docgen: footer: %w", err)
	}
	para, err := f.AddParagraph()
	if err != nil {
		return fmt.Errorf("docgen: footer: %w", err)
	}
	if footer.Text != "" {
		run, err := para.AddRun()
		if err != nil {
			return fmt.Errorf("docgen: footer: %w", err)
		}
		if err := run.SetText(footer.Text); err != nil {
			return fmt.Errorf("docgen: footer: %w", err)
		}
	}
	if footer.ShowPageNumber {
		run, err := para.AddRun()
		if err != nil {
			return fmt.Errorf("docgen: footer: %w", err)
		}
		if err := run.AddField(docx.NewPageNumberField()); err != nil {
			return fmt.Errorf("docgen: footer: %w", err)
		}
	}
	return nil
}

// appendDOCXBlocks injects the content blocks into the document's body,
// after the template's existing content (design D6: the styled body
// region). Headings map onto the document's Heading 1–3 styles — a
// skeleton that does not define them keeps Word's style fallback — list
// items onto List Paragraph with per-level indentation and literal
// markers (docxgo carries no numbering part of its own, and a template's
// numbering definitions are unknowable, so markers stay visible text).
func appendDOCXBlocks(doc domain.Document, blocks []block) error {
	number := 0
	for i, b := range blocks {
		para, err := doc.AddParagraph()
		if err != nil {
			return fmt.Errorf("docgen: inject DOCX content: %w", err)
		}
		run, err := para.AddRun()
		if err != nil {
			return fmt.Errorf("docgen: inject DOCX content: %w", err)
		}
		text := b.Text
		switch b.Kind {
		case blockHeading1:
			err = para.SetStyle(domain.StyleIDHeading1)
		case blockHeading2:
			err = para.SetStyle(domain.StyleIDHeading2)
		case blockHeading3:
			err = para.SetStyle(domain.StyleIDHeading3)
		case blockParagraph:
			// Normal, the default.
		case blockBullet:
			err = applyDOCXList(para, b)
			text = "• " + text
		case blockNumbered:
			if i == 0 || !blocks[i-1].isNumbered() {
				number = 0 // a fresh list renumbers from 1
			}
			number++
			err = applyDOCXList(para, b)
			text = fmt.Sprintf("%d. ", number) + text
		}
		if err != nil {
			return fmt.Errorf("docgen: inject DOCX content: %w", err)
		}
		if err := run.SetText(text); err != nil {
			return fmt.Errorf("docgen: inject DOCX content: %w", err)
		}
	}
	return nil
}

// applyDOCXList styles one list item: List Paragraph plus a hanging indent
// per nesting level.
func applyDOCXList(para domain.Paragraph, b block) error {
	if err := para.SetStyle(domain.StyleIDListParagraph); err != nil {
		return err
	}
	return para.SetIndentLeft(720 * (b.Level + 1))
}

// firstHeading returns the first heading's text ("" when the content has
// none) — the {{title}} placeholder's value.
func firstHeading(blocks []block) string {
	for _, b := range blocks {
		switch b.Kind {
		case blockHeading1, blockHeading2, blockHeading3:
			return b.Text
		}
	}
	return ""
}

// docxPageSize maps the preset's page vocabulary onto docxgo's sizes; an
// unknown word keeps the built-in default.
func docxPageSize(size string) (docx.PageSize, bool) {
	switch size {
	case "letter":
		return docx.Letter, true
	case "a4":
		return docx.A4, true
	case "legal":
		return docx.Legal, true
	}
	return docx.PageSize{}, false
}

// sizeHalfPoints converts points to docxgo's half-point unit.
func sizeHalfPoints(points float64) int { return int(points*2 + 0.5) }

// parseHexColor decodes #RRGGBB (or RRGGBB) into a docxgo color; callers
// validated the shape at parse time.
func parseHexColor(color string) (domain.Color, error) {
	var c domain.Color
	color = strings.TrimPrefix(color, "#")
	raw, err := hex.DecodeString(color)
	if err != nil || len(raw) != 3 {
		return c, fmt.Errorf("docgen: color %q must be #RRGGBB", color)
	}
	return domain.Color{R: raw[0], G: raw[1], B: raw[2]}, nil
}
