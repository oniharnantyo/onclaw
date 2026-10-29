package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestPDF builds a deterministic minimal one-page PDF whose page draws
// contentStream (raw PDF operators, stored uncompressed). Xref offsets are
// computed, so pdfcpu reads it like any real document.
func buildTestPDF(t *testing.T, contentStream string) []byte {
	t.Helper()

	var b bytes.Buffer
	offsets := make(map[int]int, 5)
	writeObj := func(nr int, body string) {
		offsets[nr] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", nr, body)
	}

	b.WriteString("%PDF-1.4\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>")
	writeObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contentStream), contentStream))
	writeObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefStart := b.Len()
	b.WriteString("xref\n0 6\n")
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	b.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", xrefStart)
	return b.Bytes()
}

func TestPDFShownText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "Tj draws its string",
			content: "BT /F1 12 Tf 72 720 Td (Hello buddies) Tj ET",
			want:    "Hello buddies\n",
		},
		{
			name:    "TJ concatenates its fragments",
			content: "BT [(Hel) -100 (lo bud) 3 (dies)] TJ ET",
			want:    "Hello buddies\n",
		},
		{
			name:    "TJ numbers do not glue fragments across operators",
			content: "BT (a) Tj 0 -12 Td (b) Tj ET",
			want:    "a\nb\n",
		},
		{
			name:    "apostrophe moves to the next line before showing",
			content: "BT (first) Tj (second) ' ET",
			want:    "first\nsecond\n",
		},
		{
			name:    "double-quote takes numbers then shows on a new line",
			content: "BT (first) Tj 1 0 (second) \" ET",
			want:    "first\nsecond\n",
		},
		{
			name:    "escapes decode inside literal strings",
			content: `BT (line\nbreak \(paren\) \\ octal\101end) Tj ET`,
			want:    "line\nbreak (paren) \\ octalAend\n",
		},
		{
			name:    "hex strings decode",
			content: "BT <48656C6C6F> Tj ET",
			want:    "Hello\n",
		},
		{
			name:    "UTF-16BE hex string decodes",
			content: "BT <FEFF00480069> Tj ET",
			want:    "Hi\n",
		},
		{
			name:    "comments are skipped",
			content: "% a comment (not text) Tj\nBT (real) Tj ET",
			want:    "real\n",
		},
		{
			name:    "strings under non-show operators are machinery",
			content: "/OC /L1 BDC (hidden) /MCID 0 DP (shown) Tj ET",
			want:    "shown\n",
		},
		{
			name:    "inline image data is skipped",
			content: "BI /W 1 /H 1 (bin\x28ary Tj data) ID \x00\x01\x02 EI (after) Tj",
			want:    "after\n",
		},
		{
			name:    "names and dicts never show",
			content: "/F1 12 Tf << /X (nobody sees this) >> BDC (shown) Tj",
			want:    "shown\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pdfShownText([]byte(tt.content)))
		})
	}
}

func TestExtractPDFText(t *testing.T) {
	t.Run("text layer extracts", func(t *testing.T) {
		text, err := extractPDFText(context.Background(), buildTestPDF(t, "BT /F1 12 Tf 72 720 Td (Hello buddies) Tj 0 -24 Td (second line) Tj ET"))
		require.NoError(t, err)
		assert.Contains(t, text, "Hello buddies")
		assert.Contains(t, text, "second line")
	})

	t.Run("scanned document yields no extractable text", func(t *testing.T) {
		// A drawn rectangle only: the analog of an image-only page — the
		// content stream carries no text-showing operator.
		_, err := extractPDFText(context.Background(), buildTestPDF(t, "0 0 1 rg 72 720 200 100 re f"))
		require.Error(t, err)
		assert.ErrorIs(t, err, errNoExtractableText)
		assert.Contains(t, err.Error(), "no extractable text")
	})

	t.Run("empty page yields no extractable text", func(t *testing.T) {
		_, err := extractPDFText(context.Background(), buildTestPDF(t, ""))
		require.Error(t, err)
		assert.ErrorIs(t, err, errNoExtractableText)
	})

	t.Run("non-PDF bytes error readably", func(t *testing.T) {
		_, err := extractPDFText(context.Background(), []byte("this is not a pdf"))
		require.Error(t, err)
		assert.NotErrorIs(t, err, errNoExtractableText)
		assert.Contains(t, err.Error(), "extract PDF text")
	})
}

// seedPDFJail writes the given files into a fresh jail root and builds the
// jail over it with the turn's tool-result PDF carriage flag.
func seedPDFJail(t *testing.T, toolResultPDFParts bool, files map[string][]byte) einofs.MultiModalReader {
	t.Helper()
	root := t.TempDir()
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), data, 0o644))
	}
	jail, err := NewFilesystemJailedWithRootsForTurn(root, toolResultPDFParts)
	require.NoError(t, err)
	mmr, ok := jail.(einofs.MultiModalReader)
	require.True(t, ok, "the jailed backend must support multimodal reads")
	return mmr
}

