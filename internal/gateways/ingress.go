package gateways

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/attachments"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Voice-note ingress sentinels (design D8: fail-soft — a voice problem
// refuses the turn with chat feedback, it never fails the gateway).
var (
	// ErrVoiceUnavailable reports that no speech-to-text provider is
	// configured on the instance.
	ErrVoiceUnavailable = errors.New("voice notes are unavailable: no speech-to-text provider is configured")
	// ErrVoiceTranscriptionFailed reports that transcription of a downloaded
	// voice note failed.
	ErrVoiceTranscriptionFailed = errors.New("voice note could not be transcribed")
)

// VoiceNotePrefix marks a transcribed voice note in the turn input
// (design D8).
const VoiceNotePrefix = "[Voice Note]: "

// sniffHeadBytes is the classification head ingress reads ahead of the blob
// write (the web upload handler's value): >=512 bytes for the magic-byte
// sniff, a larger window for the best-effort PDF page count.
const ingressSniffHeadBytes = 512 << 10

// Transcriber is the pluggable speech-to-text port (design D8). Providers
// register implementations; the gateway fails soft when none is configured.
type Transcriber interface {
	// Transcribe converts one audio blob to its transcript text.
	Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error)
}

// StorageResolver is the narrow workspace-blob seam ingress writes downloads
// through (the web upload handler's *resolver.WorkspaceStorage satisfies it
// structurally).
type StorageResolver interface {
	ForWorkspace(ctx context.Context, workspaceID string) (storage.Storage, error)
	DriverName(ctx context.Context, workspaceID string) (string, error)
}

// Ingress normalizes inbound gateway files into the existing attachment
// pipeline (design D8): adapter downloads land through the same lane
// classification and size caps as web uploads, so oversized or unsupported
// files are refused identically and turn refs are lane-compatible.
type Ingress struct {
	adapter      PlatformAdapter
	attachments  store.AttachmentStore
	blobStorage  StorageResolver
	transcriber  Transcriber // optional capability; nil refuses voice notes
	sniffHeadLen int
}

// NewIngress creates an Ingress over the adapter, attachment store, and
// workspace blob resolver. Voice notes fail soft until WithTranscriber
// supplies a provider.
func NewIngress(adapter PlatformAdapter, attachments store.AttachmentStore, blobStorage StorageResolver, opts ...IngressOption) *Ingress {
	g := &Ingress{
		adapter:      adapter,
		attachments:  attachments,
		blobStorage:  blobStorage,
		sniffHeadLen: ingressSniffHeadBytes,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// IngressOption customizes the ingress.
type IngressOption func(*Ingress)

// WithTranscriber configures the speech-to-text provider for voice notes.
func WithTranscriber(t Transcriber) IngressOption {
	return func(g *Ingress) {
		if t != nil {
			g.transcriber = t
		}
	}
}

// Enrich implements IngressStage (router.go): it normalizes an inbound
// message's media into turn input text and attachment refs, or produces a
// chat refusal. Photos/documents land through the shared lane pipeline;
// voice notes become a "[Voice Note]: " transcript prefix. The first media
// problem wins — the refusal is returned and remaining attachments are not
// ingested.
func (g *Ingress) Enrich(ctx context.Context, workspaceID string, msg InboundMessage) (string, []agents.AttachmentRef, string, error) {
	text := msg.Text
	var refs []agents.AttachmentRef

	for _, att := range msg.Attachments {
		switch att.Kind {
		case AttachmentVoice:
			input, err := g.IngestVoice(ctx, att)
			if err != nil {
				return "", nil, RefusalText(err), nil
			}
			if text == "" {
				text = input
			} else {
				text = text + "\n\n" + input
			}
		default:
			ref, err := g.Ingest(ctx, workspaceID, msg.FromUserID, att)
			if err != nil {
				return "", nil, RefusalText(err), nil
			}
			refs = append(refs, ref)
		}
	}
	return text, refs, "", nil
}

// Ingest downloads one inbound file, classifies it through the shared lane
// rules, stores the blob under a fresh capability key, records the
// attachment row, and returns the turn ref. Classification errors (oversize,
// unsupported) pass through typed — RefusalText renders them as chat
// feedback.
func (g *Ingress) Ingest(ctx context.Context, workspaceID, userID string, att InboundAttachment) (agents.AttachmentRef, error) {
	data, err := g.adapter.DownloadFile(ctx, att.FileID)
	if err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("gateway ingress: download %q: %w", att.FileName, err)
	}
	if len(data) == 0 {
		return agents.AttachmentRef{}, fmt.Errorf("%w: attachment must not be empty", domain.ErrInvalid)
	}

	// Telegram reports photo sizes without a file name; classification needs
	// an extension for the text-family decision, so synthesize one by kind.
	name := att.FileName
	if name == "" {
		switch att.Kind {
		case AttachmentPhoto:
			name = "photo.jpg"
		default:
			name = "document"
		}
	}

	size := att.Size
	if size <= 0 {
		size = int64(len(data))
	}

	head := data
	if len(head) > g.sniffHeadLen {
		head = head[:g.sniffHeadLen]
	}
	mime, lane, err := attachments.Classify(name, size, head)
	if err != nil {
		return agents.AttachmentRef{}, err
	}

	sink, err := g.blobStorage.ForWorkspace(ctx, workspaceID)
	if err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("gateway ingress: resolve workspace storage: %w", err)
	}
	backend, err := g.blobStorage.DriverName(ctx, workspaceID)
	if err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("gateway ingress: resolve backend name: %w", err)
	}

	key, err := storage.NewKey()
	if err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("gateway ingress: capability key: %w", err)
	}
	if err := sink.Put(ctx, key, bytes.NewReader(data), size, mime); err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("gateway ingress: store blob: %w", err)
	}

	rec := &domain.Attachment{
		WorkspaceID: workspaceID,
		StorageKey:  key,
		Backend:     backend,
		Name:        name,
		MimeType:    mime,
		Size:        size,
		Lane:        lane,
		CreatedBy:   userID,
	}
	if err := g.attachments.Create(ctx, rec); err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("gateway ingress: record attachment: %w", err)
	}

	return agents.AttachmentRef{
		ID:       rec.ID,
		Name:     rec.Name,
		MimeType: rec.MimeType,
		Lane:     rec.Lane,
		Size:     rec.Size,
	}, nil
}

