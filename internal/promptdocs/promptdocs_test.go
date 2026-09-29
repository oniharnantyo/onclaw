package promptdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeedWorkspaceCreatesDirWithoutBasePrompt pins the seeding contract
// after markdown-card-elements D8: the directory is created, but the L1 base
// prompt is never materialized — the composer injects promptdocs.BasePrompt
// per build.
func TestSeedWorkspaceCreatesDirWithoutBasePrompt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents", "atlas")

	if err := SeedWorkspace(dir); err != nil {
		t.Fatalf("SeedWorkspace() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat agent workspace dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("SeedWorkspace must create the agent workspace directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("SeedWorkspace must not write AGENTS.md, stat err: %v", err)
	}
}

// TestSeedWorkspaceClearsGeneratedDocumentsOnly pins the rest of the seeding
// contract: generated documents (and their backups) from a previous agent in
// the same slug-derived directory are cleared, while every other file —
// including a stray AGENTS.md, which is the startup sweep's job
// (markdown-card-elements D8) — is untouched.
func TestSeedWorkspaceClearsGeneratedDocumentsOnly(t *testing.T) {
	dir := t.TempDir()
	seeds := map[string]string{
		"IDENTITY.md":      "old identity",
		"SOUL.md":          "old soul",
		"BOOTSTRAP.md":     "old bootstrap",
		"IDENTITY.md.bak":  "older identity",
		"SOUL.md.bak":      "older soul",
		"BOOTSTRAP.md.bak": "older bootstrap",
		"AGENTS.md":        "stray seeded base prompt",
		"HEARTBEAT.md":     "checklist",
		"skills/README.md": "workspace skills",
	}
	for name, content := range seeds {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if err := SeedWorkspace(dir); err != nil {
		t.Fatalf("SeedWorkspace() error = %v", err)
	}

	for _, name := range []string{
		"IDENTITY.md", "SOUL.md", "BOOTSTRAP.md",
		"IDENTITY.md.bak", "SOUL.md.bak", "BOOTSTRAP.md.bak",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s must be cleared by SeedWorkspace, stat err: %v", name, err)
		}
	}
	for _, name := range []string{"AGENTS.md", "HEARTBEAT.md", "skills/README.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s after SeedWorkspace: %v", name, err)
		}
		if string(got) != seeds[name] {
			t.Errorf("%s = %q, want untouched %q", name, string(got), seeds[name])
		}
	}
}

