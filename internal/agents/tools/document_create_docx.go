package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// generateDOCX builds a minimal but valid OOXML document from markdown input
// (design.md D2): data = {"markdown": "..."} (data.text accepted as alias),
// supporting h1-h3 headings, paragraphs, **bold** / *italic* inline runs,
// "- " bullets, and | pipe tables |. Hand-written XML templates per D2 —
// no styles.xml is shipped; Word applies its built-in heading styles.
func generateDOCX(ctx context.Context, data json.RawMessage, outputPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("docx data is required: {\"markdown\": \"...\"}")
	}
	var in struct {
		Markdown string `json:"markdown"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse docx data: %w", err)
	}
	markdown := in.Markdown
	if markdown == "" {
		markdown = in.Text
	}
	if strings.TrimSpace(markdown) == "" {
		return fmt.Errorf("docx data must contain non-empty \"markdown\" text")
	}

	body, err := markdownToDocxBody(ctx, markdown)
	if err != nil {
		return err
	}

	entries := []zipEntry{
		{Name: "[Content_Types].xml", Data: []byte(docxContentTypes)},
		{Name: "_rels/.rels", Data: []byte(docxRootRels)},
		{Name: "word/document.xml", Data: []byte(docxDocumentXML(body))},
	}
	if err := writeZipFile(outputPath, entries); err != nil {
		return fmt.Errorf("write docx: %w", err)
	}
	return nil
}

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const docxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

// docxDocumentXML wraps a rendered body in the document part skeleton (A4
// page with 1-inch margins).
func docxDocumentXML(body string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>` + body +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>` +
		`</w:body></w:document>`
}

var (
	docxHeadingRe = regexp.MustCompile(`^(#{1,3})\s+(.*)$`)
	docxBulletRe  = regexp.MustCompile(`^[-*]\s+(.*)$`)
	docxInlineRe  = regexp.MustCompile(`\*\*[^*]+\*\*|\*[^*]+\*`)
)

// markdownToDocxBody converts markdown lines into w:body content: headings,
// bullets, pipe tables (consecutive |-prefixed lines), and paragraphs.
func markdownToDocxBody(ctx context.Context, markdown string) (string, error) {
	var sb strings.Builder
	var tableRows [][]string

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		sb.WriteString(docxTableXML(tableRows))
		tableRows = nil
	}

	for _, line := range strings.Split(markdown, "\n") {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "|") {
			cells := splitPipeRow(trimmed)
			if len(cells) > 0 && !isTableSeparatorRow(cells) {
				tableRows = append(tableRows, cells)
				continue
			}
			if len(cells) > 0 {
				continue // separator row inside a table block
			}
		}
		flushTable()

		if trimmed == "" {
			continue
		}
		if m := docxHeadingRe.FindStringSubmatch(trimmed); m != nil {
			level := len(m[1])
			sb.WriteString(docxParagraphXML(fmt.Sprintf("Heading%d", level), docxInlineRuns(m[2])))
			continue
		}
		if m := docxBulletRe.FindStringSubmatch(trimmed); m != nil {
			sb.WriteString(docxBulletXML(docxInlineRuns(m[1])))
			continue
		}
		sb.WriteString(docxParagraphXML("", docxInlineRuns(trimmed)))
	}
	flushTable()
	return sb.String(), nil
}

// docxInlineRuns converts **bold** / *italic* markdown spans into w:r runs;
// plain segments become unstyled runs.
func docxInlineRuns(text string) string {
	var sb strings.Builder
	pos := 0
	for _, loc := range docxInlineRe.FindAllStringIndex(text, -1) {
		if literal := text[pos:loc[0]]; literal != "" {
			sb.WriteString(docxRunXML("", literal))
		}
		span := text[loc[0]:loc[1]]
		if strings.HasPrefix(span, "**") {
			sb.WriteString(docxRunXML("<w:b/>", span[2:len(span)-2]))
		} else {
			sb.WriteString(docxRunXML("<w:i/>", span[1:len(span)-1]))
		}
		pos = loc[1]
	}
	if literal := text[pos:]; literal != "" {
		sb.WriteString(docxRunXML("", literal))
	}
	return sb.String()
}

func docxRunXML(rPr, text string) string {
	return "<w:r>" + rPr + `<w:t xml:space="preserve">` + xmlEscapeText(text) + "</w:t></w:r>"
}

func docxParagraphXML(style, runsXML string) string {
	pPr := ""
	if style != "" {
		pPr = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return "<w:p>" + pPr + runsXML + "</w:p>"
}

// docxBulletXML renders a "- " bullet as an indented literal bullet paragraph
// (v1 docx authoring carries no numbering.xml part).
func docxBulletXML(runsXML string) string {
	return `<w:p><w:pPr><w:ind w:left="720"/></w:pPr>` + docxRunXML("", "•  ") + runsXML + "</w:p>"
}

// docxTableXML renders pipe-table rows as a bordered w:tbl with evenly split
// column widths (9360 twips of usable A4 width).
func docxTableXML(rows [][]string) string {
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return ""
	}
	colW := 9360 / cols

	var grid strings.Builder
	for i := 0; i < cols; i++ {
		grid.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, colW))
	}

	const borders = `<w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="E5E5E5"/>` +
		`<w:left w:val="single" w:sz="4" w:space="0" w:color="E5E5E5"/>` +
		`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="E5E5E5"/>` +
		`<w:right w:val="single" w:sz="4" w:space="0" w:color="E5E5E5"/>` +
		`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="E5E5E5"/>` +
		`<w:insideV w:val="single" w:sz="4" w:space="0" w:color="E5E5E5"/>` +
		`</w:tblBorders>`

	var trs strings.Builder
	for _, row := range rows {
		trs.WriteString("<w:tr>")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			runs := ""
			if cell != "" {
				runs = docxRunXML("", cell)
			}
			trs.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr><w:p>` + runs + `</w:p></w:tc>`)
		}
		trs.WriteString("</w:tr>")
	}

	return `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/>` + borders + `</w:tblPr><w:tblGrid>` +
		grid.String() + `</w:tblGrid>` + trs.String() + `</w:tbl>`
}

// docxPlaceholderPartRe matches the WordprocessingML parts whose text the
// template overlay fills: the document body plus headers and footers.
var docxPlaceholderPartRe = regexp.MustCompile(`^word/(document|header[0-9]+|footer[0-9]+)\.xml$`)

// fillDOCXTemplate clones a .docx template and replaces {{placeholder}} text
// in document/header/footer parts after merging split runs (design.md D3).
func fillDOCXTemplate(ctx context.Context, templatePath string, data json.RawMessage, outputPath string) error {
	values, err := templateStringValues(data)
	if err != nil {
		return fmt.Errorf("docx template fill: %w", err)
	}
	if err := rewriteOOXMLZip(ctx, templatePath, outputPath, func(name string, part []byte) ([]byte, bool, error) {
		if !docxPlaceholderPartRe.MatchString(name) {
			return nil, false, nil
		}
		filled := fillPlaceholdersInPart(string(part), values, wordTags)
		return []byte(filled), true, nil
	}); err != nil {
		return fmt.Errorf("docx template fill: %w", err)
	}
	return nil
}
