package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// generateXLSX builds a workbook from structured sheet/row input (design.md
// D2): data = {"sheets":[{"name","rows":[[cell,...],...],"bold_first_row"}]}
// with a simple {"text": pipe-table} fallback. Cells accept JSON strings,
// numbers, booleans, and null (skipped).
func generateXLSX(ctx context.Context, data json.RawMessage, outputPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("xlsx data is required: {\"sheets\":[{\"name\",\"rows\"}]} or {\"text\": \"| a | b |\"}")
	}
	var in xlsxInput
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse xlsx data: %w", err)
	}

	f := excelize.NewFile()
	defer f.Close()

	if len(in.Sheets) == 0 {
		if strings.TrimSpace(in.Text) == "" {
			return fmt.Errorf("xlsx data must contain \"sheets\" (with \"rows\") or \"text\" (a pipe table)")
		}
		rows, err := parsePipeTable(in.Text)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("xlsx \"text\" contains no table rows")
		}
		if err := writeXLSSheet(f, "Sheet1", rows, false, 0); err != nil {
			return err
		}
	} else {
		for i, sheet := range in.Sheets {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := sheet.Name
			if name == "" {
				name = fmt.Sprintf("Sheet%d", i+1)
			}
			if err := writeXLSSheet(f, name, sheet.Rows, sheet.BoldFirstRow, i); err != nil {
				return err
			}
		}
	}

	if err := f.SaveAs(outputPath); err != nil {
		return fmt.Errorf("write xlsx: %w", err)
	}
	return nil
}

// xlsxInput is the from-scratch xlsx data shape.
type xlsxInput struct {
	Sheets []xlsxSheetInput `json:"sheets"`
	Text   string           `json:"text"`
}

type xlsxSheetInput struct {
	Name         string  `json:"name"`
	Rows         [][]any `json:"rows"`
	BoldFirstRow bool    `json:"bold_first_row"`
}

// writeXLSSheet writes one sheet's rows; idx 0 renames the default Sheet1,
// later indexes create new sheets. boldFirstRow applies a bold style to the
// header row (cell-level styles reachable per the spec's open question).
func writeXLSSheet(f *excelize.File, name string, rows [][]any, boldFirstRow bool, idx int) error {
	if idx == 0 {
		if err := f.SetSheetName("Sheet1", name); err != nil {
			return fmt.Errorf("rename sheet: %w", err)
		}
	} else if _, err := f.NewSheet(name); err != nil {
		return fmt.Errorf("create sheet %q: %w", name, err)
	}
	for r, row := range rows {
		for c, cell := range row {
			cellRef, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return fmt.Errorf("sheet %q: %w", name, err)
			}
			if err := setXLSCell(f, name, cellRef, cell, r+1, c+1); err != nil {
				return err
			}
		}
	}
	if boldFirstRow && len(rows) > 0 && len(rows[0]) > 0 {
		style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
		if err != nil {
			return fmt.Errorf("build header style: %w", err)
		}
		lastRef, err := excelize.CoordinatesToCellName(len(rows[0]), 1)
		if err != nil {
			return fmt.Errorf("sheet %q: %w", name, err)
		}
		if err := f.SetCellStyle(name, "A1", lastRef, style); err != nil {
			return fmt.Errorf("apply header style on %q: %w", name, err)
		}
	}
	return nil
}

func setXLSCell(f *excelize.File, sheet, cellRef string, value any, row, col int) error {
	var v any
	switch tv := value.(type) {
	case nil:
		return nil
	case string:
		v = tv
	case float64:
		v = tv
	case bool:
		v = tv
	default:
		return fmt.Errorf("sheet %q row %d col %d: unsupported cell value type %T", sheet, row, col, value)
	}
	if err := f.SetCellValue(sheet, cellRef, v); err != nil {
		return fmt.Errorf("sheet %q cell %s: %w", sheet, cellRef, err)
	}
	return nil
}

