// Package sessions models the event-sourced chat session: session lifecycle
// per agent, typed turn events mapped onto the store's append-only event log
// (JSON payload column), and compaction checkpoints mapped onto the store's
// checkpoint blobs. Its types are JSON-serializable and carry no model
// runtime types, so the agent runtime can stream them to UI bridges verbatim.
package sessions
