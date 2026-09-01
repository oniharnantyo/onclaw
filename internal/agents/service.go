package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DefaultTimeout is the default duration allocated to a synchronous prompt generation.
const DefaultTimeout = 3 * time.Minute

// Option configures the generation Service.
type Option func(*Service)

// WithAgentPromptGeneratorModelFactory overrides the model factory used by the service.
func WithAgentPromptGeneratorModelFactory(factory ModelFactory) Option {
	return func(s *Service) {
		if factory != nil {
			s.agentPromptGeneratorModelFactory = factory
		}
	}
}

// WithTimeout sets the per-generation background context timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Service) {
		if timeout > 0 {
			s.timeout = timeout
		}
	}
}

// Service orchestrates agent prompt generation.
type Service struct {
	store                            store.Store
	encryptionKey                    []byte
	agentPromptGeneratorModelFactory ModelFactory
	timeout                          time.Duration
}

// NewService creates a new prompt generation service.
func NewService(s store.Store, encryptionKey []byte, opts ...Option) *Service {
	svc := &Service{
		store:                            s,
		encryptionKey:                    encryptionKey,
		agentPromptGeneratorModelFactory: AgentPromptGeneratorModelFactory,
		timeout:                          DefaultTimeout,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Generate executes prompt generation for a persisted agent synchronously —
// the regenerate path. A non-empty instruction carries the client's requested
// changes into the model call. Delete-during-flight is safely ignored as a
// no-op status write; a failed generation transitions the row to failed and
// leaves any previous documents on disk untouched.
func (s *Service) Generate(ctx context.Context, dir, workspaceID, agentID, instruction string) error {
	if workspaceID == "" || agentID == "" {
		return domain.ErrInvalid
	}

	// 1. Fetch Agent
	agent, err := s.store.Agents().ByID(ctx, workspaceID, agentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Agent was deleted in flight: no-op
			return nil
		}
		return fmt.Errorf("fetch agent: %w", err)
	}

	// Current documents feed enhance mode: regeneration strengthens what is on
	// disk instead of replacing it. No documents (or an unreadable workspace)
	// generate fresh. dir is derived by the caller from the current root and
	// slugs; the row records no path.
	current := readCurrentPrompts(dir)
	identity, soul, bootstrap, err := s.generateDocuments(ctx, workspaceID, agent, current, instruction)
	if err != nil {
		return s.fail(ctx, workspaceID, agentID, SanitizeError(err))
	}

	// Re-verify the agent row before writing files: a delete-then-recreate of
	// the same slug reuses the derived directory, so a stale in-flight
	// generation must not write into the new agent's workspace. A vanished row
	// is the delete-during-flight no-op; a different row under the same dir
	// means this generation is no longer this agent's.
	fresh, err := s.store.Agents().ByID(ctx, workspaceID, agentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("re-verify agent before write: %w", err)
	}
	if fresh.ID != agent.ID {
		return nil
	}

	// Write prompt documents — files land before the ready transition, so a
	// ready agent always has documents on disk; a failed regeneration leaves
	// the previous files untouched.
	if err := WritePromptDocuments(dir, identity, soul, bootstrap); err != nil {
		// Carry the underlying cause so prompts_error stays diagnosable;
		// SanitizeErrorString trims it to a safe, single-line reason.
		return s.fail(ctx, workspaceID, agentID, fmt.Sprintf("failed to write prompt documents: %v", err))
	}

	// Commit Ready State — via a terminal write context: a generation that
	// finished at the edge of its budget must still land ready.
	writeCtx, cancel := terminalWriteCtx(ctx)
	defer cancel()
	err = s.store.Agents().SetPromptState(writeCtx, workspaceID, agentID, domain.PromptsStatusReady, nil)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("set prompt state ready: %w", err)
	}

	return nil
}

// GenerateForCreate runs the generation pipeline for a not-yet-persisted agent
// and writes the documents into agent.WorkspaceDir — always fresh generation,
// since a new agent owns no documents. It performs no store agent reads and no
// status writes: the caller persists the row only after this succeeds, so a
// failed generation leaves no agent behind.
func (s *Service) GenerateForCreate(ctx context.Context, dir, workspaceID string, agent *domain.Agent) error {
	identity, soul, bootstrap, err := s.generateDocuments(ctx, workspaceID, agent, nil, "")
	if err != nil {
		return err
	}
	return WritePromptDocuments(dir, identity, soul, bootstrap)
}

// readCurrentPrompts loads the agent's generated documents for enhance mode.
// A directory without documents, or an unreadable one, yields nil — fresh
// generation — rather than failing the run.
func readCurrentPrompts(dir string) *GeneratedPrompts {
	if dir == "" {
		return nil
	}
	identity, soul, bootstrap, err := ReadPromptDocuments(dir)
	if err != nil || (identity == "" && soul == "" && bootstrap == "") {
		return nil
	}
	return &GeneratedPrompts{Identity: identity, Soul: soul, Bootstrap: bootstrap}
}