// TestBasePromptCarriesRichCards pins the rich-cards fence conventions in the
// embedded L1 template (markdown-card-elements D7; enhance-agent-base-prompt
// 1.6-1.8): the section heading, the transport rules the renderer relies on,
// the 13-tag end-state catalogue (no table, no math — remove-markdown-
// redundant-fences deleted them web-side) with per-tag purpose/trigger
// guidance, the exception-tag examples, and the chooser/anti-pattern lines.
func TestBasePromptCarriesRichCards(t *testing.T) {
	if strings.TrimSpace(BasePrompt) == "" {
		t.Fatal("BasePrompt = empty, want embedded template")
	}
	for _, marker := range []string{
		"## Rich cards",
		"only when a visual beats prose",
		"shows as plain code",
		// The taught structure (live-pass fix 2026-09-21: a shape catalog of
		// inline ```tag {json} examples taught models to write the JSON on the
		// tag line, where markdown reads it as info-string meta, not body —
		// the template now shows the multiline exemplar and lists shapes as
		// bare tag bullets, and pins the never-inline rule).
		"Never put the JSON on the same line as the tag",
		"```chart\n{\"",
		"- chart: {\"",
		"- timeline: {\"",
		"- preview: {\"",
		"- ticker: {\"",
		"- activity: {\"",
		"- spec: {\"",
		"- compare: {\"",
		"- progress: {\"",
		"- score: {\"",
		"- flow: {\"",
		// Markdown-first clause (enhance-agent-base-prompt 1.6): tabular data
		// needs no fence — native markdown tables render in chat.
		"tabular data needs no fence",
		// The raw-source exception tags, each with a complete example fence
		// (enhance-agent-base-prompt 1.7) and the diagram missing-title
		// silent-degradation warning.
		"raw mermaid source, not JSON",
		"```diagram Payment flow\n",
		"```mermaid\n",
		"silently degrades to a plain code block",
		// Cross-tag chooser and anti-pattern lines (enhance-agent-base-prompt 1.8).
		"Chooser:",
		"A diagram is not a substitute for the answer text",
		// The `ui` composition tag (generative-ui adoption): the tree shape, the
		// sparing-use rule, and the two numeric/enum traps the validator guards
		// (0-8 tokens, the closed icon set) are all taught explicitly.
		"### Composing several cards: the `ui` tag",
		"```ui\n{\"$type\"",
		"ONLY when the arrangement carries meaning",
		"never wrap a single card in `ui`",
		"`gap` and `padding` are 0-8",
		"`Icon.name` must be one of:",
		// The Chart variant enum must match the schema's zod enum exactly
		// (live-pass fix 2026-09-22: the doc taught "bars" — with an s — which
		// the schema rejects, so every doc-following Chart node silently
		// dropped under interior-tolerant semantics).
		`Chart (variant "bar"|"line"|"area"|"sparkline"`,
		// Select/RadioGroup options are {value,label} objects, not strings —
		// string options fail safeParse and the node silently drops (D2).
		"Select (label?, options [{value, label}])",
	} {
		if !strings.Contains(BasePrompt, marker) {
			t.Errorf("BasePrompt missing rich-cards marker %q", marker)
		}
	}
	// The superseded Chart enum must not resurface (schema enum is bar/line/
	// area/sparkline — "bars" fails every safeParse).
	if strings.Contains(BasePrompt, `"line"|"area"|"bars"`) {
		t.Errorf(`BasePrompt teaches the rejected Chart variant "bars" (schema enum is "bar"|"line"|"area"|"sparkline")`)
	}
	// The deleted fence tags must not resurface in the catalogue
	// (remove-markdown-redundant-fences deleted table/math web-side; the
	// prompt ships the 13-tag end state — markdown tables need no fence and
	// KaTeX renders natively). The ui section's Table/Chart vocabulary nodes
	// are component names, not fence tags, so the pinned catalogue-line
	// shapes are the discriminating markers.
	for _, removed := range []string{
		"- table: {\"",
		"- math: {\"",
		`"expression": "<LaTeX string>"`,
	} {
		if strings.Contains(BasePrompt, removed) {
			t.Errorf("BasePrompt still teaches the deleted %q fence tag", removed)
		}
	}
}

// TestBasePromptCarriesReferenceDocuments pins the reference-documents block
// in the embedded L1 template (add-reference-documents 7.1-7.2): the
// read-only `references/` library and its run-visible manifest, the
// never-shell guardrail (fix-reference-document-retrieval D5), the
// discover/search/scoped-read loop over the document tools, the
// document+locator citation rule with the mount-path link, and the subagent
// delegation hint for heavy multi-document research.
func TestBasePromptCarriesReferenceDocuments(t *testing.T) {
	for _, marker := range []string{
		"## Reference documents",
		// The library: persistent, read-only mount, run-visible documents
		// listed in the injected manifest.
		"mounted read-only at `references/`",
		"reference-documents manifest",
		// The guardrail: documents are already mounted and served by the
		// document tools — no find/grep/cat hunts through the shell; the
		// file tools may confirm the mount, search/read stay with the
		// document tools.
		"and served by `document.search`/`document.read`",
		"never hunt for these documents through the shell",
		"(`find`/`grep`/`cat`)",
		"but only the document tools search and read it",
		// The usage loop: manifest discovery, keyword search, scoped reads
		// (pages for PDF ranges, section for heading/slide/sheet), full reads
		// on any document path.
		"`document.search`",
		"`pages` for a PDF page range",
		"`section` for a heading, slide, or sheet title",
		"full `document.read` also works on any document path",
		// Citations: document AND locator, linked mount path.
		"name the document AND its locator",
		"(`references/manual.pdf`)",
		// Subagent delegation hint for heavy multi-document research.
		"delegate via the `agent` tool",
		"explicit citation requirements",
	} {
		if !strings.Contains(BasePrompt, marker) {
			t.Errorf("BasePrompt missing reference-documents marker %q", marker)
		}
	}
}

