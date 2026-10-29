package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type agentStore struct{ s *Store }

func (a agentStore) Create(_ context.Context, agent *store.Agent) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	if agent.ID == "" {
		agent.ID = nextID()
	}
	if agent.PromptsStatus == "" {
		// Mirrors the sqlite adapter: a new agent's document lifecycle
		// starts at ready unless the caller says otherwise.
		agent.PromptsStatus = store.PromptsStatusReady
	}
	// Mirrors the sqlite UNIQUE index on slug: NULLs (empty slugs) stay
	// distinct, non-empty duplicates are rejected.
	if err := a.slugConflict(agent.ID, agent.Slug); err != nil {
		return err
	}
	if _, exists := a.s.agents[agent.ID]; exists {
		return fmt.Errorf("agent %s already exists", agent.ID)
	}
	stamp(&agent.CreatedAt, &agent.UpdatedAt)
	a.s.agents[agent.ID] = cloneAgent(*agent)
	return nil
}

func (a agentStore) ByID(_ context.Context, id string) (*store.Agent, error) {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	agent, ok := a.s.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %s: %w", id, store.ErrNotFound)
	}
	out := cloneAgent(agent)
	return &out, nil
}

func (a agentStore) List(_ context.Context) ([]store.Agent, error) {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	out := make([]store.Agent, 0, len(a.s.agents))
	for _, agent := range a.s.agents {
		out = append(out, cloneAgent(agent))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (a agentStore) Update(_ context.Context, agent *store.Agent) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	stored, ok := a.s.agents[agent.ID]
	if !ok {
		return fmt.Errorf("agent %s: %w", agent.ID, store.ErrNotFound)
	}
	if agent.PromptsStatus == "" {
		agent.PromptsStatus = store.PromptsStatusReady
	}
	// Mirrors the sqlite UNIQUE index on slug (see Create).
	if err := a.slugConflict(agent.ID, agent.Slug); err != nil {
		return err
	}
	agent.CreatedAt = stored.CreatedAt
	agent.UpdatedAt = nowUTC()
	a.s.agents[agent.ID] = cloneAgent(*agent)
	return nil
}

func (a agentStore) Delete(_ context.Context, id string) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()

	if _, ok := a.s.agents[id]; !ok {
		return fmt.Errorf("agent %s: %w", id, store.ErrNotFound)
	}
	delete(a.s.agents, id)
	// Mirrors the sqlite FK cascade: the agent's hook assignments go too.
	a.s.deleteAgentHooks(id)
	// Mirrors the sqlite FK cascade (add-scheduler design D9): the agent's
	// schedules go too.
	a.s.deleteSchedules(id)
	// Mirrors the sqlite FK cascade (add-knowledge-corpus design D2): the
	// agent's scoped knowledge documents go too.
	a.s.deleteDocuments(id)
	// Mirrors the sqlite FK cascade (add-agent-memory design D1): the
	// agent's derived memory rows go too.
	a.s.deleteMemory(id)
	return nil
}

// slugConflict rejects a non-empty slug already held by another agent,
// mirroring the sqlite adapter's UNIQUE index on agents.slug. The caller
// must hold a.s.mu (the store mutex is not reentrant).
func (a agentStore) slugConflict(id, slug string) error {
	if slug == "" {
		return nil
	}
	for otherID, other := range a.s.agents {
		if otherID != id && other.Slug == slug {
			return fmt.Errorf("agent slug %q already exists", slug)
		}
	}
	return nil
}
