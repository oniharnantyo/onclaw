package agents

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The attachment turn-path tests (task 5.3, design D5, spec chat-attachments
// "Multi-part user messages" and "Model capability degradation"): the send
// persists attachment metadata for replay, the model input carries content
// parts when the effective model can read them, and a reference pointer note
// when it cannot.

// newAttachmentFixture builds a fixture with the attachment capability wired
// and returns it with the attachment store (over the same fake registry and
// directory the manager reads).
func newAttachmentFixture(t *testing.T, reply func(input []*schema.Message) fakeReply) (*fixture, *attachments.Store) {
	t.Helper()
	dir := t.TempDir()
	f := newFixture(t, reply, WithAttachments(dir))
	require.NotNil(t, f.mgr.attachments, "the fixture manager carries the attachment store")
	return f, attachments.NewStore(dir, f.store.Attachments())
}

// saveUpload stores one upload through the given store and returns its
// capability key.
func saveUpload(t *testing.T, atts *attachments.Store, name, mime, content string) sessions.Attachment {
	t.Helper()
	att, err := atts.Save(t.Context(), name, mime, []byte(content))
	require.NoError(t, err)
	return att
}

// TestSend_AttachmentMetadataPersists pins the replay payload: the
// user_message event carries the send's attachment metadata, and a replay
// reconstructs it verbatim (spec: replay reconstructs the metadata).
func TestSend_AttachmentMetadataPersists(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "got it", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)
	att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "look at this", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, string(sessions.KindUserMessage), string(events[0].Kind))
	require.Len(t, events[0].Attachments, 1)
	require.Equal(t, att, events[0].Attachments[0], "the registry metadata persists for replay")
	require.Equal(t, "look at this", events[0].Text)

	// The metadata is the capability key, not a path.
	require.Regexp(t, `^[0-9a-f]{32}$`, events[0].Attachments[0].Key)
}

// TestSend_AttachmentOnlyMessage pins the attachment-only send (spec
// chat-attachments): a message with attachments and no text sends, and an
// untitled session adopts the first file's name.
func TestSend_AttachmentOnlyMessage(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "read the doc", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)
	pdf := saveUpload(t, atts, "report.pdf", "application/pdf", "%PDF-1.4")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "", pdf.Key)
	require.NoError(t, err, "an attachment-only message must send")
	require.NoError(t, turn.Wait())

	titled, err := f.sessions.Session(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "report.pdf", titled.Title, "the title falls back to the file name")
}

// TestSend_RejectsBadAttachmentLists pins the send-path validations: over
// the per-message cap, unknown keys, and a bare empty message.
func TestSend_RejectsBadAttachmentLists(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok"}
	})
	sess := f.newSession(t)

	var keys []string
	for i := 0; i < attachments.MaxPerMessage+1; i++ {
		keys = append(keys, saveUpload(t, atts, "f.txt", "text/plain", "x").Key)
	}
	_, err := f.mgr.Send(t.Context(), sess.ID, "too many", keys...)
	require.ErrorIs(t, err, attachments.ErrRejected, "more than the cap is rejected with a reason")

	_, err = f.mgr.Send(t.Context(), sess.ID, "ghost", "ffffffffffffffffffffffffffffffff")
	require.Error(t, err, "an unknown key is refused")

	_, err = f.mgr.Send(t.Context(), sess.ID, "   ")
	require.Error(t, err, "an empty message without attachments is still rejected")

	// None of the refusals touched the transcript.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, events)
}

// TestTurn_TextOnlyModelReceivesPointerNote pins the degradation (spec
// chat-attachments "Image with a text-only model"): a model without image
// input receives the reference note instead of bytes, and the send succeeds.
func TestTurn_TextOnlyModelReceivesPointerNote(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(input []*schema.Message) fakeReply {
		// The turn only completes if the model saw the pointer, not bytes.
		last := input[len(input)-1]
		if len(last.UserInputMultiContent) == 0 {
			return fakeReply{err: errString("the multi-part message never reached the model")}
		}
		for _, part := range last.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL {
				return fakeReply{err: errString("a text-only model received image bytes")}
			}
		}
		sawNote := false
		for _, part := range last.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeText && strings.Contains(part.Text, "cannot read image input") {
				sawNote = true
			}
		}
		if !sawNote {
			return fakeReply{err: errString("the reference pointer note never reached the model")}
		}
		return fakeReply{text: "I can mention the file", promptTokens: 10, completionTokens: 2}
	})

	// A model outside the catalog reads no attachment content (unknown
	// models degrade to text-only, see catalog).
	agent := f.agent
	agent.Model = "totally-unknown-model"
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent

	sess, err := f.mgr.CreateSession(t.Context(), agent.ID)
	require.NoError(t, err)
	att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what is this?", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "sending must not fail because the model lacks image input")
}

