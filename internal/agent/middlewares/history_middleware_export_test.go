package middlewares

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

const PersistedKey = persistedKey

func GetRunCursor(ctx context.Context) (*RunCursor, bool) {
	cursor, ok := ctx.Value(cursorKey).(*RunCursor)
	return cursor, ok
}

func (h *HistoryMiddleware) SaveMessage(ctx context.Context, msg *schema.AgenticMessage) (int64, error) {
	return h.saveMessage(ctx, msg)
}

func (h *HistoryMiddleware) SaveUnmarkedMessages(ctx context.Context, stateMessages []*schema.AgenticMessage) error {
	return h.saveUnmarkedMessages(ctx, stateMessages)
}
