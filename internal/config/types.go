package config

// Workspace memory subtree names (add-agent-memory design D1): every agent
// workspace gains a memory/ directory — the curated index MEMORY.md, the
// fact notes, the daily notes — with an archive/ subdirectory inside it.
// Both are created on demand (the first turn for the agent), not at first
// run, so existing workspaces need nothing.
const (
	MemoryDirName        = "memory"
	MemoryArchiveDirName = "archive"
)

// Layout describes the on-disk data directory layout: everything the app
// persists lives under one root (normally ~/.buddies).
type Layout struct {
	// Root is the data directory itself (e.g. ~/.buddies).
	Root string
	// DBPath is the SQLite database file inside Root.
	DBPath string
	// Skills is the directory holding skill markdown folders.
	Skills string
	// Workspaces is the directory holding the agents' per-slug workspace
	// directories (their file-backed prompt documents).
	Workspaces string
	// Attachments is the directory holding stored chat-attachment files,
	// named by capability key (design D5).
	Attachments string
	// Knowledge is the root of the durable knowledge corpus
	// (add-knowledge-corpus design D2): global documents under global/,
	// agent-scoped documents under agents/<agent-slug>/.
	Knowledge string
	// Browser is the root for the browser tool's on-disk state: each named
	// profile's Chromium user-data directory lives under profiles/, and
	// screenshots under screenshots/.
	Browser string
	// Logs is the directory holding application logs.
	Logs string
}
