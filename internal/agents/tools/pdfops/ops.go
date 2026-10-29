package pdfops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// pageSelection is a validated page selection bound to an open input file.
type pageSelection struct {
	reader *os.File
	pages  []string // nil means all pages
	count  int      // matched page numbers; 0 when pages is nil
}

// close closes the selection's input file.
func (s *pageSelection) close() {
	closeInput(s.reader)
}

// checkedSelection validates pages against the input's page count and
// returns the selection bound to the open input file. An empty selection
// means all pages and needs no validation.
func checkedSelection(ctx context.Context, input, pages string) (*pageSelection, error) {
	in, err := openInput(input)
	if err != nil {
		return nil, err
	}
	if pages == "" {
		return &pageSelection{reader: in}, nil
	}
	pageCount, err := api.PageCount(ctx, in, nil)
	if err != nil {
		closeInput(in)
		return nil, rejectf("reading %s failed: %v", input, err)
	}
	if err := rewind(in); err != nil {
		closeInput(in)
		return nil, err
	}
	numbers, err := api.PagesForSelection(pageCount, []string{pages}, false)
	if err != nil {
		closeInput(in)
		return nil, rejectf("invalid page selection %q for %s (%d pages): %v", pages, input, pageCount, err)
	}
	if len(numbers) == 0 {
		closeInput(in)
		return nil, rejectf("page selection %q matches no pages of %s (%d pages)", pages, input, pageCount)
	}
	return &pageSelection{reader: in, pages: []string{pages}, count: len(numbers)}, nil
}

// merge concatenates the inputs, in order, into one output PDF. pdfcpu
// reads every input and writes only the output.
func (e *pdfcpuEngine) merge(ctx context.Context, op Merge) (Result, error) {
	if len(op.Inputs) < 2 {
		return Result{}, rejectf("merge needs at least two input PDFs, got %d", len(op.Inputs))
	}
	if op.Output == "" {
		return Result{}, rejectf("merge needs an output path")
	}
	for _, in := range op.Inputs {
		if err := checkInput(in); err != nil {
			return Result{}, err
		}
	}
	if err := ensureOutputDistinct(op.Output, op.Inputs...); err != nil {
		return Result{}, err
	}

	out, err := stageOutput(op.Output)
	if err != nil {
		return Result{}, err
	}
	if err := api.Merge(ctx, "", op.Inputs, out, nil, false); err != nil {
		discardOutput(out)
		return Result{}, rejectf("merging the PDFs failed: %v", err)
	}
	pages, err := countPages(ctx, out.Name())
	if err != nil {
		discardOutput(out)
		return Result{}, err
	}
	if err := commitOutput(out, op.Output); err != nil {
		return Result{}, err
	}
	return newSuccess(OpMerge, op.Output,
		fmt.Sprintf("merged %d PDFs into %s (%d pages)", len(op.Inputs), op.Output, pages), pages), nil
}

// extract writes the selected pages of one PDF to a new file.
func (e *pdfcpuEngine) extract(ctx context.Context, op Extract) (Result, error) {
	if op.Output == "" {
		return Result{}, rejectf("extract needs an output path")
	}
	if strings.TrimSpace(op.Pages) == "" {
		return Result{}, rejectf("extract needs a page selection such as \"1-3\" or \"even\"")
	}
	if err := checkInput(op.Input); err != nil {
		return Result{}, err
	}
	if err := ensureOutputDistinct(op.Output, op.Input); err != nil {
		return Result{}, err
	}

	selection, err := checkedSelection(ctx, op.Input, op.Pages)
	if err != nil {
		return Result{}, err
	}
	defer selection.close()

	out, err := stageOutput(op.Output)
	if err != nil {
		return Result{}, err
	}
	if err := api.Trim(ctx, selection.reader, out, selection.pages, nil); err != nil {
		discardOutput(out)
		return Result{}, rejectf("extracting pages from %s failed: %v", op.Input, err)
	}
	pages, err := countPages(ctx, out.Name())
	if err != nil {
		discardOutput(out)
		return Result{}, err
	}
	if err := commitOutput(out, op.Output); err != nil {
		return Result{}, err
	}
	return newSuccess(OpExtract, op.Output,
		fmt.Sprintf("extracted %d pages (%s) from %s into %s", selection.count, op.Pages, op.Input, op.Output), pages), nil
}

// rotate turns the selected pages clockwise by op.Angle degrees.
func (e *pdfcpuEngine) rotate(ctx context.Context, op Rotate) (Result, error) {
	if op.Output == "" {
		return Result{}, rejectf("rotate needs an output path")
	}
	if op.Angle%90 != 0 {
		return Result{}, rejectf("rotation angle must be a multiple of 90 degrees, got %d", op.Angle)
	}
	if err := checkInput(op.Input); err != nil {
		return Result{}, err
	}
	if err := ensureOutputDistinct(op.Output, op.Input); err != nil {
		return Result{}, err
	}

	selection, err := checkedSelection(ctx, op.Input, op.Pages)
	if err != nil {
		return Result{}, err
	}
	defer selection.close()

	out, err := stageOutput(op.Output)
	if err != nil {
		return Result{}, err
	}
	if err := api.Rotate(ctx, selection.reader, out, op.Angle, selection.pages, nil); err != nil {
		discardOutput(out)
		return Result{}, rejectf("rotating pages of %s failed: %v", op.Input, err)
	}
	pages, err := countPages(ctx, out.Name())
	if err != nil {
		discardOutput(out)
		return Result{}, err
	}
	if err := commitOutput(out, op.Output); err != nil {
		return Result{}, err
	}
	return newSuccess(OpRotate, op.Output, rotateMessage(op, selection.count), pages), nil
}

