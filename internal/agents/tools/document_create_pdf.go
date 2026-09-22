package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
)

// generatePDF dispatches the pdf format routes:
// 1. data.source "html" (or data.html present): renders agent-authored HTML
//    through the injected PDFHTMLRenderer (headless Chrome).
// 2. data.source "structured" (or data.invoice present): renders an invoice
//    with pure-Go fpdf — available on every deployment.
// 3. data.markdown / data.text / data.content (or source "markdown"): renders
//    general articles, notes, reports with pure-Go fpdf.
func generatePDF(ctx context.Context, renderer PDFHTMLRenderer, data json.RawMessage, outputPath string) error {
	if len(data) == 0 {
		return fmt.Errorf("pdf data is required: {\"markdown\":\"# Title\\n...\"} or {\"html\":\"...\"} or {\"invoice\":{...}}")
	}
	var probe struct {
		Source   string          `json:"source"`
		HTML     string          `json:"html"`
		Markdown string          `json:"markdown"`
		Text     string          `json:"text"`
		Content  string          `json:"content"`
		Invoice  json.RawMessage `json:"invoice"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("parse pdf data: %w", err)
	}
	src := strings.ToLower(probe.Source)
	if src == "html" || probe.HTML != "" {
		return generatePDFHTML(ctx, renderer, data, outputPath)
	}
	if src == "invoice" || (src == "structured" && len(probe.Invoice) > 0) || (src == "" && len(probe.Invoice) > 0) {
		return generatePDFStructured(ctx, data, outputPath)
	}
	if src == "markdown" || src == "text" || src == "document" || probe.Markdown != "" || probe.Text != "" || probe.Content != "" {
		return generatePDFMarkdown(ctx, data, outputPath)
	}
	if src == "structured" {
		return generatePDFStructured(ctx, data, outputPath)
	}
	return generatePDFMarkdown(ctx, data, outputPath)
}

// generatePDFMarkdown renders a markdown document into an A4 PDF using pure-Go
// fpdf (available on every deployment).
func generatePDFMarkdown(ctx context.Context, data json.RawMessage, outputPath string) error {
	var in struct {
		Markdown string `json:"markdown"`
		Text     string `json:"text"`
		Content  string `json:"content"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse pdf markdown data: %w", err)
	}
	md := in.Markdown
	if md == "" {
		md = in.Text
	}
	if md == "" {
		md = in.Content
	}
	if in.Title != "" && !strings.HasPrefix(strings.TrimSpace(md), "#") {
		md = "# " + in.Title + "\n\n" + md
	}
	if strings.TrimSpace(md) == "" {
		return fmt.Errorf("pdf structured data must contain \"markdown\", \"html\", or \"invoice\" {seller, buyer, number, date, line_items, tax_rate, notes}")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	pdfBytes, err := renderMarkdownPDF(md)
	if err != nil {
		return fmt.Errorf("render markdown pdf: %w", err)
	}
	if err := os.WriteFile(outputPath, pdfBytes, 0644); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}
	return nil
}

func cleanPDFText(s string) string {
	r := strings.NewReplacer(
		"“", "\"", "”", "\"",
		"‘", "'", "’", "'",
		"—", " - ", "–", "-",
		"…", "...",
		"\u00a0", " ",
		"•", "-",
	)
	return r.Replace(s)
}

