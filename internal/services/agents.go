package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/promptgen"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// Sentinel errors surfaced by the agents service. Callers test them with
// errors.Is; concrete failures wrap them with %w and context.
var (
	// ErrPromptGeneration reports a create or regenerate whose synchronous
	// prompt generation failed: the agent row exists with prompt status
	// failed (reason in PromptsError) and stays editable — the error is a
	// report of the failure, not a rejection of the row.
	ErrPromptGeneration = errors.New("prompt generation failed")
	// ErrGenerationInFlight rejects a regeneration while another generation
	// is running for the same agent: single-flight is enforced by the
	// prompt status (design D6).
	ErrGenerationInFlight = errors.New("prompt generation already in flight")
)

// interruptedGenerationReason is the canonical PromptsError the startup
// sweep stamps on rows orphaned by a crash mid-generation (spec text).
const interruptedGenerationReason = "prompt generation interrupted — retry"

// PromptGenerator is the generation seam behind the agents service's prompt
// status machine: the two promptgen entrypoints the service drives. It
// exists so tests can inject a fake generator instead of a model-backed one.
type PromptGenerator interface {
	GenerateForCreate(ctx context.Context, slug string, params promptgen.Params) (promptgen.Documents, error)
	Regenerate(ctx context.Context, slug string, params promptgen.Params) (promptgen.Documents, error)
}

// CreateAgentInput is the write shape of agent creation, as bound to the
// frontend. It mirrors ProviderInput: only the fields a client may choose —
// the store assigns ID and timestamps, and the prompt status moves only
// through the service's generation paths, so no status field crosses the
// bridge. The brief is required: it drives prompt generation and is
// persisted so regeneration works without client state. The slug names the
// agent's workspace directory, which is why it must pass workspaces.ValidSlug
// and is immutable after create.
type CreateAgentInput struct {
	Name string
	Slug string
	// Description is the optional card summary. Avatar is the optional
	// procedural-avatar config (the wizard's AvatarPicker output), stored
	// verbatim as a JSON object; an empty map derives the face from the id.
	Description string
	Brief       string
	Avatar      map[string]any
	ProviderID  string
	Model       string
	// Effort is the agent's default reasoning effort (spec agent-management
	// "Edit agent configuration"): stored on the row and applied to turns
	// whenever a session carries no effort override. Empty clears/inherit.
	// Saved values are validated against the selected model's supported
	// effort set — an unsupported effort is a validation error.
	Effort        string
	Temperature   float64
	MaxTokens     int
	ContextWindow int
	Tools         []string
	Skills        []string
	MCPServers    []string
	// SubagentsEnabled is the dedicated subagents capability toggle (design
	// D3). It is a pointer so "omitted" is distinguishable from an explicit
	// false: nil (the wizard's default) means on — the same default the
	// column carries (migration 000007's DEFAULT 1).
	SubagentsEnabled *bool
}

// AgentsService is the bound service for agent definitions: CRUD over the
// store's agent table plus the prompt-document lifecycle — synchronous
// generation at create (design D4), regeneration, the documents' read/save
// surface for the config Identity tab, and the startup sweep. Turn-time
// composition stays in the runtime (task 3.1); this service never reads
// document content except through the Get/Save prompt-document calls.
type AgentsService struct {
	store      store.Store
	prompts    PromptGenerator
	workspaces *workspaces.Manager
	// knowledge removes the agent's knowledge scope directory after the row
	// delete cascaded its document rows (add-knowledge-corpus design D9);
	// nil — a runtime built without a corpus — skips the cleanup.
	knowledge *knowledge.Store
}

// NewAgentsService returns the agent management service over s, generating
// prompt documents through prompts and keeping their files under ws. All
// three dependencies are required. knowledge is the durable corpus store
// whose agent scopes Delete cleans up; nil skips that cleanup.
func NewAgentsService(s store.Store, prompts PromptGenerator, ws *workspaces.Manager, k *knowledge.Store) *AgentsService {
	return &AgentsService{store: s, prompts: prompts, workspaces: ws, knowledge: k}
}

