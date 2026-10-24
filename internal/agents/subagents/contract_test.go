package subagents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantChildNotes is the reduced child variant verbatim from design.md's
// appendix block "The reduced child variant (D5)", against which the constant
// is pinned (design D6: contract text pinned by tests).
const wantChildNotes = `# Operating notes

- Return your findings in your final reply. Do not write report, summary,
  or notes files — the parent agent reads your reply, not your files.
- Use paths under your mounted workspace; a working directory does not
  carry over between shell calls.`

// TestChildInstruction_ComposesPersonaThenNotes pins the composition New()
// feeds the child: the caller's persona text first, the reduced operating
// notes after (spec agent-prompts "Child turn carries the reduced notes").
func TestChildInstruction_ComposesPersonaThenNotes(t *testing.T) {
	out := childInstruction("You research wildfire reports.")
	require.Contains(t, out, "You research wildfire reports.")
	require.Contains(t, out, "# Operating notes")
	require.Less(t, strings.Index(out, "You research wildfire reports."),
		strings.Index(out, "# Operating notes"),
		"persona precedes the operating notes")
	assert.Contains(t, out, childOperatingNotes)
}

// TestChildInstruction_EmptyPersonaYieldsNotesAlone pins the empty-persona
// case: the notes alone, exactly.
func TestChildInstruction_EmptyPersonaYieldsNotesAlone(t *testing.T) {
	require.Equal(t, wantChildNotes, childInstruction(""))
}

// TestChildInstruction_ExcludesOperatingModes pins spec agent-prompts "Child
// does not inherit operating modes": the full contract's user-facing mode
// guidance never reaches the child.
func TestChildInstruction_ExcludesOperatingModes(t *testing.T) {
	out := childInstruction("persona")
	assert.NotContains(t, out, "Operating modes")
	assert.NotContains(t, out, "Live chat")
	assert.NotContains(t, out, "Autonomous run")
}

// TestChildOperatingNotes_PinsBehaviors pins the constant to the design text
// and to its two behaviors: findings in the final reply rather than report
// files, and workspace-safe paths under the mount.
func TestChildOperatingNotes_PinsBehaviors(t *testing.T) {
	require.Equal(t, wantChildNotes, childOperatingNotes)

	assert.Contains(t, childOperatingNotes, "final reply")
	assert.Contains(t, childOperatingNotes, "Do not write report")
	assert.Contains(t, childOperatingNotes, "mounted workspace")
}
