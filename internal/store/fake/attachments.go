package fake

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type attachmentStore struct{ s *Store }

func (x attachmentStore) Put(_ context.Context, att store.Attachment) error {
	if att.Key == "" {
		return fmt.Errorf("put attachment: empty key")
	}

	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	x.s.attachments[att.Key] = att
	return nil
}

func (x attachmentStore) ByKey(_ context.Context, key string) (*store.Attachment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	att, ok := x.s.attachments[key]
	if !ok {
		return nil, fmt.Errorf("attachment %s: %w", key, store.ErrNotFound)
	}
	return &att, nil
}
