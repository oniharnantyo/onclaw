package domain

// LocatorKind* values label the form of a document section's rendered
// locator string (add-reference-documents design D3): a section stores the
// human-readable locator ("p. 30", "slide 5", "sheet Users", heading text,
// or empty) alongside one of these kinds so search hits and citation chips
// render uniformly.
const (
	LocatorKindNone    = "none"    // no locator: txt/csv sections and preambles
	LocatorKindPage    = "page"    // PDF page: "p. 30"
	LocatorKindSlide   = "slide"   // pptx slide: "slide 5"
	LocatorKindSheet   = "sheet"   // xlsx sheet: "sheet Users"
	LocatorKindHeading = "heading" // docx/md/html heading: the heading text
)
