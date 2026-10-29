package carriage

import "testing"

// TestCarriageMatrix asserts every (kind, lane) cell per provider family
// (design D1/D4): the matrix constants are the flippable record of connector
// behavior, so each family's expected cells are spelled out here.
func TestCarriageMatrix(t *testing.T) {
	tests := []struct {
		name          string
		providerType  string
		pdfMessage    bool
		videoMessage  bool
		pdfToolResult bool
	}{
		{"gemini", "gemini", true, true, false},
		{"anthropic", "anthropic", true, false, true},
		{"anthropic-compatible", "anthropic-compatible", true, false, true},
		{"openai", "openai", false, false, false},
		{"openrouter", "openrouter", false, false, false},
		{"openai-compatible", "openai-compatible", false, false, false},
		{"unknown", "something-else", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CarriesPDFParts(tt.providerType); got != tt.pdfMessage {
				t.Errorf("CarriesPDFParts(%q) = %v, want %v", tt.providerType, got, tt.pdfMessage)
			}
			if got := CarriesVideoParts(tt.providerType); got != tt.videoMessage {
				t.Errorf("CarriesVideoParts(%q) = %v, want %v", tt.providerType, got, tt.videoMessage)
			}
			if got := CarriesPDFPartsInToolResults(tt.providerType); got != tt.pdfToolResult {
				t.Errorf("CarriesPDFPartsInToolResults(%q) = %v, want %v", tt.providerType, got, tt.pdfToolResult)
			}
		})
	}
}