// TestBasePromptCarriesBehavioralSections pins the reframed directives and the
// four peer-grade teaching sections (enhance-agent-base-prompt 1.1-1.5, spec
// agent-prompts "Behavioral sections present"): Core Directives 4→3 with the
// verbatim boundary/persona content, plus Execution, Finishing & Honesty,
// Follow-through, and Communication & Output.
func TestBasePromptCarriesBehavioralSections(t *testing.T) {
	for _, marker := range []string{
		// Reframed directives (D4): standalone sections, boundary and persona
		// content verbatim, tool-usage prose folded into Capability Scope.
		"## Workspace & Tenant Boundaries",
		"You operate strictly within the context of your designated workspace.",
		"## Persona & Alignment",
		"prioritize safety, workspace policy, and core directives over persona styling",
		"## Capability Scope",
		"Use only the tools, skills, and MCP capabilities assigned to you",
		// Execution: act now, batching, prerequisites, live-check,
		// vary-then-conclude, persistence.
		"## Execution",
		"no pre-refusal",
		"policy gates and approvals own the risk",
		"Batch independent tool calls into one turn",
		"resolve prerequisite steps first",
		"Live-check mutable facts",
		"vary the query, path, or source before concluding",
		"continue to done or a real blocker",
		// Finishing & Honesty: real artifact, verification, read-back,
		// never fabricate, literal preservation, missing-context ladder.
		"## Finishing & Honesty",
		"a real result backed by tool output, not a description",
		"read back state-changing external writes",
		"Never fabricate data, file contents, or API responses",
		"Preserve identifiers and values exactly as given",
		"ask only when irretrievable",
		// Follow-through: progress ≠ answer, ownership, schedule over polling.
		"## Follow-through",
		"A progress statement is not an answer",
		"preferring the `schedule` tool over polling or waiting",
		"progress is not completion",
		// Communication & Output: sizing, no filler, earned depth, three-way
		// distinction.
		"## Communication & Output",
		"Reply length matches the weight of the ask",
		"never restate the request or narrate visible tool calls",
		"distinguish between confirmed facts, tool outputs, and model reasoning",
	} {
		if !strings.Contains(BasePrompt, marker) {
			t.Errorf("BasePrompt missing behavioral marker %q", marker)
		}
	}
	// The old four-directive wrapper must not resurface.
	if strings.Contains(BasePrompt, "## Core Directives") {
		t.Error("BasePrompt still carries the superseded ## Core Directives wrapper")
	}
}

func TestWritePromptDocumentRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if err := WritePromptDocument(dir, "IDENTITY.md", "# who\n"); err != nil {
		t.Fatalf("WritePromptDocument() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("read IDENTITY.md: %v", err)
	}
	if string(got) != "# who\n" {
		t.Errorf("IDENTITY.md = %q, want round-tripped content", string(got))
	}

	info, err := os.Stat(filepath.Join(dir, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("stat IDENTITY.md: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("file mode = %o, want 644", perm)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("dir contains %d entries (%v), want only the target file — temp residue left behind", len(entries), names)
	}
}

func TestWritePromptDocumentsWritesIdentityAndSoul(t *testing.T) {
	dir := t.TempDir()

	if err := WritePromptDocuments(dir, "id", "soul"); err != nil {
		t.Fatalf("WritePromptDocuments() error = %v", err)
	}

	for name, want := range map[string]string{
		"IDENTITY.md": "id",
		"SOUL.md":     "soul",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, string(got), want)
		}
	}
}

func TestWritePromptDocumentsCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents", "atlas")

	if err := WritePromptDocuments(dir, "id", "soul"); err != nil {
		t.Fatalf("WritePromptDocuments() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("read IDENTITY.md: %v", err)
	}
	if string(got) != "id" {
		t.Errorf("IDENTITY.md = %q, want %q", string(got), "id")
	}
}

// TestHeartbeatTemplateCarriesSilenceContract pins the seeded checklist's
// load-bearing wording (add-agent-heartbeat D3/D7): the exact NO_REPLY token
// and the "what to check" section the user fills in.
func TestHeartbeatTemplateCarriesSilenceContract(t *testing.T) {
	if strings.TrimSpace(HeartbeatTemplate) == "" {
		t.Fatal("HeartbeatTemplate = empty, want embedded template")
	}
	if !strings.Contains(HeartbeatTemplate, "NO_REPLY") {
		t.Error("HeartbeatTemplate missing the exact NO_REPLY silence token")
	}
	if !strings.Contains(HeartbeatTemplate, "What to check") {
		t.Error("HeartbeatTemplate missing the user-editable \"What to check\" section")
	}
}

func TestReadPromptDocumentsMissingDirComposesEmpty(t *testing.T) {
	identity, soul, err := ReadPromptDocuments(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("ReadPromptDocuments() error = %v, want nil for missing dir", err)
	}
	if identity != "" || soul != "" {
		t.Errorf("ReadPromptDocuments() = (%q, %q), want empty strings", identity, soul)
	}
}

func TestReadPromptDocumentsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WritePromptDocuments(dir, "id", "soul"); err != nil {
		t.Fatalf("WritePromptDocuments() error = %v", err)
	}

	identity, soul, err := ReadPromptDocuments(dir)
	if err != nil {
		t.Fatalf("ReadPromptDocuments() error = %v", err)
	}
	if identity != "id" || soul != "soul" {
		t.Errorf("ReadPromptDocuments() = (%q, %q), want (id, soul)", identity, soul)
	}
}

func TestWritePromptDocumentsBacksUpPreviousGeneration(t *testing.T) {
	dir := t.TempDir()

	// First write: nothing to back up.
	if err := WritePromptDocuments(dir, "old identity", "old soul"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "IDENTITY.md.bak")); !os.IsNotExist(err) {
		t.Fatalf("first write must not create a backup, stat err: %v", err)
	}

	// Second write: the previous generation is preserved beside the files.
	if err := WritePromptDocuments(dir, "new identity", "new soul"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	for name, want := range map[string]string{
		"IDENTITY.md":     "new identity",
		"SOUL.md":         "new soul",
		"IDENTITY.md.bak": "old identity",
		"SOUL.md.bak":     "old soul",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, string(got), want)
		}
	}
}

func TestWritePromptDocumentsBackupFailureAbortsCommit(t *testing.T) {
	dir := t.TempDir()
	if err := WritePromptDocuments(dir, "old identity", "old soul"); err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	// A directory where IDENTITY.md.bak belongs makes the backup rename fail,
	// which must abort the whole write before any commit rename happens.
	if err := os.Mkdir(filepath.Join(dir, "IDENTITY.md.bak"), 0o755); err != nil {
		t.Fatalf("create backup blocker: %v", err)
	}

	if err := WritePromptDocuments(dir, "new identity", "new soul"); err == nil {
		t.Fatal("expected the write to fail when the backup cannot be created")
	}

	identity, soul, err := ReadPromptDocuments(dir)
	if err != nil {
		t.Fatalf("read back documents: %v", err)
	}
	if identity != "old identity" || soul != "old soul" {
		t.Errorf("originals must stay untouched after a backup failure, got (%q, %q)", identity, soul)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("staged temp file %s left behind after aborted write", e.Name())
		}
	}
}