// TestTurn_CapableModelReceivesContent pins the capable path (spec
// chat-attachments "Capable model receives content"): the image rides the
// user message as a base64 content part, and a later turn's replayed context
// re-sends the stored content from disk.
func TestTurn_CapableModelReceivesContent(t *testing.T) {
	calls := 0
	f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		return fakeReply{text: "nice image", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)
	att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what is this?", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	first := f.model.calls()[0]
	last := first[len(first)-1]
	require.NotEmpty(t, last.UserInputMultiContent, "the user message is multi-part")
	text := last.UserInputMultiContent[0]
	require.Equal(t, schema.ChatMessagePartTypeText, text.Type)
	require.Equal(t, "what is this?", text.Text)
	image := last.UserInputMultiContent[1]
	require.Equal(t, schema.ChatMessagePartTypeImageURL, image.Type)
	require.NotNil(t, image.Image.Base64Data)
	require.Equal(t, "png-bytes", decodeB64(t, *image.Image.Base64Data))
	require.Equal(t, "image/png", image.Image.MIMEType)

	// The next turn replays the stored content from disk (never re-upload):
	// the replayed user message carries the same content part.
	second, err := f.mgr.Send(t.Context(), sess.ID, "and again")
	require.NoError(t, err)
	require.NoError(t, second.Wait())

	secondInput := f.model.calls()[1]
	var replayed *schema.MessageInputPart
	for _, msg := range secondInput {
		if msg.Role != schema.User {
			continue
		}
		for i := range msg.UserInputMultiContent {
			if msg.UserInputMultiContent[i].Type == schema.ChatMessagePartTypeImageURL {
				replayed = &msg.UserInputMultiContent[i]
			}
		}
	}
	require.NotNil(t, replayed, "the replayed user message carries the image content")
	require.NotNil(t, replayed.Image.Base64Data)
	require.Equal(t, "png-bytes", decodeB64(t, *replayed.Image.Base64Data))
}

// TestTurn_PDFRidesWhereTheConnectorCarriesIt pins the PDF lane: a PDF
// degrades to the pointer note on providers whose connector cannot carry
// file parts (claude, OpenAI-shaped), and rides a file part on gemini.
func TestTurn_PDFRidesWhereTheConnectorCarriesIt(t *testing.T) {
	newSessionWithPDF := func(t *testing.T, providerType, model string) (*fixture, sessions.Session, sessions.Attachment) {
		t.Helper()
		f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "got the doc", promptTokens: 10, completionTokens: 2}
		})
		provider, err := f.store.Providers().ByID(t.Context(), f.agent.ProviderID)
		require.NoError(t, err)
		provider.Type = providerType
		require.NoError(t, f.store.Providers().Update(t.Context(), provider))
		agent := f.agent
		agent.Model = model
		require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
		f.agent = agent
		sess, err := f.mgr.CreateSession(t.Context(), agent.ID)
		require.NoError(t, err)
		pdf := saveUpload(t, atts, "doc.pdf", "application/pdf", "%PDF-1.4")
		return f, sess, pdf
	}

	// claude reads PDFs by catalog but its pinned connector cannot carry
	// file parts: the pointer note stands in, and the send must not fail.
	f, sess, pdf := newSessionWithPDF(t, ProviderAnthropic, "claude-sonnet-4-5")
	turn, err := f.mgr.Send(t.Context(), sess.ID, "read this", pdf.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	last := f.model.calls()[0]
	lastMsg := last[len(last)-1]
	sawPointer := false
	for _, part := range lastMsg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && strings.Contains(part.Text, "cannot read PDF input") {
			sawPointer = true
		}
	}
	require.True(t, sawPointer, "the claude connector receives the pointer note, not file bytes")

	// gemini's connector carries file parts and the model reads PDFs: the
	// bytes ride as a file content part.
	f, sess, pdf = newSessionWithPDF(t, ProviderGemini, "gemini-2.5-pro")
	turn, err = f.mgr.Send(t.Context(), sess.ID, "read this", pdf.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	last = f.model.calls()[0]
	lastMsg = last[len(last)-1]
	var filePart *schema.MessageInputPart
	for i := range lastMsg.UserInputMultiContent {
		if lastMsg.UserInputMultiContent[i].Type == schema.ChatMessagePartTypeFileURL {
			filePart = &lastMsg.UserInputMultiContent[i]
		}
	}
	require.NotNil(t, filePart, "gemini receives the PDF as a file part")
	require.NotNil(t, filePart.File.Base64Data)
	require.Equal(t, "%PDF-1.4", decodeB64(t, *filePart.File.Base64Data))
	require.Equal(t, "application/pdf", filePart.File.MIMEType)
}

