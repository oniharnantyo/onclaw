package domain

import (
	"fmt"
	"strings"
)

// Recipe tool tiers (add-integration-authority tasks.md 1.1): every tool a
// recipe's connection exposes is classified read or write in recipe data.
// The tier is the gate's data half (design.md D2/D5): read-tier tools ride
// workspace membership, write-tier tools require IntegrationsWrite. The
// fail-safe default is write — RecipeToolTierDefault — so a tool the recipe
// does not declare (or a recipe release that ships a new tool before a tier
// list catches up) never silently becomes member-accessible.
const (
	RecipeToolTierRead  = "read"
	RecipeToolTierWrite = "write"
)

// RecipeToolTierDefault is the tier of every undeclared tool (spec:
// "Tools without a declared tier SHALL be treated as write — fail-safe").
const RecipeToolTierDefault = RecipeToolTierWrite

// IsValidRecipeToolTier reports whether tier is one of the catalog values.
func IsValidRecipeToolTier(tier string) bool {
	switch tier {
	case RecipeToolTierRead, RecipeToolTierWrite:
		return true
	default:
		return false
	}
}

// RecipeToolTierRule is one tool's tier declaration in an mcp-kind recipe's
// tool-tier list. MCP tools are runtime-discovered (the remote server's tool
// catalog is not recipe data), so the tier list is the recipe's explicit
// classification layer over the names the server assigns. A slice, not a
// map, so the served JSON order is deterministic (the RecipeScopes
// precedent).
type RecipeToolTierRule struct {
	// Tool is the MCP tool's server-assigned name as the recipe declares it —
	// e.g. "create_pull_request" — NOT the prefixed runtime name
	// ("mcp__github__create_pull_request"). Restricted to the characters MCP
	// name sanitization preserves ([a-zA-Z0-9_-]), so a declared name always
	// matches the runtime raw name exactly (see EffectiveToolTier).
	Tool string `json:"tool"`
	// Tier is one of the RecipeToolTier* constants (required — a rule without
	// a tier is meaningless; validation rejects anything outside the
	// catalog).
	Tier string `json:"tier"`
}

// RecipeTierCounts is a recipe's declared read/write tool split — the
// per-connection projection the agent config integrations section renders
// (spec: "Visible effective tiers"). Counts are declaration-derived: they sum
// the recipe's declared tiers (verbs for http kind, the tool-tier list for
// mcp kind); an undeclared tool also gates as write but is not counted —
// the live-verification pass keeps the lists representative.
type RecipeTierCounts struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

// validateRecipeToolTiers checks an mcp-kind recipe's tool-tier list
// (add-integration-authority tasks.md 1.1): well-formed unique tool names and
// catalog tiers. Well-formedness reuses the parameter-name charset — exactly
// the characters MCP name sanitization preserves — so a declared name
// survives the runtime rename unchanged and the tier lookup stays an exact
// match (a name the sanitizer would alter, like one carrying a dot, could
// never match its sanitized runtime form and would silently gate as write).
func validateRecipeToolTiers(r *Recipe) error {
	seen := make(map[string]bool, len(r.ToolTiers))
	for _, rule := range r.ToolTiers {
		// The list is keyed by server-assigned raw names; an entry carrying
		// the applied runtime composition can only be a declaration mistake —
		// it would never match the names the gate recovers for this recipe's
		// tools, silently gating everything the author meant to classify.
		if strings.HasPrefix(rule.Tool, "mcp__") {
			return fmt.Errorf("%w: recipe %q tool tier %q must be the server-assigned raw tool name, not the applied mcp__<server>__<tool> runtime name", ErrInvalid, r.ID, rule.Tool)
		}
		if !isValidParamName(rule.Tool) {
			return fmt.Errorf("%w: recipe %q tool tier %q is not a valid tool name (letters, digits, underscore, hyphen)", ErrInvalid, r.ID, rule.Tool)
		}
		if seen[rule.Tool] {
			return fmt.Errorf("%w: recipe %q tool tier %q is duplicated", ErrInvalid, r.ID, rule.Tool)
		}
		seen[rule.Tool] = true
		if !IsValidRecipeToolTier(rule.Tier) {
			return fmt.Errorf("%w: recipe %q tool tier for %q must be %s or %s", ErrInvalid, r.ID, rule.Tool, RecipeToolTierRead, RecipeToolTierWrite)
		}
	}
	return nil
}