func TestMultiModalRead_PDFCarriage(t *testing.T) {
	ctx := context.Background()
	pdfBytes := buildTestPDF(t, "BT /F1 12 Tf 72 720 Td (Hello buddies) Tj ET")
	scannedBytes := buildTestPDF(t, "0 0 1 rg 72 720 200 100 re f")
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n', 0, 0, 0, 13}

	t.Run("connector without carriage degrades to extracted text", func(t *testing.T) {
		jail := seedPDFJail(t, false, map[string][]byte{"doc.pdf": pdfBytes})

		res, err := jail.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
			ReadRequest: einofs.ReadRequest{FilePath: DefaultMountPoint + "/doc.pdf"},
		})
		require.NoError(t, err)

		require.NotNil(t, res.FileContent, "degraded read must be a text result, not parts")
		assert.Empty(t, res.Parts, "no inline PDF part may ride a connector without carriage")
		assert.Contains(t, res.FileContent.Content, pdfBytesWithheldNote)
		assert.Contains(t, res.FileContent.Content, "Hello buddies")
	})

	t.Run("connector without carriage errors readably on a scanned PDF", func(t *testing.T) {
		jail := seedPDFJail(t, false, map[string][]byte{"scan.pdf": scannedBytes})

		_, err := jail.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
			ReadRequest: einofs.ReadRequest{FilePath: DefaultMountPoint + "/scan.pdf"},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, errNoExtractableText)
		assert.Contains(t, err.Error(), "no extractable text")
	})

	t.Run("connector with carriage serves the inline PDF part", func(t *testing.T) {
		jail := seedPDFJail(t, true, map[string][]byte{"doc.pdf": pdfBytes})

		res, err := jail.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
			ReadRequest: einofs.ReadRequest{FilePath: DefaultMountPoint + "/doc.pdf"},
		})
		require.NoError(t, err)

		require.Nil(t, res.FileContent)
		require.Len(t, res.Parts, 1)
		part := res.Parts[0]
		assert.Equal(t, einofs.FileContentPartTypePDF, part.Type)
		assert.Equal(t, "application/pdf", part.MIMEType)
		assert.Equal(t, pdfBytes, part.Data)
		assert.NotContains(t, part.Data, pdfBytesWithheldNote)
	})

	t.Run("images stay inline whatever the flag", func(t *testing.T) {
		for _, flag := range []bool{false, true} {
			jail := seedPDFJail(t, flag, map[string][]byte{"pic.png": pngBytes})

			res, err := jail.MultiModalRead(ctx, &einofs.MultiModalReadRequest{
				ReadRequest: einofs.ReadRequest{FilePath: DefaultMountPoint + "/pic.png"},
			})
			require.NoError(t, err)

			require.Len(t, res.Parts, 1, "flag %v", flag)
			assert.Equal(t, einofs.FileContentPartTypeImage, res.Parts[0].Type)
			assert.Equal(t, pngBytes, res.Parts[0].Data)
		}
	})
}

func TestJailConstructors_ToolResultPDFParts(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name  string
		build func(t *testing.T) einofs.Backend
		want  bool
	}{
		{"NewFilesystemJail defaults to inline", func(t *testing.T) einofs.Backend {
			jail, err := NewFilesystemJail(root)
			require.NoError(t, err)
			return jail
		}, true},
		{"NewFilesystemJailedWithRoots defaults to inline", func(t *testing.T) einofs.Backend {
			jail, err := NewFilesystemJailedWithRoots(root)
			require.NoError(t, err)
			return jail
		}, true},
		{"NewFilesystemJailedWithKnowledge defaults to inline", func(t *testing.T) einofs.Backend {
			jail, err := NewFilesystemJailedWithKnowledge(root, KnowledgeRoots{})
			require.NoError(t, err)
			return jail
		}, true},
		{"ForTurn stores false", func(t *testing.T) einofs.Backend {
			jail, err := NewFilesystemJailedWithRootsForTurn(root, false)
			require.NoError(t, err)
			return jail
		}, false},
		{"ForTurn stores true", func(t *testing.T) einofs.Backend {
			jail, err := NewFilesystemJailedWithRootsForTurn(root, true)
			require.NoError(t, err)
			return jail
		}, true},
		{"ForTurn with knowledge stores true", func(t *testing.T) einofs.Backend {
			jail, err := NewFilesystemJailedWithKnowledgeForTurn(root, KnowledgeRoots{}, true)
			require.NoError(t, err)
			return jail
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jail := tt.build(t)
			jb, ok := jail.(*fsJailedBackend)
			require.True(t, ok, "constructor must return the jailed backend")
			assert.Equal(t, tt.want, jb.toolResultPDFParts)
		})
	}

	t.Run("missing primary root still fails", func(t *testing.T) {
		_, err := NewFilesystemJailedWithRootsForTurn("/nonexistent/primary/root", true)
		require.Error(t, err)
	})
}
