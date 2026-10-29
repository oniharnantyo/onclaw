package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SkillName is the seeded memory skill's directory name (add-agent-memory
// tasks 3.1, spec agent-memory "Memory skill"). It rides the global skills
// tier: fresh-per-turn discovery, the standard eligibility gate, explicit
// $memory invocation, and workspace-tier shadowing all work unchanged.
const SkillName = "memory"

// skillContent is the seeded SKILL.md: frontmatter whose description states
// the directed activation trigger, then the read-first recall playbook, the
// write conventions (the frontmatter schema, supersede rather than
// contradict, daily notes for working observations), and query refinement —
// mirroring the knowledge-search skill's shape.
const skillContent = `---
name: memory
description: Use whenever a request concerns the user's preferences, past decisions, or earlier work with this agent — anything recalled from memory rather than learned this session
---

# Memory

You keep long-term memory as plain Markdown under /workspace/memory.
MEMORY.md is the curated index — one line per active note — fact notes are
atomic files written to exactly /workspace/memory/<name>.md (one level deep,
never memory/memory/), /workspace/memory/YYYY-MM-DD.md is the day's working
note for observations not yet durable enough for a fact note, and
/workspace/memory/archive/ holds notes that fell out of use (moved, never
deleted — restoring one is just moving it back).

## Playbook

1. **Read the index first.** Read /workspace/memory/MEMORY.md before
   searching. A note's index line (its name and description) often answers
   the question outright or names the note to open.

2. **Search with memory_search.** For anything the index does not settle,
   run memory_search with the request's distinctive terms — names, error
   strings, identifiers, and plain phrases from the question.

3. **Read the matching note.** Open the best-scoring note with the read
   tool and read enough of it to answer, not just the matching line. Trust
   the note over your own guess: it is what survived earlier sessions.

4. **Refine when the first query misses.** Broaden with synonyms and related
   terms, then narrow around the best hits. Notes carry a concepts field —
   search its terms too, and retry with words from the descriptions the
   index shows. If nothing relevant exists, say so plainly instead of
   guessing.

5. **Save what deserves to outlive the turn.** Durable facts about the user
   and the project, stated preferences, decisions reached, and corrections
   the user makes each become one fact note with the write or edit tool.
   Its frontmatter carries name (equal to the filename without .md),
   description (one line), and type (one of user, feedback, project,
   reference), plus optional concepts and relations.

6. **Supersede, never contradict.** To replace an outdated note, write a
   new note whose supersedes field names the old one — never edit the old
   note into a contradiction.

7. **Working observations go in the daily note.** Anything not yet durable
   enough for a fact note — what was tried, what failed, what was decided
   today — belongs in memory/YYYY-MM-DD.md. The consolidation sweep
   promotes recurring lines into the index on its own.
`

// SeedSkill seeds the memory skill into the global skills tier when missing
// (add-agent-memory tasks 3.1, mirroring the knowledge-search seed): the
// repair-if-deleted precedent. An existing file is never overwritten — a
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
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("seed %s skill: %w", SkillName, err)
	}
	if err := os.WriteFile(path, []byte(skillContent), filePerm); err != nil {
		return fmt.Errorf("seed %s skill: %w", SkillName, err)
	}
	return nil
}
