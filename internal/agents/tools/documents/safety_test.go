package documents

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunGuarded_PanicBecomesReadableResult pins design D9's core promise:
// a library panic under the guard is a readable failure Result, never a
// gateway crash, and never a Go error.
func TestRunGuarded_PanicBecomesReadableResult(t *testing.T) {
	res, err := runGuarded(func() (Result, error) {
		panic("nil map write in a hostile document")
	})
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "document processing failed")
	require.Contains(t, res.Error, "nil map write in a hostile document")
}

// TestRunGuarded_PassesThroughResults covers the steady state: the guard
// returns the applied outcome untouched when nothing panics.
func TestRunGuarded_PassesThroughResults(t *testing.T) {
	res, err := runGuarded(func() (Result, error) {
		return Result{OK: true}, nil
	})
	require.NoError(t, err)
	require.True(t, res.OK)
}

// TestCheckInputFile covers the stat-before-parse gate (design D9): a file
// over the cap is a readable rejection, a missing file an infrastructure
// error.
func TestCheckInputFile(t *testing.T) {
	t.Run("within the cap", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "small.bin")
		require.NoError(t, os.WriteFile(path, []byte("tiny"), 0o600))
		require.NoError(t, checkInputFile(path))
	})

	t.Run("over the cap", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "big.bin")
		f, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(maxInputBytes+1))
		require.NoError(t, f.Close())
		err = checkInputFile(path)
		require.Error(t, err)
		var re *readableError
		require.ErrorAs(t, err, &re, "oversize input is the model's to fix, so it reads as a rejection")
		require.Contains(t, err.Error(), "20 MiB limit")
	})

	t.Run("missing file", func(t *testing.T) {
		err := checkInputFile(filepath.Join(t.TempDir(), "absent.bin"))
		require.Error(t, err)
		var re *readableError
		require.NotErrorAs(t, err, &re, "an unreadable file is an infrastructure failure")
	})
}

// makeZip builds an in-memory zip with one deflate-compressed member.
func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	})
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// TestScanZipBounds covers the zip pre-scan (design D9): ordinary archives
// pass; an archive that decompresses past the cap — even a tiny one, thanks
// to compression — is refused as a possible zip bomb.
func TestScanZipBounds(t *testing.T) {
	t.Run("ordinary archive", func(t *testing.T) {
		require.NoError(t, scanZipBounds(makeZip(t, "word/document.xml", []byte("<w:body/>"))))
	})

	t.Run("not an archive", func(t *testing.T) {
		err := scanZipBounds([]byte("definitely not a zip"))
		require.Error(t, err)
		var re *readableError
		require.ErrorAs(t, err, &re, "a corrupt archive is a readable rejection")
	})

	t.Run("decompression bomb", func(t *testing.T) {
		// 512 MiB + 1 of zeros deflates to a few hundred kilobytes: a
		// well-formed, small archive that decompresses huge.
		bomb := makeZip(t, "word/document.xml", make([]byte, maxDecompressedBytes+1))
		require.Less(t, int64(len(bomb)), int64(4<<20), "the bomb must be small on disk")
		err := scanZipBounds(bomb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "zip bomb")
	})
}
