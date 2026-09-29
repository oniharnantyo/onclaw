// Package attachments classifies uploaded chat attachments into the
// format matrix of add-chat-attachments / add-document-read-tool design:
// inline-image, inline-text, drop, and reject. Classification is
// server-side — the sniffed magic bytes and the filename extension table —
// and rejection happens at upload time, never at turn time.
package attachments

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Per-lane byte caps.
const (
	MaxInlineImageBytes = 5 << 20  // 5 MB
	MaxInlinePDFBytes   = 20 << 20 // 20 MB
	MaxInlineTextBytes  = 200 << 10
	MaxDropBytes        = 50 << 20 // 50 MB

	// MaxPDFPages is the approximate page cap for PDFs; the count is
	// best-effort, derived from the readable file head.
	MaxPDFPages = 100
)

// ErrTooLarge is the typed oversize rejection: the handler maps it to 413
// with a message naming the cap (workspace-attachments spec: "413 with a
// message naming the 5 MB image cap").
type ErrTooLarge struct {
	Type string // human-readable type name ("image", "PDF", "file")
	Cap  int64  // byte cap that was exceeded
}

func (e *ErrTooLarge) Error() string {
	return fmt.Sprintf("%s exceeds the %d byte upload cap", e.Type, e.Cap)
}

// Unwrap anchors the error to the payload-too-large sentinel so the standard
// ErrorToStatus mapping serves 413 without an attachments-specific branch.
func (e *ErrTooLarge) Unwrap() error { return domain.ErrPayloadTooLarge }

// Classify determines the server-side lane from the sniffed content head and
// the original filename (attachments design D1). head is the first ≥512 bytes
// of the file (fewer for smaller files) — larger heads improve the PDF page
// count's reach but the count stays best-effort regardless. Returns the
// sniffed mime, the lane (a domain.AttachmentLane* value), and a rejection
// error for the reject lane: zero-byte/empty-name/disallowed types wrap
// domain.ErrInvalid (400) and oversize files are *ErrTooLarge (413).
func Classify(name string, size int64, head []byte) (string, string, error) {
	if name == "" {
		return "", "", fmt.Errorf("%w: attachment name is required", domain.ErrInvalid)
	}
	if size <= 0 {
		return "", "", fmt.Errorf("%w: attachment must not be empty", domain.ErrInvalid)
	}

	// Sniff the real type from magic bytes — the client-declared type is
	// never trusted (workspace-attachments spec, upload validation).
	sniffed := http.DetectContentType(head)

	if isSniffedImage(sniffed) {
		if size > MaxInlineImageBytes {
			return sniffed, "", &ErrTooLarge{Type: "image", Cap: MaxInlineImageBytes}
		}
		return sniffed, domain.AttachmentLaneInlineImage, nil
	}

	if sniffed == "application/pdf" {
		if size > MaxDropBytes {
			return sniffed, "", &ErrTooLarge{Type: "PDF", Cap: MaxDropBytes}
		}
		if pages := pdfPageCount(head); pages > MaxPDFPages {
			return sniffed, "", fmt.Errorf("%w: PDF exceeds the approximately %d page upload cap", domain.ErrPayloadTooLarge, MaxPDFPages)
		}
		return sniffed, domain.AttachmentLaneDrop, nil
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))

	if modernOfficeExts[ext] {
		if size > MaxDropBytes {
			return sniffed, "", &ErrTooLarge{Type: "file", Cap: MaxDropBytes}
		}
		return sniffed, domain.AttachmentLaneDrop, nil
	}

	if legacyOfficeExts[ext] {
		return sniffed, "", fmt.Errorf("%w: legacy binary office formats (.doc, .xls, .ppt) are not supported — convert to modern formats (.docx, .xlsx, .pptx) or PDF", domain.ErrInvalid)
	}

	if archiveExts[ext] || isSniffedArchive(sniffed) {
		return sniffed, "", fmt.Errorf("%w: archives are not a supported attachment type", domain.ErrInvalid)
	}
	if executableExts[ext] || hasExecutableMagic(head) {
		return sniffed, "", fmt.Errorf("%w: executables are not a supported attachment type", domain.ErrInvalid)
	}

	// The text family: extension drives inline vs drop; the head's null-byte
	// sniff confirms the bytes are actually text-ish.
	textFamily := textExts[ext] || isSniffedTextish(sniffed)
	if textFamily && hasNullByte(head) {
		return sniffed, "", fmt.Errorf("%w: file content is binary, not the text its name suggests", domain.ErrInvalid)
	}
	if textFamily {
		switch {
		case size <= MaxInlineTextBytes && textExts[ext]:
			return sniffed, domain.AttachmentLaneInlineText, nil
		case size <= MaxDropBytes:
			return sniffed, domain.AttachmentLaneDrop, nil
		default:
			return sniffed, "", &ErrTooLarge{Type: "file", Cap: MaxDropBytes}
		}
	}

	return sniffed, "", fmt.Errorf("%w: unsupported attachment type", domain.ErrInvalid)
}

