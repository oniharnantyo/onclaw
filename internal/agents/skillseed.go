package agents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SkillName is the seeded create-skill skill's directory name
// (add-agent-created-skills tasks 1.2, design D1 — authoring is taught, not
// tooled). It rides the global skills tier like the memory skill: fresh
// per-turn discovery, the standard eligibility gate, explicit $create-skill
// invocation, and workspace-tier shadowing all work unchanged.
const SkillName = "create-skill"

// seedDirPerm and seedFilePerm mirror the memory package's private-to-user
// modes (memory/layout.go).
const (
	seedDirPerm  os.FileMode = 0o700
	seedFilePerm os.FileMode = 0o600
)

// skillContent is the seeded SKILL.md: frontmatter whose description states
// the activation trigger — saving a reusable workflow or a corrected
// procedure — then the authoring playbook: the directory layout, the
// DNS-label naming rule, the frontmatter conventions, when a skill is worth
// saving, a worked example, and the anti-patterns.
const skillContent = `---
name: create-skill
description: Use whenever saving a reusable workflow or a corrected procedure as a skill — the full authoring playbook for writing a valid skill into the workspace skills directory
---

# Create skill

Skills live in this agent's own workspace skills directory under
/workspace/skills. A skill is one directory there — the directory name is
the skill's name — containing a SKILL.md file. Skills are read fresh every
turn, so a valid skill is usable from the next turn on, no restart. Skills
you save are personal to this agent until the user promotes them to the
global library.

## Playbook

1. **Name the skill first.** The directory name must be a DNS label:
   lowercase letters, digits, and hyphens, starting with a letter or digit
   (deploy-runbook works, Deploy Runbook does not). The name is also how the
   skill is invoked later ($name).

2. **Save only what recurs.** A skill earns its place when a multi-step
   workflow has repeated or a correction has produced a procedure that will
   recur. One-off steps do not need a skill, and facts about the user or the
   project belong in memory instead.

3. **Check the global library first.** The skill listings already show the
   library skills this agent includes. Do not author a skill duplicating one
   of them without checking — a same-named personal skill shadows the
   library one, an override to choose deliberately, not to stumble into.

4. **Write SKILL.md with frontmatter.** The frontmatter declares name (equal
   to the directory name) and description. The description is what skill
   listings and later turns show, so state the activation trigger concretely
   — "Use whenever compiling the weekly status report", not "reports". Below
   the frontmatter goes the body: the procedure itself, plain Markdown,
   written to be followed step by step.

5. **Keep references relative.** Supporting files — templates, checklists —
   live beside SKILL.md in the skill's own directory and are referenced by
   relative path. A skill that references absolute machine paths breaks the
   moment anything moves and never survives a promotion to the library.

6. **A minimal skill.** Saving a report procedure creates
   /workspace/skills/weekly-report/SKILL.md:

   ---
   name: weekly-report
   description: Use whenever compiling the weekly status report
   ---

   # Weekly report

   1. Read this week's notes under /workspace/notes.
   2. Draft the report with the write tool and show it to the user for
      review before saving the final copy.

7. **Malformed skills fail quietly.** Discovery skips directories that break
   the conventions without failing the turn — a typo in the frontmatter
   silences the skill instead of raising an error, so follow the shapes
   above exactly.
`

// SeedCreateSkill seeds the create-skill skill into the global skills tier
// when missing (add-agent-created-skills tasks 1.2, mirroring
// memory.SeedSkill): the repair-if-deleted precedent. An existing file is
// never overwritten — a user- or agent-tailored playbook stands — and
// calling again on a seeded layout is a no-op.
func SeedCreateSkill(skillsDir string) error {
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
