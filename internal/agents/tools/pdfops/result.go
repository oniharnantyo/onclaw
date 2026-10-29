package pdfops

// Result reports the outcome of one operation. The PDF tool surfaces these
// fields on its result card and in the model's turn.
type Result struct {
	// OK is true when the operation completed and wrote OutputPath.
	OK bool
	// Op is the name of the operation that produced this result.
	Op string
	// Message is a readable explanation, always set: what happened, or why
	// the operation was rejected or failed.
	Message string
	// OutputPath is the workspace path of the written PDF, set when OK.
	OutputPath string
	// PageCount is the page count of the written PDF, set when OK.
	PageCount int
}

// newSuccess builds the result of a completed operation.
func newSuccess(op, output, message string, pages int) Result {
	return Result{
		OK:         true,
		Op:         op,
		Message:    message,
		OutputPath: output,
		PageCount:  pages,
	}
}

// newFailure builds the readable result of a rejected or failed operation.
func newFailure(op, message string) Result {
	return Result{OK: false, Op: op, Message: message}
}
