package references

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Per-type upload caps, mirroring the attachment contract (spec: "per-type
// size caps mirroring the attachment contract — PDF 20 MB; office documents
// and text-like files per drop-lane caps").
const (
	// MaxReferencePDFBytes is the PDF upload cap (the attachment PDF cap).
	MaxReferencePDFBytes = 20 << 20 // 20 MB
	// MaxReferenceFileBytes is the cap for every other allowed type (the
	// drop-lane cap).
	MaxReferenceFileBytes = 50 << 20 // 50 MB
)

// sniffHeadBytes is the classification window: the first 512 bytes decide the
// real type — the client-declared content type is never trusted (spec:
// magic-byte detection overrides the claimed type).
const sniffHeadBytes = 512

// ErrUnsupportedType is the wrong/unknown/legacy format rejection. The
// wrapped message carries the guidance: legacy doc/ppt/xls names the modern
// conversion target, everything else lists the allowed types (spec: "rejected
// at upload with guidance").
var ErrUnsupportedType = errors.New("unsupported reference document type")

// ErrTooLarge is the typed oversize rejection: the handler maps it to 413
// with a message naming the cap. Unwrap anchors it to
// domain.ErrPayloadTooLarge so the generic server error mapping serves 413
// without a references-specific branch (the attachments.ErrTooLarge posture).
var ErrTooLarge error = errTooLarge{}

type errTooLarge struct{}

func (errTooLarge) Error() string {
	return "reference document exceeds the upload size cap"
}

func (errTooLarge) Unwrap() error { return domain.ErrPayloadTooLarge }

// oleCFBMagic is the OLE Compound File Binary signature — the container of
// the legacy .doc/.ppt/.xls formats. A file carrying it is rejected even when
// renamed to a modern extension (spec: sniffed type beats the claimed name).
var oleCFBMagic = []byte{0xD0, 0xCF, 0x11, 0xE0}

// referenceDocOfficeExts are the OOXML extensions the upload accepts. OOXML
// files are zip archives at the byte level, so they are resolved from the
// extension when the sniff reports a zip container (the attachments
// Classify precedent — checked before the archive rejection).
var referenceDocOfficeExts = map[string]bool{
	"docx": true,
	"pptx": true,
	"xlsx": true,
}

// referenceDocLegacyOfficeExts are the legacy binary office formats rejected
// with conversion guidance.
var referenceDocLegacyOfficeExts = map[string]bool{
	"doc": true,
	"ppt": true,
	"xls": true,
}

// referenceDocTextExts are the text-family extensions the upload accepts.
// Extension drives which text type the file is (md vs txt vs html vs csv);
// the sniff's null-byte / UTF-8 check confirms the bytes are decodable text.
var referenceDocTextExts = map[string]bool{
	"md":       true,
	"markdown": true,
	"txt":      true,
	"html":     true,
	"htm":      true,
	"csv":      true,
}

// allowedReferenceDocTypes is the rejection guidance's allowed-type list.
const allowedReferenceDocTypes = "pdf, docx, pptx, xlsx, md, txt, html, csv"

// classifyUpload sniffs the real type from the first 512 bytes — never the
// client-declared content type — and resolves it against the references
// allowlist (pdf, docx, pptx, xlsx, md, txt, html, csv). It returns the
// canonical document type (the converter/sectioner key and the capability
// mount's extension) and the mime recorded on the row. Rejections:
//
//   - legacy .doc/.ppt/.xls, by extension OR by OLE magic under any name,
//     wrap ErrUnsupportedType with conversion guidance;
//   - binary content that disagrees with the name (an executable renamed
//     .pdf, a zip renamed .md) wraps ErrUnsupportedType;
//   - text content that is not decodable UTF-8 wraps ErrUnsupportedType;
//   - oversize files wrap ErrTooLarge (PDF 20 MB, others 50 MB).
func classifyUpload(filename string, data []byte) (docType, mime string, err error) {
	if strings.TrimSpace(filename) == "" {
		return "", "", fmt.Errorf("%w: filename is required", domain.ErrInvalid)
	}
	if len(data) == 0 {
		return "", "", fmt.Errorf("%w: reference document must not be empty", domain.ErrInvalid)
	}

	head := data
	if len(head) > sniffHeadBytes {
		head = head[:sniffHeadBytes]
	}
	sniffed := http.DetectContentType(head)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))

	// Legacy binary office formats never enter the allowlist: rejected by
	// extension or by the OLE container magic under any name, with the
	// conversion guidance (spec: legacy formats rejected with guidance).
	if referenceDocLegacyOfficeExts[ext] || bytes.HasPrefix(head, oleCFBMagic) {
		return "", "", fmt.Errorf("%w: %s", ErrUnsupportedType, legacyOfficeGuidance(ext))
	}

	switch {
	case sniffed == "application/pdf":
		if ext != "pdf" {
			return "", "", fmt.Errorf("%w: file content is a PDF but the name does not end in .pdf — rename the file and re-upload", ErrUnsupportedType)
		}
		docType = "pdf"

	case sniffed == "application/zip":
		// OOXML containers resolve by extension (attachments precedent:
		// zip-level files are extension-routed before the archive check).
		if !referenceDocOfficeExts[ext] {
			return "", "", fmt.Errorf("%w: archives are not a supported reference document type — allowed: %s", ErrUnsupportedType, allowedReferenceDocTypes)
		}
		docType = ext

	case strings.HasPrefix(sniffed, "text/"):
		if !referenceDocTextExts[ext] {
			return "", "", fmt.Errorf("%w: sniffed %q — allowed: %s", ErrUnsupportedType, sniffed, allowedReferenceDocTypes)
		}
		if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(head) {
			return "", "", fmt.Errorf("%w: file content is binary, not the decodable text its name suggests", ErrUnsupportedType)
		}
		docType = textDocType(ext)

	default:
		return "", "", fmt.Errorf("%w: sniffed %q — allowed: %s", ErrUnsupportedType, sniffed, allowedReferenceDocTypes)
	}

	byteCap := int64(MaxReferenceFileBytes)
	if docType == "pdf" {
		byteCap = MaxReferencePDFBytes
	}
	if int64(len(data)) > byteCap {
		return "", "", fmt.Errorf("%w: %s document exceeds the %d byte upload cap", ErrTooLarge, docType, byteCap)
	}
	return docType, docTypeMime(docType), nil
}

