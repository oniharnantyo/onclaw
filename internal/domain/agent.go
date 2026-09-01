package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AgentAutonomy represents the autonomy level of an agent.
type AgentAutonomy string

const (
	AutonomyApproval AgentAutonomy = "approval"
	AutonomySuggest  AgentAutonomy = "suggest"
	AutonomyFull     AgentAutonomy = "full"
)

// PromptsStatus represents the generation status of an agent's prompts.
type PromptsStatus string

const (
	PromptsStatusGenerating PromptsStatus = "generating"
	PromptsStatusReady      PromptsStatus = "ready"
	PromptsStatusFailed     PromptsStatus = "failed"
)

// MaxAvatarBytes is the maximum allowed size for an agent avatar JSON object (2 KB).
const MaxAvatarBytes = 2048

// Agent represents an autonomous persona configured in a workspace.
type Agent struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Slug        string `json:"slug"`
	Name          string          `json:"name"`
	Role          string          `json:"role"`
	Description   string          `json:"description"`
	Brief         string          `json:"brief"`
	Identity      string          `json:"identity"`
	Soul          string          `json:"soul"`
	Bootstrap     string          `json:"bootstrap"`
	ProviderID    string          `json:"provider_id"`
	Model         string          `json:"model"`
	Temperature   float64         `json:"temperature"`
	MaxTokens     *int            `json:"max_tokens,omitempty"`
	Effort        *string         `json:"effort,omitempty"`
	Autonomy      AgentAutonomy   `json:"autonomy"`
	Tools         []string        `json:"tools"`
	Skills        []string        `json:"skills"`
	MCP           []string        `json:"mcp"`
	Avatar        json.RawMessage `json:"avatar"`
	PromptsStatus PromptsStatus   `json:"prompts_status"`
	PromptsError  *string         `json:"prompts_error,omitempty"`
	CreatedBy     *string         `json:"created_by,omitempty"`
	UpdatedBy     *string         `json:"updated_by,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// WorkspaceSkill represents a SKILL.md document stored at the workspace level.
type WorkspaceSkill struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Body        string    `json:"body,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AgentUserMemory represents per-user persistent memory for an agent.
type AgentUserMemory struct {
	AgentID     string    `json:"agent_id"`
	UserID      string    `json:"user_id"`
	WorkspaceID string    `json:"workspace_id"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ValidateAgentAutonomy validates that autonomy is one of the allowed values (approval, suggest, full).
func ValidateAgentAutonomy(autonomy AgentAutonomy) error {
	switch autonomy {
	case AutonomyApproval, AutonomySuggest, AutonomyFull:
		return nil
	default:
		return fmt.Errorf("%w: invalid autonomy %q, must be approval, suggest, or full", ErrInvalid, autonomy)
	}
}

// ValidateAgentTemperature validates that temperature is within the [0.0, 2.0] range.
func ValidateAgentTemperature(temp float64) error {
	if temp < 0.0 || temp > 2.0 {
		return fmt.Errorf("%w: temperature must be between 0.0 and 2.0", ErrInvalid)
	}
	return nil
}

// ValidateAgentAvatar validates that avatar is a JSON object and does not exceed 2 KB.
func ValidateAgentAvatar(avatar json.RawMessage) error {
	trimmed := strings.TrimSpace(string(avatar))
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	if len(avatar) > MaxAvatarBytes {
		return fmt.Errorf("%w: avatar exceeds maximum size of 2KB (%d bytes)", ErrInvalid, MaxAvatarBytes)
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return fmt.Errorf("%w: avatar must be a JSON object", ErrInvalid)
	}
	var obj map[string]any
	if err := json.Unmarshal(avatar, &obj); err != nil {
		return fmt.Errorf("%w: avatar must be a valid JSON object", ErrInvalid)
	}
	return nil
}

// ValidateAgentSlug validates that an agent slug adheres to the shared workspace slug rules.
func ValidateAgentSlug(slug string) error {
	return ValidateSlug(slug)
}

// DefaultWorkspaceDir returns the default agent workspace root:
// $HOME/.onclaw/workspaces, falling back to a relative .onclaw/workspaces when
// no home directory is available.
func DefaultWorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".onclaw", "workspaces")
	}
	return filepath.Join(home, ".onclaw", "workspaces")
}

// AgentWorkspaceDir returns the agent's on-disk workspace directory under the
// configured workspace root:
// <root>/<tenant_slug>/agents/<agent_slug>
//
// Both slugs are validated ([a-z0-9-]), so the path cannot traverse.
func AgentWorkspaceDir(root, tenantSlug, agentSlug string) string {
	return filepath.Join(root, tenantSlug, "agents", agentSlug)
}

// ValidateSkillName validates that a skill name is not empty.
func ValidateSkillName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w: skill name cannot be empty", ErrInvalid)
	}
	return nil
}
