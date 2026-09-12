package domain

import (
	"time"
)

// Attachment is one uploaded chat attachment (add-chat-attachments design
// D3): the capability key under which the blob bytes live, the backend
// holding those bytes, and the metadata the transcript and /v1 wire need.
// The bytes themselves never persist in session events or ride the turn
// request — only this reference does.

// AttachmentLane values stored on attachments.lane (design D4).
const (
	AttachmentLaneInlineImage = "inline-image"
	AttachmentLaneInlinePDF   = "inline-pdf"
	AttachmentLaneInlineText  = "inline-text"
	AttachmentLaneDrop        = "drop"
)

type Attachment struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	StorageKey  string    `json:"-"`       // capability key; never serialized
	Backend     string    `json:"backend"` // storage driver holding the blob ("local" | "s3")
	Name        string    `json:"name"`
	MimeType    string    `json:"mime"`
	Size        int64     `json:"size"`
	Lane        string    `json:"lane"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}