// textDocType canonicalizes a text extension to the converter/sectioner key.
func textDocType(ext string) string {
	switch ext {
	case "markdown":
		return "md"
	case "htm":
		return "html"
	default:
		return ext
	}
}

// docTypeMime maps the canonical document type to the wire mime recorded on
// the row (the domain.ValidReferenceDocMime allowlist).
func docTypeMime(docType string) string {
	switch docType {
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "md":
		return "text/markdown"
	case "txt":
		return "text/plain"
	case "html":
		return "text/html"
	case "csv":
		return "text/csv"
	default:
		return ""
	}
}

// mimeDocType is docTypeMime's inverse: the document type a row's recorded
// mime resolves to. Empty for mimes outside the allowlist.
func mimeDocType(mime string) string {
	for _, docType := range []string{"pdf", "docx", "pptx", "xlsx", "md", "txt", "html", "csv"} {
		if docTypeMime(docType) == mime {
			return docType
		}
	}
	return ""
}

// legacyOfficeGuidance renders the per-format conversion guidance (spec: "the
// upload is rejected with a message suggesting conversion to a modern format
// or PDF").
func legacyOfficeGuidance(ext string) string {
	switch ext {
	case "doc":
		return "legacy .doc files are not supported — convert to .docx or PDF and re-upload"
	case "ppt":
		return "legacy .ppt files are not supported — convert to .pptx or PDF and re-upload"
	case "xls":
		return "legacy .xls files are not supported — convert to .xlsx and re-upload"
	default:
		return "legacy binary office formats (.doc, .ppt, .xls) are not supported — convert to .docx, .pptx, .xlsx or PDF and re-upload"
	}
}
