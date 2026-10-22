package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type providerModelStore struct{ s *Store }

func (p *providerModelStore) Create(_ context.Context, model *store.ProviderModel) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	for _, m := range p.s.providerModels {
		if m.ProviderID == model.ProviderID && m.ModelID == model.ModelID {
			return fmt.Errorf("create provider model: %w", store.ErrDuplicate)
		}
	}

	if model.ID == 0 {
		model.ID = p.s.nextModelID
		p.s.nextModelID++
	} else if model.ID >= p.s.nextModelID {
		p.s.nextModelID = model.ID + 1
	}

	stamp(&model.CreatedAt, &model.UpdatedAt)
	p.s.providerModels[model.ID] = cloneProviderModel(*model)
	return nil
}

func (p *providerModelStore) ByID(_ context.Context, id int64) (*store.ProviderModel, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	m, ok := p.s.providerModels[id]
	if !ok {
		return nil, fmt.Errorf("provider model %d: %w", id, store.ErrNotFound)
	}
	out := cloneProviderModel(m)
	return &out, nil
}

func (p *providerModelStore) ByProvider(_ context.Context, providerID string) ([]*store.ProviderModel, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	out := make([]*store.ProviderModel, 0)
	for _, m := range p.s.providerModels {
		if m.ProviderID == providerID {
			copyModel := cloneProviderModel(m)
			out = append(out, &copyModel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (p *providerModelStore) Update(_ context.Context, model *store.ProviderModel) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	stored, ok := p.s.providerModels[model.ID]
	if !ok {
		return fmt.Errorf("provider model %d: %w", model.ID, store.ErrNotFound)
	}

	for _, m := range p.s.providerModels {
		if m.ID != model.ID && m.ProviderID == model.ProviderID && m.ModelID == model.ModelID {
			return fmt.Errorf("update provider model %d: %w", model.ID, store.ErrDuplicate)
		}
	}

	model.CreatedAt = stored.CreatedAt
	model.UpdatedAt = nowUTC()
	p.s.providerModels[model.ID] = cloneProviderModel(*model)
	return nil
}

func (p *providerModelStore) Delete(_ context.Context, id int64) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if _, ok := p.s.providerModels[id]; !ok {
		return fmt.Errorf("delete provider model %d: %w", id, store.ErrNotFound)
	}
	delete(p.s.providerModels, id)
	return nil
}

func (p *providerModelStore) DeleteByProvider(_ context.Context, providerID string) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	p.s.deleteProviderModelsLocked(providerID)
	return nil
}

func (p *providerModelStore) UpsertMissing(_ context.Context, models []*store.ProviderModel) (int, error) {
	if len(models) == 0 {
		return 0, nil
	}
	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	inserted := 0
	for _, m := range models {
		exists := false
		for _, stored := range p.s.providerModels {
			if stored.ProviderID == m.ProviderID && stored.ModelID == m.ModelID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}

		if m.ID == 0 {
			m.ID = p.s.nextModelID
			p.s.nextModelID++
		} else if m.ID >= p.s.nextModelID {
			p.s.nextModelID = m.ID + 1
		}
		stamp(&m.CreatedAt, &m.UpdatedAt)
		p.s.providerModels[m.ID] = cloneProviderModel(*m)
		inserted++
	}
	return inserted, nil
}

// cloneProviderModel copies a model row including its Efforts slice so the
// stored value never aliases a caller's backing array (see clone.go).
func cloneProviderModel(in store.ProviderModel) store.ProviderModel {
	in.Efforts = cloneStrings(in.Efforts)
	return in
}
