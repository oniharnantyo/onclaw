package attachments

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

var pngHead = append([]byte("\x89PNG\x0D\x0A\x1A\x0A"), bytes.Repeat([]byte{0x00, 0x01, 0x02}, 200)...)

func TestClassify_Images(t *testing.T) {
	for name, head := range map[string][]byte{
		"png":  []byte("\x89PNG\x0D\x0A\x1A\x0A" + strings.Repeat("x", 600)),
		"gif":  []byte("GIF89a" + strings.Repeat("x", 600)),
		"jpeg": []byte("\xFF\xD8\xFF\xE0" + strings.Repeat("x", 600)),
	} {
		mime, lane, err := Classify("shot."+name, 1<<20, head)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if lane != domain.AttachmentLaneInlineImage {
			t.Errorf("%s: lane = %q, want inline-image", name, lane)
		}
		if mime != "image/"+map[string]string{"png": "png", "gif": "gif", "jpeg": "jpeg"}[name] {
			t.Errorf("%s: mime = %q", name, mime)
		}
	}
}

func TestClassify_OversizeImageNamesCap(t *testing.T) {
	_, _, err := Classify("shot.png", 8<<20, pngHead)
	var tooLarge *ErrTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
	if tooLarge.Cap != MaxInlineImageBytes {
		t.Errorf("Cap = %d, want %d", tooLarge.Cap, MaxInlineImageBytes)
	}
	if !strings.Contains(tooLarge.Error(), "5242880") {
		t.Errorf("message must name the cap: %q", tooLarge.Error())
	}
	if !errors.Is(err, domain.ErrPayloadTooLarge) {
		t.Error("ErrTooLarge must unwrap to domain.ErrPayloadTooLarge (413 mapping)")
	}
}

func TestClassify_PDF(t *testing.T) {
	head := []byte("%PDF-1.7\n" + strings.Repeat("/Type /Page\n", 10))
	mime, lane, err := Classify("report.pdf", 1<<20, head)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "application/pdf" || lane != domain.AttachmentLaneDrop {
		t.Errorf("mime = %q lane = %q, want application/pdf drop", mime, lane)
	}

	// Page-tree nodes ("/Type /Pages") must not count as page objects.
	head = []byte("%PDF-1.7\n/Type /Pages /Count 3\n/Type /Page\n")
	if _, _, err := Classify("report.pdf", 1<<20, head); err != nil {
		t.Errorf("page-tree marker miscounted as a page object: %v", err)
	}

	// Over the approximate page cap: rejected with the cap stated.
	head = []byte("%PDF-1.7\n" + strings.Repeat("/Type /Page\n", MaxPDFPages+1))
	_, _, err = Classify("report.pdf", 1<<20, head)
	if !errors.Is(err, domain.ErrPayloadTooLarge) || !strings.Contains(err.Error(), "100 page") {
		t.Errorf("expected 413 page-cap rejection, got %v", err)
	}

	// Over the byte cap (MaxDropBytes = 50 MB): 413 naming the size.
	_, _, err = Classify("report.pdf", 60<<20, []byte("%PDF-1.7\n"+strings.Repeat("x", 600)))
	var tooLarge *ErrTooLarge
	if !errors.As(err, &tooLarge) || tooLarge.Cap != MaxDropBytes {
		t.Errorf("expected ErrTooLarge with the 50 MB drop cap, got %v", err)
	}
}

func TestClassify_ModernOfficeDocuments(t *testing.T) {
	zipHead := []byte("PK\x03\x04" + strings.Repeat("x", 600))
	for _, ext := range []string{"docx", "xlsx", "pptx"} {
		mime, lane, err := Classify("doc."+ext, 1<<20, zipHead)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", ext, err)
		}
		if lane != domain.AttachmentLaneDrop {
			t.Errorf("%s: lane = %q, want drop", ext, lane)
		}
		if mime == "" {
			t.Errorf("%s: mime is empty", ext)
		}

		// Oversize modern office doc
		_, _, err = Classify("huge."+ext, 60<<20, zipHead)
		var tooLarge *ErrTooLarge
		if !errors.As(err, &tooLarge) || tooLarge.Cap != MaxDropBytes {
			t.Errorf("%s: expected ErrTooLarge with 50MB drop cap, got %v", ext, err)
		}
	}
}

