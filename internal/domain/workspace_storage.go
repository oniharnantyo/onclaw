package domain

import (
	"time"
)

// WorkspaceStorageConfig is one workspace's blob-storage configuration
// (design D16). SecretAccessKey holds the SEALED envelope string at rest —
// sealing/unsealing is the caller's job (handlers seal on write, the
// storage resolver unseals on driver construction); the store persists it opaquely.
type WorkspaceStorageConfig struct {
	WorkspaceID     string    `json:"workspace_id"`
	Driver          string    `json:"driver"` // "local" | "s3"
	Endpoint        string    `json:"endpoint,omitempty"`
	Region          string    `json:"region,omitempty"`
	Bucket          string    `json:"bucket,omitempty"`
	AccessKeyID     string    `json:"access_key_id,omitempty"`
	SecretAccessKey string    `json:"-"` // sealed envelope; never serialized
	UsePathStyle    bool      `json:"use_path_style"`
	UpdatedAt       time.Time `json:"updated_at"`
}
