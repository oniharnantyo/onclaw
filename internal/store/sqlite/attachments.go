package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type attachmentStore struct{ e executor }

func (s *attachmentStore) Put(ctx context.Context, att store.Attachment) error {
	if att.Key == "" {
		return fmt.Errorf("put attachment: empty key")
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO attachments (key, name, mime, size)
VALUES (?, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET name = excluded.name, mime = excluded.mime, size = excluded.size`,
		att.Key, att.Name, att.Mime, att.Size,
	)
	if err != nil {
		return fmt.Errorf("put attachment %s: %w", att.Key, err)
	}
	return nil
}

func (s *attachmentStore) ByKey(ctx context.Context, key string) (*store.Attachment, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT key, name, mime, size FROM attachments WHERE key = ?`, key)

	var att store.Attachment
	if err := row.Scan(&att.Key, &att.Name, &att.Mime, &att.Size); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("attachment %s: %w", key, store.ErrNotFound)
		}
		return nil, readErr("get attachment", err)
	}
	return &att, nil
}
