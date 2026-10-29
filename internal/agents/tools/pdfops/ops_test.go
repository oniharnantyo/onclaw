package pdfops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// pageContains asserts that one page's content stream carries the text.
func pageContains(t *testing.T, path string, pageNr int, want string) {
	t.Helper()
	require.Contains(t, pageText(t, path, pageNr), want, "page %d of %s", pageNr, path)
}

func TestMerge(t *testing.T) {
	ctx := context.Background()

	t.Run("concatenates inputs in order and leaves them unchanged", func(t *testing.T) {
		dir := t.TempDir()
		a := makePDF(t, dir, "a.pdf", "alpha", 2)
		b := makePDF(t, dir, "b.pdf", "beta", 2)
		aDigest, bDigest := fileDigest(t, a), fileDigest(t, b)

		res, err := NewEngine().Apply(ctx, Merge{Inputs: []string{a, b}, Output: filepath.Join(dir, "merged.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, OpMerge, res.Op)
		require.Equal(t, 4, res.PageCount)
		require.FileExists(t, res.OutputPath)

		pageContains(t, res.OutputPath, 1, "alpha page 1")
		pageContains(t, res.OutputPath, 2, "alpha page 2")
		pageContains(t, res.OutputPath, 3, "beta page 1")
		pageContains(t, res.OutputPath, 4, "beta page 2")

		require.Equal(t, aDigest, fileDigest(t, a), "input a was modified")
		require.Equal(t, bDigest, fileDigest(t, b), "input b was modified")
	})

	t.Run("overwrites an existing output", func(t *testing.T) {
		dir := t.TempDir()
		a := makePDF(t, dir, "a.pdf", "alpha", 1)
		b := makePDF(t, dir, "b.pdf", "beta", 1)
		out := filepath.Join(dir, "merged.pdf")
		require.NoError(t, os.WriteFile(out, []byte("stale"), 0o644))

		res, err := NewEngine().Apply(ctx, Merge{Inputs: []string{a, b}, Output: out})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, 2, res.PageCount)
	})

	t.Run("rejects fewer than two inputs", func(t *testing.T) {
		dir := t.TempDir()
		a := makePDF(t, dir, "a.pdf", "alpha", 1)
		applyRejected(t, Merge{Inputs: []string{a}, Output: filepath.Join(dir, "m.pdf")}, "at least two")
	})

	t.Run("rejects a missing input", func(t *testing.T) {
		dir := t.TempDir()
		a := makePDF(t, dir, "a.pdf", "alpha", 1)
		applyRejected(t, Merge{Inputs: []string{a, filepath.Join(dir, "missing.pdf")}, Output: filepath.Join(dir, "m.pdf")}, "not found", "stage the file")
	})

	t.Run("rejects an output that would overwrite an input", func(t *testing.T) {
		dir := t.TempDir()
		a := makePDF(t, dir, "a.pdf", "alpha", 1)
		b := makePDF(t, dir, "b.pdf", "beta", 1)
		applyRejected(t, Merge{Inputs: []string{a, b}, Output: a}, "overwrite")
	})
}

func TestExtract(t *testing.T) {
	ctx := context.Background()

	t.Run("extracts a page range", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 3)
		srcDigest := fileDigest(t, src)

		res, err := NewEngine().Apply(ctx, Extract{Input: src, Pages: "2", Output: filepath.Join(dir, "one.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, 1, res.PageCount)
		pageContains(t, res.OutputPath, 1, "doc page 2")
		require.Equal(t, srcDigest, fileDigest(t, src), "input was modified")
	})

	t.Run("extracts even pages", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 4)

		res, err := NewEngine().Apply(ctx, Extract{Input: src, Pages: "even", Output: filepath.Join(dir, "even.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, 2, res.PageCount)
		pageContains(t, res.OutputPath, 1, "doc page 2")
		pageContains(t, res.OutputPath, 2, "doc page 4")
	})

	t.Run("rejects an empty page selection", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 2)
		applyRejected(t, Extract{Input: src, Output: filepath.Join(dir, "x.pdf")}, "page selection")
	})

	t.Run("rejects a selection matching no pages", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 3)
		applyRejected(t, Extract{Input: src, Pages: "9", Output: filepath.Join(dir, "x.pdf")}, "matches no pages")
	})

	t.Run("rejects an invalid selection", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 3)
		applyRejected(t, Extract{Input: src, Pages: "abc", Output: filepath.Join(dir, "x.pdf")}, "invalid page selection")
	})
}

