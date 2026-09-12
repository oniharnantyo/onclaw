package teams

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// SlotBinding binds one template slot during materialization: exactly one of
// AgentID (an existing workspace agent) or Spawn (create a new one) is set.
type SlotBinding struct {
	AgentID string
	Spawn   bool
}

// MaterializeRequest is one template materialization: the channel identity
// (name, slug, purpose) plus a binding for every slot of the template.
type MaterializeRequest struct {
	TemplateID string
	Name       string
	Slug       string
	Purpose    string
	Slots      map[string]SlotBinding
}

// MaterializeResult reports what one materialization created: the channel,
// its full roster (including the human creator), and the agents spawned for
// spawn-bound slots.
type MaterializeResult struct {
	Channel       domain.Channel
	Members       []domain.ChannelMember
	CreatedAgents []domain.Agent
}

// FieldError is one fielded validation failure.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError is the fielded validation failure the materialize payload
// produces (missing slot bindings, malformed bindings, blank identity fields).
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return "validation failed"
	}
	return e.Fields[0].Field + ": " + e.Fields[0].Message
}

// SpawnAgentRequest is what the materializer hands its agent creation
// collaborator for one spawn-bound slot. When ProviderID is empty the
// collaborator resolves the workspace's default provider — the collaborator
// owns that policy, not the template layer.
type SpawnAgentRequest struct {
	WorkspaceID string
	UserID      string
	Name        string
	Slug        string
	Role        string
	Description string
	Brief       string
	ProviderID  string
	Model       string
}

// AgentCreator spawns agents through the production creation path — the same
// slug pre-check, workspace seeding, role-informed prompt generation, and
// persistence the agents REST handler runs. Implemented by the server layer
// (internal/server/handlers) around the shared creation collaborator.
type AgentCreator interface {
	SpawnAgent(ctx context.Context, req SpawnAgentRequest) (domain.Agent, error)
}

// Materializer materializes built-in templates into channels (design D6).
//
// Partial-failure policy: the operation spans the filesystem-backed agent
// creation path (synchronous prompt generation) and multiple channel writes,
// so no single store transaction can cover it. Failures fail fast where
// cheap (validation, existing-agent resolution) and degrade to documented
// best-effort afterwards: agents spawned before a later failure persist (they
// are first-class workspace agents and harmless), and roster rows added
// before a failure persist.
type Materializer struct {
	channels store.ChannelStore
	agents   store.AgentStore
	creator  AgentCreator
}

// NewMaterializer creates the materializer from its positional dependencies:
// the channel store (channel + roster writes), the agent store (binding
// verification and spawn-slug availability), and the agent creation
// collaborator.
func NewMaterializer(channelStore store.ChannelStore, agents store.AgentStore, creator AgentCreator) *Materializer {
	return &Materializer{channels: channelStore, agents: agents, creator: creator}
}

// Materialize validates the request, creates the channel with the template's
// conventions prefill, adds the creating user as a human member, and binds
// every slot: spawn-bound slots create an agent through the collaborator;
// existing-bound slots verify the agent belongs to the workspace. The
// facilitator slot materializes with the facilitator roster role (design D2).
func (m *Materializer) Materialize(ctx context.Context, workspaceID, userID string, req MaterializeRequest) (MaterializeResult, error) {
	template, ok := Get(req.TemplateID)
	if !ok {
		return MaterializeResult{}, fmt.Errorf("%w: team template %q does not exist", domain.ErrNotFound, req.TemplateID)
	}

	name := strings.TrimSpace(req.Name)
	slug := strings.TrimSpace(req.Slug)
	var fields []FieldError
	if name == "" {
		fields = append(fields, FieldError{Field: "name", Message: "name cannot be empty"})
	}
	if err := domain.ValidateChannelSlug(slug); err != nil {
		fields = append(fields, FieldError{Field: "slug", Message: err.Error()})
	}
	fields = append(fields, validateBindings(template, req.Slots)...)
	if len(fields) > 0 {
		return MaterializeResult{}, &ValidationError{Fields: fields}
	}

	// Fail fast on existing bindings before any write: an agent outside the
	// workspace (or missing) is a 404 with nothing left behind.
	for _, slot := range template.Slots {
		binding := req.Slots[slot.ID]
		if binding.Spawn {
			continue
		}
		if _, err := m.agents.ByID(ctx, workspaceID, binding.AgentID); err != nil {
			return MaterializeResult{}, fmt.Errorf("%w: agent for slot %q does not exist in this workspace", domain.ErrNotFound, slot.ID)
		}
	}

	channel := &domain.Channel{
		WorkspaceID: workspaceID,
		Name:        name,
		Slug:        slug,
		Purpose:     req.Purpose,
		Conventions: template.Conventions,
	}
	if userID != "" {
		createdBy := userID
		channel.CreatedBy = &createdBy
	}
	if err := m.channels.CreateChannel(ctx, channel); err != nil {
		return MaterializeResult{}, err
	}

	result := MaterializeResult{Channel: *channel}

	// The creating human joins the roster first: their replies resume paused
	// sessions (design D4), so the room is never agent-only.
	if userID != "" {
		member := &domain.ChannelMember{
			WorkspaceID: workspaceID,
			ChannelID:   channel.ID,
			MemberType:  domain.ChannelMemberTypeUser,
			UserID:      userID,
		}
		if err := m.channels.AddChannelMember(ctx, member); err != nil {
			return result, fmt.Errorf("add human creator to roster: %w", err)
		}
		result.Members = append(result.Members, *member)
	}

	for _, slot := range template.Slots {
		binding := req.Slots[slot.ID]

		agentID := binding.AgentID
		if binding.Spawn {
			spawned, err := m.spawnSlotAgent(ctx, workspaceID, userID, slot)
			if err != nil {
				return result, err
			}
			agentID = spawned.ID
			result.CreatedAgents = append(result.CreatedAgents, spawned)
		}

		role := domain.ChannelMemberRoleMember
		if slot.Facilitator {
			role = domain.ChannelMemberRoleFacilitator
		}
		member := &domain.ChannelMember{
			WorkspaceID:    workspaceID,
			ChannelID:      channel.ID,
			MemberType:     domain.ChannelMemberTypeAgent,
			AgentID:        agentID,
			Specialization: slot.Specialization,
			Role:           role,
		}
		if err := m.channels.AddChannelMember(ctx, member); err != nil {
			return result, fmt.Errorf("add agent for slot %q to roster: %w", slot.ID, err)
		}
		result.Members = append(result.Members, *member)
	}

	return result, nil
}

