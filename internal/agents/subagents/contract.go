package subagents

// childOperatingNotes is the reduced operating contract composed into every
// subagent child turn (design D5, spec agent-prompts "Subagent child operating
// notes"). A jailed child cannot talk to the user, so the full operating
// contract's user-facing mode guidance is meaningless here: the notes carry
// only what a child worker needs — findings return in the final reply instead
// of report files, and paths stay under the mounted workspace. The text is
// copied verbatim from design.md's appendix block "The reduced child variant
// (D5)".
const childOperatingNotes = `# Operating notes

- Return your findings in your final reply. Do not write report, summary,
  or notes files — the parent agent reads your reply, not your files.
- Use paths under your mounted workspace; a working directory does not
  carry over between shell calls.`

// childInstruction composes the child agent's system instruction: the caller's
// persona text (which may be empty) first, then the reduced operating notes.
func childInstruction(persona string) string {
	if persona == "" {
		return childOperatingNotes
	}
	return persona + "\n\n" + childOperatingNotes
}
