package tools

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
	"golang.org/x/net/html"

	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// NameDocumentRead is the dotted capability name registered in the tool registry.
const NameDocumentRead = "document.read"

// MaxDocumentReadOutputBytes is the maximum allowed markdown output length (200 KB)
// before truncation occurs, aligned with the inline-text 200 KB ceiling.
const MaxDocumentReadOutputBytes = 200 << 10 // 204800 bytes

// defaultDocumentReadTimeout is the bounded conversion deadline applied to each
// in-process conversion (design.md D2): converters must honor context
// cancellation and never run unbounded.
const defaultDocumentReadTimeout = 30 * time.Second

// truncationDocumentReadNotice is appended when converted markdown exceeds MaxDocumentReadOutputBytes.
const truncationDocumentReadNotice = "\n\n[Output truncated at 200 KB — use filesystem or grep tools if you need to search specific sections]"

// documentConverter converts one on-disk document into markdown. Implementations
// must honor ctx cancellation and treat the file at path as read-only input.
type documentConverter func(ctx context.Context, path string) (string, error)

// defaultDocumentConverters is the in-process converter registry (design.md D2):
// a pure-Go converter per supported extension, registered like any other
// implementation of the same seam — a new format is a new registration.
func defaultDocumentConverters() map[string]documentConverter {
	return map[string]documentConverter{
		".pdf":  convertPDF,
		".xlsx": convertXLSX,
		".docx": convertDOCX,
		".pptx": convertPPTX,
		".html": convertHTML,
		".htm":  convertHTML,
		".csv":  convertDelimited(','),
		".tsv":  convertDelimited('\t'),
	}
}

// supportedDocumentFormats lists the registered extensions (sorted, dotless)
// for the unsupported-format error copy.
func supportedDocumentFormats() []string {
	exts := make([]string, 0, 8)
	for ext := range defaultDocumentConverters() {
		exts = append(exts, strings.TrimPrefix(ext, "."))
	}
	sort.Strings(exts)
	return exts
}

// DocumentReadOption configures the document.read tool.
type DocumentReadOption func(*documentReadTool)

// WithReadOnlyRoots configures extra read-only roots accessible to the tool
// (e.g. drop-lane attachment run directories).
func WithReadOnlyRoots(roots ...string) DocumentReadOption {
	return func(t *documentReadTool) {
		for _, r := range roots {
			if r == "" {
				continue
			}
			abs, err := filepath.Abs(r)
			if err != nil {
				continue
			}
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				// If directory doesn't exist yet, clean and store canonical form
				t.readOnlyRoots = append(t.readOnlyRoots, strings.TrimSuffix(filepath.Clean(abs), string(filepath.Separator)))
				continue
			}
			t.readOnlyRoots = append(t.readOnlyRoots, strings.TrimSuffix(resolved, string(filepath.Separator)))
		}
	}
}

// WithDocumentReadMount overrides the default model-facing mount prefix (default: /workspace).
func WithDocumentReadMount(mount string) DocumentReadOption {
	return func(t *documentReadTool) {
		if mount != "" {
			t.mount = mount
		}
	}
}

// WithDocumentReadTimeout overrides the bounded conversion deadline.
func WithDocumentReadTimeout(timeout time.Duration) DocumentReadOption {
	return func(t *documentReadTool) {
		if timeout > 0 {
			t.timeout = timeout
		}
	}
}

type documentReadTool struct {
	agentDir      string
	readOnlyRoots []string
	mount         string
	timeout       time.Duration
	converters    map[string]documentConverter
}

