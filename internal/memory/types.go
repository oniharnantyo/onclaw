package memory

import (
	"path/filepath"
)

// Note types (add-agent-memory design D4, ZCode's proven taxonomy).
const (
	TypeUser      = "user"
	TypeFeedback  = "feedback"
	TypeProject   = "project"
	TypeReference = "reference"
)

// Relation predicates (SCM's six, design D4). A relation entry is
// "<predicate> <target>", e.g. "prefers dark-mode".
const (
	PredHasProperty = "has_property"
	PredPrefers     = "prefers"
	PredRelatedTo   = "related_to"
	PredContradicts = "contradicts"
	PredCauses      = "causes"
	PredPartOf      = "part_of"
)

// File and directory names inside a workspace memory directory (spec
// agent-memory "File-backed memory notes").
const (
	DirName        = "memory"
	ArchiveDirName = "archive"
	IndexFileName  = "MEMORY.md"
)

// Frontmatter is the raw YAML frontmatter block of a fact note: the on-disk
// vocabulary the agent writes (design D4). Relations stay raw strings here —
// each must parse as "<predicate> <target>" and pass predicate validation
// before a Note is produced.
type Frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Type        string   `yaml:"type"`
	Concepts    []string `yaml:"concepts"`
	Relations   []string `yaml:"relations"`
	Supersedes  []string `yaml:"supersedes"`
}

// Relation is one validated frontmatter relation: a predicate from the SCM
// vocabulary (design D4) and its target note or concept name. Relations and
// wiki-links are authored by the agent in-turn; they are the graph edges of a
// later phase, captured now at zero cost.
type Relation struct {
	Predicate string
	Target    string
}

// Note is one validated memory fact note: the parsed frontmatter plus the
// wiki-links extracted from the body. Content lives only on disk; nothing
// here is persisted to the database.
type Note struct {
	// Name is the note slug, equal to the filename without the .md suffix.
	Name string
	// Description is the one-line summary surfaced by the index.
	Description string
	// Type is one of the four note types (user, feedback, project, reference).
	Type string
	// Concepts are the recall handles the agent assigns to the note.
	Concepts []string
	// Relations are the validated frontmatter relations.
	Relations []Relation
	// Supersedes names the notes this note replaces (the supersede convention).
	Supersedes []string
	// Links are the deduplicated [[wiki-links]] found in the body, in order
	// of first appearance.
	Links []string
	// Body is the note text below the frontmatter block.
	Body string
	// Path is the note's file path on disk.
	Path string
}

// Layout resolves the paths of one workspace's memory directory (design D1):
// the curated index MEMORY.md, the atomic fact notes, the daily notes, and
// the archive/ subtree, all under one directory.
type Layout struct {
	// Dir is the memory directory itself (<workspace>/memory).
	Dir string
}

// NewLayout returns the memory layout rooted at the workspace directory
// workspaceDir (the agent's workspace, e.g. workspaces/<slug>).
func NewLayout(workspaceDir string) Layout {
	return Layout{Dir: filepath.Join(workspaceDir, DirName)}
}

// IndexPath returns the curated index file path (<memory>/MEMORY.md).
func (l Layout) IndexPath() string {
	return filepath.Join(l.Dir, IndexFileName)
}

// NotePath returns the path of the fact note named name (<memory>/<name>.md).
func (l Layout) NotePath(name string) string {
	return filepath.Join(l.Dir, name+".md")
}

// ArchiveDir returns the archive subdirectory path (<memory>/archive) — the
// destination of below-floor and superseded notes, never a delete target.
func (l Layout) ArchiveDir() string {
	return filepath.Join(l.Dir, ArchiveDirName)
}
