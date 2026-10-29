package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// TestComposeSystem_ContractAlone pins the contract-on-every-turn guarantee
// (spec agent-prompts "Contract present without persona documents"): with no
// persona and no summary the system instruction is exactly the contract —
// an agent whose prompt generation failed still gets it.
func TestComposeSystem_ContractAlone(t *testing.T) {
	require.Equal(t, operatingContract, composeSystem("", "", "", "", "", ""))
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
	got := composeSystem("PERSONA", "", "", "", "", "SUM")
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
	got := composeSystem("PERSONA", "", knowledgeGuidance, "", "", "SUM")
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

// TestComposeSystem_SkillsGuidanceOrder pins the skills-authoring guidance's
// position (add-agent-created-skills design D1): after the knowledge
// guidance, before the memory block and the compaction summary.
func TestComposeSystem_SkillsGuidanceOrder(t *testing.T) {
	got := composeSystem("PERSONA", "", knowledgeGuidance, skillsGuidance, memoryGuidance, "SUM")
	require.Less(t, strings.Index(got, "PERSONA"), strings.Index(got, "# AGENTS.md"),
		"persona documents come before the contract")
	require.Less(t, strings.Index(got, "# AGENTS.md"), strings.Index(got, "## Knowledge corpus"),
		"the contract comes before the knowledge guidance")
	require.Less(t, strings.Index(got, "## Knowledge corpus"), strings.Index(got, "## Skills you can save"),
		"the knowledge guidance comes before the skills guidance")
	require.Less(t, strings.Index(got, "## Skills you can save"), strings.Index(got, "## Memory"),
		"the skills guidance comes before the memory block")
	require.Less(t, strings.Index(got, "## Memory"), strings.Index(got, "# Earlier conversation summary"),
		"the memory block comes before the compaction summary")
}

// TestComposeSystem_SkillsGuidanceContent spot-checks the claims the spec
// depends on (add-agent-created-skills design D1, spec agent-skills "The
// authoring contract rides every turn"): the workspace skills directory, the
// per-skill SKILL.md, the DNS-label name rule, the description frontmatter,
// the personal-until-promoted boundary, and the $create-skill activation —
// and no create_skill tool, which does not exist (authoring rides the jailed
// write tools the turn already has).
func TestComposeSystem_SkillsGuidanceContent(t *testing.T) {
	require.Contains(t, skillsGuidance, "/workspace/skills")
	require.Contains(t, skillsGuidance, "SKILL.md")
	require.Contains(t, skillsGuidance, "DNS label")
	require.Contains(t, skillsGuidance, "description")
	require.Contains(t, skillsGuidance, "personal")
	require.Contains(t, skillsGuidance, "$create-skill")
	require.NotContains(t, skillsGuidance, "create_skill",
		"no create_skill tool exists — authoring rides the jailed write tools")
}

// TestComposeSystem_EmptySkillsByteIdentical pins the skills-free shape
// (add-agent-created-skills design D1): with no skills text the composed
// output is exactly the old persona → contract → knowledge → memory →
// summary join, so skill-free turns compose byte-identical system context.
func TestComposeSystem_EmptySkillsByteIdentical(t *testing.T) {
	want := "PERSONA" + "\n\n" + operatingContract + "\n\n" + knowledgeGuidance + "\n\n" + memoryGuidance + "\n\n" + "# Earlier conversation summary\n\n" + "SUM"
	composed := composeSystem("PERSONA", "", knowledgeGuidance, "", memoryGuidance, "SUM")
	require.Equal(t, want, composed)
	require.NotContains(t, composed, "## Skills you can save", "no skills text composes no skills paragraph")
}

// TestTurnInput_SkillsGuidanceOnlyWhenResolved pins the composer threading
// (add-agent-created-skills design D1, the knowledge precedent): the
// skills-authoring guidance rides the assembled instruction exactly when the
// turn's skill backend resolved (tc.skills), and stays absent on a
// knowledge-only turn — the two gates are independent.
func TestTurnInput_SkillsGuidanceOnlyWhenResolved(t *testing.T) {
	stubClock(t, time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC))

	resolved, _, err := assembleTurnInput(persona{}, sessions.Checkpoint{}, nil,
		turnRequest{message: "hi"}, turnComposer{providerType: "gemini", model: "some-model", skills: true})
	require.NoError(t, err)
	require.Contains(t, resolved, "## Skills you can save")
	require.Contains(t, resolved, "$create-skill")

	knowledgeOnly, _, err := assembleTurnInput(persona{}, sessions.Checkpoint{}, nil,
		turnRequest{message: "hi"}, turnComposer{providerType: "gemini", model: "some-model", knowledge: true})
	require.NoError(t, err)
	require.Contains(t, knowledgeOnly, "## Knowledge corpus", "the knowledge-only turn keeps its own guidance")
	require.NotContains(t, knowledgeOnly, "## Skills you can save")
}

