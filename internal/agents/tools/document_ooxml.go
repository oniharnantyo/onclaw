package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// zipEntry is one file in a constructed OOXML package.
type zipEntry struct {
	Name string
	Data []byte
}

// writeZipFile writes entries to path as a deflated zip archive.
func writeZipFile(path string, entries []zipEntry) error {
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate})
		if err != nil {
			return fmt.Errorf("zip entry %s: %w", e.Name, err)
		}
		if _, err := w.Write(e.Data); err != nil {
			return fmt.Errorf("zip entry %s: %w", e.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output file: %w", err)
	}
	return nil
}

// ooxmlPartTransform rewrites one package part; returning ok=false copies the
// entry verbatim.
type ooxmlPartTransform func(name string, data []byte) (out []byte, ok bool, err error)

// rewriteOOXMLZip clones the template archive at srcPath to outputPath,
// applying transform to matching parts (design.md D3 template-fill: clone
// template, inject data at placeholders).
func rewriteOOXMLZip(ctx context.Context, srcPath, outputPath string, transform ooxmlPartTransform) error {
	zr, err := zip.OpenReader(srcPath)
	if err != nil {
		return fmt.Errorf("not a valid template (corrupt or not a zip archive): %w", err)
	}
	defer zr.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := copyZipEntry(zw, f, transform); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output file: %w", err)
	}
	return nil
}

func copyZipEntry(zw *zip.Writer, f *zip.File, transform ooxmlPartTransform) error {
	if f.FileInfo().IsDir() {
		_, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name})
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open template entry %s: %w", f.Name, err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("read template entry %s: %w", f.Name, err)
	}

	out, ok, err := transform(f.Name, data)
	if err != nil {
		return fmt.Errorf("transform template entry %s: %w", f.Name, err)
	}
	if !ok {
		out = data
	}

	w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("zip entry %s: %w", f.Name, err)
	}
	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("zip entry %s: %w", f.Name, err)
	}
	return nil
}

// xmlEscapeText escapes text for inclusion in XML element content.
func xmlEscapeText(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(s)
}

// xmlEntityRe matches the XML named and numeric entities produced by Word /
// PowerPoint when placeholder text is serialized.
var xmlEntityRe = regexp.MustCompile(`&(#[0-9]+;|#[xX][0-9A-Fa-f]+;|amp;|lt;|gt;|quot;|apos;)`)

// xmlUnescape reverses the entities xmlEscapeText handles plus numeric
// character references, in a single pass (no double-decoding).
func xmlUnescape(s string) string {
	return xmlEntityRe.ReplaceAllStringFunc(s, func(m string) string {
		body := m[1 : len(m)-1] // strip & and ;
		switch body {
		case "amp":
			return "&"
		case "lt":
			return "<"
		case "gt":
			return ">"
		case "quot":
			return `"`
		case "apos":
			return "'"
		}
		if strings.HasPrefix(body, "#") {
			digits := body[1:]
			base := 10
			if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
				base = 16
				digits = digits[1:]
			}
			if n, err := strconv.ParseInt(digits, base, 32); err == nil {
				return string(rune(n))
			}
		}
		return m
	})
}

// ooxmlRunTags describes one markup vocabulary for placeholder filling:
// WordprocessingML (w:) and DrawingML (a:).
type ooxmlRunTags struct {
	para          string // paragraph element, e.g. "w:p"
	pPr           string // paragraph properties, e.g. "w:pPr"
	run           string // run element, e.g. "w:r"
	rPr           string // run properties, e.g. "w:rPr"
	text          string // text element, e.g. "w:t"
	spacePreserve bool   // emit xml:space="preserve" on text elements (Word needs it)
}

var (
	wordTags  = ooxmlRunTags{para: "w:p", pPr: "w:pPr", run: "w:r", rPr: "w:rPr", text: "w:t", spacePreserve: true}
	slideTags = ooxmlRunTags{para: "a:p", pPr: "a:pPr", run: "a:r", rPr: "a:rPr", text: "a:t"}
)

