package mcp

import (
	"fmt"
	"strings"
)

// maxToolNameLen is the provider-safe function-name limit
// (^[a-zA-Z0-9_-]{1,64}$); names are clamped to it.
const maxToolNameLen = 64

// ToolName builds the addressable name for one MCP tool:
// mcp__<server>__<tool>, sanitized provider-safe. Convenience for single
// tool names (probe/display); resolution passes use a [Namer] so collisions
// after sanitization get deterministic suffixes.
func ToolName(server, tool string) string {
	n := NewNamer()
	return n.Name(server, tool)
}

// Namer assigns mcp__<server>__<tool> names across one resolve pass. It is
// stateful by design: two tools whose sanitized names collide get numeric
// suffixes in assignment order, so the mapping is deterministic given a
// stable server (and per-server tool) order.
type Namer struct {
	used map[string]struct{}
}

// NewNamer returns a Namer for one resolve pass.
func NewNamer() *Namer {
	return &Namer{used: make(map[string]struct{})}
}

// Name returns the provider-safe name for the server tool, marking it used.
// Collisions after sanitization append _2, _3, …; over-length names are
// sanitize-and-truncated with a short suffix hash so distinct pathological
// inputs stay individually addressable (design.md D7).
func (n *Namer) Name(server, tool string) string {
	s, t := sanitizeSegment(server), sanitizeSegment(tool)
	name := "mcp__" + s + "__" + t
	if len(name) > maxToolNameLen {
		name = truncateWithHash(s, t)
	}
	// Numeric-suffix loop over the FINAL name: a suffixed name may itself
	// collide (or exceed the limit after suffixing), so clamp per attempt.
	candidate := name
	for k := 2; ; k++ {
		if _, taken := n.used[candidate]; !taken {
			break
		}
		suffix := fmt.Sprintf("_%d", k)
		candidate = clampWithSuffix(name, suffix)
	}
	n.used[candidate] = struct{}{}
	return candidate
}

// sanitizeSegment lowercases and maps every character outside [a-z0-9_-] to
// '_'. Server names sanitize case-insensitively per design.md D7; tool names
// follow the same rule so case variants cannot smuggle collisions past the
// sanitization. A segment of all-invalid characters still yields underscores,
// never an empty string.
func sanitizeSegment(seg string) string {
	var b strings.Builder
	b.Grow(len(seg))
	for _, r := range strings.ToLower(seg) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// truncateWithHash builds "mcp__<s>__<t>_<hash8>" clamped to
// maxToolNameLen. The hash is FNV-1a over the sanitized segments, so distinct
// over-length inputs keep distinct names while the same input always yields
// the same name.
func truncateWithHash(s, t string) string {
	// Masked to 32 bits so the hex form is always exactly 8 chars.
	h := fmt.Sprintf("%08x", fnv1a(s+"\x00"+t)&0xFFFFFFFF)
	// mcp__(5) + s + __(2) + t + _(1) + hash(8) = 16 + len(s) + len(t) ≤ 64
	// → s and t share 48 chars; server side is capped at half so long tool
	// names keep most of the budget.
	sBudget := min(len(s), 24)
	tBudget := min(len(t), 48-sBudget)
	return fmt.Sprintf("mcp__%s__%s_%s", s[:sBudget], t[:tBudget], h)
}

// clampWithSuffix appends suffix to name within maxToolNameLen, trimming the
// base when the suffix would overflow. Trailing underscores are stripped
// before appending so the seam reads cleanly.
func clampWithSuffix(name, suffix string) string {
	budget := maxToolNameLen - len(suffix)
	if budget < 0 {
		budget = 0
	}
	if len(name) > budget {
		name = strings.TrimRight(name[:budget], "_")
	}
	return name + suffix
}

// fnv1a is the 64-bit FNV-1a hash, used only for name-suffix entropy.
func fnv1a(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
