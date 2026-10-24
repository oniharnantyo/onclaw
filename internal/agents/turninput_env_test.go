package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// stubClock pins the package clock for one test and restores it on cleanup
// (the manager_impl.go seam the turn's date reads through).
func stubClock(t *testing.T, at time.Time) {
	t.Helper()
	restore := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = restore })
}

// TestEnvironmentBlock_ChatOriginPresent covers the spec's chat-origin
// scenario (spec agent-prompts "Turn environment block"): a user turn — an
// explicit user origin or the empty origin, which reads as the user — names
// the user origin and states that the user is present.
func TestEnvironmentBlock_ChatOriginPresent(t *testing.T) {
	for _, origin := range []string{"", hooks.OriginUser} {
		block := environmentBlock("gemini", "some-model", origin, time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC))
		require.Contains(t, block, "Origin: "+hooks.OriginUser, "the block names the user origin")
		require.Contains(t, block, "the user is present", "the block states the user is present")
		require.NotContains(t, block, "not present", "the user-origin block never denies presence")
	}
}

// TestEnvironmentBlock_AutonomousOriginAbsent covers the spec's autonomous
// origin scenario: a scheduler origin — and any non-user trigger surface,
// present or future — names that origin and states that the user is not
// present, so the model picks the autonomous-run mode.
func TestEnvironmentBlock_AutonomousOriginAbsent(t *testing.T) {
	for _, origin := range []string{hooks.OriginScheduler, "webhook", "channel"} {
		block := environmentBlock("gemini", "some-model", origin, time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC))
		require.Contains(t, block, "Origin: "+origin, "the block names the turn's origin")
		require.Contains(t, block, "the user is not present", "the block states the user is absent")
	}
}

// TestTurnInput_EnvironmentDateCovers the spec's current-date scenario
// through the real seam: the composed instruction states the clock's date,
// formatted YYYY-MM-DD.
func TestTurnInput_EnvironmentDate(t *testing.T) {
	stubClock(t, time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC))

	instruction, _, err := assembleTurnInput(persona{}, sessions.Checkpoint{}, nil,
		turnRequest{message: "hi"}, turnComposer{providerType: "gemini", model: "some-model"})
	require.NoError(t, err)
	require.Contains(t, instruction, "Current date: 2026-10-24")
}

// TestTurnInput_EnvironmentProviderModel asserts the block states the
// effective provider and model the turn composes with.
func TestTurnInput_EnvironmentProviderModel(t *testing.T) {
	stubClock(t, time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC))

	instruction, _, err := assembleTurnInput(persona{}, sessions.Checkpoint{}, nil,
		turnRequest{message: "hi"}, turnComposer{providerType: "anthropic", model: "claude-opus-4-1"})
	require.NoError(t, err)
	require.Contains(t, instruction, "Provider/model: anthropic / claude-opus-4-1")
}

// TestTurnInput_CompositionOrder pins the complete composed instruction's
// order (design D2/D4): persona documents, then the operating contract, then
// the compaction summary, then the environment block — the last position
// before the fresh user input.
func TestTurnInput_CompositionOrder(t *testing.T) {
	stubClock(t, time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC))

	instruction, _, err := assembleTurnInput(
		persona{identity: "PERSONA"}, sessions.Checkpoint{Summary: "SUMMARY BODY"}, nil,
		turnRequest{message: "hi", origin: hooks.OriginScheduler},
		turnComposer{providerType: "gemini", model: "some-model"})
	require.NoError(t, err)
	require.Less(t, strings.Index(instruction, "PERSONA"), strings.Index(instruction, "# AGENTS.md"),
		"persona documents come before the contract")
	require.Less(t, strings.Index(instruction, "# AGENTS.md"), strings.Index(instruction, "# Earlier conversation summary"),
		"the contract comes before the summary")
	require.Less(t, strings.Index(instruction, "# Earlier conversation summary"), strings.Index(instruction, "# Environment"),
		"the summary comes before the environment block")
}

// TestEnvironmentBlock_NoMountGuidance pins design D4's exclusion: the
// environment block never restates the workspace mount or jail — the
// filesystem middleware appends its own guidance, and duplication would
// invite drift between two sources of the same fact.
func TestEnvironmentBlock_NoMountGuidance(t *testing.T) {
	block := environmentBlock("gemini", "some-model", hooks.OriginScheduler, time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC))
	for _, fragment := range []string{"mount", "workspace", "jail"} {
		require.NotContains(t, strings.ToLower(block), fragment)
	}
}