// rotateMessage describes a completed rotation.
func rotateMessage(op Rotate, matched int) string {
	angle := fmt.Sprintf("%d degrees", op.Angle)
	if op.Pages == "" {
		return fmt.Sprintf("rotated all pages of %s by %s into %s", op.Input, angle, op.Output)
	}
	return fmt.Sprintf("rotated %d pages (%s) of %s by %s into %s",
		matched, op.Pages, op.Input, angle, op.Output)
}

// stamp draws op.Text on the selected pages, above or beneath content.
func (e *pdfcpuEngine) stamp(ctx context.Context, op Stamp) (Result, error) {
	if op.Output == "" {
		return Result{}, rejectf("stamp needs an output path")
	}
	if strings.TrimSpace(op.Text) == "" {
		return Result{}, rejectf("stamp needs text to place on the pages")
	}
	if err := checkInput(op.Input); err != nil {
		return Result{}, err
	}
	if err := ensureOutputDistinct(op.Output, op.Input); err != nil {
		return Result{}, err
	}

	selection, err := checkedSelection(ctx, op.Input, op.Pages)
	if err != nil {
		return Result{}, err
	}
	defer selection.close()

	// AddTextWatermarksFile stages and publishes the output file itself;
	// prove the output location is writable first so an unwritable path is
	// reported as an infrastructure failure, not an operation failure.
	probe, err := stageOutput(op.Output)
	if err != nil {
		return Result{}, err
	}
	discardOutput(probe)

	if err := api.AddTextWatermarksFile(ctx, op.Input, op.Output, selection.pages,
		op.Options.OnTop, op.Text, stampDescription(op.Options), nil); err != nil {
		return Result{}, rejectf("stamping %s failed: %v", op.Input, err)
	}
	pages, err := countPages(ctx, op.Output)
	if err != nil {
		return Result{}, err
	}
	return newSuccess(OpStamp, op.Output,
		fmt.Sprintf("stamped %q on %s into %s (%d pages)", op.Text, op.Input, op.Output, pages), pages), nil
}

// stampDescription renders the op's options as pdfcpu's watermark
// description string; absent options keep pdfcpu's defaults.
func stampDescription(o StampOptions) string {
	var parts []string
	if o.Position != "" {
		parts = append(parts, "position:"+o.Position)
	}
	if o.FontSize > 0 {
		parts = append(parts, "points:"+strconv.FormatFloat(o.FontSize, 'f', -1, 64))
	}
	if o.Rotation != 0 {
		parts = append(parts, "rotation:"+strconv.Itoa(o.Rotation))
	}
	if o.Opacity > 0 {
		parts = append(parts, "opacity:"+strconv.FormatFloat(o.Opacity, 'f', -1, 64))
	}
	return strings.Join(parts, ", ")
}

// fillForm sets AcroForm fields: the input's form is exported through the
// API, the requested values are overlaid onto the exported JSON, and pdfcpu
// fills the form from it.
func (e *pdfcpuEngine) fillForm(ctx context.Context, op FillForm) (Result, error) {
	if op.Output == "" {
		return Result{}, rejectf("fill_form needs an output path")
	}
	if len(op.Fields) == 0 {
		return Result{}, rejectf("fill_form needs at least one form field to set")
	}
	if err := checkInput(op.Input); err != nil {
		return Result{}, err
	}
	if err := ensureOutputDistinct(op.Output, op.Input); err != nil {
		return Result{}, err
	}

	in, err := openInput(op.Input)
	if err != nil {
		return Result{}, err
	}
	defer closeInput(in)

	var exported bytes.Buffer
	if err := api.ExportFormJSON(ctx, in, &exported, op.Input, nil); err != nil {
		return Result{}, rejectf("%s has no fillable form: %v", op.Input, err)
	}
	if err := rewind(in); err != nil {
		return Result{}, err
	}
	data, filled, err := overlayFormFields(exported.Bytes(), op.Fields)
	if err != nil {
		return Result{}, err
	}

	out, err := stageOutput(op.Output)
	if err != nil {
		return Result{}, err
	}
	if err := api.FillForm(ctx, in, bytes.NewReader(data), out, nil); err != nil {
		discardOutput(out)
		return Result{}, rejectf("filling the form of %s failed: %v", op.Input, err)
	}
	pages, err := countPages(ctx, out.Name())
	if err != nil {
		discardOutput(out)
		return Result{}, err
	}
	if err := commitOutput(out, op.Output); err != nil {
		return Result{}, err
	}
	return newSuccess(OpFillForm, op.Output,
		fmt.Sprintf("filled %d form fields in %s (%d pages)", filled, op.Output, pages), pages), nil
}

// rewriteRedirect is the structured answer to a text-rewrite request: PDF
// text is changed by regenerating the document (design D7).
const rewriteRedirect = "PDF text is changed by regenerating the document — compose the revised content and use the create_document tool to generate a new PDF."

// rewriteText performs no operation; it answers the request with the
// regeneration redirect so the model can reroute.
func (e *pdfcpuEngine) rewriteText(_ context.Context, op RewriteText) (Result, error) {
	if op.Input == "" {
		return newFailure(OpRewriteText, rewriteRedirect), nil
	}
	return newFailure(OpRewriteText,
		fmt.Sprintf("The wording of %s cannot be changed in place. %s", op.Input, rewriteRedirect)), nil
}