// parsePipeTable parses a markdown-ish pipe table into rows, skipping
// separator rows (| --- | --- |). Escaped pipes (\|) survive the split.
func parsePipeTable(text string) ([][]any, error) {
	var rows [][]any
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitPipeRow(trimmed)
		if len(cells) == 0 {
			continue
		}
		if isTableSeparatorRow(cells) {
			continue
		}
		row := make([]any, len(cells))
		for i, cell := range cells {
			row[i] = cell
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// splitPipeRow splits "| a | b |" into ["a","b"], honoring escaped pipes.
func splitPipeRow(line string) []string {
	const esc = "\x00"
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	line = strings.ReplaceAll(line, `\|`, esc)
	parts := strings.Split(line, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		out[i] = strings.ReplaceAll(p, esc, "|")
	}
	return out
}

func isTableSeparatorRow(cells []string) bool {
	for _, cell := range cells {
		trimmed := strings.Trim(cell, ":-")
		if strings.TrimSpace(cell) != "" && strings.TrimRight(trimmed, "-") != "" {
			return false
		}
	}
	return true
}

// fillXLSXTemplate opens a template workbook and overlays data
// (design.md D3): {"cells": {"Sheet1!B3": value, ...}} writes named cells,
// {"append_rows": {"Sheet1": [[cell,...],...]}} appends below existing data.
// The result is saved-as to the output path; the template file is untouched.
func fillXLSXTemplate(ctx context.Context, templatePath string, data json.RawMessage, outputPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("xlsx template data is required: {\"cells\":{\"Sheet1!B3\":\"v\"}} and/or {\"append_rows\":{\"Sheet1\":[[\"a\"]]}}")
	}
	var in xlsxTemplateData
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse xlsx template data: %w", err)
	}
	if len(in.Cells) == 0 && len(in.AppendRows) == 0 {
		return fmt.Errorf("xlsx template data must contain \"cells\" and/or \"append_rows\"")
	}

	f, err := excelize.OpenFile(templatePath)
	if err != nil {
		return fmt.Errorf("not a valid xlsx template: %w", err)
	}
	defer f.Close()

	for ref, value := range in.Cells {
		if err := ctx.Err(); err != nil {
			return err
		}
		sheet, cellRef, err := splitSheetCellRef(ref)
		if err != nil {
			return err
		}
		if !xlsxHasSheet(f, sheet) {
			return fmt.Errorf("template has no sheet %q", sheet)
		}
		if err := setXLSCell(f, sheet, cellRef, value, 0, 0); err != nil {
			return err
		}
	}
	for sheet, rows := range in.AppendRows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !xlsxHasSheet(f, sheet) {
			return fmt.Errorf("template has no sheet %q", sheet)
		}
		existing, err := f.GetRows(sheet)
		if err != nil {
			return fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		for i, row := range rows {
			for c, cell := range row {
				cellRef, err := excelize.CoordinatesToCellName(c+1, len(existing)+i+1)
				if err != nil {
					return fmt.Errorf("sheet %q: %w", sheet, err)
				}
				if err := setXLSCell(f, sheet, cellRef, cell, i+1, c+1); err != nil {
					return err
				}
			}
		}
	}

	if err := f.SaveAs(outputPath); err != nil {
		return fmt.Errorf("write filled xlsx: %w", err)
	}
	return nil
}

// xlsxTemplateData is the template-overlay data shape (design.md D3).
type xlsxTemplateData struct {
	Cells      map[string]any     `json:"cells"`
	AppendRows map[string][][]any `json:"append_rows"`
}

// splitSheetCellRef splits "Sheet1!B3" into sheet and cell coordinates.
// "!" is not legal inside Excel sheet names, so the first "!" is the split.
func splitSheetCellRef(ref string) (sheet, cell string, err error) {
	parts := strings.SplitN(ref, "!", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid cell reference %q: expected \"Sheet!Cell\" (e.g. \"Sheet1!B3\")", ref)
	}
	return parts[0], parts[1], nil
}

func xlsxHasSheet(f *excelize.File, name string) bool {
	for _, s := range f.GetSheetList() {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}