// Create stores a new agent and generates its prompt documents synchronously
// from the brief (design D4): the row persists with prompt status generating,
// the workspace directory is created even before generation finishes (design
// D11), and generation runs on the agent's own provider and model before the
// call returns. On success the status commits ready; on failure it commits
// failed with the sanitized reason, the returned error wraps
// ErrPromptGeneration, and the created agent still comes back — the failure
// is reported, the row survives and stays editable. Validation failures
// (empty name, invalid slug, empty brief, unknown provider, duplicate slug)
// reject without leaving a row.
func (s *AgentsService) Create(ctx context.Context, input CreateAgentInput) (store.Agent, error) {
	if err := validateAgentName(input.Name); err != nil {
		return store.Agent{}, err
	}
	if !workspaces.ValidSlug(input.Slug) {
		return store.Agent{}, fmt.Errorf("invalid agent slug %q (want a DNS label: lowercase letters, digits, single interior dashes, at most 63 characters)", input.Slug)
	}
	if strings.TrimSpace(input.Brief) == "" {
		return store.Agent{}, errors.New("agent brief must not be empty: it drives prompt generation")
	}
	provider, err := s.store.Providers().ByID(ctx, input.ProviderID)
	if err != nil {
		return store.Agent{}, fmt.Errorf("create agent: resolve provider %q: %w", input.ProviderID, err)
	}

	agent := store.Agent{
		Name:          input.Name,
		Slug:          input.Slug,
		Description:   input.Description,
		Brief:         input.Brief,
		Avatar:        input.Avatar,
		PromptsStatus: store.PromptsStatusGenerating,
		ProviderID:    input.ProviderID,
		Model:         input.Model,
		Effort:        input.Effort,
		Temperature:   input.Temperature,
		MaxTokens:     input.MaxTokens,
		ContextWindow: input.ContextWindow,
		Tools:         input.Tools,
		Skills:        input.Skills,
		MCPServers:    input.MCPServers,
		// Subagents default on (design D3, migration 000007's DEFAULT 1): an
		// omitted toggle starts the capability enabled; only an explicit false
		// turns it off.
		SubagentsEnabled: input.SubagentsEnabled == nil || *input.SubagentsEnabled,
	}
	// The default effort must be one the selected model supports (spec
	// agent-management: options are limited to the model's set); the empty
	// effort is the cleared/inherit state and always valid. The provider is
	// already resolved above.
	if err := validateAgentEffort(provider.Type, agent.Model, agent.Effort); err != nil {
		return store.Agent{}, fmt.Errorf("create agent: %w", err)
	}
	// A duplicate slug is rejected here by the store's UNIQUE index; the
	// error surfaces as a step-level validation error to the wizard.
	if err := s.store.Agents().Create(ctx, &agent); err != nil {
		return store.Agent{}, fmt.Errorf("create agent: %w", err)
	}

	// The workspace directory exists even before generation finishes:
	// partial directories from failed generations are legitimate (design D11).
	if _, err := s.workspaces.Create(agent.Slug); err != nil {
		return s.failCreate(ctx, agent, err)
	}

	params := promptgen.Params{
		Provider:  *provider,
		Model:     agent.Model,
		AgentName: agent.Name,
		Brief:     agent.Brief,
	}
	if _, err := s.prompts.GenerateForCreate(ctx, agent.Slug, params); err != nil {
		return s.failCreate(ctx, agent, err)
	}

	agent, err = s.setStatus(ctx, agent, store.PromptsStatusReady, "")
	if err != nil {
		return store.Agent{}, err
	}
	return agent, nil
}