// fillPlaceholdersInPart merges text runs within each paragraph of one OOXML
// part and replaces {{placeholder}} occurrences with values (design.md D3:
// Word splits {{na|me}} across w:r runs — a known problem with a known fix).
// Runs are merged per paragraph: every paragraph whose concatenated text
// contains a placeholder is rebuilt as pPr + one run per text segment, reusing
// the paragraph's pPr and the first body run's rPr for consistent styling.
// Placeholders with no entry in values are left as literal text so the model
// can see what did not fill.
func fillPlaceholdersInPart(partXML string, values map[string]string, tags ooxmlRunTags) string {
	paraRe := regexp.MustCompile(`(?s)<` + tags.para + `(?: [^>]*)?>.*?</` + tags.para + `>`)
	textRe := regexp.MustCompile(`(?s)<` + tags.text + `(?: [^>]*)?>(.*?)</` + tags.text + `>`)
	pPrRe := regexp.MustCompile(`(?s)<` + tags.pPr + `(?: [^>]*)?>.*?</` + tags.pPr + `>`)
	rPrRe := regexp.MustCompile(`(?s)<` + tags.rPr + `(?: [^>]*)?>.*?</` + tags.rPr + `>`)
	placeholderRe := regexp.MustCompile(`\{\{[^{}]*\}\}`)

	return paraRe.ReplaceAllStringFunc(partXML, func(para string) string {
		matches := textRe.FindAllStringSubmatch(para, -1)
		if len(matches) == 0 {
			return para
		}
		var full strings.Builder
		for _, m := range matches {
			full.WriteString(xmlUnescape(m[1]))
		}
		text := full.String()
		if !strings.Contains(text, "{{") {
			return para
		}

		// Preserve paragraph properties and the first run's run properties.
		pPr := ""
		searchable := para
		if m := pPrRe.FindStringIndex(para); m != nil {
			pPr = para[m[0]:m[1]]
			searchable = para[:m[0]] + para[m[1]:]
		}
		rPr := ""
		if m := rPrRe.FindStringIndex(searchable); m != nil {
			rPr = searchable[m[0]:m[1]]
		}

		var runs strings.Builder
		pos := 0
		for _, loc := range placeholderRe.FindAllStringIndex(text, -1) {
			if literal := text[pos:loc[0]]; literal != "" {
				runs.WriteString(ooxmlRun(tags, rPr, literal))
			}
			name := text[loc[0]+2 : loc[1]-2]
			if value, ok := values[name]; ok {
				if value != "" {
					runs.WriteString(ooxmlRun(tags, rPr, value))
				}
			} else {
				runs.WriteString(ooxmlRun(tags, rPr, text[loc[0]:loc[1]]))
			}
			pos = loc[1]
		}
		if literal := text[pos:]; literal != "" {
			runs.WriteString(ooxmlRun(tags, rPr, literal))
		}

		return "<" + tags.para + ">" + pPr + runs.String() + "</" + tags.para + ">"
	})
}

// ooxmlRun renders one run carrying a single text segment.
func ooxmlRun(tags ooxmlRunTags, rPr, text string) string {
	textOpen := "<" + tags.text + ">"
	if tags.spacePreserve {
		textOpen = `<` + tags.text + ` xml:space="preserve">`
	}
	return "<" + tags.run + ">" + rPr + textOpen + xmlEscapeText(text) + "</" + tags.text + "></" + tags.run + ">"
}

// templateStringValues normalizes template-fill data (map[string]any) into
// placeholder string values: strings as-is, numbers without float noise,
// booleans as true/false, null as empty.
func templateStringValues(data []byte) (map[string]string, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("template data is required: a JSON object of placeholder values")
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse template data: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("template data must be a non-empty JSON object of placeholder values")
	}
	values := make(map[string]string, len(raw))
	for k, v := range raw {
		switch tv := v.(type) {
		case string:
			values[k] = tv
		case float64:
			values[k] = strconv.FormatFloat(tv, 'f', -1, 64)
		case bool:
			values[k] = strconv.FormatBool(tv)
		case nil:
			values[k] = ""
		default:
			values[k] = fmt.Sprint(v)
		}
	}
	return values, nil
}