// NewDocumentRead constructs the document.read tool for one agent workspace.
// A missing agentDir is created at construction (self-heal); the root resolves
// symlinks, and construction fails only on a genuinely unwritable path or a
// non-directory occupying it. Conversion is fully in-process — no external
// runtime exists to be missing (design.md D3).
func NewDocumentRead(agentDir string, opts ...DocumentReadOption) (tool.BaseTool, error) {
	if agentDir == "" {
		return nil, fmt.Errorf("agent workspace directory cannot be empty")
	}
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent workspace path: %w", err)
	}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		return nil, fmt.Errorf("agent workspace is not a directory: %s", abs)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create agent workspace directory: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks in agent workspace path: %w", err)
	}

	t := &documentReadTool{
		agentDir:   strings.TrimSuffix(resolvedRoot, string(filepath.Separator)),
		mount:      backend.DefaultMountPoint,
		timeout:    defaultDocumentReadTimeout,
		converters: defaultDocumentConverters(),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *documentReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameDocumentRead,
		Desc: "Convert a document (PDF, DOCX, XLSX, PPTX, HTML, CSV, TSV) to markdown text using built-in in-process conversion. " +
			"Provide an absolute path under " + t.mount + " or a mounted drop-lane attachment path. " +
			"Corrupt, password-protected, or unsupported documents return an error result naming the document; " +
			"scanned or image-only PDFs report that no extractable text was found.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "The document file path to read.",
				Required: true,
			},
		}),
	}, nil
}

// documentReadArgs is the deserialized tool-call argument shape.
type documentReadArgs struct {
	Path string `json:"path"`
}

// resolve maps the user-provided path against the allowed jail roots (the agent workspace
// and any read-only roots like drop-lane mounts).
func (t *documentReadTool) resolve(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	// Reject obvious escape attempts
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// If path starts with the workspace mount (e.g. /workspace/...), unmount relative to agentDir.
	var candidate string
	if userPath == t.mount {
		candidate = t.agentDir
	} else if rel := strings.TrimPrefix(userPath, t.mount+"/"); rel != userPath && rel != "" {
		candidate = filepath.Join(t.agentDir, filepath.Clean(rel))
	} else if filepath.IsAbs(userPath) {
		// Absolute path on host: check against agentDir or readOnlyRoots
		candidate = filepath.Clean(userPath)
	} else {
		// Relative path: resolve under agentDir
		candidate = filepath.Join(t.agentDir, filepath.Clean(userPath))
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no such file: %s", userPath)
		}
		return "", fmt.Errorf("resolve path: %w", err)
	}

	if !t.isWithinAllowedRoots(resolved) {
		return "", fmt.Errorf("path is outside allowed directories: %q", userPath)
	}

	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat document file: %w", err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", userPath)
	}

	return resolved, nil
}

func (t *documentReadTool) isWithinAllowedRoots(path string) bool {
	cleaned := filepath.Clean(path)
	if isWithinRoot(t.agentDir, cleaned) {
		return true
	}
	for _, root := range t.readOnlyRoots {
		if isWithinRoot(root, cleaned) {
			return true
		}
	}
	return false
}

