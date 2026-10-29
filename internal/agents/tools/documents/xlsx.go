package documents

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// XlsxEngine applies targeted operations to a staged XLSX workbook and
// writes the result. excelize re-serializes the workbook, leaving untouched
// sheets and cells — and their styling — in place.
type XlsxEngine struct{}

// NewXlsxEngine constructs the XLSX edit engine.
func NewXlsxEngine() *XlsxEngine { return &XlsxEngine{} }

// Edit applies ops to the workbook at src in the order given and writes the
// result to dst. Rejected operations and processing failures come back as
// readable Result values and nothing is written; a Go error is only for
// infrastructure failures.
func (e *XlsxEngine) Edit(_ context.Context, src, dst string, ops []Op) (Result, error) {
	return runGuarded(func() (Result, error) { return e.edit(src, dst, ops) })
}

func (e *XlsxEngine) edit(src, dst string, ops []Op) (Result, error) {
	data, res, err := gateInput(src)
	if err != nil {
		return Result{}, err
	}
	if !res.OK {
		return res, nil
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return failf("not a readable XLSX file: %v", err), nil
	}
	defer f.Close()

	for _, op := range ops {
		switch op.Type {
		case OpSetCell:
			if res := setCell(f, op); !res.OK {
				return res, nil
			}
		case OpClearCell:
			if res := clearCell(f, op); !res.OK {
				return res, nil
			}
		case OpInsertRow:
			if res := insertRow(f, op); !res.OK {
				return res, nil
			}
		case OpDeleteRow:
			if res := deleteRow(f, op); !res.OK {
				return res, nil
			}
		default:
			return failf("unknown operation %q for an XLSX — supported: set_cell, clear_cell, insert_row, delete_row", op.Type), nil
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return failf("serialize the edited workbook: %v", err), nil
	}
	return writeResult(dst, buf.Bytes())
}

// resolveSheet validates the sheet name against the workbook, returning a
// readable rejection that names the sheets that exist.
func resolveSheet(f *excelize.File, sheet, operation string) error {
	if sheet == "" {
		return readablef("%s needs a sheet name", operation)
	}
	for _, name := range f.GetSheetList() {
		if name == sheet {
			return nil
		}
	}
	return readablef("sheet %q not found: the workbook has %s", sheet, sheetList(f))
}

// sheetList renders the workbook's sheet names for rejection messages.
func sheetList(f *excelize.File) string {
	names := f.GetSheetList()
	if len(names) == 0 {
		return "no sheets"
	}
	return "these sheets: " + strings.Join(names, ", ")
}

// setCell writes op.Text to the addressed cell. Numeric-looking values are
// written as numbers and true/false as booleans, matching spreadsheet entry
// behavior; anything else is written as text.
func setCell(f *excelize.File, op Op) Result {
	if err := resolveSheet(f, op.Sheet, OpSetCell); err != nil {
		return failf("%s", err)
	}
	if op.Cell == "" {
		return failf("set_cell needs a cell address (e.g. B3, as the views print it)")
	}
	value := cellValue(op.Text)
	if err := f.SetCellValue(op.Sheet, op.Cell, value); err != nil {
		return failf("set %s!%s: %v — check the cell address (e.g. B3, as the views print it)", op.Sheet, op.Cell, err)
	}
	return Result{OK: true}
}

// clearCell empties the addressed cell, leaving its formatting in place.
func clearCell(f *excelize.File, op Op) Result {
	if err := resolveSheet(f, op.Sheet, OpClearCell); err != nil {
		return failf("%s", err)
	}
	if op.Cell == "" {
		return failf("clear_cell needs a cell address (e.g. B3, as the views print it)")
	}
	if err := f.SetCellValue(op.Sheet, op.Cell, nil); err != nil {
		return failf("clear %s!%s: %v — check the cell address (e.g. B3, as the views print it)", op.Sheet, op.Cell, err)
	}
	return Result{OK: true}
}

// insertRow inserts one empty row above the given 1-based row number,
// shifting the rows below it down.
func insertRow(f *excelize.File, op Op) Result {
	if err := resolveSheet(f, op.Sheet, OpInsertRow); err != nil {
		return failf("%s", err)
	}
	if op.Index < 1 {
		return failf("insert_row needs a 1-based row number (the rows the views print)")
	}
	if err := f.InsertRows(op.Sheet, op.Index, 1); err != nil {
		return failf("insert row %d on %q: %v", op.Index, op.Sheet, err)
	}
	return Result{OK: true}
}

// deleteRow removes the given 1-based row number, shifting the rows below
// it up.
func deleteRow(f *excelize.File, op Op) Result {
	if err := resolveSheet(f, op.Sheet, OpDeleteRow); err != nil {
		return failf("%s", err)
	}
	if op.Index < 1 {
		return failf("delete_row needs a 1-based row number (the rows the views print)")
	}
	if err := f.RemoveRow(op.Sheet, op.Index); err != nil {
		return failf("delete row %d on %q: %v", op.Index, op.Sheet, err)
	}
	return Result{OK: true}
}

// cellValue types op.Text the way a spreadsheet entry would: integers and
// decimals become numbers, true/false become booleans, anything else stays
// text.
func cellValue(text string) any {
	switch strings.ToLower(text) {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseFloat(text, 64); err == nil {
		return n
	}
	return text
}
