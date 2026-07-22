// Package tokens provides shared token-count heuristics used across the agent,
// middleware, and conversation layers.
package tokens

// Estimate approximates the number of model tokens for the given character
// length using the chars/4 heuristic. It is intentionally a coarse estimate
// suitable for budgeting and safety guards, not billing or exact accounting.
func Estimate(charLen int) int {
	return charLen / 4
}
