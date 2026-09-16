package whatsappmd

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/gateways"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestJIDDigits(t *testing.T) {
	tests := []struct {
		name      string
		localpart string
		want      string
	}{
		{"plain phone digits", "12025550123", "12025550123"},
		{"plus prefix stripped", "+12025550123", "12025550123"},
		{"separators stripped", "1202-555-0123", "12025550123"},
		{"empty localpart", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := jidDigits(tc.localpart); got != tc.want {
				t.Errorf("jidDigits(%q) = %q, want %q", tc.localpart, got, tc.want)
			}
		})
	}
}

func TestNormalizeInboundMessages(t *testing.T) {
	tests := []struct {
		name     string
		evt      *events.Message
		want     *inboundCheck
		wantDrop bool
	}{
		{
			name: "text dm",
			evt:  dmEvent("m-1", "12025550123", "hello agent"),
			want: &inboundCheck{
				chatID: "12025550123", fromUserID: "12025550123", fromUsername: "Oni",
				text: "hello agent", kind: gateways.InboundDM,
			},
		},
		{
			name: "extended text message",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-2",
				},
				Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: proto.String("linked text"),
				}},
			},
			want: &inboundCheck{chatID: "12025550123", text: "linked text", kind: gateways.InboundDM},
		},
		{
			name: "own echo dropped",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: userJID("12025550123"), Sender: userJID("12025550123"), IsFromMe: true,
					},
					ID: "m-3",
				},
				Message: &waE2E.Message{Conversation: proto.String("my own send")},
			},
			wantDrop: true,
		},
		{
			name: "group chat dropped entirely",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: types.NewJID("12025550002", types.GroupServer), Sender: userJID("12025550123"),
					},
					ID: "m-4",
				},
				Message: &waE2E.Message{Conversation: proto.String("group chatter")},
			},
			wantDrop: true,
		},
		{
			name: "non-user server dropped",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat: types.NewJID("status", types.BroadcastServer), Sender: userJID("12025550123"),
					},
					ID: "m-5",
				},
				Message: &waE2E.Message{Conversation: proto.String("status")},
			},
			wantDrop: true,
		},
		{
			name: "inbound edit dropped",
			evt: func() *events.Message {
				e := dmEvent("m-6", "12025550123", "edited text")
				e.IsEdit = true
				return e
			}(),
			wantDrop: true,
		},
		{
			name: "video lands as document kind",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-7",
				},
				Message: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
					Mimetype:   proto.String("video/mp4"),
					FileLength: proto.Uint64(2048),
				}},
			},
			want: &inboundCheck{
				chatID: "12025550123", kind: gateways.InboundDM,
				attachments: 1,
				attKind:     gateways.AttachmentDocument,
				attName:     "video.mp4",
				attMime:     "video/mp4",
				attSize:     2048,
			},
		},
		{
			name: "document keeps its file name",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-8",
				},
				Message: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
					FileName:   proto.String("report.pdf"),
					Mimetype:   proto.String("application/pdf"),
					FileLength: proto.Uint64(10),
				}},
			},
			want: &inboundCheck{
				chatID: "12025550123", kind: gateways.InboundDM,
				attachments: 1,
				attKind:     gateways.AttachmentDocument,
				attName:     "report.pdf",
				attMime:     "application/pdf",
				attSize:     10,
			},
		},
		{
			name: "voice note carries duration",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-9",
				},
				Message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
					Mimetype:   proto.String("audio/ogg; codecs=opus"),
					FileLength: proto.Uint64(320),
					Seconds:    proto.Uint32(42),
					PTT:        proto.Bool(true),
				}},
			},
			want: &inboundCheck{
				chatID: "12025550123", kind: gateways.InboundDM,
				attachments: 1,
				attKind:     gateways.AttachmentVoice,
				attMime:     "audio/ogg; codecs=opus",
				attSize:     320,
				attDuration: 42,
			},
		},
		{
			name: "photo with caption",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-10",
				},
				Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					Mimetype:   proto.String("image/jpeg"),
					FileLength: proto.Uint64(512),
					Caption:    proto.String("what is in this chart?"),
				}},
			},
			want: &inboundCheck{
				chatID: "12025550123", kind: gateways.InboundDM,
				text:        "what is in this chart?",
				attachments: 1,
				attKind:     gateways.AttachmentPhoto,
				attMime:     "image/jpeg",
				attSize:     512,
			},
		},
		{
			name: "sticker refused, not ingested",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-11",
				},
				Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}},
			},
			wantDrop: true,
		},
		{
			name: "empty message dropped",
			evt: &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: userJID("12025550123"), Sender: userJID("12025550123")},
					ID:            "m-12",
				},
				Message: &waE2E.Message{},
			},
			wantDrop: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			device := newFakeDevice()
			a := newTestAdapter(t, device)
			handler := &stubHandler{}
			a.handler = handler

			a.onMessage(tc.evt)

			if tc.wantDrop {
				if handler.count() != 0 {
					t.Fatalf("message ingested, want dropped: %+v", handler.last())
				}
				// Stickers get the refusal notice; other drops stay silent.
				if tc.evt.Message.GetStickerMessage() != nil {
					sends := device.recordedSends()
					if len(sends) != 1 || sends[0].Text != stickerNoticeText {
						t.Errorf("sticker notice = %+v, want one %q", sends, stickerNoticeText)
					}
				} else if len(device.recordedSends()) != 0 {
					t.Errorf("silent drop sent a notice: %+v", device.recordedSends())
				}
				return
			}

			if handler.count() != 1 {
				t.Fatalf("ingested %d messages, want 1", handler.count())
			}
			checkInbound(t, handler.last(), tc.want)
		})
	}
}