// IngestVoice downloads and transcribes a voice note, returning the turn
// input prefixed "[Voice Note]: " (design D8). No attachment record is
// created — the transcript replaces the audio. Unconfigured STT and
// transcription failures are fail-soft refusals: typed errors the caller
// turns into chat feedback, never gateway failures.
func (g *Ingress) IngestVoice(ctx context.Context, att InboundAttachment) (string, error) {
	if g.transcriber == nil {
		return "", ErrVoiceUnavailable
	}

	data, err := g.adapter.DownloadFile(ctx, att.FileID)
	if err != nil {
		return "", fmt.Errorf("%w: download failed: %v", ErrVoiceTranscriptionFailed, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("%w: empty audio", ErrVoiceTranscriptionFailed)
	}

	mime := att.MimeType
	if mime == "" {
		mime = "audio/ogg"
	}
	transcript, err := g.transcriber.Transcribe(ctx, data, mime)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrVoiceTranscriptionFailed, err)
	}
	return VoiceNotePrefix + strings.TrimSpace(transcript), nil
}

// RefusalText renders an ingress error as friendly chat feedback (design D8:
// "size-cap refusals with chat feedback"). Unknown errors get a generic
// notice — provider details never leak into the chat.
func RefusalText(err error) string {
	var tooLarge *attachments.ErrTooLarge
	switch {
	case errors.As(err, &tooLarge):
		return fmt.Sprintf("That %s is too large (limit: %s). Please compress or split it.",
			tooLarge.Type, humanBytes(tooLarge.Cap))
	case errors.Is(err, ErrVoiceUnavailable):
		return "Voice notes are unavailable — no speech-to-text provider is configured on this instance."
	case errors.Is(err, ErrVoiceTranscriptionFailed):
		return "I couldn't transcribe that voice note. Please try again or type your message."
	case errors.Is(err, domain.ErrInvalid):
		return "I couldn't process that attachment — it looks empty or unsupported."
	case errors.Is(err, domain.ErrPayloadTooLarge):
		return "That file is too large for me to read."
	default:
		slog.Debug("gateway ingress: unclassified refusal", "error", err)
		return "I couldn't process that attachment."
	}
}

// humanBytes renders a byte count as a human-readable cap ("5 MB").
func humanBytes(n int64) string {
	const mb = 1 << 20
	const kb = 1 << 10
	switch {
	case n >= mb:
		return fmt.Sprintf("%d MB", n/mb)
	case n >= kb:
		return fmt.Sprintf("%d KB", n/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
