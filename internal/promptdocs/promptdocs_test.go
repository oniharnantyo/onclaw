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
// embedded L1 template (markdown-card-elements D7): the section heading and
// the transport rules the renderer relies on.
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
		"- table: {\"",
		"- ticker: {\"",
		"- activity: {\"",
		"- spec: {\"",
		"- compare: {\"",
		"- progress: {\"",
		"- score: {\"",
		"- flow: {\"",
		"- math: {\"",
		"```diagram Payment flow",
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

func TestSeedBootstrapDocumentWritesTemplate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents", "atlas")

	if err := SeedBootstrapDocument(dir); err != nil {
		t.Fatalf("SeedBootstrapDocument() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "BOOTSTRAP.md"))
	if err != nil {
		t.Fatalf("read BOOTSTRAP.md: %v", err)
	}
	if string(got) != BootstrapTemplate {
		t.Errorf("BOOTSTRAP.md = %q, want embedded template", string(got))
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
	identity, soul, bootstrap, err := ReadPromptDocuments(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("ReadPromptDocuments() error = %v, want nil for missing dir", err)
	}
	if identity != "" || soul != "" || bootstrap != "" {
		t.Errorf("ReadPromptDocuments() = (%q, %q, %q), want empty strings", identity, soul, bootstrap)
	}
}

func TestReadPromptDocumentsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WritePromptDocuments(dir, "id", "soul"); err != nil {
		t.Fatalf("WritePromptDocuments() error = %v", err)
	}

	identity, soul, bootstrap, err := ReadPromptDocuments(dir)
	if err != nil {
		t.Fatalf("ReadPromptDocuments() error = %v", err)
	}
	if identity != "id" || soul != "soul" || bootstrap != "" {
		t.Errorf("ReadPromptDocuments() = (%q, %q, %q), want (id, soul, empty)", identity, soul, bootstrap)
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

	identity, soul, _, err := ReadPromptDocuments(dir)
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

// TestSweepSeededBasePromptsRemovesOnlyBasePrompt covers the startup sweep
// (markdown-card-elements D8): a seeded AGENTS.md is removed, while the
// generated documents, their backups, HEARTBEAT, and workspace skills files
// stay untouched.
func TestSweepSeededBasePromptsRemovesOnlyBasePrompt(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"AGENTS.md":       "# OnClaw Agent Base System Prompt (L1)\n\nstale seeded copy",
		"IDENTITY.md":     "# Identity",
		"SOUL.md":         "# Soul",
		"BOOTSTRAP.md":    "# Bootstrap",
		"IDENTITY.md.bak": "# Identity (previous)",
		"SOUL.md.bak":     "# Soul (previous)",
		"HEARTBEAT.md":    "# Heartbeat checklist",
		"skills/feed.md":  "workspace skill body",
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

	removed, err := SweepSeededBasePrompts([]string{dir})
	if err != nil {
		t.Fatalf("SweepSeededBasePrompts() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != filepath.Join(dir, "AGENTS.md") {
		t.Fatalf("removed = %v, want exactly the seeded AGENTS.md path", removed)
	}

	for name, want := range files {
		if name == "AGENTS.md" {
			if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
				t.Errorf("AGENTS.md must be removed, stat err: %v", statErr)
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

// TestSweepSeededBasePromptsToleratesMissing covers the no-op cases: a
// directory without a base prompt and a directory that does not exist at all
// sweep nothing and return no error.
func TestSweepSeededBasePromptsToleratesMissing(t *testing.T) {
	empty := t.TempDir()
	missing := filepath.Join(t.TempDir(), "agents", "ghost")

	removed, err := SweepSeededBasePrompts([]string{empty, missing})
	if err != nil {
		t.Fatalf("SweepSeededBasePrompts() error = %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}
}