func TestRotate(t *testing.T) {
	ctx := context.Background()

	t.Run("rotates all pages and swaps their dimensions", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 1)
		w, h := pageDim(t, src, 1)

		res, err := NewEngine().Apply(ctx, Rotate{Input: src, Angle: 90, Output: filepath.Join(dir, "rot.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, 1, res.PageCount)

		rw, rh := pageDim(t, res.OutputPath, 1)
		require.Equal(t, h, rw, "width should become the original height")
		require.Equal(t, w, rh, "height should become the original width")
		pageContains(t, res.OutputPath, 1, "doc page 1")
	})

	t.Run("rotates only the selected pages", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 2)
		w, h := pageDim(t, src, 1)

		res, err := NewEngine().Apply(ctx, Rotate{Input: src, Pages: "1", Angle: 90, Output: filepath.Join(dir, "rot.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)

		rw, rh := pageDim(t, res.OutputPath, 1)
		require.Equal(t, h, rw)
		require.Equal(t, w, rh)
		rw2, rh2 := pageDim(t, res.OutputPath, 2)
		require.Equal(t, w, rw2, "unselected page must keep its dimensions")
		require.Equal(t, h, rh2)
	})

	t.Run("rejects a non-right angle", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 1)
		applyRejected(t, Rotate{Input: src, Angle: 45, Output: filepath.Join(dir, "x.pdf")}, "multiple of 90")
	})

	t.Run("rejects a selection matching no pages", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 2)
		applyRejected(t, Rotate{Input: src, Pages: "9", Angle: 90, Output: filepath.Join(dir, "x.pdf")}, "matches no pages")
	})
}

func TestStamp(t *testing.T) {
	ctx := context.Background()

	t.Run("stamps all pages and leaves the input unchanged", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 2)
		srcDigest := fileDigest(t, src)
		pages := pageCountOf(t, src)

		res, err := NewEngine().Apply(ctx, Stamp{Input: src, Text: "CONFIDENTIAL", Output: filepath.Join(dir, "stamped.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, pages, res.PageCount, "page count must survive stamping")
		require.True(t, stamped(t, res.OutputPath), "output must carry the stamp")
		require.False(t, stamped(t, src), "input must stay unstamped")
		require.Equal(t, srcDigest, fileDigest(t, src), "input was modified")
	})

	t.Run("stamps with options", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 1)

		res, err := NewEngine().Apply(ctx, Stamp{
			Input: src, Text: "DRAFT",
			Options: StampOptions{Position: "c", FontSize: 48, Rotation: 45, Opacity: 0.3},
			Output:  filepath.Join(dir, "stamped.pdf"),
		})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.True(t, stamped(t, res.OutputPath))
	})

	t.Run("stamps beneath content as a watermark", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 1)

		res, err := NewEngine().Apply(ctx, Stamp{
			Input: src, Text: "WATERMARK", Options: StampOptions{OnTop: false},
			Output: filepath.Join(dir, "stamped.pdf"),
		})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.True(t, stamped(t, res.OutputPath))
	})

	t.Run("stamps only the selected pages", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 2)

		res, err := NewEngine().Apply(ctx, Stamp{Input: src, Text: "S1", Pages: "1", Output: filepath.Join(dir, "stamped.pdf")})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.True(t, stamped(t, res.OutputPath))
	})

	t.Run("rejects empty text", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "src.pdf", "doc", 1)
		applyRejected(t, Stamp{Input: src, Text: " ", Output: filepath.Join(dir, "x.pdf")}, "text")
	})
}

