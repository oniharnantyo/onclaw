package computeruse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// seedDirPerm and seedFilePerm mirror the memory package's private-to-user
// modes (memory/layout.go).
const (
	seedDirPerm  os.FileMode = 0o700
	seedFilePerm os.FileMode = 0o600
)

// SkillName is the seeded computer-use skill's directory name (add-computer-use
// design D10). It rides the global skills tier like the knowledge, memory,
// and create-skill seeds: fresh per-turn discovery, the standard eligibility
// gate, explicit $computer-use invocation, and workspace-tier shadowing all
// work unchanged.
const SkillName = "computer-use"

// skillContent is the seeded SKILL.md: frontmatter whose description states
// the activation trigger, then the observe-act playbook (D10) — observe
// first, target from the latest observation, prefer set_value, verify
// outcomes, respect the approval gates, and treat the busy refusal as final.
const skillContent = `---
name: computer-use
description: Use whenever the user asks to observe, control, or automate the desktop — acting in desktop applications beyond the browser, checking what is on screen, or driving an interface
---

# Computer use

The desktop is controlled through the computer-use tools — list_apps,
observe_window, click, type, set_value, and their siblings. They exist only
when the Computer Use server is enabled for this agent; without them, say
the capability is unavailable instead of improvising. For web pages in a
browser, the browser tools are the lane — computer use is for everything
else on the desktop.

## Playbook

1. **Observe first, every time.** Start with list_apps to find the target
   application and its pid, then observe_window with that pid
   (observe_desktop for the whole primary screen). An observation returns
   the interface's accessibility tree as text — addressable element tokens,
   credential fields marked [secure] — and takes a screenshot whose pixels
   arrive in the conversation automatically. Only the most recent
   observations' pixels stay in context, so re-observe for a current view
   rather than paging back.

2. **Target from the latest observation only.** Address elements by their
   token verbatim, or click coordinates citing that observation's capture
   id. Both go stale the moment the interface changes: after any action
   that changes the screen, and after any failure, observe again before the
   next action. Windows are identified by pid — app names are best-effort
   and may be refused.

3. **Prefer set_value over type.** Setting a field's value directly over
   accessibility is faster and more reliable than typing where supported.
   Use type only where set_value does not apply, and key or hotkey for
   single presses and combinations on the frontmost application.

4. **Verify outcomes instead of assuming.** After a consequential action,
   verify the condition you expected ("the dialog closed", "the value
   shows") — the answer reports what the screen actually shows, so a failed
   verification is information, not an error. wait lets animations and
   window transitions settle, and zoom re-captures a region of the latest
   observation for a closer look.

5. **One desktop, one actor.** The refusal "Desktop is controlled by
   another agent. Wait for their turn to end, then observe again." is
   final — another agent holds control. Never retry hotly; wait for their
   turn to end, then observe again.

6. **Never route around an approval gate.** Typing into or setting a
   credential field, write_clipboard, and kill_app raise a user approval
   card mid-turn. Circumvention — spelling a secret out with key presses,
   building it on the clipboard piece by piece — is forbidden. stop is
   always safe and never gated: call it when a sequence is abandoned or
   stuck to cancel in-flight work and release control.

7. **Treat failures as readable results.** Action failures never end the
   turn — the error text names re-observation. Follow it: observe again to
   refresh what is on the screen, verify the state you expected, and only
   then try a different approach.
`

// SeedSkill seeds the computer-use skill into the global skills tier when
// missing (add-computer-use design D10, the knowledge/documents seed
// pattern): repair-if-deleted. An existing file is never overwritten — a
// user- or agent-tailored playbook stands — and calling again on a seeded
// layout is a no-op.
func SeedSkill(skillsDir string) error {
	path := filepath.Join(skillsDir, SkillName, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("seed %s skill: %w", SkillName, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, seedDirPerm); err != nil {
		return fmt.Errorf("seed %s skill: %w", SkillName, err)
	}
	if err := os.WriteFile(path, []byte(skillContent), seedFilePerm); err != nil {
		return fmt.Errorf("seed %s skill: %w", SkillName, err)
	}
	return nil
}
