package documents

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	// maxInputBytes is the input-file cap (design D9): an edit never parses
	// a document larger than this. It mirrors ingestion's 20 MiB cap.
	maxInputBytes = 20 << 20

	// maxDecompressedBytes caps the total uncompressed size an input
	// archive may declare and actually hold (design D9's zip-bomb defeat).
	maxDecompressedBytes = 512 << 20
)

// readableError is a failure the model can act on — an oversized or hostile
// file, a rejected operation, a processing failure. Engines surface it as a
// readable Result value; only infrastructure failures stay Go errors.
type readableError struct{ msg string }

func (e *readableError) Error() string { return e.msg }

// readablef builds a readableError.
func readablef(format string, args ...any) error {
	return &readableError{msg: fmt.Sprintf(format, args...)}
}

// asResult maps err into the result convention: a readableError becomes a
// failure Result; anything else is an infrastructure failure and is
// returned unchanged.
func asResult(err error) (Result, error) {
	var re *readableError
	if errors.As(err, &re) {
		return failf("%s", re.msg), nil
	}
	return Result{}, err
}

// runGuarded applies ops under panic recovery (design D9): buddies is an
// always-on tray gateway, so a library panic on a hostile document must
// become a readable failure Result, not a gateway crash. The Go error is
// reserved for infrastructure failures.
func runGuarded(apply func() (Result, error)) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = failf("document processing failed: %v", r)
			err = nil
		}
	}()
	return apply()
}

// checkInputFile stats the input before any parser sees it (design D9) and
// enforces the input cap. An oversize file is a readable rejection; a
// missing or unreadable file is an infrastructure error.
func checkInputFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("documents: stat input: %w", err)
	}
	if fi.Size() > maxInputBytes {
		return readablef("input is %.1f MiB, over the %d MiB limit — use a smaller document",
			float64(fi.Size())/(1<<20), maxInputBytes>>20)
	}
	return nil
}

// scanZipBounds pre-scans an OOXML archive with the standard library before
// any document library decompresses it (design D9): every member is
// decompressed under a running budget and the archive is rejected once the
// total uncompressed size exceeds the cap. This defeats zip bombs —
// including members whose declared sizes lie — at a bounded cost.
func scanZipBounds(data []byte) error {
	reject := func() error {
		return readablef("archive decompresses beyond the %d MiB safety cap — refusing a possible zip bomb",
			maxDecompressedBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return readablef("not a readable office document (corrupt archive): %v", err)
	}
	var total int64
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			return readablef("unreadable archive member %s: %v", zf.Name, err)
		}
		// Read one byte past the remaining budget: a member that yields it
		// pushes the total over the cap even though its declared size may not.
		n, err := io.CopyN(io.Discard, rc, maxDecompressedBytes-total+1)
		closeErr := rc.Close()
		total += n
		if errors.Is(err, io.EOF) {
			continue
		}
		if err == nil {
			return reject()
		}
		if closeErr != nil {
			err = closeErr
		}
		return readablef("unreadable archive member %s: %v", zf.Name, err)
	}
	if total > maxDecompressedBytes {
		return reject()
	}
	return nil
}

// gateInput runs the shared input gates (design D9) and reports the outcome
// in the result convention: a failure Result for a readable rejection, a Go
// error for an unreadable file.
func gateInput(src string) (data []byte, res Result, err error) {
	if err := checkInputFile(src); err != nil {
		res, err = asResult(err)
		return nil, res, err
	}
	data, err = os.ReadFile(src)
	if err != nil {
		return nil, Result{}, fmt.Errorf("documents: read input: %w", err)
	}
	if err := scanZipBounds(data); err != nil {
		res, err = asResult(err)
		return nil, res, err
	}
	return data, Result{OK: true}, nil
}

// writeResult writes the edited bytes to dst. A write failure is an
// infrastructure error.
func writeResult(dst string, data []byte) (Result, error) {
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return Result{}, fmt.Errorf("documents: write result: %w", err)
	}
	return Result{OK: true}, nil
}
