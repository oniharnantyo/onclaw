package memory

import (
	"fmt"
	"regexp"
)

var (
	// Case-insensitive regexes for prompt injection patterns
	injectionRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)ignore\s+(?:previous\s+)?instructions`),
		regexp.MustCompile(`(?i)ignore\s+the\s+above`),
		regexp.MustCompile(`(?i)you\s+are\s+now\s+a`),
		regexp.MustCompile(`(?i)system\s+prompt`),
		regexp.MustCompile(`(?i)developer\s+mode`),
		regexp.MustCompile(`(?i)assistant\s+bypass`),
	}

	// Regexes for common credential patterns
	credentialRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:api[_-]?key|password|secret|token|passwd|bearer)\s*[:=\s]\s*[a-zA-Z0-9_\-\.\~]{16,}`),
		regexp.MustCompile(`\bsk-[a-zA-Z0-9]{20,}\b`),         // OpenAI standard keys
		regexp.MustCompile(`\bsk-proj-[a-zA-Z0-9_\-]{20,}\b`), // OpenAI project keys
		regexp.MustCompile(`\bAIzaSy[a-zA-Z0-9_\-]{33}\b`),    // Google API keys
		regexp.MustCompile(`\bgsk_[a-zA-Z0-9]{20,}\b`),        // Groq API keys
	}
)

// ScanContent scans the content for prompt-injection, credentials, or invisible Unicode patterns.
// Returns an error if any threat pattern is detected.
func ScanContent(content string) error {
	// 1. Scan for invisible Unicode / direction override characters
	for _, r := range content {
		if r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\u200E' || r == '\u200F' ||
			r == '\u202A' || r == '\u202B' || r == '\u202C' || r == '\u202D' || r == '\u202E' ||
			r == '\uFEFF' {
			return fmt.Errorf("security threat detected: invisible Unicode or direction override character found")
		}
	}

	// 2. Scan for prompt injection patterns
	for _, re := range injectionRegexes {
		if re.MatchString(content) {
			return fmt.Errorf("security threat detected: possible prompt injection pattern matched: %q", re.String())
		}
	}

	// 3. Scan for credentials
	for _, re := range credentialRegexes {
		if re.MatchString(content) {
			return fmt.Errorf("security threat detected: potential credential or API key matched")
		}
	}

	return nil
}
