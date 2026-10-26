package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SkillName is the seeded corpus-exploration skill's directory name (design
// D10). It rides the global skills tier: fresh-per-turn discovery, the
// standard eligibility gate, explicit $knowledge-search invocation, and
// workspace-tier shadowing all work unchanged.
const SkillName = "knowledge-search"

// skillContent is the seeded SKILL.md: frontmatter whose description states
// the directed activation trigger, then the DCI playbook (D10) — indexes
// first, lexical anchors, read around hits, PDFs by page range, refine, and
// when to persist findings.
const skillContent = `---
name: knowledge-search
description: Use whenever the user asks about, quotes, or references saved documents or the knowledge corpus, or when a question might be answerable from them
---

# Knowledge search

Saved documents live in a read-only knowledge corpus mounted at /knowledge.
The global scope (every agent) is under /knowledge/global/; this agent's own
scope is under /knowledge/agent/. INDEX.md in each scope lists every document
with its description and size — it is the discovery handle, especially for
PDFs, whose contents grep cannot see.

## Playbook

1. **Read the scope indexes first.** Read /knowledge/global/INDEX.md and
   /knowledge/agent/INDEX.md before searching. A filename or description
   often answers the question outright or names the file to open.

2. **Grep for lexical anchors.** Search the corpus with grep -i using quoted
   phrases from the request, identifiers, error strings, names, and
   synonyms — one anchor at a time, across /knowledge/global/ and
   /knowledge/agent/.

3. **Read around the hits.** Open the matching files with read_file and read
   enough surrounding context to answer, not just the matching line.

4. **Read PDFs by page range.** PDFs cannot be grepped — locate them by
   filename or description (step 1), then read_file them page by page or in
   page ranges until the relevant section is found.

5. **Refine when the first query misses.** Broaden with synonyms and related
   terms, then narrow around the best hits. If the indexes show nothing
   relevant, say so plainly instead of guessing.

6. **Persist what is worth keeping.** When a finding deserves to outlive the
   turn, save it to the corpus with the save_document tool (a filename,
   description, and content), so the next search finds it.
`

// SeedSkill seeds the knowledge-search skill into the global skills tier when
// missing (design D10): the repair-if-deleted precedent. An existing file is
// never overwritten — a user- or agent-tailored playbook stands — and calling
// again on a seeded layout is a no-op.
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
