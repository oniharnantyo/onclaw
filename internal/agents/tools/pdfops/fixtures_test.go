package pdfops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/stretchr/testify/require"
)

// makePDF builds an n-page PDF at dir/name whose page i displays
// "marker page i", so tests can verify page order by text.
func makePDF(t *testing.T, dir, name, marker string, pages int) string {
	t.Helper()
	var sb bytes.Buffer
	sb.WriteString(`{"pages":{`)
	for i := 1; i <= pages; i++ {
		if i > 1 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"%d":{"content":{"text":[{"value":%q,"font":{"name":"Courier","size":14},"pos":[50,750]}]}}`,
			i, fmt.Sprintf("%s page %d", marker, i))
	}
	sb.WriteString(`}}`)
	return createFromJSON(t, dir, name, sb.String())
}

// makeFormPDF builds a one-page PDF with two text fields (full_name, city)
// and one checkbox (agree).
func makeFormPDF(t *testing.T, dir, name string) string {
	t.Helper()
	script := `{"fonts":{"input":{"name":"Courier","size":12}},"pages":{"1":{"content":{` +
		`"textfield":[{"id":"full_name","value":"","pos":[50,600],"width":200,"height":24},` +
		`{"id":"city","value":"","pos":[50,540],"width":200,"height":24}],` +
		`"checkbox":[{"id":"agree","pos":[50,480],"width":16,"height":16}]}}}}`
	return createFromJSON(t, dir, name, script)
}

// createFromJSON renders a pdfcpu create-script JSON into dir/name.
func createFromJSON(t *testing.T, dir, name, script string) string {
	t.Helper()
	scriptPath := filepath.Join(dir, name+".create.json")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o644))
	out := filepath.Join(dir, name)
	require.NoError(t, api.CreateFile(context.Background(), "", scriptPath, out, nil))
	return out
}

// pageCountOf returns the page count of a PDF file.
func pageCountOf(t *testing.T, path string) int {
	t.Helper()
	n, err := api.PageCountFile(context.Background(), path)
	require.NoError(t, err)
	return n
}

// pageText extracts one page's text content.
func pageText(t *testing.T, path string, pageNr int) string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer closeInput(f)

	var text bytes.Buffer
	digest := func(r io.Reader, _ int) error {
		_, err := io.Copy(&text, r)
		return err
	}
	require.NoError(t, api.ExtractContent(context.Background(), f,
		[]string{strconv.Itoa(pageNr)}, digest, nil))
	return text.String()
}

// pageDim returns the width and height of one page; page rotation swaps
// them, which is how rotation is verified.
func pageDim(t *testing.T, path string, pageNr int) (width, height float64) {
	t.Helper()
	dims, err := api.PageDimsFile(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, dims, pageCountOf(t, path))
	return dims[pageNr-1].Width, dims[pageNr-1].Height
}

// stamped reports whether a PDF carries watermarks.
func stamped(t *testing.T, path string) bool {
	t.Helper()
	ok, err := api.HasWatermarksFile(context.Background(), path, nil)
	require.NoError(t, err)
	return ok
}

// formValues exports a PDF's form fields as name → value.
func formValues(t *testing.T, path string) map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer closeInput(f)

	var buf bytes.Buffer
	require.NoError(t, api.ExportFormJSON(context.Background(), f, &buf, path, nil))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	values := map[string]any{}
	for _, form := range formArray(doc) {
		for _, kind := range formFieldKinds {
			for _, field := range fieldArray(form, kind) {
				name, _ := field["name"].(string)
				if name != "" {
					values[name] = field["value"]
				}
			}
		}
	}
	return values
}

// fileDigest returns the SHA-256 of a file, for byte-for-byte unchanged
// assertions on inputs.
func fileDigest(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(b)
	return sum[:]
}

// applyRejected runs op and asserts a readable {ok:false} result whose
// message contains every given substring.
func applyRejected(t *testing.T, op Op, contains ...string) Result {
	t.Helper()
	res, err := NewEngine().Apply(context.Background(), op)
	require.NoError(t, err, "rejected operations must not surface as Go errors")
	require.False(t, res.OK, "expected a rejected result, got: %s", res.Message)
	for _, s := range contains {
		require.Contains(t, res.Message, s)
	}
	return res
}