// TestTurn_VideoRidesWhereTheConnectorCarriesIt pins the video lane (task
// 3.2, design D6): a video rides a video content part on a gemini-type
// provider whose stored row declares video input, and degrades to the
// pointer note when the row declares none or the connector cannot carry
// video parts (no catalog model advertises video today, so the flag always
// arrives through the stored row).
func TestTurn_VideoRidesWhereTheConnectorCarriesIt(t *testing.T) {
	newSessionWithVideo := func(t *testing.T, providerType string, videoInput bool) (*fixture, sessions.Session, sessions.Attachment) {
		t.Helper()
		f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "got the clip", promptTokens: 10, completionTokens: 2}
		})
		provider, err := f.store.Providers().ByID(t.Context(), f.agent.ProviderID)
		require.NoError(t, err)
		provider.Type = providerType
		require.NoError(t, f.store.Providers().Update(t.Context(), provider))
		agent := f.agent
		agent.Model = "video-capable-model"
		require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
		f.agent = agent
		require.NoError(t, f.store.ProviderModels().Create(t.Context(), &store.ProviderModel{
			ProviderID: agent.ProviderID,
			ModelID:    "video-capable-model",
			VideoInput: videoInput,
			Enabled:    true,
		}))
		sess, err := f.mgr.CreateSession(t.Context(), agent.ID)
		require.NoError(t, err)
		clip := saveUpload(t, atts, "clip.mp4", "video/mp4", "mp4-bytes")
		return f, sess, clip
	}

	sawPointerNote := func(t *testing.T, f *fixture) {
		t.Helper()
		last := f.model.calls()[0]
		lastMsg := last[len(last)-1]
		sawPointer := false
		for _, part := range lastMsg.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeText && strings.Contains(part.Text, "cannot read video input") {
				sawPointer = true
			}
			if part.Type == schema.ChatMessagePartTypeVideoURL {
				t.Fatal("video bytes reached a connector that cannot carry them")
			}
		}
		require.True(t, sawPointer, "the connector receives the pointer note, not video bytes")
	}

	// gemini's connector carries video parts and the stored row declares
	// video input: the bytes ride as a video content part.
	f, sess, clip := newSessionWithVideo(t, ProviderGemini, true)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "watch this", clip.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	last := f.model.calls()[0]
	lastMsg := last[len(last)-1]
	var videoPart *schema.MessageInputPart
	for i := range lastMsg.UserInputMultiContent {
		if lastMsg.UserInputMultiContent[i].Type == schema.ChatMessagePartTypeVideoURL {
			videoPart = &lastMsg.UserInputMultiContent[i]
		}
	}
	require.NotNil(t, videoPart, "gemini receives the video as a video part")
	require.NotNil(t, videoPart.Video.Base64Data)
	require.Equal(t, "mp4-bytes", decodeB64(t, *videoPart.Video.Base64Data))
	require.Equal(t, "video/mp4", videoPart.Video.MIMEType)

	// The stored row declares no video input: the pointer note stands in,
	// and the send must not fail.
	f, sess, clip = newSessionWithVideo(t, ProviderGemini, false)
	turn, err = f.mgr.Send(t.Context(), sess.ID, "watch this", clip.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "sending must not fail because the model lacks video input")
	sawPointerNote(t, f)

	// The row declares video input but the openai-shaped connector cannot
	// carry video parts: the pointer note stands in all the same.
	f, sess, clip = newSessionWithVideo(t, ProviderOpenAI, true)
	turn, err = f.mgr.Send(t.Context(), sess.ID, "watch this", clip.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "sending must not fail because the connector lacks video support")
	sawPointerNote(t, f)
}

