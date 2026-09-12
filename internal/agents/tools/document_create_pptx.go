package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// generatePPTX builds a deck from the embedded skeleton (design.md D2):
// data = {"slides":[{"title","bullets",...}]}, cloned per call with one
// generated slideN.xml per slide registered in presentation.xml, its rels,
// and the content types. Notes are skipped silently in v1 (the skeleton
// carries no notes master).
func generatePPTX(ctx context.Context, data json.RawMessage, outputPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("pptx data is required: {\"slides\":[{\"title\",\"bullets\"}]}")
	}
	var in struct {
		Slides []pptxSlideInput `json:"slides"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse pptx data: %w", err)
	}
	if len(in.Slides) == 0 {
		return fmt.Errorf("pptx data must contain at least one slide in \"slides\"")
	}
	for i, slide := range in.Slides {
		if err := ctx.Err(); err != nil {
			return err
		}
		if slide.Title == "" && len(slide.Bullets) == 0 {
			return fmt.Errorf("slide %d has no title and no bullets", i+1)
		}
	}
	if err := writeZipFile(outputPath, buildPPTXZip(in.Slides)); err != nil {
		return fmt.Errorf("write pptx: %w", err)
	}
	return nil
}

// pptxSlideInput is one slide in the from-scratch pptx data shape.
type pptxSlideInput struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
	Notes   string   `json:"notes"` // v1: skipped silently
}

// fillPPTXTemplate clones a .pptx template and replaces {{placeholder}} text
// in slide parts after merging split runs (design.md D3).
func fillPPTXTemplate(ctx context.Context, templatePath string, data json.RawMessage, outputPath string) error {
	values, err := templateStringValues(data)
	if err != nil {
		return fmt.Errorf("pptx template fill: %w", err)
	}
	if err := rewriteOOXMLZip(ctx, templatePath, outputPath, func(name string, part []byte) ([]byte, bool, error) {
		if !pptxSlidePartRe.MatchString(name) {
			return nil, false, nil
		}
		filled := fillPlaceholdersInPart(string(part), values, slideTags)
		return []byte(filled), true, nil
	}); err != nil {
		return fmt.Errorf("pptx template fill: %w", err)
	}
	return nil
}
