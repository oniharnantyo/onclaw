package pdfops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// pdfcpuEngine is the Engine implementation on top of pdfcpu's public API
// (github.com/pdfcpu/pdfcpu/pkg/api — never its CLI packages, design D2).
type pdfcpuEngine struct{}

// Compile-time check that the implementation satisfies the seam.
var _ Engine = (*pdfcpuEngine)(nil)

// Apply runs op with panic containment (design D9) and routes rejected
// operations to readable results.
func (e *pdfcpuEngine) Apply(ctx context.Context, op Op) (Result, error) {
	res, err := safeApply(op, func() (Result, error) {
		return e.dispatch(ctx, op)
	})
	if err != nil {
		msg, rejected := isRejection(err)
		if !rejected {
			return Result{}, err
		}
		return newFailure(op.opName(), msg), nil
	}
	return res, nil
}

// dispatch runs one concrete operation.
func (e *pdfcpuEngine) dispatch(ctx context.Context, op Op) (Result, error) {
	switch o := op.(type) {
	case nil:
		return Result{}, errors.New("pdfops: nil operation")
	case Merge:
		return e.merge(ctx, o)
	case Extract:
		return e.extract(ctx, o)
	case Rotate:
		return e.rotate(ctx, o)
	case Stamp:
		return e.stamp(ctx, o)
	case FillForm:
		return e.fillForm(ctx, o)
	case RewriteText:
		return e.rewriteText(ctx, o)
	default:
		// Op is sealed by its unexported marker method, so this is only
		// reachable for in-package mistakes.
		return Result{}, fmt.Errorf("pdfops: unsupported operation type %T", op)
	}
}

// openInput opens one checked input file for reading; a file that stats but
// cannot be opened is an infrastructure failure.
func openInput(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read input %s: %w", path, err)
	}
	return f, nil
}

// closeInput closes a read-only input file; nothing can act on its close
// error.
func closeInput(f *os.File) {
	_ = f.Close()
}

// rewind seeks an input file back to its start between pdfcpu reads.
func rewind(f *os.File) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind input %s: %w", f.Name(), err)
	}
	return nil
}

// stageOutput creates a temporary file next to output, so a failed
// operation never leaves a partial PDF at the output path. A file that
// cannot be created is an infrastructure failure.
func stageOutput(output string) (*os.File, error) {
	f, err := os.CreateTemp(filepath.Dir(output), "."+filepath.Base(output)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create output %s: %w", output, err)
	}
	return f, nil
}

// commitOutput closes the staged file and moves it to its output path.
func commitOutput(f *os.File, output string) error {
	name := f.Name()
	if err := f.Close(); err != nil {
		removeStaged(name)
		return fmt.Errorf("close output %s: %w", output, err)
	}
	if err := os.Rename(name, output); err != nil {
		removeStaged(name)
		return fmt.Errorf("publish output %s: %w", output, err)
	}
	return nil
}

// discardOutput closes and removes a staged file after a failure; the
// failure is already reported, so cleanup errors are dropped.
func discardOutput(f *os.File) {
	if f == nil {
		return
	}
	name := f.Name()
	if err := f.Close(); err == nil {
		removeStaged(name)
	}
}

// removeStaged removes a staged temporary file after a failure was already
// reported; cleanup errors are dropped.
func removeStaged(name string) {
	_ = os.Remove(name)
}

// countPages reports the page count of a written PDF.
func countPages(ctx context.Context, path string) (int, error) {
	pages, err := api.PageCountFile(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("count pages of %s: %w", path, err)
	}
	return pages, nil
}
