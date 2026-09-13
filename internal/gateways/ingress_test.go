package gateways

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/attachments"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func newIngressTestEnv(t *testing.T, transcribers ...Transcriber) (*Ingress, *testPlatformAdapter, *testMemStorage) {
	t.Helper()
	st := fake.New()
	ctx := context.Background()
	if err := st.Workspaces().Create(ctx, domainWorkspace("ws1")); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Users().Create(ctx, domainUser("user1")); err != nil {
		t.Fatalf("create user: %v", err)
	}

	adapter := newTestPlatformAdapter()
	mem := newTestMemStorage()
	resolver := &testStorageResolver{storage: mem, backend: "local"}
	var opts []IngressOption
	if len(transcribers) > 0 {
		opts = append(opts, WithTranscriber(transcribers[0]))
	}
	ingress := NewIngress(adapter, st.Attachments(), resolver, opts...)
	return ingress, adapter, mem
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

func TestIngestPhotoBecomesInlineImage(t *testing.T) {
	ingress, adapter, mem := newIngressTestEnv(t)
	data := append(append([]byte{}, pngMagic...), make([]byte, 1024)...)
	adapter.downloads["file-photo"] = data

	ref, err := ingress.Ingest(context.Background(), "ws1", "user1", InboundAttachment{
		Kind:   AttachmentPhoto,
		FileID: "file-photo",
		Size:   int64(len(data)),
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if ref.ID == "" {
		t.Fatalf("attachment id missing on ref: %+v", ref)
	}
	if ref.Lane != "inline-image" {
		t.Fatalf("photo lane = %q, want inline-image", ref.Lane)
	}
	if ref.Name != "photo.jpg" {
		t.Fatalf("synthesized photo name = %q", ref.Name)
	}
	if ref.Size != int64(len(data)) {
		t.Fatalf("ref size = %d, want %d", ref.Size, len(data))
	}
	if len(mem.blobs) != 1 {
		t.Fatalf("blob not stored: %d blobs", len(mem.blobs))
	}
}

func TestIngestTextDocumentLanes(t *testing.T) {
	ingress, adapter, _ := newIngressTestEnv(t)

	t.Run("small text is inline", func(t *testing.T) {
		adapter.downloads["file-small"] = []byte("hello world")
		ref, err := ingress.Ingest(context.Background(), "ws1", "user1", InboundAttachment{
			Kind:     AttachmentDocument,
			FileID:   "file-small",
			FileName: "notes.txt",
			Size:     11,
		})
		if err != nil {
			t.Fatalf("Ingest small text: %v", err)
		}
		if ref.Lane != "inline-text" {
			t.Fatalf("lane = %q, want inline-text", ref.Lane)
		}
	})

	t.Run("large text drops", func(t *testing.T) {
		big := strings.Repeat("a", attachments.MaxInlineTextBytes+1)
		adapter.downloads["file-big"] = []byte(big)
		ref, err := ingress.Ingest(context.Background(), "ws1", "user1", InboundAttachment{
			Kind:     AttachmentDocument,
			FileID:   "file-big",
			FileName: "big.txt",
			Size:     int64(len(big)),
		})
		if err != nil {
			t.Fatalf("Ingest big text: %v", err)
		}
		if ref.Lane != "drop" {
			t.Fatalf("lane = %q, want drop", ref.Lane)
		}
	})
}

func TestIngestSizeCapRefusal(t *testing.T) {
	ingress, adapter, _ := newIngressTestEnv(t)
	data := append(append([]byte{}, pngMagic...), make([]byte, attachments.MaxInlineImageBytes+1)...)
	adapter.downloads["file-huge"] = data

	_, err := ingress.Ingest(context.Background(), "ws1", "user1", InboundAttachment{
		Kind:   AttachmentPhoto,
		FileID: "file-huge",
		Size:   int64(len(data)),
	})
	var tooLarge *attachments.ErrTooLarge
	if err == nil || !errors.As(err, &tooLarge) {
		t.Fatalf("oversize image must be refused with ErrTooLarge, got %v", err)
	}
	text := RefusalText(err)
	if !strings.Contains(text, "too large") || !strings.Contains(text, "5 MB") {
		t.Fatalf("refusal text must name the cap: %q", text)
	}
}

func TestIngestVoiceFailSoftWithoutTranscriber(t *testing.T) {
	ingress, _, _ := newIngressTestEnv(t)

	_, err := ingress.IngestVoice(context.Background(), InboundAttachment{Kind: AttachmentVoice, FileID: "v1"})
	if err == nil || err != ErrVoiceUnavailable {
		t.Fatalf("unconfigured STT must fail soft with ErrVoiceUnavailable, got %v", err)
	}
	if !strings.Contains(RefusalText(err), "Voice notes are unavailable") {
		t.Fatalf("voice refusal text must be specific: %q", RefusalText(err))
	}
}

func TestIngestVoiceTranscribedWithPrefix(t *testing.T) {
	tr := &testTranscriber{transcript: "deploy the fix now"}
	ingress, adapter, _ := newIngressTestEnv(t, tr)
	adapter.downloads["voice-1"] = []byte("oggbytes")

	input, err := ingress.IngestVoice(context.Background(), InboundAttachment{
		Kind:     AttachmentVoice,
		FileID:   "voice-1",
		MimeType: "audio/ogg",
	})
	if err != nil {
		t.Fatalf("IngestVoice: %v", err)
	}
	if input != "[Voice Note]: deploy the fix now" {
		t.Fatalf("voice input = %q, want the prefixed transcript", input)
	}
	if tr.calls != 1 {
		t.Fatalf("transcriber calls = %d, want 1", tr.calls)
	}
}

func TestIngestVoiceTranscriptionFailureIsFailSoft(t *testing.T) {
	tr := &testTranscriber{err: context.DeadlineExceeded}
	ingress, adapter, _ := newIngressTestEnv(t, tr)
	adapter.downloads["voice-1"] = []byte("oggbytes")

	_, err := ingress.IngestVoice(context.Background(), InboundAttachment{Kind: AttachmentVoice, FileID: "voice-1"})
	if err == nil || !strings.Contains(err.Error(), ErrVoiceTranscriptionFailed.Error()) {
		t.Fatalf("transcription failure must be fail-soft typed, got %v", err)
	}
	if !strings.Contains(RefusalText(err), "transcribe") {
		t.Fatalf("refusal text must explain the transcription problem: %q", RefusalText(err))
	}
}

func TestIngressEnrichBuildsTurnInput(t *testing.T) {
	ingress, adapter, _ := newIngressTestEnv(t)
	adapter.downloads["file-photo"] = append(append([]byte{}, pngMagic...), make([]byte, 64)...)

	t.Run("photo plus caption", func(t *testing.T) {
		text, refs, refusal, err := ingress.Enrich(context.Background(), "ws1", InboundMessage{
			FromUserID: "user1",
			Text:       "what is in this chart?",
			Attachments: []InboundAttachment{
				{Kind: AttachmentPhoto, FileID: "file-photo", Size: 1072},
			},
		})
		if err != nil {
			t.Fatalf("Enrich: %v", err)
		}
		if refusal != "" {
			t.Fatalf("unexpected refusal: %q", refusal)
		}
		if text != "what is in this chart?" {
			t.Fatalf("caption text = %q", text)
		}
		if len(refs) != 1 || refs[0].Lane != "inline-image" {
			t.Fatalf("photo ref missing: %#v", refs)
		}
	})

	t.Run("voice replaces empty text", func(t *testing.T) {
		tr := &testTranscriber{transcript: "status report"}
		ingress2, adapter2, _ := newIngressTestEnv(t, tr)
		adapter2.downloads["v2"] = []byte("ogg")

		text, refs, refusal, err := ingress2.Enrich(context.Background(), "ws1", InboundMessage{
			Attachments: []InboundAttachment{{Kind: AttachmentVoice, FileID: "v2"}},
		})
		if err != nil || refusal != "" {
			t.Fatalf("Enrich error/refusal: %v %q", err, refusal)
		}
		if text != "[Voice Note]: status report" {
			t.Fatalf("voice text = %q", text)
		}
		if len(refs) != 0 {
			t.Fatalf("voice must not create refs: %#v", refs)
		}
	})

	t.Run("oversized attachment refuses with feedback", func(t *testing.T) {
		adapter.downloads["huge"] = append(append([]byte{}, pngMagic...), make([]byte, attachments.MaxInlineImageBytes+1)...)
		_, _, refusal, err := ingress.Enrich(context.Background(), "ws1", InboundMessage{
			Text: "look",
			Attachments: []InboundAttachment{
				{Kind: AttachmentPhoto, FileID: "huge", Size: attachments.MaxInlineImageBytes + 8 + 1},
			},
		})
		if err != nil {
			t.Fatalf("refusals are chat feedback, not errors: %v", err)
		}
		if !strings.Contains(refusal, "too large") {
			t.Fatalf("refusal = %q", refusal)
		}
	})
}
