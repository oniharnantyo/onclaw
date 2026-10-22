// Package attachments implements the local attachment store (design D5,
// spec chat-attachments "Capability-gated storage"): uploaded files land
// under the attachments directory named by an unguessable capability key,
// their transcript metadata is registered in the store's attachments table,
// and content is served only by key. Raw filesystem paths never cross the
// boundary — the capability key is the only address any caller sees.
package attachments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Attachment lanes (spec chat-attachments "Attachment selection and limits"):
// the per-message file count cap and each lane's size cap. A file whose mime
// falls in no lane is rejected.
const (
	// MaxPerMessage caps the attachments one message may carry; enforced on
	// the send path (the runtime), not per upload.
	MaxPerMessage = 4
	// MaxImageBytes caps image/* uploads at 5 MB.
	MaxImageBytes = 5 << 20
	// MaxPDFBytes caps application/pdf uploads at 20 MB.
	MaxPDFBytes = 20 << 20
	// MaxVideoBytes caps video/* uploads at 20 MB.
	MaxVideoBytes = 20 << 20
	// MaxTextBytes caps inline text/* uploads at 1 MB.
	MaxTextBytes = 1 << 20
)

// Lane names the attachment kinds the store accepts.
const (
	LaneImage = "image"
	LanePDF   = "pdf"
	LaneVideo = "video"
	LaneText  = "text"
)

// filePerm is the stored file mode, matching the app layout's private-to-user
// files (internal/config).
const filePerm os.FileMode = 0600

// ErrRejected wraps every upload rejection: oversize, unknown lane, or a
// send over the per-message cap. Callers surface the wrapped reason.
var ErrRejected = errors.New("attachment rejected")

// keyShape matches exactly the keys this store issues: 32 lowercase hex
// characters. Anything else is refused before the filesystem is touched, so
// a key can never smuggle a path.
var keyShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Store manages the attachments directory and its registry rows. It is safe
// for concurrent use: every method touches distinct key-derived paths.
type Store struct {
	dir  string
	rows store.AttachmentStore
}

// NewStore returns an attachment store rooting files at dir and registering
// metadata rows through rows (the store's attachments table). The directory
// must exist (the home-layout bootstrap creates it).
func NewStore(dir string, rows store.AttachmentStore) *Store {
	return &Store{dir: dir, rows: rows}
}

// Save stores one upload: the bytes land at <dir>/<key> and the metadata row
// is registered under the same capability key. Lane classification and size
// caps run first — an over-cap or unknown-lane file is rejected with the
// reason (spec: rejected WITH a reason), leaving nothing behind.
func (s *Store) Save(ctx context.Context, name, mime string, data []byte) (sessions.Attachment, error) {
	lane, limit, err := laneOf(mime)
	if err != nil {
		return sessions.Attachment{}, err
	}
	if int64(len(data)) > limit {
		return sessions.Attachment{}, fmt.Errorf("%w: %s attachments are limited to %s (got %s)",
			ErrRejected, lane, humanBytes(limit), humanBytes(int64(len(data))))
	}

	key, err := newCapabilityKey()
	if err != nil {
		return sessions.Attachment{}, fmt.Errorf("save attachment: %w", err)
	}

	// The file lands first; a registry failure removes it so a saved key
	// always resolves. (The inverse order would orphan a registered key with
	// no content.)
	path := filepath.Join(s.dir, key)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return sessions.Attachment{}, fmt.Errorf("save attachment: %w", err)
	}
	att := store.Attachment{Key: key, Name: name, Mime: mime, Size: int64(len(data))}
	if err := s.rows.Put(ctx, att); err != nil {
		_ = os.Remove(path)
		return sessions.Attachment{}, fmt.Errorf("save attachment: %w", err)
	}
	return sessions.Attachment{Key: key, Name: name, Mime: mime, Size: int64(len(data))}, nil
}

// Content is one attachment's stored content: the registry metadata plus the
// raw bytes read from disk.
type Content struct {
	Meta sessions.Attachment
	Data []byte
}

// Read resolves one capability key to its metadata and bytes. An unknown key
// (or anything that is not a key this store issues) is refused; the on-disk
// address is never exposed.
func (s *Store) Read(ctx context.Context, key string) (Content, error) {
	if !keyShape.MatchString(key) {
		return Content{}, fmt.Errorf("read attachment: %w", store.ErrNotFound)
	}
	row, err := s.rows.ByKey(ctx, key)
	if err != nil {
		return Content{}, fmt.Errorf("read attachment: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, key))
	if err != nil {
		return Content{}, fmt.Errorf("read attachment %s: %w", key, err)
	}
	return Content{
		Meta: sessions.Attachment{Key: row.Key, Name: row.Name, Mime: row.Mime, Size: row.Size},
		Data: data,
	}, nil
}

// Resolve returns the registered metadata for one capability key without
// reading the bytes; the send path validates keys against it. Unknown keys
// are refused.
func (s *Store) Resolve(ctx context.Context, key string) (sessions.Attachment, error) {
	if !keyShape.MatchString(key) {
		return sessions.Attachment{}, fmt.Errorf("resolve attachment: %w", store.ErrNotFound)
	}
	row, err := s.rows.ByKey(ctx, key)
	if err != nil {
		return sessions.Attachment{}, fmt.Errorf("resolve attachment: %w", err)
	}
	return sessions.Attachment{Key: row.Key, Name: row.Name, Mime: row.Mime, Size: row.Size}, nil
}

// laneOf classifies an upload's mime into its lane and size cap (spec
// chat-attachments "Attachment selection and limits"). An unknown lane is a
// rejection with the reason.
func laneOf(mime string) (lane string, limit int64, err error) {
	switch mime {
	case "application/pdf":
		return LanePDF, MaxPDFBytes, nil
	}
	if prefix, ok := mimePrefix(mime); ok {
		switch prefix {
		case "image/":
			return LaneImage, MaxImageBytes, nil
		case "video/":
			return LaneVideo, MaxVideoBytes, nil
		case "text/":
			return LaneText, MaxTextBytes, nil
		}
	}
	return "", 0, fmt.Errorf("%w: unsupported file type %q (images, PDFs, videos, and text files are accepted)", ErrRejected, mime)
}

// mimePrefix splits "type/subtype" at the slash.
func mimePrefix(mime string) (string, bool) {
	for i := 0; i < len(mime); i++ {
		if mime[i] == '/' {
			return mime[:i+1], true
		}
	}
	return "", false
}

// humanBytes renders a byte count for rejection reasons.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// newCapabilityKey issues one unguessable capability key: 128 bits of random
// hex, the store-assigned id convention.
func newCapabilityKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate capability key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