// Get returns the agent with the given ID; store.ErrNotFound when absent.
func (s *AgentsService) Get(ctx context.Context, id string) (store.Agent, error) {
	agent, err := s.store.Agents().ByID(ctx, id)
	if err != nil {
		return store.Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return *agent, nil
}

// List returns all agents, oldest first. The result is never nil.
func (s *AgentsService) List(ctx context.Context) ([]store.Agent, error) {
	agents, err := s.store.Agents().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return agents, nil
}

// Update rewrites an existing agent's editable configuration: name,
// description, provider, model, default reasoning effort, and the capability
// fields. The slug and the brief are immutable after create — the stored
// values win no matter what the caller sends — and so are the prompt status
// fields, which move only through the generation paths and the startup
// sweep. A non-empty effort must be one the (possibly new) provider and
// model combination supports; the empty effort clears back to inherit. The
// row is addressed by agent.ID; store.ErrNotFound when absent.
func (s *AgentsService) Update(ctx context.Context, agent store.Agent) (store.Agent, error) {
	if err := validateAgentName(agent.Name); err != nil {
		return store.Agent{}, err
	}
	stored, err := s.store.Agents().ByID(ctx, agent.ID)
	if err != nil {
		return store.Agent{}, fmt.Errorf("update agent: %w", err)
	}
	if agent.Effort != "" {
		provider, err := s.store.Providers().ByID(ctx, agent.ProviderID)
		if err != nil {
			return store.Agent{}, fmt.Errorf("update agent: resolve provider %q: %w", agent.ProviderID, err)
		}
		if err := validateAgentEffort(provider.Type, agent.Model, agent.Effort); err != nil {
			return store.Agent{}, fmt.Errorf("update agent: %w", err)
		}
	}
	agent.Slug = stored.Slug
	agent.Brief = stored.Brief
	agent.PromptsStatus = stored.PromptsStatus
	agent.PromptsError = stored.PromptsError
	if err := s.store.Agents().Update(ctx, &agent); err != nil {
		return store.Agent{}, fmt.Errorf("update agent: %w", err)
	}
	return agent, nil
}

// Delete removes the agent; store.ErrNotFound when absent. The agent's
// session history is preserved (spec: deleting an agent keeps its sessions),
// the workspace directory is removed best-effort after the row delete, and
// the agent's knowledge scope follows it (the FK cascade removed its
// document rows; the files go best-effort — a removal failure is logged and
// never fails the delete, design D11 / add-knowledge-corpus D9).
func (s *AgentsService) Delete(ctx context.Context, id string) error {
	stored, err := s.store.Agents().ByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if err := s.store.Agents().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if stored.Slug == "" {
		// Nothing to clean up: the row never had a workspace (pre-backfill).
		return nil
	}
	if err := s.workspaces.Remove(stored.Slug); err != nil {
		slog.Warn("delete agent: workspace removal failed", "slug", stored.Slug, "error", err)
	}
	if s.knowledge != nil {
		// The corpus lives outside the workspace (add-knowledge-corpus D2),
		// so the workspace removal above never touches it.
		if err := s.knowledge.RemoveAgentScope(ctx, stored.Slug); err != nil {
			slog.Warn("delete agent: knowledge scope removal failed", "slug", stored.Slug, "error", err)
		}
	}
	return nil
}

// RegeneratePrompts regenerates the agent's prompt documents synchronously
// from the stored brief, on the agent's own provider and model, with the
// optional instruction riding along as a requested-changes section. While a
// generation is already in flight the call is rejected with
// ErrGenerationInFlight; otherwise the status moves generating → ready |
// failed, and a generation failure commits the sanitized reason and comes
// back wrapped in ErrPromptGeneration with the existing documents untouched.
func (s *AgentsService) RegeneratePrompts(ctx context.Context, agentID, instruction string) error {
	stored, err := s.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("regenerate prompts: %w", err)
	}
	if stored.PromptsStatus == store.PromptsStatusGenerating {
		return fmt.Errorf("regenerate prompts for agent %q: %w", stored.Slug, ErrGenerationInFlight)
	}
	// Resolved before the generating stamp so an unresolvable provider fails
	// without moving the status: nothing ran, so nothing is in flight.
	provider, err := s.store.Providers().ByID(ctx, stored.ProviderID)
	if err != nil {
		return fmt.Errorf("regenerate prompts: resolve provider %q: %w", stored.ProviderID, err)
	}

	if _, err := s.setStatus(ctx, *stored, store.PromptsStatusGenerating, ""); err != nil {
		return err
	}
	params := promptgen.Params{
		Provider:    *provider,
		Model:       stored.Model,
		AgentName:   stored.Name,
		Brief:       stored.Brief,
		Instruction: instruction,
	}
	if _, err := s.prompts.Regenerate(ctx, stored.Slug, params); err != nil {
		return s.failPrompts(ctx, *stored, err)
	}
	_, err = s.setStatus(ctx, *stored, store.PromptsStatusReady, "")
	return err
}

