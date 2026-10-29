package compose

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildCarriageFixturePDF builds a deterministic minimal one-page PDF whose
// page draws the text "composed turn" (uncompressed operators, computed xref
// offsets), so the jailed backend's PDF carriage seam has a real document to
// serve.
func buildCarriageFixturePDF(t *testing.T) []byte {
	t.Helper()

	const contentStream = "BT /F1 12 Tf 72 720 Td (composed turn) Tj ET"
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
	b.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	b.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", xrefStart)
	return b.Bytes()
}

// TestFilesystemJail_ToolResultPDFPartsSeam asserts the jail construction the
// filesystem middleware wires (BuildHandlers section 4) honors the turn's
// tool-result PDF carriage flag end to end: the same constructors compose.go
// calls, exercised through MultiModalRead — a carried flag serves the inline
// PDF part, a degraded flag serves the extracted text behind the
// withheld-bytes note.
func TestFilesystemJail_ToolResultPDFPartsSeam(t *testing.T) {
	pdfBytes := buildCarriageFixturePDF(t)

	tests := []struct {
		name       string
		flag       bool
		wantInline bool
	}{
		{"connector without carriage degrades to text", false, false},
		{"connector with carriage serves the part", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "doc.pdf"), pdfBytes, 0o644))

			jail, err := backend.NewFilesystemJailedWithRootsForTurn(dir, tt.flag)
			require.NoError(t, err)
			mmr, ok := jail.(einofs.MultiModalReader)
			require.True(t, ok, "the jailed backend must support multimodal reads")

			res, err := mmr.MultiModalRead(context.Background(), &einofs.MultiModalReadRequest{
				ReadRequest: einofs.ReadRequest{FilePath: backend.DefaultMountPoint + "/doc.pdf"},
			})
			require.NoError(t, err)

			if tt.wantInline {
				require.Len(t, res.Parts, 1)
				assert.Equal(t, einofs.FileContentPartTypePDF, res.Parts[0].Type)
				assert.Equal(t, pdfBytes, res.Parts[0].Data)
				return
			}
			require.NotNil(t, res.FileContent)
			assert.Empty(t, res.Parts)
			assert.Contains(t, res.FileContent.Content, "[PDF bytes not attached:")
			assert.Contains(t, res.FileContent.Content, "composed turn")
		})
	}
}
