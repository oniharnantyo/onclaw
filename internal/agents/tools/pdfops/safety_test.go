package pdfops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeApply_RecoversPanics(t *testing.T) {
	panicOp := fakeOp{name: OpExtract}
	res, err := safeApply(panicOp, func() (Result, error) {
		panic("pdfcpu exploded")
	})
	require.NoError(t, err, "a recovered panic must not surface as a Go error")
	require.False(t, res.OK)
	require.Equal(t, OpExtract, res.Op)
	require.Contains(t, res.Message, "failed unexpectedly")
	require.Contains(t, res.Message, "pdfcpu exploded")

	t.Run("with a nil op", func(t *testing.T) {
		res, err := safeApply(nil, func() (Result, error) {
			panic("boom")
		})
		require.NoError(t, err)
		require.False(t, res.OK)
	})
}

func TestSafeApply_PassesResultsThrough(t *testing.T) {
	want := newSuccess(OpMerge, "out.pdf", "done", 3)
	res, err := safeApply(fakeOp{name: OpMerge}, func() (Result, error) {
		return want, nil
	})
	require.NoError(t, err)
	require.Equal(t, want, res)

	wantErr := errors.New("disk full")
	_, err = safeApply(fakeOp{name: OpMerge}, func() (Result, error) {
		return Result{}, wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

func TestCheckInput(t *testing.T) {
	dir := t.TempDir()

	t.Run("rejects a missing file", func(t *testing.T) {
		err := checkInput(filepath.Join(dir, "missing.pdf"))
		requireRejection(t, err, "not found", "stage the file")
	})

	t.Run("rejects a directory", func(t *testing.T) {
		err := checkInput(dir)
		requireRejection(t, err, "directory")
	})

	t.Run("rejects a file over the cap", func(t *testing.T) {
		path := filepath.Join(dir, "big.pdf")
		oversized(t, path, MaxInputBytes+1)
		err := checkInput(path)
		requireRejection(t, err, "20 MiB", "bytes")
	})

	t.Run("accepts a file at the cap", func(t *testing.T) {
		path := filepath.Join(dir, "exact.pdf")
		oversized(t, path, MaxInputBytes)
		require.NoError(t, checkInput(path))
	})
}

func TestOpenInput_UnreadableFileIsInfrastructure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.pdf")
	require.NoError(t, os.WriteFile(path, []byte("%PDF-1.7"), 0o644))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := openInput(path)
	require.Error(t, err, "an unreadable input is an infrastructure failure")
	require.NotErrorIs(t, err, &rejectedError{})
}

func TestEnsureOutputDistinct(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.pdf")
	require.NoError(t, os.WriteFile(input, []byte("%PDF-1.7"), 0o644))

	t.Run("rejects the exact input path", func(t *testing.T) {
		requireRejection(t, ensureOutputDistinct(input, input), "overwrite")
	})

	t.Run("rejects a hardlinked output", func(t *testing.T) {
		alias := filepath.Join(dir, "alias.pdf")
		require.NoError(t, os.Link(input, alias))
		t.Cleanup(func() { _ = os.Remove(alias) })
		requireRejection(t, ensureOutputDistinct(alias, input), "same file")
	})

	t.Run("accepts a distinct path", func(t *testing.T) {
		require.NoError(t, ensureOutputDistinct(filepath.Join(dir, "out.pdf"), input))
	})
}

// oversized creates a file of exactly size bytes without paying for it all
// (a sparse write at the end).
func oversized(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer closeInput(f)
	require.NoError(t, f.Truncate(size))
}

// requireRejection asserts err is a readable rejected-operation error whose
// message contains every given substring.
func requireRejection(t *testing.T, err error, contains ...string) {
	t.Helper()
	require.Error(t, err)
	msg, ok := isRejection(err)
	require.True(t, ok, "expected a rejection, got: %v", err)
	for _, s := range contains {
		require.Contains(t, msg, s)
	}
}
