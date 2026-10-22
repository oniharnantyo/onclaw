package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// TestAttachmentStore_PutAndByKey covers the upload registry (design D5): a
// put registers the transcript metadata under the capability key, the read
// returns it verbatim, and an unknown key is ErrNotFound.
func TestAttachmentStore_PutAndByKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	att := store.Attachment{Key: "0123456789abcdef0123456789abcdef", Name: "shot.png", Mime: "image/png", Size: 1234}
	require.NoError(t, s.Attachments().Put(ctx, att))

	got, err := s.Attachments().ByKey(ctx, att.Key)
	require.NoError(t, err)
	require.Equal(t, att, *got)

	_, err = s.Attachments().ByKey(ctx, "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestAttachmentStore_EmptyKeyRejected pins the registry's only input rule:
// a row without a capability key cannot exist.
func TestAttachmentStore_EmptyKeyRejected(t *testing.T) {
	s := newTestStore(t)

	err := s.Attachments().Put(context.Background(), store.Attachment{Name: "x"})
	require.Error(t, err)
}
