package pdfops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MaxInputBytes caps the size of one input file. It mirrors the ingestion
// cap that bounds hostile documents (design D9): every input is stat'ed
// before it is parsed.
const MaxInputBytes = 20 << 20

// rejectedError marks an operation the model can correct — a missing input,
// a bad page range, unknown form fields. It never leaves the package as a
// Go error: Apply converts it into a readable Result.
type rejectedError struct {
	msg string
}

func (e *rejectedError) Error() string { return e.msg }

// rejectf builds a rejected-operation error; see rejectedError.
func rejectf(format string, args ...any) error {
	return &rejectedError{msg: fmt.Sprintf(format, args...)}
}

// isRejection reports whether err is a rejected operation and returns its
// message.
func isRejection(err error) (string, bool) {
	var r *rejectedError
	if errors.As(err, &r) {
		return r.msg, true
	}
	return "", false
}

// safeApply runs apply with panic containment: a panic inside a document
// library becomes a readable failure result, never a gateway crash
// (design D9).
func safeApply(op Op, apply func() (Result, error)) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			name := ""
			if op != nil {
				name = op.opName()
			}
			res = newFailure(name, fmt.Sprintf("the PDF operation failed unexpectedly (%v) — the input file may be malformed", r))
			err = nil
		}
	}()
	return apply()
}

// checkInput stats one input file: a missing file and an oversized file are
// rejections, another stat failure is an infrastructure error.
func checkInput(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rejectf("input %q not found — stage the file into the workspace first", path)
		}
		return fmt.Errorf("stat input %s: %w", path, err)
	}
	if fi.IsDir() {
		return rejectf("input %q is a directory, not a PDF file", path)
	}
	if fi.Size() > MaxInputBytes {
		return rejectf("input %q is %d bytes — PDF operations accept files up to %d bytes (%d MiB); split or compress it first",
			path, fi.Size(), MaxInputBytes, MaxInputBytes>>20)
	}
	return nil
}

// ensureOutputDistinct rejects an output path that would overwrite one of
// the operation's inputs. Same-file aliases (hardlinks, case-insensitive
// spellings) are caught with os.SameFile when the output already exists.
func ensureOutputDistinct(output string, inputs ...string) error {
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve output path %s: %w", output, err)
	}
	var outInfo os.FileInfo
	if fi, err := os.Stat(output); err == nil {
		outInfo = fi
	}
	for _, in := range inputs {
		inAbs, err := filepath.Abs(in)
		if err != nil {
			return fmt.Errorf("resolve input path %s: %w", in, err)
		}
		if inAbs == outAbs {
			return rejectf("output %q would overwrite input %q — choose a different output path", output, in)
		}
		if outInfo != nil {
			if fi, err := os.Stat(in); err == nil && os.SameFile(fi, outInfo) {
				return rejectf("output %q is the same file as input %q — choose a different output path", output, in)
			}
		}
	}
	return nil
}
