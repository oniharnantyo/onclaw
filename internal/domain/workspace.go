package domain

import (
	"fmt"
	"time"
)

// MasterWorkspaceSlug is the reserved slug for the master tenant.
const MasterWorkspaceSlug = "master"

// DefaultModelPair is the workspace default model binding agents may inherit:
// both fields set pins the default, absent/empty leaves it unset.
type DefaultModelPair struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

// Workspace represents a tenant in OnClaw.
type Workspace struct {
	ID           string            `json:"id"`
	Slug         string            `json:"slug"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Timezone     string            `json:"timezone"`
	Policy       string            `json:"policy,omitempty"`
	Language     *string           `json:"language,omitempty"`
	DefaultModel *DefaultModelPair `json:"default_model"`
	IsMaster     bool              `json:"is_master"`
	DisabledAt   *time.Time        `json:"disabled_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ValidateDefaultModelPair enforces both-or-neither on the workspace default
// model pair (mirror of ValidateAgentMemorySidecall).
func ValidateDefaultModelPair(providerID, model string) error {
	if (providerID == "") != (model == "") {
		return fmt.Errorf("%w: default model needs both provider and model, or neither", ErrInvalid)
	}
	return nil
}

// IsSuspended reports whether the workspace is suspended (disabled).
func (w *Workspace) IsSuspended() bool {
	return w != nil && w.DisabledAt != nil
}
