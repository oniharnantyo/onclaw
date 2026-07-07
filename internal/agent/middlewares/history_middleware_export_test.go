package middlewares

import (
	"context"
)

const PersistedKey = persistedKey

func GetRunCursor(ctx context.Context) (*RunCursor, bool) {
	cursor, ok := ctx.Value(cursorKey).(*RunCursor)
	return cursor, ok
}