func TestDedupRingDropsReplays(t *testing.T) {
	device := newFakeDevice()
	a := newTestAdapter(t, device)
	handler := &stubHandler{}
	a.handler = handler

	a.onMessage(dmEvent("dup-1", "12025550123", "first"))
	a.onMessage(dmEvent("dup-1", "12025550123", "replayed"))
	if got := handler.count(); got != 1 {
		t.Fatalf("ingested %d messages after replay, want 1", got)
	}

	// A different id still lands.
	a.onMessage(dmEvent("dup-2", "12025550123", "second"))
	if got := handler.count(); got != 2 {
		t.Fatalf("ingested %d messages, want 2", got)
	}
}

func TestDedupRingEvictsOldest(t *testing.T) {
	a := newTestAdapter(t, newFakeDevice())
	for i := 0; i < dedupRingSize+10; i++ {
		if !a.rememberMessage(ringID(i)) {
			t.Fatalf("id %d refused on a fresh ring", i)
		}
	}
	// The oldest ids fell out of the ring; they are new again.
	if !a.rememberMessage(ringID(0)) {
		t.Error("oldest id still remembered after ring overflow")
	}
	// Recent ids stay deduped.
	if a.rememberMessage(ringID(dedupRingSize + 9)) {
		t.Error("recent id evicted too early")
	}
}

// ringID mints distinct ids for the ring test (i up to a few hundred).
func ringID(i int) string {
	const digits = "0123456789"
	id := make([]byte, 0, 4)
	for ; i > 0 || len(id) < 4; i /= 10 {
		id = append(id, digits[i%10])
	}
	return "m-" + string(id)
}

// -------------------------------------------------------------------------
// Inbound assertion helpers
// -------------------------------------------------------------------------

type inboundCheck struct {
	chatID       string
	fromUserID   string
	fromUsername string
	text         string
	kind         gateways.InboundKind
	attachments  int
	attKind      gateways.AttachmentKind
	attName      string
	attMime      string
	attSize      int64
	attDuration  int
}

func checkInbound(t *testing.T, got gateways.InboundMessage, want *inboundCheck) {
	t.Helper()
	if want.chatID != "" && got.ChatID != want.chatID {
		t.Errorf("ChatID = %q, want %q", got.ChatID, want.chatID)
	}
	if want.fromUserID != "" && got.FromUserID != want.fromUserID {
		t.Errorf("FromUserID = %q, want %q", got.FromUserID, want.fromUserID)
	}
	if want.fromUsername != "" && got.FromUsername != want.fromUsername {
		t.Errorf("FromUsername = %q, want %q", got.FromUsername, want.fromUsername)
	}
	if got.Text != want.text {
		t.Errorf("Text = %q, want %q", got.Text, want.text)
	}
	if got.Kind != want.kind {
		t.Errorf("Kind = %q, want %q", got.Kind, want.kind)
	}
	if got.Platform != gateways.PlatformWhatsApp {
		t.Errorf("Platform = %q, want whatsapp", got.Platform)
	}
	if got.FromBot {
		t.Error("FromBot = true, want false (own echo never reaches here)")
	}
	if len(got.Attachments) != want.attachments {
		t.Fatalf("attachments = %d, want %d (%+v)", len(got.Attachments), want.attachments, got.Attachments)
	}
	if want.attachments == 0 {
		return
	}
	att := got.Attachments[0]
	if att.Kind != want.attKind {
		t.Errorf("attachment kind = %q, want %q", att.Kind, want.attKind)
	}
	if want.attName != "" && att.FileName != want.attName {
		t.Errorf("attachment name = %q, want %q", att.FileName, want.attName)
	}
	if want.attMime != "" && att.MimeType != want.attMime {
		t.Errorf("attachment mime = %q, want %q", att.MimeType, want.attMime)
	}
	if want.attSize != 0 && att.Size != want.attSize {
		t.Errorf("attachment size = %d, want %d", att.Size, want.attSize)
	}
	if att.Duration != want.attDuration {
		t.Errorf("attachment duration = %d, want %d", att.Duration, want.attDuration)
	}
	if att.FileID == "" {
		t.Error("attachment FileID empty — media would be undownloadable")
	}
}
