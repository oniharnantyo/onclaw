package agents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestComposeSystem_ContractAlone pins the contract-on-every-turn guarantee
// (spec agent-prompts "Contract present without persona documents"): with no
// persona and no summary the system instruction is exactly the contract —
// an agent whose prompt generation failed still gets it.
func TestComposeSystem_ContractAlone(t *testing.T) {
	require.Equal(t, operatingContract, composeSystem("", ""))
}

// TestOperatingContract_SectionsOrder pins the contract's section set and
// order (design D6): a refactor that silently drops or reorders a section
// fails here.
func TestOperatingContract_SectionsOrder(t *testing.T) {
	sections := []string{
		"## Harness",
		"## Communicating",
		"## Working in the workspace",
		"## Operating modes",
		"## Context",
	}
	prev := -1
	for _, section := range sections {
		idx := strings.Index(operatingContract, section)
		require.Greaterf(t, idx, prev, "section %q missing or out of order", section)
		prev = idx
	}
}

// TestOperatingContract_Precedence pins the precedence statement (spec
// agent-prompts "Contract states precedence"): the contract states that it
// takes precedence over the persona documents.
func TestOperatingContract_Precedence(t *testing.T) {
	require.Contains(t, operatingContract, "take precedence over")
	require.Contains(t, operatingContract, "persona documents")
}

// TestComposeSystem_CompositionOrder pins the composed instruction's order
// (design D2): persona documents, then the operating contract, then the
// compaction summary — recency favors the rules over persona drift.
func TestComposeSystem_CompositionOrder(t *testing.T) {
	got := composeSystem("PERSONA", "SUM")
	require.Less(t, strings.Index(got, "PERSONA"), strings.Index(got, "# AGENTS.md"),
		"persona documents come before the contract")
	require.Less(t, strings.Index(got, "# AGENTS.md"), strings.Index(got, "# Earlier conversation summary"),
		"the contract comes before the summary")
	require.Less(t, strings.Index(got, "# Earlier conversation summary"), strings.Index(got, "SUM"),
		"the summary heading comes before the summary body")
}

// TestOperatingContract_ContentSpotChecks spot-checks behavior-bearing lines
// (design D6/D7): the denial semantics, hook feedback, and parallel tool
// calls are claims the specs depend on — prose drift on them gets caught.
func TestOperatingContract_ContentSpotChecks(t *testing.T) {
	for _, fragment := range []string{
		"A denial is a decision",  // approval-gate semantics (design D7)
		"Hook output is feedback", // hook output is feedback, not a tool result
		"run in parallel",         // independent tool calls may run in parallel
	} {
		require.Contains(t, operatingContract, fragment)
	}
}
