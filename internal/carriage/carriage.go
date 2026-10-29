// Package carriage is the single source of truth for which content kinds a
// provider family's connector can carry (fix-pdf-attachment-carriage design
// D1/D4): one predicate per (kind, lane) cell, cited against the pinned
// eino-ext connector sources. It replaces the hardcoded gemini-only gates in
// the turn's model-input assembly. A cell flips when a live smoke or a
// connector upgrade proves the connector's behavior changed — nowhere else
// may a provider-type comparison decide carriage.
package carriage

// CarriesPDFParts reports whether the provider family's connector maps a PDF
// file part to native content in a user message. Verified against the pinned
// connectors: agenticgemini maps file parts to inline data (v0.2.4), and
// agenticclaude maps base64 file blocks to native PDF document blocks
// (v0.1.6); the OpenAI-family chat conversion (agenticopenai v0.2.3) has no
// file-block case and errors a turn that tries. For an anthropic-compatible
// endpoint the per-model PDF-input checkbox is the user's assertion that the
// endpoint passes document blocks.
func CarriesPDFParts(providerType string) bool {
	switch providerType {
	case "gemini", "anthropic", "anthropic-compatible":
		return true
	default:
		return false
	}
}

// CarriesVideoParts reports whether the provider family's connector maps a
// video part to native content in a user message: today only the gemini
// connector maps video parts (inline data); the claude connector and the
// OpenAI-shaped endpoints reject them.
func CarriesVideoParts(providerType string) bool {
	return providerType == "gemini"
}

// CarriesPDFPartsInToolResults reports whether the provider family's
// connector maps a PDF file part inside a tool_result to native content
// (design D4). Verified against the pinned connectors: agenticclaude maps
// tool-result files to document blocks (v0.1.6) — the cell starts inline and
// a live smoke flips it to false if the API rejects a document inside
// tool_result content; the OpenAI chat conversion has no file case
// (agenticopenai v0.2.3) and the gemini connector carries text-only tool
// results (agenticgemini v0.2.4), so both families extract instead.
func CarriesPDFPartsInToolResults(providerType string) bool {
	switch providerType {
	case "anthropic", "anthropic-compatible":
		return true
	default:
		return false
	}
}
