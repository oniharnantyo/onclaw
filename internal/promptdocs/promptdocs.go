// Package promptdocs manages agent workspace prompt documents: seeding the L1
// base prompt, atomically writing generated documents with backups, and
// reading them back for instruction composition.
package promptdocs

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed AGENTS.md
var BasePrompt string

// BootstrapTemplate is the embedded birth-sequence document seeded into every
// new agent workspace; BOOTSTRAP.md is never LLM-generated.
//
//go:embed BOOTSTRAP.md
var BootstrapTemplate string

// Workspace prompt document file names.
const (
	basePromptFileName     = "AGENTS.md"
	identityFileName       = "IDENTITY.md"
	soulFileName           = "SOUL.md"
	bootstrapFileName      = "BOOTSTRAP.md"
	promptDocumentFilePerm = 0o644
	workspaceDirPerm       = 0o755
	backupFileSuffix       = ".bak"
)

// SeedWorkspace creates the agent workspace directory if needed, seeds it with
// the L1 base prompt, and clears any generated documents left behind by a
// previous agent that lived in the same slug-derived directory — a newly
// created agent owns no generated documents yet. It is idempotent: an existing
// AGENTS.md is never overwritten.
func SeedWorkspace(dir string) error {
	if err := os.MkdirAll(dir, workspaceDirPerm); err != nil {
		return fmt.Errorf("create agent workspace dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, basePromptFileName)); errors.Is(err, fs.ErrNotExist) {
		if err := WritePromptDocument(dir, basePromptFileName, BasePrompt); err != nil {
			return fmt.Errorf("seed %s: %w", basePromptFileName, err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", basePromptFileName, err)
	}
	for _, name := range []string{
		identityFileName, soulFileName, bootstrapFileName,
		identityFileName + backupFileSuffix, soulFileName + backupFileSuffix, bootstrapFileName + backupFileSuffix,
	} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("clear %s: %w", name, err)
		}
	}
	return nil
}

// ensureDir creates the agent workspace directory if missing. Every writer
// self-heals a deleted or never-created directory instead of failing on the
// temp-file open inside it.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, workspaceDirPerm); err != nil {
		return fmt.Errorf("create agent workspace dir: %w", err)
	}
	return nil
}

// SeedBootstrapDocument writes the embedded BOOTSTRAP.md template into the
// agent workspace directory. A new agent starts with the shared birth-sequence
// template; BOOTSTRAP.md is never LLM-generated, and regeneration leaves the
// existing document untouched.
func SeedBootstrapDocument(dir string) error {
	if err := WritePromptDocument(dir, bootstrapFileName, BootstrapTemplate); err != nil {
		return fmt.Errorf("seed %s: %w", bootstrapFileName, err)
	}
	return nil
}

// stagePromptDocument writes content to a temp file inside dir and returns its
// path; the caller renames it into place (commitPromptDocument) or removes it.
func stagePromptDocument(dir, name, content string) (string, error) {
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-")
	if err != nil {
		return "", fmt.Errorf("create temp file for %s: %w", name, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("write temp file for %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("close temp file for %s: %w", name, err)
	}
	if err := os.Chmod(tmpName, promptDocumentFilePerm); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("chmod temp file for %s: %w", name, err)
	}
	return tmpName, nil
}

// commitPromptDocument renames a staged temp file over the target document.
func commitPromptDocument(dir, name, staged string) error {
	if err := os.Rename(staged, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("rename temp file for %s: %w", name, err)
	}
	return nil
}

// WritePromptDocument atomically writes a prompt document: content lands in a
// temp file inside the same directory, which is then renamed over the target
// so readers never observe a partial file.
func WritePromptDocument(dir, name, content string) error {
	if err := ensureDir(dir); err != nil {
		return err
	}
	staged, err := stagePromptDocument(dir, name, content)
	if err != nil {
		return err
	}
	if err := commitPromptDocument(dir, name, staged); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}

// WritePromptDocuments writes the three generated prompt documents into the
// agent workspace directory. Every document is staged first and the renames
// happen back-to-back only once all staging succeeded, so a failure while
// staging leaves the previous documents untouched (a crash between renames can
// still leave a partial mix — accepted).
func WritePromptDocuments(dir, identity, soul string) error {
	if err := ensureDir(dir); err != nil {
		return err
	}
	docs := []struct{ name, content string }{
		{identityFileName, identity},
		{soulFileName, soul},
	}

	staged := make([]string, len(docs))
	for i, doc := range docs {
		name, err := stagePromptDocument(dir, doc.name, doc.content)
		if err != nil {
			removeStaged(staged[:i])
			return err
		}
		staged[i] = name
	}

	// Back up the current documents before any commit rename: an existing
	// document is never overwritten without its previous version preserved
	// beside it. A failed backup aborts the write with the originals intact.
	for _, doc := range docs {
		if err := backupPromptDocument(dir, doc.name); err != nil {
			removeStaged(staged)
			return err
		}
	}

	for i, doc := range docs {
		if err := commitPromptDocument(dir, doc.name, staged[i]); err != nil {
			removeStaged(staged[i:]) // earlier entries are already renamed away
			return err
		}
	}
	return nil
}

// backupPromptDocument preserves the current content of dir/name as
// dir/name.bak via the same staged-temp + rename pattern as the documents. A
// missing document has nothing to preserve — a first write backs up nothing.
func backupPromptDocument(dir, name string) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", name, err)
	}
	if err := WritePromptDocument(dir, name+backupFileSuffix, string(data)); err != nil {
		return fmt.Errorf("write %s%s: %w", name, backupFileSuffix, err)
	}
	return nil
}

// removeStaged discards staged temp files; entries already renamed away are a
// no-op.
func removeStaged(staged []string) {
	for _, name := range staged {
		os.Remove(name)
	}
}

// ReadPromptDocuments reads the generated prompt documents from the agent
// workspace directory. A missing document composes as an empty string; only a
// real I/O error (e.g. the directory is unreadable) is returned.
func ReadPromptDocuments(dir string) (identity, soul, bootstrap string, err error) {
	identity, err = readPromptDocument(dir, identityFileName)
	if err != nil {
		return "", "", "", err
	}
	soul, err = readPromptDocument(dir, soulFileName)
	if err != nil {
		return "", "", "", err
	}
	bootstrap, err = readPromptDocument(dir, bootstrapFileName)
	if err != nil {
		return "", "", "", err
	}
	return identity, soul, bootstrap, nil
}

// readPromptDocument reads a single prompt document; a missing file is an
// empty document rather than an error.
func readPromptDocument(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return string(data), nil
}
