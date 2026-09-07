package domain

import (
	"time"
)

// SessionEvent represents a persisted session event record in the append-only log.
type SessionEvent struct {
	SessionID   string    `json:"session_id"`
	EventID     string    `json:"event_id"`
	TurnID      string    `json:"turn_id"`
	Seq         int64     `json:"seq"`
	Kind        string    `json:"kind"`
	Payload     []byte    `json:"payload"`
	OccurredAt  time.Time `json:"occurred_at"`
	WorkspaceID string    `json:"workspace_id"`
}

// SessionCheckpoint represents a stored interrupt or execution checkpoint.
type SessionCheckpoint struct {
	CheckpointID string `json:"checkpoint_id"`
	Data         []byte `json:"data"`
}
