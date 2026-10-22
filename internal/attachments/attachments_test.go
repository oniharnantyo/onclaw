package attachments

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// newTestStore roots an attachment store in a temp directory over the fake
// store's registry.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st := fake.New()
	return NewStore(t.TempDir(), st.Attachments())
}

// TestSave_AcceptsPerLane caps the accept path per lane (spec chat-attachments
// "Attachment selection and limits"): images, PDFs, videos, and text files
// store and round-trip through their capability key.
func TestSave_AcceptsPerLane(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name string
		mime string
		data []byte
	}{
		{"shot.png", "image/png", []byte("png-bytes")},
		{"doc.pdf", "application/pdf", []byte("%PDF-1.4")},
		{"clip.mp4", "video/mp4", []byte("mp4-bytes")},
		{"notes.txt", "text/plain", []byte("hello")},
	}
	for _, tc := range cases {
		att, err := s.Save(ctx, tc.name, tc.mime, tc.data)
		require.NoError(t, err, "%s must be accepted", tc.mime)
		require.NotEmpty(t, att.Key)
		require.Len(t, att.Key, 32, "the capability key is 128 bits of hex")
		require.Equal(t, tc.name, att.Name)
		require.Equal(t, tc.mime, att.Mime)
		require.Equal(t, int64(len(tc.data)), att.Size)

		content, err := s.Read(ctx, att.Key)
		require.NoError(t, err)
		require.Equal(t, tc.data, content.Data, "content round-trips")
		require.Equal(t, att, content.Meta)
	}
}

// TestSave_RejectsOverCap pins each lane's size cap with its reason (spec:
// rejected WITH a reason).
func TestSave_RejectsOverCap(t *testing.T) {
	s := newTestStore(t)

	cases := []struct {
		name string
		mime string
		size int
		want string
	}{
		{"big.png", "image/png", MaxImageBytes + 1, "5 MB"},
		{"big.pdf", "application/pdf", MaxPDFBytes + 1, "20 MB"},
		{"big.mp4", "video/mp4", MaxVideoBytes + 1, "20 MB"},
		{"big.txt", "text/plain", MaxTextBytes + 1, "1 MB"},
	}
	for _, tc := range cases {
		_, err := s.Save(context.Background(), tc.name, tc.mime, make([]byte, tc.size))
		require.ErrorIs(t, err, ErrRejected, "%s over the cap must be rejected", tc.mime)
		require.ErrorContains(t, err, tc.want, "the rejection carries the limit")
	}
}

// TestSave_RejectsUnknownLane pins the lane classifier: a mime in no lane is
// rejected with the reason and leaves nothing behind.
func TestSave_RejectsUnknownLane(t *testing.T) {
	s := newTestStore(t)

	att, err := s.Save(context.Background(), "x.bin", "application/octet-stream", []byte("nope"))
	require.ErrorIs(t, err, ErrRejected)
	require.ErrorContains(t, err, "application/octet-stream")
	require.Empty(t, att.Key)

	// Nothing was registered and no file landed.
	_, err = s.Read(context.Background(), att.Key)
	require.Error(t, err)
}

// TestRead_RefusesUnknownKey pins capability-gated serving (design D5):
// malformed keys and unregistered keys are refused alike, without touching
// the filesystem shape of the request.
func TestRead_RefusesUnknownKey(t *testing.T) {
	s := newTestStore(t)

	for _, key := range []string{
		"",
		"../../etc/passwd",
		"0123456789ABCDEF0123456789ABCDEF", // wrong shape (uppercase)
		"short",
		"ffffffffffffffffffffffffffffffff", // right shape, never uploaded
	} {
		_, err := s.Read(context.Background(), key)
		require.ErrorIs(t, err, store.ErrNotFound, "key %q must be refused as unknown", key)
	}
}

// TestResolve_RefusesUnknownKey covers the send-path metadata check.
func TestResolve_RefusesUnknownKey(t *testing.T) {
	s := newTestStore(t)

	_, err := s.Resolve(context.Background(), "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)

	att, err := s.Save(context.Background(), "a.txt", "text/plain", []byte("x"))
	require.NoError(t, err)
	got, err := s.Resolve(context.Background(), att.Key)
	require.NoError(t, err)
	require.Equal(t, att, got)
}

// TestSave_RegistryFailureLeavesNothing pins the save order: a failing
// registry write removes the file, so a key that resolves always has bytes.
func TestSave_RegistryFailureLeavesNothing(t *testing.T) {
	s := NewStore(t.TempDir(), failingAttachmentRegistry{})

	att, err := s.Save(context.Background(), "a.txt", "text/plain", []byte("x"))
	require.Error(t, err)
	require.Empty(t, att.Key)

	_, err = s.Read(context.Background(), att.Key)
	require.Error(t, err)
}

// failingAttachmentRegistry is a one-method registry that always fails.
type failingAttachmentRegistry struct{}

func (failingAttachmentRegistry) Put(_ context.Context, _ store.Attachment) error {
	return errRegistryDown
}

func (failingAttachmentRegistry) ByKey(_ context.Context, _ string) (*store.Attachment, error) {
	return nil, errRegistryDown
}

var errRegistryDown = errString("registry down")

type errString string

func (e errString) Error() string { return string(e) }

// TestKeysAreUnguessable pins the capability key property: two saves of the
// same content get different keys (spec: named by an opaque random key).
func TestKeysAreUnguessable(t *testing.T) {
	s := newTestStore(t)

	first, err := s.Save(context.Background(), "a.txt", "text/plain", []byte("same"))
	require.NoError(t, err)
	second, err := s.Save(context.Background(), "a.txt", "text/plain", []byte("same"))
	require.NoError(t, err)
	require.NotEqual(t, first.Key, second.Key)
	require.True(t, strings.Contains(first.Name, "a.txt"))
}
