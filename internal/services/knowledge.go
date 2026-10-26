package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/store"
)

// KnowledgeDocument is one knowledge document's bound view (add-knowledge-
// corpus design D8): the catalog row the Knowledge menu and the chat right
// bar's knowledge panel list, preview, and manage.
type KnowledgeDocument struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	MIME        string    `json:"mime"`
	Size        int64     `json:"size"`
	Scope       string    `json:"scope"`
	AgentID     string    `json:"agentId"`
	AgentSlug   string    `json:"agentSlug"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"createdAt"`
}

// KnowledgeService is the bound service over the knowledge corpus (design
// D8): one service backs both management surfaces (the Knowledge menu and the
// right bar's knowledge panel). Every mutation is announced through the
// emitter as EventKnowledgeChanged so open surfaces re-list.
type KnowledgeService struct {
	k       *knowledge.Store
	agents  store.AgentStore
	emitter Emitter
}

// NewKnowledgeService returns the knowledge service over the corpus store,
// resolving agent-scope targets through the agents store and announcing every
// mutation through emitter.
func NewKnowledgeService(k *knowledge.Store, agents store.AgentStore, emitter Emitter) *KnowledgeService {
	return &KnowledgeService{k: k, agents: agents, emitter: emitter}
}

// List returns every cataloged document. The result is empty (not nil) when
// the corpus is empty and never contains the scope indexes (they are not
// cataloged).
func (s *KnowledgeService) List(ctx context.Context) ([]KnowledgeDocument, error) {
	docs, err := s.k.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	views := make([]KnowledgeDocument, 0, len(docs))
	for _, doc := range docs {
		views = append(views, newKnowledgeDocument(doc))
	}
	return views, nil
}

// Upload stores one uploaded document (design D8). data is the base64-encoded
// file content (the chat-attachments upload pattern); scope is global or
// agent, and an agent scope requires a known agentID, resolved to its slug
// here. The corpus gate (formats and size caps) rejects unsupported or
// oversize documents with the reason; duplicate names within one scope are
// rejected, never overwritten.
func (s *KnowledgeService) Upload(ctx context.Context, filename, mime, data, scope, agentID, description string) (KnowledgeDocument, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("upload document: decode payload: %w", err)
	}
	agentSlug, err := s.resolveAgent(ctx, scope, agentID)
	if err != nil {
		return KnowledgeDocument{}, err
	}
	doc, err := s.k.Save(ctx, knowledge.SaveInput{
		Filename:    filename,
		MIME:        mime,
		Data:        raw,
		Scope:       scope,
		AgentID:     agentID,
		AgentSlug:   agentSlug,
		Description: description,
		Source:      knowledge.SourceUser,
	})
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("upload document: %w", err)
	}
	go s.emitter.Emit(EventKnowledgeChanged, KnowledgeChangedEvent{})
	return newKnowledgeDocument(doc), nil
}

// Delete removes one document — catalog row and stored file; an unknown id
// wraps store.ErrNotFound.
func (s *KnowledgeService) Delete(ctx context.Context, id string) error {
	if err := s.k.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	go s.emitter.Emit(EventKnowledgeChanged, KnowledgeChangedEvent{})
	return nil
}

// SetScope promotes or demotes one document (design D6): scope global
// promotes (agentID is ignored); scope agent demotes to the agent resolved
// from agentID. The file move and the catalog flip are one store operation.
func (s *KnowledgeService) SetScope(ctx context.Context, id, scope, agentID string) (KnowledgeDocument, error) {
	agentSlug, err := s.resolveAgent(ctx, scope, agentID)
	if err != nil {
		return KnowledgeDocument{}, err
	}
	doc, err := s.k.SetScope(ctx, id, scope, agentID, agentSlug)
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("set document scope: %w", err)
	}
	go s.emitter.Emit(EventKnowledgeChanged, KnowledgeChangedEvent{})
	return newKnowledgeDocument(doc), nil
}

// SetDescription rewrites one document's description — the primary discovery
// handle for PDFs under raw storage.
func (s *KnowledgeService) SetDescription(ctx context.Context, id, description string) (KnowledgeDocument, error) {
	doc, err := s.k.SetDescription(ctx, id, description)
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("set document description: %w", err)
	}
	go s.emitter.Emit(EventKnowledgeChanged, KnowledgeChangedEvent{})
	return newKnowledgeDocument(doc), nil
}

// Content returns one document's base64-encoded bytes for preview (the
// chat-attachments serving pattern); the frontend renders text as text and
// PDFs as documents. An unknown id wraps store.ErrNotFound.
func (s *KnowledgeService) Content(ctx context.Context, id string) (string, error) {
	data, err := s.k.Content(ctx, id)
	if err != nil {
		return "", fmt.Errorf("document content: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// resolveAgent validates the scope vocabulary and resolves an agent scope's
// slug from its id; a global scope clears the agent. An unknown agent wraps
// store.ErrNotFound.
func (s *KnowledgeService) resolveAgent(ctx context.Context, scope, agentID string) (string, error) {
	switch scope {
	case knowledge.ScopeGlobal:
		return "", nil
	case knowledge.ScopeAgent:
		agent, err := s.agents.ByID(ctx, agentID)
		if err != nil {
			return "", fmt.Errorf("resolve agent %s: %w", agentID, err)
		}
		return agent.Slug, nil
	default:
		return "", fmt.Errorf("unknown document scope %q", scope)
	}
}

// newKnowledgeDocument maps one catalog row onto the bound view.
func newKnowledgeDocument(doc store.Document) KnowledgeDocument {
	return KnowledgeDocument{
		ID:          doc.ID,
		Filename:    doc.Filename,
		MIME:        doc.MIME,
		Size:        doc.Size,
		Scope:       doc.Scope,
		AgentID:     doc.AgentID,
		AgentSlug:   doc.AgentSlug,
		Description: doc.Description,
		Source:      doc.Source,
		CreatedAt:   doc.CreatedAt,
	}
}
