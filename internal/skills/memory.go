package skills

import (
	"context"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// memoryStore is an in-memory Store: the package-local test fake. It will
// be superseded by the fake in internal/store/fake once
// store.WorkspaceSkillStore lands; it is exported so handler-layer tests
// written in parallel can use it until then.
type memoryStore struct {
	mu     sync.RWMutex
	rows   map[string]*Skill // key: workspaceID + ":" + name
	now    func() time.Time
	nextID int
}

// NewMemoryStore builds an in-memory skill Store.
func NewMemoryStore() Store {
	return &memoryStore{rows: map[string]*Skill{}, now: time.Now}
}

func key(workspaceID, name string) string { return workspaceID + ":" + name }

func (m *memoryStore) Create(ctx context.Context, skill *Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(skill.WorkspaceID, skill.Name)
	if _, exists := m.rows[k]; exists {
		return domain.ErrConflict
	}
	stored := *skill
	m.rows[k] = &stored
	return nil
}

func (m *memoryStore) Get(ctx context.Context, workspaceID, name string) (*Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[key(workspaceID, name)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	stored := *row
	return &stored, nil
}

func (m *memoryStore) list(workspaceID string, enabledOnly bool) []Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var rows []Skill
	for _, row := range m.rows {
		if row.WorkspaceID != workspaceID {
			continue
		}
		if enabledOnly && !row.Enabled {
			continue
		}
		stored := *row
		rows = append(rows, stored)
	}
	return rows
}

func (m *memoryStore) List(ctx context.Context, workspaceID string) ([]Skill, error) {
	return m.list(workspaceID, false), nil
}

func (m *memoryStore) ListEnabled(ctx context.Context, workspaceID string) ([]Skill, error) {
	return m.list(workspaceID, true), nil
}

func (m *memoryStore) SetEnabled(ctx context.Context, workspaceID, name string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[key(workspaceID, name)]
	if !ok {
		return domain.ErrNotFound
	}
	row.Enabled = enabled
	row.UpdatedAt = m.now()
	return nil
}

func (m *memoryStore) Update(ctx context.Context, skill *Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(skill.WorkspaceID, skill.Name)
	if _, ok := m.rows[k]; !ok {
		return domain.ErrNotFound
	}
	stored := *skill
	m.rows[k] = &stored
	return nil
}

func (m *memoryStore) Delete(ctx context.Context, workspaceID, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(workspaceID, name)
	if _, ok := m.rows[k]; !ok {
		return domain.ErrNotFound
	}
	delete(m.rows, k)
	return nil
}