// SweepInterruptedGenerations moves every agent stuck in the generating
// prompt status — orphaned by a crash mid-generation — to failed with the
// canonical interrupted reason (design D4). It runs once at startup, after
// the store opens and before services accept calls; ready and failed rows
// are untouched, and no row is left in generating.
func (s *AgentsService) SweepInterruptedGenerations(ctx context.Context) error {
	agents, err := s.store.Agents().List(ctx)
	if err != nil {
		return fmt.Errorf("sweep interrupted prompt generations: %w", err)
	}
	for _, agent := range agents {
		if agent.PromptsStatus != store.PromptsStatusGenerating {
			continue
		}
		if _, err := s.setStatus(ctx, agent, store.PromptsStatusFailed, interruptedGenerationReason); err != nil {
			return fmt.Errorf("sweep interrupted prompt generations: %w", err)
		}
	}
	return nil
}

// setStatus commits one prompt-status transition for the agent's row and
// returns the updated row (with the store-restamped UpdatedAt).
func (s *AgentsService) setStatus(ctx context.Context, agent store.Agent, status, promptErr string) (store.Agent, error) {
	agent.PromptsStatus = status
	agent.PromptsError = promptErr
	if err := s.store.Agents().Update(ctx, &agent); err != nil {
		return store.Agent{}, fmt.Errorf("commit %s prompt status for agent %q: %w", status, agent.Slug, err)
	}
	return agent, nil
}

// failCreate commits the failed prompt status with a sanitized reason and
// returns the surviving row alongside the ErrPromptGeneration report.
func (s *AgentsService) failCreate(ctx context.Context, agent store.Agent, genErr error) (store.Agent, error) {
	reason := promptgen.SanitizeError(genErr)
	agent, err := s.setStatus(ctx, agent, store.PromptsStatusFailed, reason)
	if err != nil {
		return store.Agent{}, fmt.Errorf("%w (generation error: %v)", err, genErr)
	}
	return agent, fmt.Errorf("%w: %s", ErrPromptGeneration, reason)
}

// failPrompts commits the failed prompt status with a sanitized reason and
// returns the failure wrapped in ErrPromptGeneration.
func (s *AgentsService) failPrompts(ctx context.Context, agent store.Agent, genErr error) error {
	reason := promptgen.SanitizeError(genErr)
	if _, err := s.setStatus(ctx, agent, store.PromptsStatusFailed, reason); err != nil {
		return fmt.Errorf("%w (generation error: %v)", err, genErr)
	}
	return fmt.Errorf("%w: %s", ErrPromptGeneration, reason)
}

func validateAgentName(name string) error {
	if name == "" {
		return errors.New("agent name must not be empty")
	}
	return nil
}

// validateAgentEffort rejects a default reasoning effort the selected model
// does not support (spec agent-management: the effort control offers only
// the model's supported options). The empty effort is the cleared/inherit
// state and always valid.
func validateAgentEffort(providerType, model, effort string) error {
	if effort == "" {
		return nil
	}
	for _, supported := range agents.Efforts(providerType, model) {
		if supported == effort {
			return nil
		}
	}
	return fmt.Errorf("model %q does not support reasoning effort %q", model, effort)
}
