// Package memory implements file-backed agent memory (add-agent-memory): a
// per-workspace memory directory holding the curated index MEMORY.md, atomic
// frontmatter fact notes, and YYYY-MM-DD.md daily working notes, plus an
// archive/ subtree that bounds growth without deleting. Files are the source
// of truth; the database holds derived state only. Nothing in this package
// makes a model call.
package memory
