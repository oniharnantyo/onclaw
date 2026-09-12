package domain

import (
	"time"
)

// AgentSession is one row of the durable agent session index
// (agent-session-index design D1): a per-user record that an agent chat
// session exists, carrying its birth-derived title and last-activity time so
// sidebar history follows the account rather than the browser. Deletion is
// soft — DeletedAt hides the row from listings while the transcript events
// and checkpoints in session_events stay intact.
type AgentSession struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	AgentID      string     `json:"agent_id"`
	UserID       string     `json:"user_id"`
	SessionID    string     `json:"session_id"`
	Title        string     `json:"title"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastActiveAt time.Time  `json:"last_active_at"`
}

// AgentSessionUpsert is the write model for indexing one persistent run at
// run start (design D2): the client-minted session id and the input-derived
// title candidate. Title is honored only on birth — an upsert against a row
// whose title is already set never rewrites it (an empty Title stores the
// empty string on birth and leaves an existing title untouched on conflict).
type AgentSessionUpsert struct {
	SessionID string
	Title     string
}