func TestFillForm(t *testing.T) {
	ctx := context.Background()

	t.Run("fills text fields and a checkbox", func(t *testing.T) {
		dir := t.TempDir()
		src := makeFormPDF(t, dir, "form.pdf")
		srcDigest := fileDigest(t, src)

		res, err := NewEngine().Apply(ctx, FillForm{
			Input:  src,
			Fields: map[string]any{"full_name": "Alice Example", "city": "Springfield", "agree": true},
			Output: filepath.Join(dir, "filled.pdf"),
		})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, OpFillForm, res.Op)

		values := formValues(t, res.OutputPath)
		require.Equal(t, "Alice Example", values["full_name"])
		require.Equal(t, "Springfield", values["city"])
		require.Equal(t, true, values["agree"])
		require.Equal(t, srcDigest, fileDigest(t, src), "input was modified")
	})

	t.Run("accepts string booleans for checkboxes", func(t *testing.T) {
		dir := t.TempDir()
		src := makeFormPDF(t, dir, "form.pdf")

		res, err := NewEngine().Apply(ctx, FillForm{
			Input: src, Fields: map[string]any{"agree": "true"}, Output: filepath.Join(dir, "filled.pdf"),
		})
		require.NoError(t, err)
		require.True(t, res.OK, res.Message)
		require.Equal(t, true, formValues(t, res.OutputPath)["agree"])
	})

	t.Run("rejects unknown fields naming the available ones", func(t *testing.T) {
		dir := t.TempDir()
		src := makeFormPDF(t, dir, "form.pdf")

		res := applyRejected(t, FillForm{Input: src, Fields: map[string]any{"bogus": "x"}, Output: filepath.Join(dir, "filled.pdf")}, "bogus", "full_name")
		require.Equal(t, OpFillForm, res.Op)
		require.NoFileExists(t, filepath.Join(dir, "filled.pdf"), "a rejected operation writes no output")
	})

	t.Run("rejects a checkbox value that is not boolean", func(t *testing.T) {
		dir := t.TempDir()
		src := makeFormPDF(t, dir, "form.pdf")
		applyRejected(t, FillForm{Input: src, Fields: map[string]any{"agree": 42}, Output: filepath.Join(dir, "filled.pdf")}, "checkbox", "true or false")
	})

	t.Run("rejects a PDF without a form", func(t *testing.T) {
		dir := t.TempDir()
		plain := makePDF(t, dir, "plain.pdf", "doc", 1)
		applyRejected(t, FillForm{Input: plain, Fields: map[string]any{"full_name": "x"}, Output: filepath.Join(dir, "filled.pdf")}, "no fillable form")
	})

	t.Run("rejects an empty field map", func(t *testing.T) {
		dir := t.TempDir()
		src := makeFormPDF(t, dir, "form.pdf")
		applyRejected(t, FillForm{Input: src, Fields: map[string]any{}, Output: filepath.Join(dir, "filled.pdf")}, "at least one form field")
	})
}

func TestRewriteText(t *testing.T) {
	ctx := context.Background()

	t.Run("redirects to regeneration through create_document", func(t *testing.T) {
		dir := t.TempDir()
		src := makePDF(t, dir, "doc.pdf", "doc", 1)

		res, err := NewEngine().Apply(ctx, RewriteText{Input: src})
		require.NoError(t, err, "a text-rewrite redirect must not be a Go error")
		require.False(t, res.OK)
		require.Equal(t, OpRewriteText, res.Op)
		require.Contains(t, res.Message, "regenerating the document")
		require.Contains(t, res.Message, "create_document")
	})

	t.Run("redirects without an input too", func(t *testing.T) {
		res, err := NewEngine().Apply(ctx, RewriteText{})
		require.NoError(t, err)
		require.False(t, res.OK)
		require.Contains(t, res.Message, "create_document")
	})
}

func TestApply_ProcessingFailureIsReadable(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pdf")
	require.NoError(t, os.WriteFile(garbage, []byte("%PDF-1.7 this is not a PDF"), 0o644))

	res, err := NewEngine().Apply(context.Background(),
		Extract{Input: garbage, Pages: "1", Output: filepath.Join(dir, "out.pdf")})
	require.NoError(t, err, "a corrupt document is a processing failure, not an infrastructure error")
	require.False(t, res.OK)
	require.Contains(t, res.Message, "failed")
	require.NoFileExists(t, filepath.Join(dir, "out.pdf"), "a failed operation writes no output")
}
