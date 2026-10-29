package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// errNoExtractableText is returned when a PDF yields no text the extractor
// can recover — a scanned or image-only document (fix-pdf-attachment-carriage
// design D4). MultiModalRead propagates it and the tool-error-result
// middleware renders it as a readable error result, so the turn continues.
var errNoExtractableText = errors.New("no extractable text in PDF (likely a scanned document)")

// extractPDFText extracts the document's text layer (fix-pdf-attachment-carriage
// design D4's narrow seam): the first implementation rides pdfcpu — its public
// API hands back each page's decoded content stream, and the stream is reduced
// to the text its text-showing operators draw. Intentionally lossy: font
// encodings beyond the byte-transparent and UTF-16BE cases render as
// approximations, and layout is approximated with line breaks at the
// text-positioning operators. Swapping the implementation is a file-local
// change; the jail's carriage logic never sees the extractor.
func extractPDFText(ctx context.Context, data []byte) (string, error) {
	var content bytes.Buffer
	err := api.ExtractContent(ctx, bytes.NewReader(data), nil, func(page io.Reader, _ int) error {
		if _, err := io.Copy(&content, page); err != nil {
			return err
		}
		content.WriteByte('\n') // page boundary: never glue two pages' operators
		return nil
	}, nil)
	if err != nil {
		return "", fmt.Errorf("extract PDF text: %w", err)
	}

	text := strings.TrimSpace(pdfShownText(content.Bytes()))
	if text == "" {
		return "", errNoExtractableText
	}
	return text, nil
}

// pdfShownText reduces a decoded PDF content stream to the text its
// text-showing operators draw: the string operands of Tj, TJ, ' and " ,
// concatenated within a line, with the text-positioning operators (T* Td TD
// Tm ET and the implied line moves of ' and ") ending the line. Everything
// else — names, numbers, dictionaries, inline images, non-show operators —
// is machinery, not text, and is skipped. This is an extraction heuristic,
// not a renderer.
func pdfShownText(content []byte) string {
	var (
		out     strings.Builder
		line    strings.Builder
		pending []string // string operands buffered since the last operator
	)
	endLine := func() {
		if line.Len() == 0 {
			return
		}
		out.WriteString(line.String())
		out.WriteByte('\n')
		line.Reset()
	}
	flush := func() {
		for _, s := range pending {
			line.WriteString(s)
		}
		pending = pending[:0]
	}
	flushOnNewLine := func() { // ' and " move to the next line, then show
		endLine()
		flush()
	}

	for i := 0; i < len(content); {
		c := content[i]
		switch {
		case pdfIsWhitespace(c):
			i++
		case c == '%': // comment to end of line
			for i < len(content) && content[i] != '\n' && content[i] != '\r' {
				i++
			}
		case c == '(':
			s, n := pdfLiteralString(content[i:])
			pending = append(pending, s)
			i += n
		case c == '<' && i+1 < len(content) && content[i+1] == '<':
			i += 2 // dictionary open: its strings belong to non-show operators
		case c == '<':
			s, n := pdfHexString(content[i:])
			pending = append(pending, s)
			i += n
		case c == '>':
			if i+1 < len(content) && content[i+1] == '>' {
				i += 2 // dictionary close
			} else {
				i++
			}
		case c == '[' || c == ']' || c == '{' || c == '}':
			i++
		case c == '/': // name
			i++
			for i < len(content) && !pdfIsWhitespace(content[i]) && !pdfIsDelimiter(content[i]) {
				i++
			}
		default:
			start := i
			for i < len(content) && !pdfIsWhitespace(content[i]) && !pdfIsDelimiter(content[i]) {
				i++
			}
			token := string(content[start:i])
			switch token {
			case "BI": // inline image: its data is binary, never text
				i += pdfSkipInlineImage(content[i:])
			case "Tj", "TJ":
				flush()
			case "'", "\"":
				flushOnNewLine()
			case "T*", "Td", "TD", "Tm", "ET":
				endLine()
			case "BT":
				// text object begins; the line continues
			default:
				// Numbers are TJ spacing adjustments between string operands
				// and must not drop them; any other operator's string
				// operands are not shown text.
				if !pdfIsNumber(token) {
					pending = pending[:0]
				}
			}
		}
	}
	endLine()
	return out.String()
}

