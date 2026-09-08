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
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	Slug          string          `json:"slug"`
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
	ContextWindow *int            `json:"context_window,omitempty"`
	Tools         []string        `json:"tools"`
	EnabledMCPS   []string        `json:"enabled_mcps"`
	Avatar        json.RawMessage `json:"avatar"`
	PromptsStatus PromptsStatus   `json:"prompts_status"`
	PromptsError  *string         `json:"prompts_error,omitempty"`
	MaxIterations *int            `json:"max_iterations,omitempty"`
	CreatedBy     *string         `json:"created_by,omitempty"`
	UpdatedBy     *string         `json:"updated_by,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
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

// DefaultContextWindow is the fallback context window size (200,000 tokens)
// when neither the agent configuration nor the model catalog specifies a limit.
const DefaultContextWindow = 200000

// ResolveContextWindow resolves the effective context window in precedence order:
// 1. Agent's explicitly stored context_window (if set and > 0)
// 2. Catalog context limit (if set and > 0)
// 3. DefaultContextWindow (200,000 tokens)
func ResolveContextWindow(agentCW *int, catalogCW *int) int {
	if agentCW != nil && *agentCW > 0 {
		return *agentCW
	}
	if catalogCW != nil && *catalogCW > 0 {
		return *catalogCW
	}
	return DefaultContextWindow
}

// ValidateAgentContextWindow validates that context_window is a positive integer when present.
func ValidateAgentContextWindow(cw *int) error {
	if cw != nil && *cw <= 0 {
		return fmt.Errorf("%w: context_window must be greater than 0", ErrInvalid)
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

// DefaultOnClawDir returns the default OnClaw root directory:
// $HOME/.onclaw, falling back to a relative .onclaw when no home directory is available.
func DefaultOnClawDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".onclaw"
	}
	return filepath.Join(home, ".onclaw")
}

// WorkspaceRoot returns the root directory for workspaces under the OnClaw root:
// <dir>/workspaces
func WorkspaceRoot(dir string) string {
	return filepath.Join(dir, "workspaces")
}

// SystemSkillsDir returns the system skills directory under the OnClaw root:
// <dir>/skills
func SystemSkillsDir(dir string) string {
	return filepath.Join(dir, "skills")
}

// WorkspaceSkillsDir returns the skills directory for a tenant workspace:
// <dir>/workspaces/<tenant_slug>/skills
func WorkspaceSkillsDir(dir, tenantSlug string) string {
	return filepath.Join(WorkspaceRoot(dir), tenantSlug, "skills")
}

// AgentSkillsDir returns the skills directory for an agent:
// <dir>/workspaces/<tenant_slug>/agents/<agent_slug>/skills
func AgentSkillsDir(dir, tenantSlug, agentSlug string) string {
	return filepath.Join(AgentWorkspaceDir(WorkspaceRoot(dir), tenantSlug, agentSlug), "skills")
}

// AgentWorkspaceDir returns the agent's on-disk workspace directory under the
// configured workspace root:
// <root>/<tenant_slug>/agents/<agent_slug>
//
// Both slugs are validated ([a-z0-9-]), so the path cannot traverse.
func AgentWorkspaceDir(root, tenantSlug, agentSlug string) string {
	return filepath.Join(root, tenantSlug, "agents", agentSlug)
}