// validateVerbTier checks one http-kind verb's tier (add-integration-authority
// tasks.md 1.1): empty is the fail-safe write default (a recipe release may
// add tiers per verb incrementally), anything present must be in the catalog.
func validateVerbTier(r *Recipe, v *RecipeVerb) error {
	if v.Tier != "" && !IsValidRecipeToolTier(v.Tier) {
		return fmt.Errorf("%w: recipe %q verb %q tier %q must be %s or %s", ErrInvalid, r.ID, v.Tool, v.Tier, RecipeToolTierRead, RecipeToolTierWrite)
	}
	return nil
}

// EffectiveToolTier resolves one connection tool's tier from its recipe
// (add-integration-authority spec: "Verb tier declarations"). toolName is the
// name the gate sees:
//
//   - http kind: the declared verb tool name verbatim (e.g.
//     "figma.get_file" — the change-3 no-rename rule);
//   - mcp kind: the server-assigned raw name (e.g. "create_pull_request") or
//     the applied runtime name of this recipe's materialized server (e.g.
//     "mcp__github__create_pull_request" — the mcp__<server>__<tool>
//     composition whose server segment is the sanitized recipe.Service).
//
// A nil recipe, an unknown tool, or an undeclared name yields
// RecipeToolTierDefault (write) — the fail-safe direction (design.md D5):
// new provider tools never silently become member-accessible. Runtime names
// the sanitizer or collision suffixes have altered beyond recognition
// (over-length hashes, _2 suffixes) match no declared name and default too.
func EffectiveToolTier(r *Recipe, toolName string) string {
	if r == nil || toolName == "" {
		return RecipeToolTierDefault
	}
	if r.Kind == RecipeKindHTTP {
		for i := range r.Verbs {
			if r.Verbs[i].Tool == toolName {
				return validTierOrDefault(r.Verbs[i].Tier)
			}
		}
		return RecipeToolTierDefault
	}
	// mcp kind: recover the raw server-assigned name from the applied runtime
	// name when the tool belongs to this recipe's materialized server.
	raw := toolName
	if prefix := appliedMCPPrefix(r.Service); strings.HasPrefix(toolName, prefix) {
		raw = toolName[len(prefix):]
	}
	for _, rule := range r.ToolTiers {
		if rule.Tool == raw {
			return validTierOrDefault(rule.Tier)
		}
	}
	return RecipeToolTierDefault
}

// ToolTierCounts counts the recipe's declared tool tiers (the RecipeTierCounts
// projection stage B serves per attached connection). Nil or empty recipes
// count zero — an mcp recipe without a tier list genuinely counts zero, since
// nothing is declared (every runtime tool still gates as write).
func ToolTierCounts(r *Recipe) RecipeTierCounts {
	var counts RecipeTierCounts
	if r == nil {
		return counts
	}
	if r.Kind == RecipeKindHTTP {
		for i := range r.Verbs {
			bumpTier(&counts, r.Verbs[i].Tier)
		}
		return counts
	}
	for _, rule := range r.ToolTiers {
		bumpTier(&counts, rule.Tier)
	}
	return counts
}

// bumpTier adds one tool to the counts, an unknown or empty tier counting as
// write (the fail-safe default, applied consistently everywhere).
func bumpTier(counts *RecipeTierCounts, tier string) {
	if tier == RecipeToolTierRead {
		counts.Read++
		return
	}
	counts.Write++
}

// validTierOrDefault maps a declared tier onto the catalog, the empty or
// unknown value falling to the fail-safe default.
func validTierOrDefault(tier string) string {
	if IsValidRecipeToolTier(tier) {
		return tier
	}
	return RecipeToolTierDefault
}

// appliedMCPPrefix builds the runtime tool-name prefix of this recipe's
// materialized server: "mcp__<server>__" with the server display name the
// connections service assigns (recipe.Service) — mcp.Namer sanitizes both
// name segments (lowercase, non-[a-z0-9_-] to '_'), so the mirror here is
// local: domain cannot import the runner, and the composition is frozen by
// the naming contract. A name carried under a different server segment does
// not strip, matches no declared rule, and defaults to write.
func appliedMCPPrefix(service string) string {
	return "mcp__" + sanitizeMCPSegment(service) + "__"
}

// sanitizeMCPSegment mirrors mcp.sanitizeSegment for prefix recovery only:
// lowercase, every character outside [a-z0-9_-] to '_'. An all-invalid
// segment yields underscores, never an empty string — same rule as the
// runner's namer, so the recovered prefix always compares equal.
func sanitizeMCPSegment(seg string) string {
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
