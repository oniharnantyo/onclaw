package fake

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// -------------------------------------------------------------------------
// AttachmentStore implementation (add-chat-attachments D3/D16). FK parity
// mirrors the postgres adapter: Create requires the workspace and the
// creator to exist. ByStorageKey is deliberately global — the capability
// key is the bearer token for serving, not a tenancy predicate.
// -------------------------------------------------------------------------

func cloneAttachment(a *domain.Attachment) *domain.Attachment {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

type attachmentStore struct {
	s *fakeStore
}

func (as *attachmentStore) Create(ctx context.Context, a *domain.Attachment) error {
	if a == nil || a.WorkspaceID == "" || a.StorageKey == "" || a.CreatedBy == "" {
		return domain.ErrInvalid
	}

	as.s.mu.Lock()
	defer as.s.mu.Unlock()

	if _, exists := as.s.workspaces[a.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if _, exists := as.s.users[a.CreatedBy]; !exists {
		return fmt.Errorf("%w: user not found", domain.ErrNotFound)
	}

	if a.ID != "" {
		if _, exists := as.s.attachments[a.ID]; exists {
			return fmt.Errorf("%w: attachment with id %q already exists", domain.ErrConflict, a.ID)
		}
	} else {
		a.ID = uuid.NewString()
	}

	a.CreatedAt = time.Now().UTC()

	as.s.attachments[a.ID] = cloneAttachment(a)
	as.s.attachmentsByStorageKey[a.StorageKey] = a.ID
	return nil
}

func (as *attachmentStore) ByID(ctx context.Context, workspaceID, id string) (*domain.Attachment, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	a, exists := as.s.attachments[id]
	if !exists || a.WorkspaceID != workspaceID {
		// Foreign and unknown are indistinguishable (tenancy requirement).
		return nil, domain.ErrNotFound
	}
	return cloneAttachment(a), nil
}

func (as *attachmentStore) ByStorageKey(ctx context.Context, key string) (*domain.Attachment, error) {
	if key == "" {
		return nil, domain.ErrNotFound
	}

	as.s.mu.RLock()
	defer as.s.mu.RUnlock()

	id, exists := as.s.attachmentsByStorageKey[key]
	if !exists {
		return nil, domain.ErrNotFound
	}
	a, exists := as.s.attachments[id]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneAttachment(a), nil
}