func stripSimpleMarkdown(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func renderMarkdownPDF(markdown string) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	lines := strings.Split(markdown, "\n")
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r ")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			pdf.Ln(3)
			continue
		}

		if strings.HasPrefix(trimmed, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			pdf.SetFont("helvetica", "B", 18)
			pdf.SetTextColor(17, 17, 17)
			pdf.Ln(2)
			pdf.MultiCell(0, 8, tr(cleanPDFText(title)), "", "L", false)
			pdf.Ln(2)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			pdf.SetFont("helvetica", "B", 14)
			pdf.SetTextColor(34, 34, 34)
			pdf.Ln(2)
			pdf.MultiCell(0, 7, tr(cleanPDFText(title)), "", "L", false)
			pdf.Ln(1)
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			pdf.SetFont("helvetica", "B", 12)
			pdf.SetTextColor(51, 51, 51)
			pdf.Ln(1)
			pdf.MultiCell(0, 6, tr(cleanPDFText(title)), "", "L", false)
			pdf.Ln(1)
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "• ") {
			bulletText := strings.TrimSpace(trimmed[2:])
			bulletText = stripSimpleMarkdown(bulletText)
			pdf.SetFont("helvetica", "", 10)
			pdf.SetTextColor(30, 30, 30)
			pdf.SetX(24)
			pdf.MultiCell(0, 5, "-  "+tr(cleanPDFText(bulletText)), "", "L", false)
			continue
		}

		pText := stripSimpleMarkdown(trimmed)
		pdf.SetFont("helvetica", "", 10)
		pdf.SetTextColor(30, 30, 30)
		pdf.MultiCell(0, 5.5, tr(cleanPDFText(pText)), "", "L", false)
		pdf.Ln(1)
	}

	if err := pdf.Error(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// generatePDFHTML renders agent-supplied HTML/CSS via the injected renderer
// (D4). A nil renderer encodes a deployment without headless Chrome; render
// failures surface as structured errors pointing at the structured route.
func generatePDFHTML(ctx context.Context, renderer PDFHTMLRenderer, data json.RawMessage, outputPath string) error {
	var in struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse pdf html data: %w", err)
	}
	if strings.TrimSpace(in.HTML) == "" {
		return fmt.Errorf("pdf html data must contain non-empty \"html\"")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if renderer == nil {
		return fmt.Errorf("HTML-to-PDF rendering is unavailable on this deployment (headless Chrome is not installed); generate the PDF from structured invoice data instead with data.source = \"structured\"")
	}
	pdfBytes, err := renderer.RenderHTMLToPDF(ctx, in.HTML)
	if err != nil {
		return fmt.Errorf("HTML rendering failed (%v); use the structured route instead: data.source = \"structured\" with invoice data", err)
	}
	if len(pdfBytes) == 0 {
		return fmt.Errorf("HTML rendering produced no output; use the structured route instead: data.source = \"structured\" with invoice data")
	}
	if err := os.WriteFile(outputPath, pdfBytes, 0644); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}
	return nil
}

