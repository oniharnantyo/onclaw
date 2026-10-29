package pdfops

// Operation names, shared with the tool layer that decodes model requests
// into operations.
const (
	OpMerge       = "merge"
	OpExtract     = "extract"
	OpRotate      = "rotate"
	OpStamp       = "stamp"
	OpFillForm    = "fill_form"
	OpRewriteText = "rewrite_text"
)

// Op is one structural operation on a PDF file. The concrete op structs in
// this package are the closed set the engine understands; the unexported
// marker method keeps other types out.
type Op interface {
	opName() string
}

// Merge concatenates the input PDFs, in order, into one output PDF.
type Merge struct {
	// Inputs are the workspace paths of the PDFs to concatenate, in order.
	Inputs []string
	// Output is the workspace path the merged PDF is written to.
	Output string
}

func (Merge) opName() string { return OpMerge }

// Extract writes the selected pages of one PDF to a new file.
type Extract struct {
	// Input is the workspace path of the source PDF.
	Input string
	// Pages is a page selection in pdfcpu syntax ("2", "1-3", "even",
	// "2-8:odd"); it selects at least one page.
	Pages string
	// Output is the workspace path the extracted pages are written to.
	Output string
}

func (Extract) opName() string { return OpExtract }

// Rotate turns the selected pages clockwise by Angle degrees.
type Rotate struct {
	// Input is the workspace path of the source PDF.
	Input string
	// Pages is a page selection in pdfcpu syntax; empty means all pages.
	Pages string
	// Angle is the clockwise rotation in degrees, a multiple of 90.
	Angle int
	// Output is the workspace path the rotated PDF is written to.
	Output string
}

func (Rotate) opName() string { return OpRotate }

// StampOptions shapes where and how a text stamp is drawn. Absent options
// keep pdfcpu's defaults.
type StampOptions struct {
	// Position anchors the stamp on the page: one of "tl", "tr", "bl",
	// "br", "c" (or a pdfcpu coordinate pair).
	Position string
	// FontSize is the stamp's text size in points; 0 keeps the default.
	FontSize float64
	// Rotation is the stamp text's clockwise rotation in degrees.
	Rotation int
	// Opacity is 0..1; 0 keeps the default.
	Opacity float64
	// OnTop draws the stamp above page content when true and beneath it
	// (watermark) when false.
	OnTop bool
}

// Stamp draws Text on the selected pages of one PDF.
type Stamp struct {
	// Input is the workspace path of the source PDF.
	Input string
	// Text is the text to stamp on the pages.
	Text string
	// Options shapes placement and rendering of the stamp.
	Options StampOptions
	// Pages is a page selection in pdfcpu syntax; empty means all pages.
	Pages string
	// Output is the workspace path the stamped PDF is written to.
	Output string
}

func (Stamp) opName() string { return OpStamp }

// FillForm sets AcroForm fields of one PDF.
type FillForm struct {
	// Input is the workspace path of the source PDF.
	Input string
	// Fields maps field names (or numeric field ids) to values: strings
	// for text-ish fields, booleans for checkboxes, a value or array for
	// list boxes.
	Fields map[string]any
	// Output is the workspace path the filled PDF is written to.
	Output string
}

func (FillForm) opName() string { return OpFillForm }

// RewriteText is a request to change a PDF's wording. It performs no
// operation: applying it returns a readable redirect naming create_document,
// because PDF text is changed by regenerating the document (design D7).
type RewriteText struct {
	// Input is the workspace path of the PDF whose wording was to change.
	Input string
}

func (RewriteText) opName() string { return OpRewriteText }
