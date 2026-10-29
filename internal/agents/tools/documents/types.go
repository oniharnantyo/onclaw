// Package documents applies targeted, element-level edits to staged office
// documents — DOCX, XLSX, and PPTX — preserving every other aspect of the
// original file: untouched text, styling, layout, and embedded objects
// (design D5). Addresses are the ones the read-path views print: DOCX
// `¶<n>` (1-based, in body order, table-cell paragraphs counted), PPTX
// `slide <n>, shape <m>` (1-based, document order), XLSX `<Sheet>!<Cell>`.
//
// Result convention (create_document's): rejected operations and processing
// failures are readable Result values the model can act on; a Go error is
// only for infrastructure failures — an unreadable input or an unwritable
// destination.
package documents

import "fmt"

// Operation names accepted by the engines' Op.Type. Each engine rejects
// unknown names and known types whose required fields are missing as
// readable failures, never as Go errors.
const (
	OpReplaceParagraph = "replace_paragraph"  // {index, text}
	OpInsertParagraph  = "insert_paragraph"   // {afterIndex, text, style optional}
	OpDeleteParagraph  = "delete_paragraph"   // {index}
	OpFillPlaceholders = "fill_placeholders"  // {values}
	OpSetCell          = "set_cell"           // {sheet, cell, text}
	OpClearCell        = "clear_cell"         // {sheet, cell}
	OpInsertRow        = "insert_row"         // {sheet, index}
	OpDeleteRow        = "delete_row"         // {sheet, index}
	OpReplaceShapeText = "replace_shape_text" // {slide, shape, text}
)

// Op is one targeted edit operation for an engine's Edit call. Which fields
// matter depends on Type (see the Op* constants); an engine treats a known
// type with missing required fields as a rejected operation.
type Op struct {
	Type string `json:"type"`

	// Index is the 1-based ¶ address for the DOCX paragraph operations and
	// the 1-based row number for the XLSX row operations.
	Index int `json:"index,omitempty"`

	// AfterIndex is the 1-based ¶ address insert_paragraph inserts after;
	// 0 inserts at the very start of the body.
	AfterIndex int `json:"afterIndex,omitempty"`

	// Style is the optional paragraph style ID (as it appears in the
	// document's styles.xml) for a paragraph inserted by insert_paragraph.
	Style string `json:"style,omitempty"`

	// Sheet and Cell address an XLSX cell: a sheet name and an A1-style
	// cell reference such as "B3".
	Sheet string `json:"sheet,omitempty"`
	Cell  string `json:"cell,omitempty"`

	// Slide and Shape address a PPTX shape: the 1-based slide position in
	// presentation order and the 1-based text-carrying shape position on
	// that slide in document order.
	Slide int `json:"slide,omitempty"`
	Shape int `json:"shape,omitempty"`

	// Text is the new text: the replacement text of replace_paragraph and
	// replace_shape_text, the inserted text of insert_paragraph, and the
	// value written by set_cell (numeric-looking values are written as
	// numbers, matching spreadsheet entry behavior).
	Text string `json:"text,omitempty"`

	// Values maps placeholder keys to their replacements for
	// fill_placeholders: key "name" fills both {{name}} and {{.name}}.
	Values map[string]string `json:"values,omitempty"`
}

// Result is the readable outcome of an edit (design D5's error convention):
// OK reports whether every operation was applied and the result written; on
// failure Error explains what to change — an unknown target, a malformed
// operation, an oversized or hostile file, a processing failure — in words
// the model can act on without ending the turn.
type Result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// failf builds a readable failure Result.
func failf(format string, args ...any) Result {
	return Result{OK: false, Error: fmt.Sprintf(format, args...)}
}