// generateDocuments resolves the agent's provider config, decrypts its key,
// invokes the configured model with a single forced tool so the documents
// arrive as structured output, and parses the three documents. The current
// documents, when non-nil, switch the call to enhance mode. It performs no
// status writes; errors carry already-sanitized, client-safe reasons.
func (s *Service) generateDocuments(ctx context.Context, workspaceID string, agent *domain.Agent, current *GeneratedPrompts, instruction string) (identity, soul, bootstrap string, err error) {
	// Fetch Provider Config
	provider, err := s.store.Providers().ByID(ctx, workspaceID, agent.ProviderID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", "", "", fmt.Errorf("provider configuration not found in workspace")
		}
		return "", "", "", fmt.Errorf("fetch provider: %w", err)
	}

	// Decrypt Provider Key
	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(s.encryptionKey, []byte(workspaceID), provider.KeyCiphertext)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to decrypt provider credentials")
		}
		apiKey = string(plaintext)
	}

	// Instantiate Model Adapter
	cred := providers.Credential{
		Type:    provider.Type,
		BaseURL: provider.BaseURL,
		APIKey:  apiKey,
	}

	chatModel, err := s.agentPromptGeneratorModelFactory(ctx, provider.Type, cred, agent.Model)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to initialize model adapter: %s", SanitizeError(err))
	}

	// Invoke LLM — bounded by the service timeout (3 minutes by default) so a
	// slow or hung provider surfaces a timeout reason instead of hanging the
	// request indefinitely.
	genCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := chatModel.Generate(genCtx, BuildGenerationMessages(agent, current, instruction), generationOptions()...)
	if err != nil {
		return "", "", "", fmt.Errorf("%s", SanitizeError(err))
	}

	if resp == nil {
		return "", "", "", fmt.Errorf("model returned empty response")
	}

	// Parse structured output
	identity, soul, bootstrap, err = ParseGenerationOutput(resp)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse generated identity and soul prompts from model output")
	}

	return identity, soul, bootstrap, nil
}

// Sweep resets all orphaned 'generating' agents to 'failed' with the standard interrupted message.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	return s.store.Agents().SweepGenerating(ctx, InterruptedErrorMessage)
}

// terminalWriteTimeout bounds a single prompt-state write once generation has
// finished; the terminal write must never wait on the (possibly dead)
// generation context.
const terminalWriteTimeout = 5 * time.Second

// terminalWriteCtx derives a context for prompt-state writes that must land
// even after the generation context has died — a timed-out or canceled
// generation still records its terminal state instead of stranding the agent
// in 'generating' forever.
func terminalWriteCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), terminalWriteTimeout)
}

// fail transitions the agent prompts_status to failed with a safe error message.
// If the agent was deleted, it silently succeeds.
func (s *Service) fail(ctx context.Context, workspaceID, agentID, reason string) error {
	sanitized := SanitizeErrorString(reason)
	writeCtx, cancel := terminalWriteCtx(ctx)
	defer cancel()
	err := s.store.Agents().SetPromptState(writeCtx, workspaceID, agentID, domain.PromptsStatusFailed, &sanitized)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("set prompt state failed: %w", err)
	}
	return fmt.Errorf("prompt generation failed: %s", sanitized)
}

// SanitizeError extracts and sanitizes an error for user-facing display.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeErrorString(err.Error())
}

// SanitizeErrorString ensures error messages are concise, safe to display, and actionable.
func SanitizeErrorString(msg string) string {
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "provider configuration not found"):
		return "provider configuration not found in workspace"
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "invalid api key") || strings.Contains(lower, "authentication"):
		return "provider authentication failed — check provider API key"
	case strings.Contains(lower, "403") || strings.Contains(lower, "permission_denied") || strings.Contains(lower, "forbidden"):
		return "provider permission denied — check API key permissions"
	case strings.Contains(lower, "model_not_found") || strings.Contains(lower, "model not found") || (strings.Contains(lower, "404") && strings.Contains(lower, "model")):
		return "configured model not found or inaccessible with provider credentials"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "quota") || strings.Contains(lower, "resource_exhausted"):
		return "provider rate limit or quota exceeded — retry later"
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout"):
		return "prompt generation timed out — retry"
	case strings.Contains(lower, "decrypt"):
		return "failed to decrypt provider credentials"
	case strings.Contains(lower, "parse"):
		return "failed to parse generated identity and soul prompts from model output"
	default:
		// Clean up any potential sensitive strings or excessive verbosity
		clean := strings.TrimSpace(msg)
		if idx := strings.Index(clean, "\n"); idx != -1 {
			clean = clean[:idx]
		}
		if len(clean) > 150 {
			clean = clean[:147] + "..."
		}
		return clean
	}
}