func TestClassify_TextFamily(t *testing.T) {
	text := []byte("key: value\n" + strings.Repeat("payload line\n", 60))

	mime, lane, err := Classify("config.yaml", 40<<10, text)
	if err != nil || lane != domain.AttachmentLaneInlineText {
		t.Errorf("40KB yaml: lane = %q err = %v, want inline-text", lane, err)
	}
	if !strings.HasPrefix(mime, "text/") {
		t.Errorf("mime = %q, want a text/* sniff", mime)
	}

	_, lane, err = Classify("dump.sql", 4<<20, text)
	if err != nil || lane != domain.AttachmentLaneDrop {
		t.Errorf("4MB sql: lane = %q err = %v, want drop", lane, err)
	}

	// Text extension over the drop cap: 413 naming the 50 MB cap.
	_, _, err = Classify("huge.log", 60<<20, text)
	var tooLarge *ErrTooLarge
	if !errors.As(err, &tooLarge) || tooLarge.Cap != MaxDropBytes {
		t.Errorf("expected ErrTooLarge with the 50 MB drop cap, got %v", err)
	}

	// Known text extension at exactly the inline boundary stays inline.
	_, lane, err = Classify("notes.md", MaxInlineTextBytes, text)
	if err != nil || lane != domain.AttachmentLaneInlineText {
		t.Errorf("boundary 200KB md: lane = %q err = %v, want inline-text", lane, err)
	}

	// A byte past the boundary with a text extension drops.
	_, lane, err = Classify("notes.md", MaxInlineTextBytes+1, text)
	if err != nil || lane != domain.AttachmentLaneDrop {
		t.Errorf("200KB+1 md: lane = %q err = %v, want drop", lane, err)
	}

	// Text-ish sniff without a known extension drops (text-like everything
	// else), it does not reject as unknown.
	_, lane, err = Classify("Makefile", 40<<10, text)
	if err != nil || lane != domain.AttachmentLaneDrop {
		t.Errorf("extension-less text: lane = %q err = %v, want drop", lane, err)
	}

	// Null bytes in a text-named file: the sniff confirms binary — reject.
	if _, _, err := Classify("config.yaml", 40<<10, []byte("key\x00value")); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("null-byte text: expected 400 reject, got %v", err)
	}
}

func TestClassify_RejectLane(t *testing.T) {
	zipHead := []byte("PK\x03\x04" + strings.Repeat("x", 600))

	tests := []struct {
		name     string
		filename string
		size     int64
		head     []byte
		wantMsg  string
	}{
		{"legacy doc", "report.doc", 1 << 20, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, "legacy binary office formats (.doc, .xls, .ppt) are not supported"},
		{"legacy xls", "sheet.xls", 1 << 20, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, "legacy binary office formats (.doc, .xls, .ppt) are not supported"},
		{"legacy ppt", "slides.ppt", 1 << 20, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, "legacy binary office formats (.doc, .xls, .ppt) are not supported"},
		{"archive by ext", "bundle.zip", 1 << 20, zipHead, "archive"},
		{"archive by sniff", "payload.dat", 1 << 20, []byte("\x1F\x8B\x08" + strings.Repeat("x", 600)), "archive"},
		{"executable by magic", "image.png", 1 << 20, append([]byte("MZ"), bytes.Repeat([]byte{0x90}, 600)...), "executable"},
		{"executable by ext", "tool.exe", 1 << 20, []byte(strings.Repeat("x", 600)), "executable"},
		{"unknown binary", "blob.dat", 1 << 20, []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03}, "unsupported"},
	}
	for _, tc := range tests {
		_, _, err := Classify(tc.filename, tc.size, tc.head)
		if err == nil {
			t.Errorf("%s: expected rejection, got none", tc.name)
			continue
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: expected 400-class reject (domain.ErrInvalid), got %v", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("%s: message %q must contain %q", tc.name, err.Error(), tc.wantMsg)
		}
	}
}

func TestClassify_ZeroByteAndEmptyName(t *testing.T) {
	if _, _, err := Classify("empty.png", 0, pngHead); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("zero-byte: expected 400 reject, got %v", err)
	}
	if _, _, err := Classify("", 1<<20, pngHead); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty name: expected 400 reject, got %v", err)
	}
}

func TestClassify_SniffOverridesExtension(t *testing.T) {
	// A real PNG wearing a text name is classified by its magic bytes.
	_, lane, err := Classify("notepad.txt", 1<<20, pngHead)
	if err != nil || lane != domain.AttachmentLaneInlineImage {
		t.Errorf("png as .txt: lane = %q err = %v, want inline-image (sniff wins)", lane, err)
	}
}