func isWithinRoot(root, path string) bool {
	if root == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// InvokableRun satisfies tool.InvokableTool. Path validation failures return
// errors (the runtime's tool-error middleware turns them into JSON error
// results); per-document conversion failures return structured error results
// directly, naming the document, so the run continues (design.md D3).
func (t *documentReadTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args documentReadArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("document.read: %w", err)
	}

	resolvedPath, err := t.resolve(args.Path)
	if err != nil {
		return "", fmt.Errorf("document.read: %w", err)
	}

	docName := filepath.Base(resolvedPath)
	ext := strings.ToLower(filepath.Ext(docName))

	converter, ok := t.converters[ext]
	if !ok {
		return documentReadFailure(args.Path, docName, fmt.Sprintf(
			"Cannot read %s: no converter is registered for the %q format. Supported formats: %s.",
			docName, ext, strings.Join(supportedDocumentFormats(), ", "))), nil
	}

	convCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	markdown, err := convertDocument(convCtx, converter, resolvedPath)
	if err != nil {
		if ctx.Err() != nil {
			// The run itself is being torn down; there is no next turn to
			// answer a result, so cancellation propagates as a real error.
			return "", fmt.Errorf("document.read: %w", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return documentReadFailure(args.Path, docName, fmt.Sprintf(
				"Cannot read %s: conversion timed out after %v.", docName, t.timeout)), nil
		}
		return documentReadFailure(args.Path, docName, fmt.Sprintf(
			"Cannot read %s: %v.", docName, err)), nil
	}

	// D5: Output contract
	trimmed := strings.TrimSpace(markdown)
	var finalMarkdown string
	switch {
	case trimmed == "":
		finalMarkdown = fmt.Sprintf("No extractable text found in %s (scanned or image-only document).", docName)
	case len(trimmed) > MaxDocumentReadOutputBytes:
		finalMarkdown = trimmed[:MaxDocumentReadOutputBytes] + truncationDocumentReadNotice
	default:
		finalMarkdown = trimmed
	}

	out, err := json.Marshal(map[string]any{
		"path":     args.Path,
		"name":     docName,
		"markdown": finalMarkdown,
	})
	if err != nil {
		return "", fmt.Errorf("document.read: encode result: %w", err)
	}

	return string(out), nil
}

// documentReadFailure renders the structured per-document error result
// (design.md D3): a result the model reads and reacts to, never a run failure.
func documentReadFailure(userPath, docName, msg string) string {
	out, err := json.Marshal(map[string]any{
		"path":     userPath,
		"name":     docName,
		"error":    msg,
		"markdown": "",
	})
	if err != nil {
		return msg
	}
	return string(out)
}

// convertDocument runs one converter under panic recovery — corrupt or
// malformed documents can make third-party parsers panic, and per design.md D3
// every such failure is a structured per-document error, not a run failure.
func convertDocument(ctx context.Context, conv documentConverter, path string) (markdown string, err error) {
	defer func() {
		if r := recover(); r != nil {
			markdown = ""
			err = fmt.Errorf("conversion failed: %v", r)
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	markdown, err = conv(ctx, path)
	if err != nil {
		return "", err
	}
	if cerr := ctx.Err(); cerr != nil {
		return "", cerr
	}
	return markdown, nil
}

// --- PDF (ledongthuc/pdf) -------------------------------------------------

// convertPDF extracts per-page text from a PDF. A scanned/image-only PDF
// yields no text runs and returns an empty string; the caller renders the
// distinct "no extractable text" result (design.md D5).
func convertPDF(ctx context.Context, path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("not a valid PDF: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("extract text from page %d: %w", i, err)
		}
		sb.WriteString(strings.TrimSpace(text))
		sb.WriteString("\n\n")
	}
	return sb.String(), nil
}

// --- XLSX (excelize/v2) ----------------------------------------------------

// convertXLSX renders each sheet as a "## <sheet name>" heading followed by a
// markdown table whose header row is the sheet's first row.
func convertXLSX(ctx context.Context, path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("not a valid xlsx: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	for _, sheet := range f.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		if len(rows) == 0 {
			continue
		}
		sb.WriteString("## " + sheet + "\n\n")
		sb.WriteString(markdownTable(rows))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// --- CSV / TSV (encoding/csv) ----------------------------------------------

// convertDelimited renders delimiter-separated data (comma for csv, tab for
// tsv) as a markdown table with the first record as the header row.
func convertDelimited(comma rune) documentConverter {
	return func(ctx context.Context, path string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		r := csv.NewReader(f)
		r.Comma = comma
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			return "", fmt.Errorf("parse delimited data: %w", err)
		}
		for len(rows) > 0 && isEmptyDelimitedRow(rows[len(rows)-1]) {
			rows = rows[:len(rows)-1]
		}
		if len(rows) == 0 {
			return "", nil
		}
		return markdownTable(rows), nil
	}
}

func isEmptyDelimitedRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// markdownTable renders rows as a GitHub-flavored markdown table, padding
// short rows to the widest row and escaping pipe characters and newlines.
func markdownTable(rows [][]string) string {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return ""
	}
	pad := func(row []string) []string {
		cells := make([]string, width)
		copy(cells, row)
		return cells
	}

	var sb strings.Builder
	sb.WriteString("| " + strings.Join(markdownCells(pad(rows[0])), " | ") + " |\n")
	sb.WriteString("|")
	for i := 0; i < width; i++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")
	for _, row := range rows[1:] {
		sb.WriteString("| " + strings.Join(markdownCells(pad(row)), " | ") + " |\n")
	}
	return sb.String()
}

func markdownCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "\\", "\\\\")
		cell = strings.ReplaceAll(cell, "|", "\\|")
		cell = strings.ReplaceAll(cell, "\r", " ")
		cell = strings.ReplaceAll(cell, "\n", " ")
		out[i] = strings.TrimSpace(cell)
	}
	return out
}