// pdfSkipInlineImage returns the offset past an inline image's data: the
// bytes between BI's dictionary and the terminating EI operator. The EI
// match requires whitespace before it and a delimiter, whitespace, or end of
// stream after it so operand data cannot masquerade as the terminator.
func pdfSkipInlineImage(data []byte) int {
	for i := 0; i+1 < len(data); i++ {
		if data[i] == 'E' && data[i+1] == 'I' &&
			(i == 0 || pdfIsWhitespace(data[i-1])) &&
			(i+2 >= len(data) || pdfIsWhitespace(data[i+2]) || pdfIsDelimiter(data[i+2])) {
			return i + 2
		}
	}
	return len(data)
}

// pdfLiteralString parses the literal string starting at s[0] == '(',
// returning its text and how much of s it consumed. Nested balanced parens
// and the PDF escape sequences are honored; a raw end of line inside the
// string becomes a newline.
func pdfLiteralString(s []byte) (string, int) {
	var raw []byte
	depth := 1
	i := 1
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\':
			i++
			if i >= len(s) {
				continue
			}
			e := s[i]
			switch {
			case e >= '0' && e <= '7': // octal escape, one to three digits
				v := int(e - '0')
				n := 1
				for n < 3 && i+n < len(s) && s[i+n] >= '0' && s[i+n] <= '7' {
					v = v*8 + int(s[i+n]-'0')
					n++
				}
				i += n
				raw = append(raw, byte(v))
			case e == 'n' || e == 'r' || e == 'f':
				raw = append(raw, '\n')
				i++
			case e == 't':
				raw = append(raw, '\t')
				i++
			case e == 'b':
				raw = append(raw, '\b')
				i++
			case e == '\r':
				if i+1 < len(s) && s[i+1] == '\n' {
					i++ // escaped CRLF line continuation
				}
				i++
			case e == '\n':
				i++ // escaped end-of-line continuation
			default:
				raw = append(raw, e)
				i++
			}
		case c == '(':
			depth++
			raw = append(raw, c)
			i++
		case c == ')':
			depth--
			if depth == 0 {
				return pdfStringToText(raw), i + 1
			}
			raw = append(raw, c)
			i++
		case c == '\r' || c == '\n':
			raw = append(raw, '\n')
			if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
		default:
			raw = append(raw, c)
			i++
		}
	}
	return pdfStringToText(raw), i
}

// pdfHexString parses the hex string starting at s[0] == '<' (not '<<'),
// returning its text and how much of s it consumed. Whitespace between the
// digits is ignored; an odd digit count pads with a trailing zero.
func pdfHexString(s []byte) (string, int) {
	var digits []byte
	i := 1
	for i < len(s) && s[i] != '>' {
		if c := s[i]; c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			digits = append(digits, c)
		}
		i++
	}
	if i < len(s) {
		i++ // '>'
	}
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	raw := make([]byte, len(digits)/2)
	for j := range raw {
		// Both digits are validated hex by the scan above, so the parse
		// cannot fail.
		v, _ := strconv.ParseUint(string(digits[j*2:j*2+2]), 16, 8)
		raw[j] = byte(v)
	}
	return pdfStringToText(raw), i
}

// pdfStringToText renders a PDF string's bytes as model-readable text. A
// UTF-16BE string (its BOM is the only encoding marker a PDF string carries)
// decodes as such; otherwise each byte maps through Latin-1 so the output is
// always valid UTF-8.
func pdfStringToText(raw []byte) string {
	if bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) {
		return pdfUTF16BEToText(raw[2:])
	}
	var b strings.Builder
	for _, c := range raw {
		switch {
		case c == '\r' || c == '\n':
			b.WriteByte('\n')
		case c == '\t':
			b.WriteByte('\t')
		case c < 0x20 || c == 0x7F:
			// other control bytes are font machinery, not text
		case c < 0x80:
			b.WriteByte(c)
		default:
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

// pdfUTF16BEToText decodes a UTF-16BE byte string without its BOM.
func pdfUTF16BEToText(raw []byte) string {
	if len(raw)%2 == 1 {
		raw = raw[:len(raw)-1]
	}
	codes := make([]uint16, len(raw)/2)
	for i := range codes {
		codes[i] = uint16(raw[i*2])<<8 | uint16(raw[i*2+1])
	}
	return string(utf16.Decode(codes))
}

// pdfIsWhitespace reports whether c is a PDF white-space character.
func pdfIsWhitespace(c byte) bool {
	switch c {
	case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20:
		return true
	}
	return false
}

// pdfIsDelimiter reports whether c is a PDF delimiter character.
func pdfIsDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

// pdfIsNumber reports whether token is a PDF number operand (an integer or
// real, with optional sign).
func pdfIsNumber(token string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(token), 64)
	return err == nil
}
