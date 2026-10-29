package documents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// filePerm and dirPerm mirror the app layout's private-to-user files.
	filePerm os.FileMode = 0o600
	dirPerm  os.FileMode = 0o700
)

// SkillName is the seeded document-playbook skill's directory name (design
// D10). It rides the global skills tier: fresh-per-turn discovery, the
// standard eligibility gate, explicit $documents invocation, and
// workspace-tier shadowing all work unchanged.
const SkillName = "documents"

// skillContent is the seeded SKILL.md: frontmatter whose description states
// the directed activation trigger, then the document playbook — view before
// edit, work on workspace files, targeted operations over regeneration,
// presets for generation, and PDFs by regeneration only.
const skillContent = `---
name: documents
description: Use whenever a request asks to read, revise, fill, or produce office documents (Word, Excel, PowerPoint) or to manipulate PDFs
---

# Documents

Office documents are edited in place with targeted operations, not
regenerated: an edit touches only what it addresses and every other
paragraph, cell, slide, style, and embedded object survives untouched.

## Playbook

1. **View a document before editing it.** Open it with read_file first —
   DOCX and PPTX render as structured text views, XLSX as a cell grid — and
   note the addresses it prints (¶n for paragraphs, slide n, shape m for
   shapes, Sheet!Cell for cells). You target those addresses in edits.

2. **Edit documents that live in your workspace.** The edit tool only
   reaches workspace files. Chat attachments are mirrored into your
   workspace at /workspace/uploads when their turn runs — read them there
   with read_file. Knowledge documents are read-only: to revise one,
   read it for content and generate a fresh document with create_document
   rather than editing it where it lives.

3. **Edit with targeted operations, not regeneration.** When fidelity
   matters — a document someone else authored, a template, anything with
   styling or embedded objects — apply one operation per change
   (replace_paragraph, fill_placeholders, set_cell, replace_shape_text, and
   their siblings) instead of recreating the file. Regenerating a whole
   document loses everything you did not reproduce.

4. **Reference the user's presets when generating.** When a request calls
   for a NEW document rather than a revision, produce it with create_document;
   its presets carry the user's saved formats and templates, so prefer them
   over inventing styling.

5. **Route PDF text changes through regeneration.** The edit tool does
   structural work only on PDFs (merge, split, rotate, stamp, form fill). If
   asked to change a PDF's wording, regenerate the document with
   create_document instead of trying to rewrite text in place.
`

// SeedDocumentsSkill seeds the documents skill into the global skills tier
// when missing (design D10): the repair-if-deleted precedent. An existing
// file is never overwritten — a user- or agent-tailored playbook stands —
// and calling again on a seeded layout is a no-op.
func SeedDocumentsSkill(skillsDir string) error {
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