// --- DOCX (stdlib archive/zip + encoding/xml over word/document.xml) -------

// docxTableState accumulates one w:tbl's rows and cells; the stack supports
// tables nested inside table cells.
type docxTableState struct {
	rows    [][]string
	cells   []string
	cell    strings.Builder
	hasCell bool
}

type docxState struct {
	sb strings.Builder

	paraText  strings.Builder
	paraStyle string

	runText   strings.Builder
	runBold   bool
	runItalic bool

	tableDepth int
	tables     []docxTableState
}

// convertDOCX walks word/document.xml with encoding/xml, mapping paragraphs
// (Heading1-3 styles → #/##/###), bold/italic runs → **/*, and w:tbl tables →
// markdown tables.
func convertDOCX(ctx context.Context, path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("not a valid docx (corrupt or not a zip archive): %w", err)
	}
	defer zr.Close()

	var document *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			document = f
			break
		}
	}
	if document == nil {
		return "", fmt.Errorf("not a valid docx (missing word/document.xml)")
	}

	rc, err := document.Open()
	if err != nil {
		return "", fmt.Errorf("open word/document.xml: %w", err)
	}
	defer rc.Close()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	st := &docxState{}
	decoder := xml.NewDecoder(rc)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse word/document.xml: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			st.start(el)
		case xml.EndElement:
			st.end(el)
		case xml.CharData:
			st.text(string(el))
		}
	}
	return st.sb.String(), nil
}

func (st *docxState) start(el xml.StartElement) {
	switch el.Name.Local {
	case "p":
		st.paraText.Reset()
		st.paraStyle = ""
	case "pStyle":
		for _, attr := range el.Attr {
			if attr.Name.Local == "val" {
				st.paraStyle = attr.Value
			}
		}
	case "r":
		st.runText.Reset()
		st.runBold = false
		st.runItalic = false
	case "b":
		st.runBold = true
	case "i":
		st.runItalic = true
	case "tab":
		st.appendRunText("\t")
	case "br":
		st.appendRunText("\n")
	case "tbl":
		st.tableDepth++
		st.tables = append(st.tables, docxTableState{})
	case "tc":
		table := &st.tables[len(st.tables)-1]
		table.cell.Reset()
		table.hasCell = true
	}
}

func (st *docxState) end(el xml.EndElement) {
	switch el.Name.Local {
	case "t", "tab", "br", "b", "i", "pStyle":
		// handled at enclosing boundaries
	case "r":
		st.flushRun()
	case "p":
		st.flushParagraph()
	case "tc":
		table := &st.tables[len(st.tables)-1]
		table.cells = append(table.cells, strings.TrimSpace(table.cell.String()))
		table.cell.Reset()
		table.hasCell = false
	case "tr":
		table := &st.tables[len(st.tables)-1]
		table.rows = append(table.rows, table.cells)
		table.cells = nil
	case "tbl":
		table := st.tables[len(st.tables)-1]
		st.tables = st.tables[:len(st.tables)-1]
		st.tableDepth--
		rendered := ""
		if len(table.rows) > 0 {
			rendered = markdownTable(table.rows)
		}
		if st.tableDepth > 0 {
			// Nested table: its markdown becomes text of the enclosing cell.
			if rendered != "" {
				st.tables[len(st.tables)-1].cell.WriteString(rendered)
			}
			return
		}
		if rendered != "" {
			st.sb.WriteString("\n\n" + rendered + "\n")
		}
	}
}

func (st *docxState) text(data string) {
	if st.runText.Len() > 0 || st.runBold || st.runItalic {
		st.runText.WriteString(data)
		return
	}
	st.paraText.WriteString(data)
}

// appendRunText writes literal whitespace tokens (tab, br) into the open run,
// or the paragraph when no run is open.
func (st *docxState) appendRunText(s string) {
	if st.runText.Len() > 0 || st.runBold || st.runItalic {
		st.runText.WriteString(s)
		return
	}
	st.paraText.WriteString(s)
}

