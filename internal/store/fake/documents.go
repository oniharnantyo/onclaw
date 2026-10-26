package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type documentStore struct{ s *Store }

func (x documentStore) PutDocument(_ context.Context, doc store.Document) (store.Document, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	if doc.ID == "" {
		doc.ID = nextID()
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = nowUTC()
	}
	// Mirrors the sqlite composite UNIQUE (scope, agent_slug, filename):
	// the per-scope filename uniqueness.
	for _, other := range x.s.documents {
		if other.Scope == doc.Scope && other.AgentSlug == doc.AgentSlug && other.Filename == doc.Filename {
			return store.Document{}, fmt.Errorf("put document %s: %w", doc.Filename, store.ErrDuplicate)
		}
	}
	x.s.documents[doc.ID] = doc
	return doc, nil
}

func (x documentStore) ListDocuments(_ context.Context) ([]store.Document, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	out := make([]store.Document, 0, len(x.s.documents))
	for _, doc := range x.s.documents {
		out = append(out, doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out, nil
}

func (x documentStore) GetDocument(_ context.Context, id string) (store.Document, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	doc, ok := x.s.documents[id]
	if !ok {
		return store.Document{}, fmt.Errorf("document %s: %w", id, store.ErrNotFound)
	}
	return doc, nil
}

func (x documentStore) DocumentByName(_ context.Context, scope, agentSlug, filename string) (store.Document, bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	for _, doc := range x.s.documents {
		if doc.Scope == scope && doc.AgentSlug == agentSlug && doc.Filename == filename {
			return doc, true, nil
		}
	}
	return store.Document{}, false, nil
}

func (x documentStore) UpdateDocumentScope(_ context.Context, id, scope, agentID, agentSlug string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	doc, ok := x.s.documents[id]
	if !ok {
		return fmt.Errorf("document %s: %w", id, store.ErrNotFound)
	}
	// Mirrors the sqlite composite UNIQUE: the target scope must not already
	// hold another document of the same name.
	for _, other := range x.s.documents {
		if other.ID != id && other.Scope == scope && other.AgentSlug == agentSlug && other.Filename == doc.Filename {
			return fmt.Errorf("update document scope %s: %w", id, store.ErrDuplicate)
		}
	}
	doc.Scope, doc.AgentID, doc.AgentSlug = scope, agentID, agentSlug
	x.s.documents[id] = doc
	return nil
}

func (x documentStore) UpdateDocumentDescription(_ context.Context, id, description string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	doc, ok := x.s.documents[id]
	if !ok {
		return fmt.Errorf("document %s: %w", id, store.ErrNotFound)
	}
	doc.Description = description
	x.s.documents[id] = doc
	return nil
}

func (x documentStore) DeleteDocument(_ context.Context, id string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	if _, ok := x.s.documents[id]; !ok {
		return fmt.Errorf("document %s: %w", id, store.ErrNotFound)
	}
	delete(x.s.documents, id)
	return nil
}

// deleteDocuments removes every agent-scoped document of the agent — the
// cascade on the documents FK (add-knowledge-corpus D2). Callers hold s.mu.
func (s *Store) deleteDocuments(agentID string) {
	for id, doc := range s.documents {
		if doc.AgentID == agentID {
			delete(s.documents, id)
		}
	}
}