// spawnSlotAgent creates one slot's agent through the collaborator: the slot
// title is the agent's name, the specialization its description, and the
// role prompt hint its brief — the fields promptgen's identity generation
// consumes, so the generated IDENTITY/SOUL documents come out role-informed.
// The slug derives from the slot id with a numeric suffix when taken.
func (m *Materializer) spawnSlotAgent(ctx context.Context, workspaceID, userID string, slot TeamSlot) (domain.Agent, error) {
	slug, err := m.freeAgentSlug(ctx, workspaceID, slot.ID)
	if err != nil {
		return domain.Agent{}, err
	}
	spawned, err := m.creator.SpawnAgent(ctx, SpawnAgentRequest{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Name:        slot.Title,
		Slug:        slug,
		Role:        slot.Title,
		Description: slot.Specialization,
		Brief:       slot.RolePrompt,
	})
	if err != nil {
		return domain.Agent{}, fmt.Errorf("spawn agent for slot %q: %w", slot.ID, err)
	}
	return spawned, nil
}

// freeAgentSlug derives the spawned agent's slug from the slot id and appends
// -2, -3, … until a free slug in the workspace is found.
func (m *Materializer) freeAgentSlug(ctx context.Context, workspaceID, base string) (string, error) {
	candidate := base
	for i := 1; ; i++ {
		_, err := m.agents.BySlug(ctx, workspaceID, candidate)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return candidate, nil
			}
			return "", fmt.Errorf("check agent slug %q: %w", candidate, err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i+1)
	}
}

// validateBindings checks the slot map against the template: every slot must
// be bound, unknown slot ids are rejected, and each binding is exactly one of
// agent_id or spawn.
func validateBindings(template TeamTemplate, slots map[string]SlotBinding) []FieldError {
	var fields []FieldError

	bound := make(map[string]bool, len(slots))
	for id := range slots {
		if !template.hasSlot(id) {
			fields = append(fields, FieldError{Field: "slots." + id, Message: fmt.Sprintf("unknown slot %q for template %q", id, template.ID)})
		}
		bound[id] = true
	}

	var missing []string
	// One agent cannot occupy two slots: the roster forbids duplicate rows,
	// so a repeated binding is rejected before any write.
	byAgent := make(map[string]string, len(slots))
	for _, slot := range template.Slots {
		if !bound[slot.ID] {
			missing = append(missing, slot.ID)
			continue
		}
		binding := slots[slot.ID]
		set := 0
		if strings.TrimSpace(binding.AgentID) != "" {
			set++
		}
		if binding.Spawn {
			set++
		}
		if set != 1 {
			fields = append(fields, FieldError{Field: "slots." + slot.ID, Message: "exactly one of agent_id or spawn is required"})
			continue
		}
		if !binding.Spawn {
			// Only existing bindings carry an agent id — spawn slots share
			// the empty key and must not collide here.
			if prev, dup := byAgent[binding.AgentID]; dup {
				fields = append(fields, FieldError{Field: "slots." + slot.ID, Message: fmt.Sprintf("agent is already bound to slot %q; each slot binds a distinct agent", prev)})
				continue
			}
			byAgent[binding.AgentID] = slot.ID
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		fields = append(fields, FieldError{Field: "slots", Message: "every slot must be bound; missing: " + strings.Join(missing, ", ")})
	}
	return fields
}

// hasSlot reports whether id is one of the template's slot ids.
func (t TeamTemplate) hasSlot(id string) bool {
	for _, slot := range t.Slots {
		if slot.ID == id {
			return true
		}
	}
	return false
}