// flushRun closes a w:r, wrapping its text in markdown emphasis per the run's
// bold/italic run properties.
func (st *docxState) flushRun() {
	text := st.runText.String()
	bold, italic := st.runBold, st.runItalic
	st.runText.Reset()
	st.runBold = false
	st.runItalic = false
	if text == "" {
		return
	}
	switch {
	case bold && italic:
		text = "***" + strings.TrimSpace(text) + "***"
	case bold:
		text = "**" + strings.TrimSpace(text) + "**"
	case italic:
		text = "*" + strings.TrimSpace(text) + "*"
	}
	st.paraText.WriteString(text)
}

// flushParagraph closes a w:p: headings map to markdown ATX prefixes, plain
// paragraphs emit as-is; inside a table cell the paragraph text joins the cell.
func (st *docxState) flushParagraph() {
	text := strings.TrimSpace(st.paraText.String())
	st.paraText.Reset()
	style := st.paraStyle
	st.paraStyle = ""

	if st.tableDepth > 0 {
		table := &st.tables[len(st.tables)-1]
		if table.hasCell {
			if table.cell.Len() > 0 {
				table.cell.WriteString(" ")
			}
			table.cell.WriteString(text)
		}
		return
	}
	if text == "" {
		return
	}
	switch style {
	case "Heading1", "heading 1":
		st.sb.WriteString("\n\n# " + text + "\n\n")
	case "Heading2", "heading 2":
		st.sb.WriteString("\n\n## " + text + "\n\n")
	case "Heading3", "heading 3":
		st.sb.WriteString("\n\n### " + text + "\n\n")
	default:
		st.sb.WriteString(text + "\n\n")
	}
}

// --- PPTX (stdlib archive/zip + encoding/xml over ppt/slides/slideN.xml) ----

// convertPPTX renders each slide (sorted numerically) as a "## Slide N"
// heading followed by its a:p paragraph text runs as lines.
func convertPPTX(ctx context.Context, path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("not a valid pptx (corrupt or not a zip archive): %w", err)
	}
	defer zr.Close()

	type slideRef struct {
		num  int
		file *zip.File
	}
	var slides []slideRef
	for _, f := range zr.File {
		if num, ok := pptxSlideNumber(f.Name); ok {
			slides = append(slides, slideRef{num: num, file: f})
		}
	}
	if len(slides) == 0 {
		return "", fmt.Errorf("not a valid pptx (no slides found)")
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].num < slides[j].num })

	var sb strings.Builder
	for _, slide := range slides {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		lines, err := pptxSlideLines(slide.file)
		if err != nil {
			return "", err
		}
		sb.WriteString(fmt.Sprintf("## Slide %d\n\n", slide.num))
		for _, line := range lines {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// pptxSlideNumber matches ppt/slides/slideN.xml and returns N.
func pptxSlideNumber(zipName string) (int, bool) {
	dir, base := filepath.Split(zipName)
	if dir != "ppt/slides/" || !strings.HasPrefix(base, "slide") || !strings.HasSuffix(base, ".xml") {
		return 0, false
	}
	num, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(base, "slide"), ".xml"))
	if err != nil || num <= 0 {
		return 0, false
	}
	return num, true
}

// pptxSlideLines extracts one slide's text: each a:p paragraph becomes a line
// from its concatenated a:t text runs.
func pptxSlideLines(slide *zip.File) ([]string, error) {
	rc, err := slide.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", slide.Name, err)
	}
	defer rc.Close()

	var lines []string
	var para strings.Builder
	decoder := xml.NewDecoder(rc)
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", slide.Name, err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "p" {
				para.Reset()
			}
		case xml.EndElement:
			if el.Name.Local == "p" {
				if text := strings.TrimSpace(para.String()); text != "" {
					lines = append(lines, text)
				}
				para.Reset()
			}
		case xml.CharData:
			para.Write(el)
		}
	}
	return lines, nil
}

// --- HTML (golang.org/x/net/html) ------------------------------------------

// convertHTML walks the parsed DOM, mapping h1-h3, p, li, strong/em, and
// tables to markdown; script/style content is stripped.
func convertHTML(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	var w htmlWalker
	w.walk(doc)
	return collapseHTMLMarkdown(w.sb.String()), nil
}

