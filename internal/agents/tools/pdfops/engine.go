package pdfops

import "context"

// Engine applies structural operations to PDF files in the workspace. The
// seam keeps pdfcpu swappable (design D11): the tool layer drives the
// interface, tests stub it, and the pdfcpu implementation stays
// package-local.
type Engine interface {
	// Apply runs op and returns a readable result. Rejected operations and
	// processing failures come back as Result{OK: false}; the error return
	// is reserved for infrastructure failures such as an unreadable input
	// or an unwritable output path.
	Apply(ctx context.Context, op Op) (Result, error)
}

// NewEngine returns the pdfcpu-backed engine.
func NewEngine() Engine {
	return &pdfcpuEngine{}
}