// TestComposeSystem_EmptyGuidanceByteIdentical pins the guidance-free shape
// (add-knowledge-corpus design D7): with no appendix and no guidance the
// composed output is exactly the old persona → contract → summary join, so
// turns without the mount compose byte-identical system context.
func TestComposeSystem_EmptyGuidanceByteIdentical(t *testing.T) {
	want := "PERSONA" + "\n\n" + operatingContract + "\n\n" + "# Earlier conversation summary\n\n" + "SUM"
	require.Equal(t, want, composeSystem("PERSONA", "", "", "", "", "SUM"))
}

// TestOperatingContract_ContentSpotChecks spot-checks behavior-bearing lines
// (design D6/D7): the denial semantics, hook feedback, and parallel tool
// calls are claims the specs depend on — prose drift on them gets caught.
func TestOperatingContract_ContentSpotChecks(t *testing.T) {
	for _, fragment := range []string{
		"A denial is a decision",  // approval-gate semantics (design D7)
		"Hook output is feedback", // hook output is feedback, not a tool result
		"run in parallel",         // independent tool calls may run in parallel
		"under artifacts/",        // deliverable files go in artifacts/ (add-turn-artifacts D9)
		"shown to the user",       // artifacts made during the turn surface with the reply
	} {
		require.Contains(t, operatingContract, fragment)
	}
}

// TestComposeSystem_PresetAppendixOrder pins the document-preset appendix's
// position (add-create-document design D6): directly after the operating
// contract, before the knowledge guidance and the compaction summary.
func TestComposeSystem_PresetAppendixOrder(t *testing.T) {
	presets := presetAppendix([]presetFormatSummary{
		{Label: "PDF", Names: []string{"Client Report", "Invoice"}, Default: "Client Report"},
	})
	got := composeSystem("PERSONA", presets, knowledgeGuidance, "", "", "SUM")
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
	got := presetAppendix([]presetFormatSummary{
		{Label: "PDF", Names: []string{"Client Report", "Invoice"}, Default: "Client Report"},
	})
	require.Contains(t, got, "## Document presets")
	require.Contains(t, got, "- PDF presets: Client Report (default), Invoice")
	require.Contains(t, got, "preset argument", "the appendix names the tool argument to pass the preset in")
}

// TestPresetAppendix_PerFormat covers the per-format appendix (design D6):
// each format's presets render as their own line with that format's default
// marked, formats without presets are skipped, and the closing line points
// at the format's default.
func TestPresetAppendix_PerFormat(t *testing.T) {
	got := presetAppendix([]presetFormatSummary{
		{Label: "PDF", Names: []string{"Client Report", "Invoice"}, Default: "Invoice"},
		{Label: "DOCX", Names: []string{"Branded Letter"}, Default: "Branded Letter"},
	})
	require.Contains(t, got, "- PDF presets: Client Report, Invoice (default)")
	require.Contains(t, got, "- DOCX presets: Branded Letter (default)")
	require.NotContains(t, got, "PPTX", "a format without presets composes nothing")
	require.Contains(t, got, "omit the argument to use the format's default")

	// The default matching is case-insensitive, mirroring the preset
	// store's ByName.
	got = presetAppendix([]presetFormatSummary{
		{Label: "PDF", Names: []string{"Client Report"}, Default: "client report"},
	})
	require.Contains(t, got, "Client Report (default)")
}

// TestPresetAppendix_NoDefaultUnmarked covers the degraded default: an
// unreadable or vanished default leaves every name unmarked instead of
// marking a wrong one.
func TestPresetAppendix_NoDefaultUnmarked(t *testing.T) {
	got := presetAppendix([]presetFormatSummary{
		{Label: "PDF", Names: []string{"Client Report", "Invoice"}},
	})
	require.Contains(t, got, "- PDF presets: Client Report, Invoice")
	require.NotContains(t, got, "(default)")
}