// TestSweepStrayPromptFilesRemovesOnlyStrayDocuments covers the startup sweep
// (markdown-card-elements D8; remove-bootstrap-doc): a seeded AGENTS.md and
// the removed birth document's BOOTSTRAP.md/.bak leftovers are removed, while
// the generated documents, their backups, HEARTBEAT, and workspace skills
// files stay untouched.
func TestSweepStrayPromptFilesRemovesOnlyStrayDocuments(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"AGENTS.md":        "# OnClaw Agent Base System Prompt (L1)\n\nstale seeded copy",
		"IDENTITY.md":      "# Identity",
		"SOUL.md":          "# Soul",
		"BOOTSTRAP.md":     "# Bootstrap",
		"BOOTSTRAP.md.bak": "# Bootstrap (previous)",
		"IDENTITY.md.bak":  "# Identity (previous)",
		"SOUL.md.bak":      "# Soul (previous)",
		"HEARTBEAT.md":     "# Heartbeat checklist",
		"skills/feed.md":   "workspace skill body",
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	removed, err := SweepStrayPromptFiles([]string{dir})
	if err != nil {
		t.Fatalf("SweepStrayPromptFiles() error = %v", err)
	}
	wantRemoved := map[string]bool{
		filepath.Join(dir, "AGENTS.md"):        true,
		filepath.Join(dir, "BOOTSTRAP.md"):     true,
		filepath.Join(dir, "BOOTSTRAP.md.bak"): true,
	}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed = %v, want exactly the stray files %v", removed, wantRemoved)
	}
	for _, path := range removed {
		if !wantRemoved[path] {
			t.Errorf("unexpected removal %s, want only %v", path, wantRemoved)
		}
	}

	for name, want := range files {
		if wantRemoved[filepath.Join(dir, name)] {
			if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
				t.Errorf("%s must be removed, stat err: %v", name, statErr)
			}
			continue
		}
		got, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatalf("read %s after sweep: %v", name, readErr)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want untouched %q", name, string(got), want)
		}
	}
}

// TestSweepStrayPromptFilesRemovesBootstrapLeftovers pins the
// remove-bootstrap-doc sweep scenario (spec agent-prompts "Startup sweep
// removes stray bootstrap documents"): a workspace pre-seeded with the
// removed birth document and its backup is cleaned, while the generated
// documents are untouched.
func TestSweepStrayPromptFilesRemovesBootstrapLeftovers(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"BOOTSTRAP.md":     "# BOOTSTRAP.md - Birth Sequence\n_You just woke up._",
		"BOOTSTRAP.md.bak": "# BOOTSTRAP.md - Birth Sequence (previous)",
		"IDENTITY.md":      "# Identity\nkept",
		"SOUL.md":          "# Soul\nkept",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	removed, err := SweepStrayPromptFiles([]string{dir})
	if err != nil {
		t.Fatalf("SweepStrayPromptFiles() error = %v", err)
	}
	wantRemoved := map[string]bool{
		filepath.Join(dir, "BOOTSTRAP.md"):     true,
		filepath.Join(dir, "BOOTSTRAP.md.bak"): true,
	}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed = %v, want exactly the bootstrap leftovers %v", removed, wantRemoved)
	}
	for _, path := range removed {
		if !wantRemoved[path] {
			t.Errorf("unexpected removal %s, want only %v", path, wantRemoved)
		}
	}
	for _, name := range []string{"IDENTITY.md", "SOUL.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("generated document %s must be untouched: %v", name, err)
		}
	}
}

// TestSweepStrayPromptFilesToleratesMissing covers the no-op cases: a
// directory without stray files and a directory that does not exist at all
// sweep nothing and return no error.
func TestSweepStrayPromptFilesToleratesMissing(t *testing.T) {
	empty := t.TempDir()
	missing := filepath.Join(t.TempDir(), "agents", "ghost")

	removed, err := SweepStrayPromptFiles([]string{empty, missing})
	if err != nil {
		t.Fatalf("SweepStrayPromptFiles() error = %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}
}
