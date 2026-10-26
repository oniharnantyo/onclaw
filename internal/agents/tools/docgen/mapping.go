package docgen

import (
	picoloom "github.com/alnah/picoloom/v2"
)

// mapPreset translates a buddies v1 preset onto picoloom's conversion input.
// Empty and zero fields are never sent: picoloom applies its built-in
// defaults for unset values, and clobbering them with zero overrides is
// exactly what the fallback rule forbids. The theme is absent by design —
// picoloom resolves styles at converter construction, so the converter owns
// the theme swap (see Converter.engineFor).
func mapPreset(p *Preset) picoloom.Input {
	var in picoloom.Input
	if p == nil {
		return in
	}
	in.Page = mapPage(p.Page)
	in.Cover = mapCover(p.Cover)
	in.Footer = mapFooter(p.Footer)
	in.Watermark = mapWatermark(p.Watermark)
	in.TOC = mapTOC(p.TOC)
	return in
}

// mapPage maps the page geometry; a nil section means no page override. The
// zero fields flow through — picoloom defaults empty size/orientation and a
// non-positive margin at render time.
func mapPage(p *PageSetup) *picoloom.PageSettings {
	if p == nil {
		return nil
	}
	return &picoloom.PageSettings{
		Size:        p.Size,
		Orientation: p.Orientation,
		Margin:      p.MarginInches,
	}
}

// mapCover maps the cover fields; a nil section means no cover page. The
// struct is sent even when every field is empty — presence is the toggle,
// and what renders on an empty cover is the preset author's call.
func mapCover(c *Cover) *picoloom.Cover {
	if c == nil {
		return nil
	}
	return &picoloom.Cover{
		Title:        c.Title,
		Subtitle:     c.Subtitle,
		Author:       c.Author,
		AuthorTitle:  c.AuthorTitle,
		Organization: c.Organization,
		Date:         c.Date,
		Version:      c.Version,
	}
}

// mapFooter maps the footer; it is dropped when it has nothing to render
// (no page number, no text) because an empty footer only widens the bottom
// margin.
func mapFooter(f *FooterStyle) *picoloom.Footer {
	if f == nil || (!f.ShowPageNumber && f.Text == "") {
		return nil
	}
	return &picoloom.Footer{
		Position:       f.Position,
		ShowPageNumber: f.ShowPageNumber,
		Text:           f.Text,
	}
}

// mapWatermark maps the watermark; it is dropped when Text is empty, matching
// picoloom's own no-op rule. The library renders Color, Opacity, and Angle
// verbatim without defaulting them, so zero values take picoloom's exported
// defaults here instead of reaching it as empties (a zero angle of exactly
// 0° is intentionally not expressible in v1).
func mapWatermark(w *Watermark) *picoloom.Watermark {
	if w == nil || w.Text == "" {
		return nil
	}
	mapped := &picoloom.Watermark{
		Text:    w.Text,
		Color:   w.Color,
		Opacity: w.Opacity,
		Angle:   w.Angle,
	}
	if mapped.Color == "" {
		mapped.Color = picoloom.DefaultWatermarkColor
	}
	if mapped.Opacity == 0 {
		mapped.Opacity = picoloom.DefaultWatermarkOpacity
	}
	if mapped.Angle == 0 {
		mapped.Angle = picoloom.DefaultWatermarkAngle
	}
	return mapped
}

// mapTOC maps the table of contents; a nil section means no TOC page. The
// zero depths flow through — picoloom substitutes its default depths when
// both are unset (see its toTOCData).
func mapTOC(t *TOCSettings) *picoloom.TOC {
	if t == nil {
		return nil
	}
	return &picoloom.TOC{
		Title:    t.Title,
		MinDepth: t.MinDepth,
		MaxDepth: t.MaxDepth,
	}
}
