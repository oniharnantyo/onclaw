package documents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// buildXlsx authors an XLSX fixture with excelize and returns its bytes.
func buildXlsx(t *testing.T, build func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	build(f)
	path := filepath.Join(t.TempDir(), "fixture.xlsx")
	require.NoError(t, f.SaveAs(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

// sheetRows re-reads a workbook from disk and captures one sheet's rows.
func sheetRows(t *testing.T, path, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenFile(path)
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	return rows
}

// gridXlsx builds the standard two-sheet fixture: a Data grid to edit and a
// Notes sheet that must never move.
func gridXlsx(t *testing.T) []byte {
	return buildXlsx(t, func(f *excelize.File) {
		require.NoError(t, f.SetSheetName("Sheet1", "Data"))
		for i, row := range [][]string{{"item", "qty"}, {"apples", "4"}, {"pears", "9"}} {
			for j, v := range row {
				cell, err := excelize.CoordinatesToCellName(j+1, i+1)
				require.NoError(t, err)
				require.NoError(t, f.SetCellValue("Data", cell, v))
			}
		}
		notes, err := f.NewSheet("Notes")
		require.NoError(t, err)
		require.NoError(t, f.SetCellValue("Notes", "A1", "keep me"))
		_ = notes
	})
}

// TestXlsxEngine_SetCell covers set_cell: text, numeric, and boolean values
// land with spreadsheet entry typing, and nothing else moves.
func TestXlsxEngine_SetCell(t *testing.T) {
	fixture := gridXlsx(t)
	src := writeFixture(t, t.TempDir(), "in.xlsx", fixture)

	dst, res := runEdit(t, NewXlsxEngine(), src, []Op{
		{Type: OpSetCell, Sheet: "Data", Cell: "B3", Text: "12"},
		{Type: OpSetCell, Sheet: "Data", Cell: "A3", Text: "plums"},
		{Type: OpSetCell, Sheet: "Data", Cell: "C1", Text: "true"},
	})
	require.True(t, res.OK, "%s", res.Error)

	f, err := excelize.OpenFile(dst)
	require.NoError(t, err)
	defer f.Close()
	got, err := f.GetCellValue("Data", "B3")
	require.NoError(t, err)
	require.Equal(t, "12", got)
	cellType, err := f.GetCellType("Data", "B3")
	require.NoError(t, err)
	require.NotEqual(t, excelize.CellTypeSharedString, cellType,
		"numeric-looking values are written as numbers, not text")
	require.NotEqual(t, excelize.CellTypeInlineString, cellType)

	got, err = f.GetCellValue("Data", "A3")
	require.NoError(t, err)
	require.Equal(t, "plums", got)
	cellType, err = f.GetCellType("Data", "C1")
	require.NoError(t, err)
	require.Equal(t, excelize.CellTypeBool, cellType, "true/false are written as booleans")

	require.Equal(t, [][]string{{"item", "qty", "TRUE"}, {"apples", "4"}, {"plums", "12"}},
		sheetRows(t, dst, "Data"), "GetRows renders booleans like Excel and trims trailing blanks")
	require.Equal(t, [][]string{{"keep me"}}, sheetRows(t, dst, "Notes"), "the untouched sheet never moves")
}

// TestXlsxEngine_ClearCell covers clear_cell: the value goes, the rest of
// the row stays.
func TestXlsxEngine_ClearCell(t *testing.T) {
	fixture := gridXlsx(t)
	src := writeFixture(t, t.TempDir(), "in.xlsx", fixture)

	dst, res := runEdit(t, NewXlsxEngine(), src, []Op{
		{Type: OpClearCell, Sheet: "Data", Cell: "B2"},
	})
	require.True(t, res.OK, "%s", res.Error)

	require.Equal(t, [][]string{{"item", "qty"}, {"apples"}, {"pears", "9"}},
		sheetRows(t, dst, "Data"))
}

// TestXlsxEngine_InsertAndDeleteRow covers the row operations: insert shifts
// the rows below down; delete shifts them back up.
func TestXlsxEngine_InsertAndDeleteRow(t *testing.T) {
	fixture := gridXlsx(t)
	src := writeFixture(t, t.TempDir(), "in.xlsx", fixture)

	dst, res := runEdit(t, NewXlsxEngine(), src, []Op{
		{Type: OpInsertRow, Sheet: "Data", Index: 2},
	})
	require.True(t, res.OK, "%s", res.Error)
	rows := sheetRows(t, dst, "Data")
	require.Len(t, rows, 4, "a row was added")
	require.Equal(t, []string{"item", "qty"}, rows[0], "the header stays")
	require.Equal(t, []string{"pears", "9"}, rows[3], "the rows below shifted down")

	dst, res = runEdit(t, NewXlsxEngine(), dst, []Op{
		{Type: OpDeleteRow, Sheet: "Data", Index: 2},
	})
	require.True(t, res.OK, "%s", res.Error)
	rows = sheetRows(t, dst, "Data")
	require.Len(t, rows, 3)
	require.Equal(t, []string{"apples", "4"}, rows[1], "the rows below shifted back up")
	require.Equal(t, [][]string{{"keep me"}}, sheetRows(t, dst, "Notes"))
}

// TestXlsxEngine_RejectionsAreReadableResults covers unknown sheets, missing
// fields, unknown operations, and non-workbook files as readable results.
func TestXlsxEngine_RejectionsAreReadableResults(t *testing.T) {
	fixture := gridXlsx(t)
	src := writeFixture(t, t.TempDir(), "in.xlsx", fixture)

	t.Run("unknown sheet", func(t *testing.T) {
		_, res := runEdit(t, NewXlsxEngine(), src, []Op{
			{Type: OpSetCell, Sheet: "Sales", Cell: "A1", Text: "x"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, `sheet "Sales" not found`)
		require.Contains(t, res.Error, "Data", "the rejection names the sheets that exist")
	})

	t.Run("missing cell", func(t *testing.T) {
		_, res := runEdit(t, NewXlsxEngine(), src, []Op{
			{Type: OpSetCell, Sheet: "Data", Text: "x"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "cell address")
	})

	t.Run("unknown operation", func(t *testing.T) {
		_, res := runEdit(t, NewXlsxEngine(), src, []Op{
			{Type: "sort_everything"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "unknown operation")
	})

	t.Run("not a workbook", func(t *testing.T) {
		plain := writeFixture(t, t.TempDir(), "in.xlsx", []byte("not a zip at all"))
		_, res := runEdit(t, NewXlsxEngine(), plain, []Op{
			{Type: OpSetCell, Sheet: "Data", Cell: "A1", Text: "x"},
		})
		require.False(t, res.OK)
		require.Contains(t, res.Error, "not a readable office document")
	})
}