// generatePDFStructured renders an invoice from structured data with fpdf
// (D4): seller, buyer, number, date, line items, tax, totals, notes.
func generatePDFStructured(ctx context.Context, data json.RawMessage, outputPath string) error {
	var in struct {
		Invoice *pdfInvoiceInput `json:"invoice"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("parse pdf invoice data: %w", err)
	}
	if in.Invoice == nil {
		return fmt.Errorf("pdf structured data must contain \"invoice\" {seller, buyer, number, date, line_items, tax_rate, notes}")
	}
	inv := in.Invoice
	if len(inv.LineItems) == 0 {
		return fmt.Errorf("pdf invoice must contain at least one line item in \"line_items\"")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	pdfBytes, err := renderInvoicePDF(inv)
	if err != nil {
		return fmt.Errorf("render invoice: %w", err)
	}
	if err := os.WriteFile(outputPath, pdfBytes, 0644); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}
	return nil
}

// pdfInvoiceInput is the structured invoice schema (design.md D4).
type pdfInvoiceInput struct {
	Seller    string           `json:"seller"`
	Buyer     string           `json:"buyer"`
	Number    string           `json:"number"`
	Date      string           `json:"date"`
	LineItems []pdfInvoiceLine `json:"line_items"`
	TaxRate   float64          `json:"tax_rate"`
	Notes     string           `json:"notes"`
}

type pdfInvoiceLine struct {
	Description string  `json:"description"`
	Qty         float64 `json:"qty"`
	UnitPrice   float64 `json:"unit_price"`
}

// renderInvoicePDF lays out a clean one-page A4 invoice: header, parties,
// item table, subtotal/tax/total box, notes.
func renderInvoicePDF(inv *pdfInvoiceInput) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	// Header: title + invoice identity.
	pdf.SetFont("helvetica", "B", 20)
	pdf.CellFormat(0, 12, "INVOICE", "", 1, "L", false, 0, "")
	pdf.SetFont("helvetica", "", 10)
	if inv.Number != "" {
		pdf.CellFormat(0, 6, "Invoice #: "+inv.Number, "", 1, "L", false, 0, "")
	}
	if inv.Date != "" {
		pdf.CellFormat(0, 6, "Date: "+inv.Date, "", 1, "L", false, 0, "")
	}
	pdf.Ln(4)

	// Parties.
	pdf.SetFont("helvetica", "B", 11)
	pdf.CellFormat(0, 7, "From", "", 1, "L", false, 0, "")
	pdf.SetFont("helvetica", "", 10)
	for _, line := range pdfTextLines(inv.Seller) {
		pdf.CellFormat(0, 6, line, "", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
	pdf.SetFont("helvetica", "B", 11)
	pdf.CellFormat(0, 7, "Bill To", "", 1, "L", false, 0, "")
	pdf.SetFont("helvetica", "", 10)
	for _, line := range pdfTextLines(inv.Buyer) {
		pdf.CellFormat(0, 6, line, "", 1, "L", false, 0, "")
	}
	pdf.Ln(4)

	// Line-item table.
	widths := []float64{90, 20, 40, 40}
	headers := []string{"Description", "Qty", "Unit Price", "Amount"}
	aligns := []string{"L", "R", "R", "R"}
	pdf.SetFont("helvetica", "B", 10)
	pdf.SetFillColor(229, 229, 229)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 8, h, "1", 0, aligns[i], true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("helvetica", "", 10)
	pdf.SetFillColor(255, 255, 255)
	var subtotal float64
	for _, item := range inv.LineItems {
		if err := pdf.Error(); err != nil {
			return nil, err
		}
		amount := item.Qty * item.UnitPrice
		subtotal += amount
		desc := item.Description
		if desc == "" {
			desc = "-"
		}
		pdf.CellFormat(widths[0], 7, desc, "1", 0, aligns[0], false, 0, "")
		pdf.CellFormat(widths[1], 7, formatQty(item.Qty), "1", 0, aligns[1], false, 0, "")
		pdf.CellFormat(widths[2], 7, formatMoney(item.UnitPrice), "1", 0, aligns[2], false, 0, "")
		pdf.CellFormat(widths[3], 7, formatMoney(amount), "1", 1, aligns[3], false, 0, "")
	}
	if err := pdf.Error(); err != nil {
		return nil, err
	}

	// Totals box, right-aligned in the amount columns.
	tax := subtotal * inv.TaxRate / 100
	total := subtotal + tax
	pdf.Ln(2)
	totals := []struct{ label, value string }{
		{"Subtotal", formatMoney(subtotal)},
	}
	if inv.TaxRate > 0 {
		totals = append(totals, struct{ label, value string }{
			"Tax (" + formatQty(inv.TaxRate) + "%)", formatMoney(tax),
		})
	}
	totals = append(totals, struct{ label, value string }{"Total", formatMoney(total)})
	for i, row := range totals {
		fontStyle := ""
		if i == len(totals)-1 {
			fontStyle = "B"
		}
		pdf.SetFont("helvetica", fontStyle, 10)
		pdf.CellFormat(110, 7, "", "", 0, "", false, 0, "")
		pdf.CellFormat(40, 7, row.label, "1", 0, "R", false, 0, "")
		pdf.CellFormat(40, 7, row.value, "1", 1, "R", false, 0, "")
	}

	if notes := strings.TrimSpace(inv.Notes); notes != "" {
		pdf.Ln(4)
		pdf.SetFont("helvetica", "I", 9)
		pdf.MultiCell(0, 5, "Notes: "+notes, "", "L", false)
	}

	if err := pdf.Error(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pdfTextLines splits a party block into renderable lines (newlines become
// separate rows); an empty block renders as a single dash line.
func pdfTextLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{"-"}
	}
	return strings.Split(s, "\n")
}

// formatMoney renders an amount with thousands separators and 2 decimals.
func formatMoney(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	dot := strings.Index(s, ".")
	intPart := s
	frac := ""
	if dot >= 0 {
		intPart, frac = s[:dot], s[dot:]
	}
	var grouped strings.Builder
	for i, d := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(d)
	}
	return grouped.String() + frac
}

// formatQty renders a quantity without float noise (2 → "2", 2.5 → "2.5").
func formatQty(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
