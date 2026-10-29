package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/stretchr/testify/require"
)

// photoUpdate builds a DM photo message with the given size ladder.
func photoUpdate(id int64, sizes ...botapi.PhotoSize) botapi.Update {
	u := dmUpdate(id, 42, "")
	u.Message.Photo = sizes
	return u
}

// documentUpdate builds a DM document message.
func documentUpdate(id int64, name, mime string, size int64) botapi.Update {
	u := dmUpdate(id, 42, "caption for the doc")
	u.Message.Document = &botapi.Document{FileID: "doc-1", FileName: name, FileSize: size, MimeType: mime}
	return u
}

// TestPhotoInbound pins design D11: the largest fitting size downloads,
// registers through the attachment registry, and submits with the caption.
func TestPhotoInbound(t *testing.T) {
	fx := newFixture(t)
	fx.bot.files["downloads/photo-big"] = []byte("jpeg-bytes")

	u := photoUpdate(1,
		botapi.PhotoSize{FileID: "photo-small", FileSize: 100, Width: 90, Height: 90},
		botapi.PhotoSize{FileID: "photo-big", FileSize: 5000, Width: 1280, Height: 720},
	)
	fx.svc.handleMessage(context.Background(), u.Message)

	eventually(t, "the media turn to submit", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	req := fx.rt.submittedReqs()[0]
	require.Equal(t, []string{"key-1"}, req.AttachmentKeys)
	require.Empty(t, req.Message, "a caption-less photo is an attachment-only send")

	saves := fx.atts.saves
	require.Len(t, saves, 1)
	require.Equal(t, "photo.jpg", saves[0].Name)
	require.Equal(t, "image/jpeg", saves[0].Mime)
	require.Equal(t, len("jpeg-bytes"), saves[0].Size)

	// The largest fitting ladder entry is the one fetched.
	var fileID string
	for _, c := range fx.bot.callsOf("getFile") {
		fileID = c.Text
	}
	require.Equal(t, "photo-big", fileID)
}

// TestPhotoCaptionRides pins the caption-as-text rule.
func TestPhotoCaptionRides(t *testing.T) {
	fx := newFixture(t)
	u := photoUpdate(1, botapi.PhotoSize{FileID: "p", FileSize: 10})
	u.Message.Caption = "what is in this picture?"

	fx.svc.handleMessage(context.Background(), u.Message)
	eventually(t, "the submit", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	require.Equal(t, "what is in this picture?", fx.rt.submittedReqs()[0].Message)
}

// TestPhotoOvercapFallsBack pins the size ladder rule: entries over the cap
// are skipped; a ladder that is entirely over the cap notifies, never silent.
func TestPhotoOvercapFallsBack(t *testing.T) {
	fx := newFixture(t)
	u := photoUpdate(1,
		botapi.PhotoSize{FileID: "huge", FileSize: MaxPhotoBytes + 1},
		botapi.PhotoSize{FileID: "fits", FileSize: 100},
	)
	fx.svc.handleMessage(context.Background(), u.Message)
	eventually(t, "the submit", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	require.Equal(t, []string{"key-1"}, fx.rt.submittedReqs()[0].AttachmentKeys)
	require.Equal(t, "fits", fx.bot.callsOf("getFile")[0].Text)
}

// TestPhotoAllOvercapNotifies pins the never-silent rule.
func TestPhotoAllOvercapNotifies(t *testing.T) {
	fx := newFixture(t)
	u := photoUpdate(1, botapi.PhotoSize{FileID: "huge", FileSize: MaxPhotoBytes * 2})
	fx.svc.handleMessage(context.Background(), u.Message)

	time.Sleep(60 * time.Millisecond)
	require.Empty(t, fx.rt.submittedReqs())
	require.Empty(t, fx.bot.callsOf("getFile"), "no download attempted")
	sends := fx.bot.callsOf("sendMessage")
	require.NotEmpty(t, sends, "the failure is never silent")
	require.Contains(t, sends[0].Text, "photo")
}

// TestDownloadFailureNotifies pins the failure path (design D11): the chat
// learns, no turn submits.
func TestDownloadFailureNotifies(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errFor["DownloadFile"] = &botapi.Error{Kind: botapi.KindTransient, Description: "network gone"}

	fx.svc.handleMessage(context.Background(), photoUpdate(1, botapi.PhotoSize{FileID: "p", FileSize: 10}).Message)
	time.Sleep(60 * time.Millisecond)

	require.Empty(t, fx.rt.submittedReqs())
	sends := fx.bot.callsOf("sendMessage")
	require.NotEmpty(t, sends)
	require.Contains(t, sends[0].Text, "photo.jpg")
}

// TestUnsupportedDocumentNotifies pins the registry-rejection path: an
// unsupported mime surfaces the reason to the chat.
func TestUnsupportedDocumentNotifies(t *testing.T) {
	fx := newFixture(t)
	fx.atts.rejectMimes = map[string]bool{"application/x-unknown": true}

	fx.svc.handleMessage(context.Background(), documentUpdate(1, "thing.bin", "application/x-unknown", 100).Message)
	time.Sleep(60 * time.Millisecond)

	require.Empty(t, fx.rt.submittedReqs())
	sends := fx.bot.callsOf("sendMessage")
	require.NotEmpty(t, sends)
	require.True(t, strings.Contains(sends[0].Text, "thing.bin"), "the chat sees which file failed")
}

// TestDocumentInboundRegisters pins the document branch: mime lanes through
// the registry and the caption rides as text.
func TestDocumentInboundRegisters(t *testing.T) {
	fx := newFixture(t)
	fx.bot.files["downloads/doc-1"] = []byte("%PDF-1.4")

	fx.svc.handleMessage(context.Background(), documentUpdate(1, "report.pdf", "application/pdf", 9).Message)
	eventually(t, "the submit", func() bool { return len(fx.rt.submittedReqs()) == 1 })

	req := fx.rt.submittedReqs()[0]
	require.Equal(t, []string{"key-1"}, req.AttachmentKeys)
	require.Equal(t, "caption for the doc", req.Message)
	require.Equal(t, "report.pdf", fx.atts.saves[0].Name)
	require.Equal(t, "application/pdf", fx.atts.saves[0].Mime)
}
