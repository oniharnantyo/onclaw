package agents

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The turn-time uploads mirror (fix-pdf-attachment-carriage design D2): every
// attachment riding a turn is copied — if absent — from the canonical
// attachments store into the agent workspace's uploads/ directory, so the
// filesystem tools can read uploaded content at /workspace/uploads/<name> and
// the pointer notes can address the final (possibly collision-suffixed) name.
// Mirroring is strictly best-effort: nothing here returns an error that could
// fail a turn — entries that cannot be mirrored are simply skipped, and the
// note falls back to the capability key alone.

// uploadFilePerm matches the app layout's private-to-user file mode
// (internal/config; the workspace documents use the same mode).
const uploadFilePerm os.FileMode = 0o600

// uploadDirPerm matches the app layout's private-to-user directory mode
// (internal/config; the workspace directories use the same mode).
const uploadDirPerm os.FileMode = 0o700

// mirrorUploads copies each attachment's bytes from the store into uploadsDir
// under its sanitized original filename and returns key → final mirrored
// filename for the entries it placed or found already present. An entry the
// mirror cannot serve — an unreadable key, an unsanitizable name, a write
// failure — is skipped, leaving no map entry. Copy-if-absent keeps
// regenerations and replays idempotent: the same content keeps its name, a
// different file under the same name lands on a numeric suffix. An empty
// uploadsDir or a nil store mirrors nothing; a list with no mirrorable entry
// returns nil — and creates nothing, the directory being made lazily on the
// first placement.
func mirrorUploads(ctx context.Context, uploadsDir string, atts *attachments.Store, list []sessions.Attachment) map[string]string {
	if uploadsDir == "" || atts == nil {
		return nil
	}
	mirrored := make(map[string]string, len(list))
	ready := false
	for _, att := range list {
		content, err := atts.Read(ctx, att.Key)
		if err != nil {
			continue // unknown or unreadable key: the note keeps the key only
		}
		name := sanitizeUploadName(att.Name)
		if name == "" {
			continue
		}
		if !ready {
			// The runner resolves the directory through workspaces.UploadsDir
			// first; the mkdir -p here is the mirror's own best-effort net.
			if err := os.MkdirAll(uploadsDir, uploadDirPerm); err != nil {
				return nil
			}
			ready = true
		}
		final, err := placeUpload(uploadsDir, name, content.Data)
		if err != nil {
			continue // a mirror failure must never fail the turn
		}
		mirrored[att.Key] = final
	}
	if len(mirrored) == 0 {
		return nil
	}
	return mirrored
}

// placeUpload copies data into uploadsDir under name, resolving collisions
// with a numeric suffix (report.pdf → report-1.pdf, report-2.pdf, …): an
// existing file with identical bytes is the idempotent hit and is kept
// as-is, anything else pushes the search to the next suffix.
func placeUpload(uploadsDir, name string, data []byte) (string, error) {
	final := name
	for i := 1; ; i++ {
		target := filepath.Join(uploadsDir, final)
		existing, err := os.ReadFile(target)
		if err == nil {
			if bytes.Equal(existing, data) {
				return final, nil // already mirrored: the idempotent hit
			}
			final = suffixedUploadName(name, i) // occupied by other content
			continue
		}
		if !os.IsNotExist(err) {
			return "", err // an unreadable target (a directory, permissions)
		}
		// A free slot: the helper's only write. A concurrent writer winning
		// the race is harmless — the copies are identical.
		if err := os.WriteFile(target, data, uploadFilePerm); err != nil {
			return "", err
		}
		return final, nil
	}
}

// suffixedUploadName inserts a numeric suffix before the extension:
// report.pdf → report-1.pdf.
func suffixedUploadName(name string, n int) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + "-" + strconv.Itoa(n) + ext
}

// sanitizeUploadName reduces an attachment's original filename to a safe
// uploads/ entry: the base name only (host and web path separators alike),
// control characters stripped, surrounding whitespace trimmed. An empty
// result — no name, only separators or control characters, or a bare "." or
// ".." — skips the entry.
func sanitizeUploadName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// turnAttachments collects every attachment riding the turn (design D2): the
// fresh send's list plus every replayed user message's attachments, deduped
// by capability key in first-seen order — a replay references the same keys
// the fresh send carries.
func turnAttachments(events []sessions.Event, fresh []sessions.Attachment) []sessions.Attachment {
	seen := make(map[string]bool, len(fresh))
	var out []sessions.Attachment
	add := func(list []sessions.Attachment) {
		for _, att := range list {
			if att.Key == "" || seen[att.Key] {
				continue
			}
			seen[att.Key] = true
			out = append(out, att)
		}
	}
	for _, ev := range events {
		if ev.Kind == sessions.KindUserMessage {
			add(ev.Attachments)
		}
	}
	add(fresh)
	return out
}
