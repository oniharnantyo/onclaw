package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedWorkspaceSeedsBasePrompt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents", "atlas")

	if err := SeedWorkspace(dir); err != nil {
		t.Fatalf("SeedWorkspace() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(got) != BasePrompt {
		t.Errorf("AGENTS.md = %q, want embedded BasePrompt", string(got))
	}
}

func TestSeedWorkspaceIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("custom"), 0o644); err != nil {
		t.Fatalf("seed existing AGENTS.md: %v", err)
	}

	if err := SeedWorkspace(dir); err != nil {
		t.Fatalf("SeedWorkspace() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(got) != "custom" {
		t.Errorf("AGENTS.md = %q, want existing content untouched", string(got))
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

func TestWritePromptDocumentsWritesAllThree(t *testing.T) {
	dir := t.TempDir()

	if err := WritePromptDocuments(dir, "id", "soul", "boot"); err != nil {
		t.Fatalf("WritePromptDocuments() error = %v", err)
	}

	for name, want := range map[string]string{
		"IDENTITY.md":  "id",
		"SOUL.md":      "soul",
		"BOOTSTRAP.md": "boot",
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
	if err := WritePromptDocuments(dir, "id", "soul", "boot"); err != nil {
		t.Fatalf("WritePromptDocuments() error = %v", err)
	}

	identity, soul, bootstrap, err := ReadPromptDocuments(dir)
	if err != nil {
		t.Fatalf("ReadPromptDocuments() error = %v", err)
	}
	if identity != "id" || soul != "soul" || bootstrap != "boot" {
		t.Errorf("ReadPromptDocuments() = (%q, %q, %q), want (id, soul, boot)", identity, soul, bootstrap)
	}
}

func TestWritePromptDocumentsBacksUpPreviousGeneration(t *testing.T) {
	dir := t.TempDir()

	// First write: nothing to back up.
	if err := WritePromptDocuments(dir, "old identity", "old soul", "old boot"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "IDENTITY.md.bak")); !os.IsNotExist(err) {
		t.Fatalf("first write must not create a backup, stat err: %v", err)
	}

	// Second write: the previous generation is preserved beside the files.
	if err := WritePromptDocuments(dir, "new identity", "new soul", "new boot"); err != nil {
		t.Fatalf("second write: %v", err)
	}

	for name, want := range map[string]string{
		"IDENTITY.md":      "new identity",
		"SOUL.md":          "new soul",
		"BOOTSTRAP.md":     "new boot",
		"IDENTITY.md.bak":  "old identity",
		"SOUL.md.bak":      "old soul",
		"BOOTSTRAP.md.bak": "old boot",
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
	if err := WritePromptDocuments(dir, "old identity", "old soul", "old boot"); err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	// A directory where IDENTITY.md.bak belongs makes the backup rename fail,
	// which must abort the whole write before any commit rename happens.
	if err := os.Mkdir(filepath.Join(dir, "IDENTITY.md.bak"), 0o755); err != nil {
		t.Fatalf("create backup blocker: %v", err)
	}

	if err := WritePromptDocuments(dir, "new identity", "new soul", "new boot"); err == nil {
		t.Fatal("expected the write to fail when the backup cannot be created")
	}

	identity, soul, bootstrap, err := ReadPromptDocuments(dir)
	if err != nil {
		t.Fatalf("read back documents: %v", err)
	}
	if identity != "old identity" || soul != "old soul" || bootstrap != "old boot" {
		t.Errorf("originals must stay untouched after a backup failure, got (%q, %q, %q)", identity, soul, bootstrap)
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
