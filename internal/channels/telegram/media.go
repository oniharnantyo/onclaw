package telegram

import (
	"context"
	"log/slog"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
)

// photoMime is Telegram's photo encoding (the size ladder always carries
// JPEG thumbs of the original).
const photoMime = "image/jpeg"

// handleMedia serves an inbound photo or document (design D11): resolve the
// file, download it, register it through the attachment registry, and submit
// the caption as the turn text with the attachment key attached. Every
// failure notifies the chat — never silent.
func (s *Service) handleMedia(ctx context.Context, at inlet, msg *botapi.Message) {
	var fileID, name, mime string
	switch {
	case len(msg.Photo) > 0:
		photo, ok := largestPhoto(msg.Photo)
		if !ok {
			s.noticeAsync(at.chatID, "The photo could not be accepted: no usable size.")
			return
		}
		fileID, name, mime = photo.FileID, "photo.jpg", photoMime
	case msg.Document != nil:
		doc := msg.Document
		fileID = doc.FileID
		name = doc.FileName
		if name == "" {
			name = "document"
		}
		mime = doc.MimeType
	default:
		return
	}

	client, _, err := s.botFor(ctx)
	if err != nil {
		s.noticeAsync(at.chatID, "Could not fetch "+name+": the bot is not connected.")
		return
	}

	f, err := client.GetFile(ctx, fileID)
	if err != nil {
		slog.Warn("telegram: getFile failed", "name", name, "error", err)
		s.noticeAsync(at.chatID, "Could not fetch "+name+": "+err.Error())
		return
	}
	data, err := client.DownloadFile(ctx, f.FilePath)
	if err != nil {
		slog.Warn("telegram: download failed", "name", name, "error", err)
		s.noticeAsync(at.chatID, "Could not download "+name+": "+err.Error())
		return
	}
	if len(data) > MaxPhotoBytes {
		s.noticeAsync(at.chatID, name+" is too large (over 20 MB); it was not attached.")
		return
	}

	att, err := s.atts.Save(ctx, name, mime, data)
	if err != nil {
		// Lane rejection (unsupported type, over the lane cap) lands here
		// with its reason — the never-silent rule.
		slog.Warn("telegram: attachment registration failed", "name", name, "error", err)
		s.noticeAsync(at.chatID, name+" could not be attached: "+err.Error())
		return
	}

	s.admitMedia(at, msg, att.Key)
}

// largestPhoto picks the largest ladder entry within the photo cap. Entries
// with an unknown size (0 — Telegram usually fills it) count as usable; the
// hard byte check runs after download.
func largestPhoto(ladder []botapi.PhotoSize) (botapi.PhotoSize, bool) {
	var best botapi.PhotoSize
	found := false
	for _, p := range ladder {
		if p.FileSize > MaxPhotoBytes {
			continue
		}
		if !found || p.Width*p.Height >= best.Width*best.Height {
			best, found = p, true
		}
	}
	return best, found
}

// admitMedia routes a registered attachment through the same submit path as
// text (session mapping, queue), with the caption as the turn text.
func (s *Service) admitMedia(at inlet, msg *botapi.Message, key string) {
	caption := messageText(msg)
	atts := []string{key}

	// Media bypasses the debounce: flush any pending text first (order),
	// then dispatch immediately.
	s.flushDebounce(at.chatID)
	s.dispatchItem(s.root, queuedItem{inlet: at, text: caption, atts: atts})
}