// TestTurn_LostFileDegradesToPointer pins the read-failure degradation: a
// registered attachment whose file vanished must not fail the send.
func TestTurn_LostFileDegradesToPointer(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(input []*schema.Message) fakeReply {
		for _, part := range input[len(input)-1].UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeText && strings.Contains(part.Text, "could not be read") {
				return fakeReply{text: "the file is gone", promptTokens: 5, completionTokens: 1}
			}
		}
		return fakeReply{err: errString("the degraded note never reached the model")}
	})
	sess := f.newSession(t)
	att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")
	// The stored file disappears (a lost/corrupted upload directory): the
	// manager's store keeps the registry row but reads from an empty
	// directory, so the content read fails while the key still resolves.
	f.mgr.attachments = attachments.NewStore(t.TempDir(), f.store.Attachments())

	turn, err := f.mgr.Send(t.Context(), sess.ID, "look", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a lost file must not fail the send")
}

// TestTurn_TextAttachmentRidesInline pins the text lane (spec
// chat-attachments "Multi-part user messages"): a text file's content rides
// the user message as a text part — every model reads text, so no capability
// pointer stands in — and a lost text file still degrades to the pointer
// note without failing the send.
func TestTurn_TextAttachmentRidesInline(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "read the notes", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)
	att := saveUpload(t, atts, "notes.md", "text/markdown", "# heading\nbody text")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "summarize this", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	last := f.model.calls()[0]
	lastMsg := last[len(last)-1]
	require.NotEmpty(t, lastMsg.UserInputMultiContent, "the user message is multi-part")
	var content string
	for _, part := range lastMsg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			content += part.Text
		}
	}
	require.Contains(t, content, "# heading\nbody text", "the text attachment's content rides inline")
	require.NotContains(t, content, "not sent", "no capability pointer stands in for readable text")

	// A lost text file degrades like every other lane: the pointer note
	// stands in and the send succeeds.
	f.mgr.attachments = attachments.NewStore(t.TempDir(), f.store.Attachments())
	second, err := f.mgr.Send(t.Context(), sess.ID, "and again", att.Key)
	require.NoError(t, err)
	require.NoError(t, second.Wait(), "a lost file must not fail the send")

	secondInput := f.model.calls()[1]
	var note string
	for _, part := range secondInput[len(secondInput)-1].UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			note += part.Text
		}
	}
	require.Contains(t, note, "could not be read", "the lost-file pointer note reaches the model")
}

// TestTurn_OfficeAttachmentRidesByReference pins the office lane's turn
// semantics (add-document-editing task 5.5, mirroring the text lane's
// non-multimodal behavior): an OOXML attachment never rides its bytes —
// every model gets the same reference note pointing at the save_document
// copy path — and the send never fails, whatever the model's capabilities.
func TestTurn_OfficeAttachmentRidesByReference(t *testing.T) {
	// A model outside the catalog reads no attachment content: the office
	// note must be capability-independent, like the text lane's inline ride.
	f, atts := newAttachmentFixture(t, func(input []*schema.Message) fakeReply {
		last := input[len(input)-1]
		if len(last.UserInputMultiContent) == 0 {
			return fakeReply{err: errString("the multi-part message never reached the model")}
		}
		for _, part := range last.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeFileURL {
				return fakeReply{err: errString("office bytes rode as a file part")}
			}
		}
		for _, part := range last.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeText && strings.Contains(part.Text, "save_document") {
				return fakeReply{text: "I will save it", promptTokens: 10, completionTokens: 2}
			}
		}
		return fakeReply{err: errString("the office reference note never reached the model")}
	})

	agent := f.agent
	agent.Model = "totally-unknown-model"
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent

	sess, err := f.mgr.CreateSession(t.Context(), agent.ID)
	require.NoError(t, err)
	att := saveUpload(t, atts, "report.docx", attachments.MimeDOCX, "docx-bytes")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "edit this report", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "sending must not fail because office bytes never ride the message")

	note := f.model.calls()[0][len(f.model.calls()[0])-1]
	var text string
	for _, part := range note.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			text += part.Text
		}
	}
	require.Contains(t, text, "save_document", "the note names the knowledge-scope copy path")
	require.Contains(t, text, att.Key, "the note carries the attachment key")
	require.NotContains(t, text, "docx-bytes", "the bytes never ride the message")
}

// decodeB64 decodes a base64 part payload for assertions.
func decodeB64(t *testing.T, raw string) string {
	t.Helper()
	out, err := base64.StdEncoding.DecodeString(raw)
	require.NoError(t, err)
	return string(out)
}

// errString is a tiny error type for scripted failures.
type errString string

func (e errString) Error() string { return string(e) }
