package config

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
	// Browser is the root for the browser tool's on-disk state: each named
	// profile's Chromium user-data directory lives under profiles/, and
	// screenshots under screenshots/.
	Browser string
	// Logs is the directory holding application logs.
	Logs string
}