// htmlWalker emits markdown while walking the DOM, tracking the last emitted
// byte and open inline-marker depth so inter-node spacing stays correct
// without trailing spaces leaking inside **/* runs.
type htmlWalker struct {
	sb          strings.Builder
	last        byte
	inlineDepth int
}

func (w *htmlWalker) writeString(s string) {
	if s == "" {
		return
	}
	w.sb.WriteString(s)
	w.last = s[len(s)-1]
}

// isWordByte reports whether b is part of a word (ASCII letters/digits and
// non-ASCII UTF-8 continuation/lead bytes).
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b >= 0x80:
		return true
	}
	return false
}

// text writes a text node, inserting a single separating space when the
// previous byte is a word character — or a closing emphasis marker.
func (w *htmlWalker) text(data string) {
	collapsed := strings.Join(strings.Fields(data), " ")
	if collapsed == "" {
		return
	}
	switch {
	case w.last == 0:
		// nothing written yet
	case isWordByte(w.last):
		w.writeString(" ")
	case w.last == '*' && w.inlineDepth == 0:
		w.writeString(" ")
	}
	w.writeString(collapsed)
}

// openInline writes an opening emphasis marker, separated from a preceding
// word by a space.
func (w *htmlWalker) openInline(marker string) {
	if isWordByte(w.last) {
		w.writeString(" ")
	}
	w.inlineDepth++
	w.writeString(marker)
}

// closeInline writes a closing emphasis marker.
func (w *htmlWalker) closeInline(marker string) {
	w.inlineDepth--
	w.writeString(marker)
}

func (w *htmlWalker) walk(n *html.Node) {
	if n.Type == html.TextNode {
		w.text(n.Data)
		return
	}
	if n.Type != html.ElementNode {
		w.walkChildren(n)
		return
	}
	switch n.Data {
	case "script", "style", "head", "noscript", "template":
		return
	case "h1", "h2", "h3":
		w.writeString("\n\n" + strings.Repeat("#", int(n.Data[1]-'0')) + " ")
		w.walkChildren(n)
		w.writeString("\n\n")
	case "p":
		w.writeString("\n\n")
		w.walkChildren(n)
		w.writeString("\n\n")
	case "br":
		w.writeString("\n")
	case "li":
		w.writeString("\n- ")
		w.walkChildren(n)
	case "strong", "b":
		w.openInline("**")
		w.walkChildren(n)
		w.closeInline("**")
	case "em", "i":
		w.openInline("*")
		w.walkChildren(n)
		w.closeInline("*")
	case "table":
		w.writeString("\n\n")
		w.writeString(htmlTableMarkdown(n))
		w.writeString("\n\n")
	default:
		w.walkChildren(n)
	}
}

func (w *htmlWalker) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
}

// collapseHTMLMarkdown normalizes the emitted markdown: collapse runs of blank
// lines and trim the edges.
func collapseHTMLMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			blanks++
			if blanks > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blanks = 0
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// htmlTableMarkdown flattens an HTML table into a markdown table: tr rows,
// th/td cells with their inline text content.
func htmlTableMarkdown(table *html.Node) string {
	var rows [][]string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "tr":
				var cells []string
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
						cells = append(cells, strings.TrimSpace(htmlTextContent(c)))
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
				return
			case "table":
				if n != table {
					return // nested table: its cell text is captured by htmlTextContent
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	if len(rows) == 0 {
		return ""
	}
	return markdownTable(rows)
}

// htmlTextContent collects an element subtree's text, preserving **/* emphasis
// from strong/b/em/i and collapsing whitespace.
func htmlTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.Join(strings.Fields(n.Data), " ")
	}
	if n.Type != html.ElementNode {
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			sb.WriteString(htmlTextContent(c))
		}
		return sb.String()
	}
	var sb strings.Builder
	switch n.Data {
	case "strong", "b":
		sb.WriteString("**")
	case "em", "i":
		sb.WriteString("*")
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(htmlTextContent(c))
	}
	switch n.Data {
	case "strong", "b":
		sb.WriteString("**")
	case "em", "i":
		sb.WriteString("*")
	}
	return sb.String()
}
