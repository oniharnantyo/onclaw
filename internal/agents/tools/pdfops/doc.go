// Package pdfops applies structural operations to PDF files in the agent
// workspace: merging documents, extracting page ranges, rotating pages,
// stamping text, and filling form fields.
//
// It is the PDF boundary of the document story (design D7): structural work
// only, and never a rewrite of a PDF's text content — wording changes belong
// to document regeneration through create_document, and the rewrite_text
// operation answers such requests with a readable redirect.
//
// pdfcpu (github.com/pdfcpu/pdfcpu) is imported through pkg/api only; its
// CLI machinery must never link into the binary (design D2). Inputs are
// never modified: every operation reads its inputs and writes an explicit
// output path.
package pdfops
