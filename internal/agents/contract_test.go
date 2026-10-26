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
	require.Equal(t, operatingContract, composeSystem("", "", "", ""))
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
	got := composeSystem("PERSONA", "", "", "SUM")
	require.Less(t, strings.Index(got, "PERSONA"), strings.Index(got, "# AGENTS.md"),
		"persona documents come before the contract")
	require.Less(t, strings.Index(got, "# AGENTS.md"), strings.Index(got, "# Earlier conversation summary"),
		"the contract comes before the summary")
	require.Less(t, strings.Index(got, "# Earlier conversation summary"), strings.Index(got, "SUM"),
		"the summary heading comes before the summary body")
}

// TestComposeSystem_KnowledgeGuidanceOrder pins the corpus guidance's
// position (add-knowledge-corpus design D7): after the operating contract,
// before the compaction summary.
func TestComposeSystem_KnowledgeGuidanceOrder(t *testing.T) {
	got := composeSystem("PERSONA", "", knowledgeGuidance, "SUM")
	require.Less(t, strings.Index(got, "PERSONA"), strings.Index(got, "# AGENTS.md"),
		"persona documents come before the contract")
	require.Less(t, strings.Index(got, "# AGENTS.md"), strings.Index(got, "## Knowledge corpus"),
		"the contract comes before the knowledge guidance")
	require.Less(t, strings.Index(got, "## Knowledge corpus"), strings.Index(got, "# Earlier conversation summary"),
		"the knowledge guidance comes before the summary")
	require.Less(t, strings.Index(got, "# Earlier conversation summary"), strings.Index(got, "SUM"),
		"the summary heading comes before the summary body")
}

// TestComposeSystem_KnowledgeGuidanceContent spot-checks the claims the spec
// depends on (add-knowledge-corpus design D7/D10): the mount path and its two
// scopes, the index-first search direction, the directed skill activation,
// and the save path.
func TestComposeSystem_KnowledgeGuidanceContent(t *testing.T) {
	require.Contains(t, knowledgeGuidance, "/knowledge/global")
	require.Contains(t, knowledgeGuidance, "/knowledge/agent")
	require.Contains(t, knowledgeGuidance, "INDEX.md")
	require.Contains(t, knowledgeGuidance, "knowledge-search")
	require.Contains(t, knowledgeGuidance, "save_document")
}

// TestComposeSystem_EmptyGuidanceByteIdentical pins the guidance-free shape
// (add-knowledge-corpus design D7): with no appendix and no guidance the
// composed output is exactly the old persona → contract → summary join, so
// turns without the mount compose byte-identical system context.
func TestComposeSystem_EmptyGuidanceByteIdentical(t *testing.T) {
	want := "PERSONA" + "\n\n" + operatingContract + "\n\n" + "# Earlier conversation summary\n\n" + "SUM"
	require.Equal(t, want, composeSystem("PERSONA", "", "", "SUM"))
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

// TestComposeSystem_PresetAppendixOrder pins the document-preset appendix's
// position (add-create-document design D6): directly after the operating
// contract, before the knowledge guidance and the compaction summary.
func TestComposeSystem_PresetAppendixOrder(t *testing.T) {
	presets := presetAppendix([]string{"Client Report", "Invoice"}, "Client Report")
	got := composeSystem("PERSONA", presets, knowledgeGuidance, "SUM")
	require.Less(t, strings.Index(got, "# AGENTS.md"), strings.Index(got, "## Document presets"),
		"the contract comes before the preset appendix")
	require.Less(t, strings.Index(got, "## Document presets"), strings.Index(got, "## Knowledge corpus"),
		"the preset appendix comes before the knowledge guidance")
	require.Less(t, strings.Index(got, "## Knowledge corpus"), strings.Index(got, "# Earlier conversation summary"),
		"the knowledge guidance comes before the summary")
}

// TestComposeSystem_PresetAppendixContent covers the spec's awareness claims
// (spec document-generation "Preset awareness in turns"): both preset names
// listed and the default marked, so the model can reference presets without
// asking.
func TestComposeSystem_PresetAppendixContent(t *testing.T) {
	got := presetAppendix([]string{"Client Report", "Invoice"}, "Client Report")
	require.Contains(t, got, "## Document presets")
	require.Contains(t, got, "Client Report (default)")
	require.Contains(t, got, "- Invoice")
	require.Contains(t, got, "preset argument", "the appendix names the tool argument to pass the preset in")
}

// TestPresetAppendix_NoDefaultUnmarked covers the degraded default: an
// unreadable or vanished default leaves every name unmarked instead of
// marking a wrong one.
func TestPresetAppendix_NoDefaultUnmarked(t *testing.T) {
	got := presetAppendix([]string{"Client Report", "Invoice"}, "")
	require.Contains(t, got, "- Client Report")
	require.Contains(t, got, "- Invoice")
	require.NotContains(t, got, "(default)")
	// The default matching is case-insensitive, mirroring the preset
	// store's ByName (fake documentpresets.go).
	got = presetAppendix([]string{"Client Report", "Invoice"}, "client report")
	require.Contains(t, got, "Client Report (default)")
}