// modernOfficeExts are the modern OOXML document formats routed to the drop lane (D1).
// Since OOXML files are zip files at the byte level, they are checked before the archive check.
var modernOfficeExts = set("docx", "xlsx", "pptx")

// legacyOfficeExts are the legacy binary office formats rejected with conversion guidance.
var legacyOfficeExts = set("doc", "xls", "ppt")

var archiveExts = set("zip", "gz", "tgz", "tar", "7z", "rar", "bz2", "xz", "zst")

var executableExts = set("exe", "dll", "msi", "dmg", "pkg", "app", "so", "dylib", "bin", "com", "scr")

// textExts are the known text extensions that qualify for the inline-text
// lane at or under 200 KB (design D4: "txt md csv json + code exts"). Larger
// members of the same family fall to the drop lane automatically.
var textExts = set(
	"txt", "md", "markdown", "csv", "tsv", "json", "jsonl", "ndjson", "yaml", "yml", "toml", "ini", "cfg", "conf", "env",
	"xml", "html", "htm", "css", "scss",
	"py", "js", "mjs", "cjs", "ts", "tsx", "jsx", "go", "rb", "sh", "bash", "zsh", "fish", "sql",
	"java", "kt", "kts", "scala", "c", "h", "cpp", "cc", "hpp", "cs", "m", "mm", "swift", "rs", "php", "pl", "lua", "r", "jl",
	"dart", "vue", "svelte", "proto", "graphql", "tf", "hcl", "dockerfile", "makefile", "cmake", "gradle",
	"log", "diff", "patch", "gitignore", "editorconfig", "properties",
)

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func isSniffedImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

func isSniffedArchive(mime string) bool {
	switch mime {
	case "application/zip", "application/x-gzip", "application/x-rar-compressed", "application/x-bzip", "application/x-bzip2", "application/x-7z-compressed", "application/x-tar":
		return true
	}
	return false
}

// isSniffedTextish reports whether the sniffed type reads as text-like
// content for a file outside the known text extensions: such files take the
// drop lane (text-like everything else, design D4) instead of the unknown
// reject bucket.
func isSniffedTextish(mime string) bool {
	return strings.HasPrefix(mime, "text/") || mime == "application/json"
}

func hasNullByte(head []byte) bool {
	return bytes.IndexByte(head, 0) >= 0
}

// hasExecutableMagic detects the native executable headers Go's sniffer does
// not model: PE (MZ), ELF, and Mach-O (32/64-bit, both endiannesses).
func hasExecutableMagic(head []byte) bool {
	magics := [][]byte{
		[]byte("MZ"),               // PE/COFF (Windows)
		[]byte("\x7FELF"),          // ELF (Linux)
		[]byte("\xFE\xED\xFA\xCE"), // Mach-O 32-bit big-endian
		[]byte("\xFE\xED\xFA\xCF"), // Mach-O 64-bit big-endian
		[]byte("\xCE\xFA\xED\xFE"), // Mach-O 32-bit little-endian
		[]byte("\xCF\xFA\xED\xFE"), // Mach-O 64-bit little-endian
		[]byte("\xCA\xFE\xBA\xBE"), // Mach-O fat/universal binary
	}
	for _, magic := range magics {
		if bytes.HasPrefix(head, magic) {
			return true
		}
	}
	return false
}

// PDFPageCount counts "/Type /Page" object markers in the given bytes,
// best-effort: page objects beyond the scanned window are invisible here and
// the byte cap remains the hard limit. Exported for the references upload
// pipeline (add-reference-documents tasks 3.3), which records the count on
// the document row from the full in-memory blob.
func PDFPageCount(head []byte) int {
	return pdfPageCount(head)
}

// pdfPageCount counts "/Type /Page" object markers in the readable head,
// best-effort (design.md): page objects beyond the head window are invisible
// here and the byte cap remains the hard limit. "/Type /Pages" (the page
// tree node) is excluded by requiring the marker not to continue with 's'.
func pdfPageCount(head []byte) int {
	count := 0
	for i := 0; i <= len(head); {
		j := bytes.Index(head[i:], []byte("/Type"))
		if j < 0 {
			break
		}
		i += j + len("/Type")
		k := i
		for k < len(head) && (head[k] == ' ' || head[k] == '\t' || head[k] == '\r' || head[k] == '\n') {
			k++
		}
		if k+5 <= len(head) && bytes.Equal(head[k:k+5], []byte("/Page")) && (k+5 >= len(head) || head[k+5] != 's') {
			count++
			i = k + 5
		}
	}
	return count
}
