package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// 6.1 — persist demotion in the session adapter
// ---------------------------------------------------------------------------

func TestSessionAdapter_DemotesAttachmentBytesOnPersist(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws-demote")

	imgBlob := []byte{0x89, 0x50, 0x4E, 0x47, 0xFA, 0xCE}
	pdfBlob := []byte("%PDF-demote")
	url := "/api/v1/files/att-cap/att-img"

	userMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: "look at these"}),
			schema.NewContentBlock(&schema.UserInputImage{
				URL:        url,
				Base64Data: base64.StdEncoding.EncodeToString(imgBlob),
				MIMEType:   "image/png",
			}),
			schema.NewContentBlock(&schema.UserInputFile{
				URL:        "/api/v1/files/att-cap/att-pdf",
				Base64Data: base64.StdEncoding.EncodeToString(pdfBlob),
				MIMEType:   "application/pdf",
				Name:       "report.pdf",
			}),
		},
	}
	setAttachmentBlockMeta(userMsg.ContentBlocks[1], attachmentBlockMeta{ID: "att-img", Name: "shot.png", Mime: "image/png", Size: int64(len(imgBlob)), Lane: attLaneInlineImage, URL: url})
	setAttachmentBlockMeta(userMsg.ContentBlocks[2], attachmentBlockMeta{ID: "att-pdf", Name: "report.pdf", Mime: "application/pdf", Size: int64(len(pdfBlob)), Lane: attLaneInlinePDF})

	ev := &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   "evt-demote-1",
		TurnID:    "turn-demote",
		Timestamp: time.Now().UTC(),
		Message:   userMsg,
	}
	if err := adapter.AppendEvents(ctx, "sess-demote", []*adk.SessionEvent[*schema.AgenticMessage]{ev}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	// The caller's event/message must be untouched: the ADK holds them for the
	// running turn.
	if got := userMsg.ContentBlocks[1].UserInputImage.Base64Data; got != base64.StdEncoding.EncodeToString(imgBlob) {
		t.Fatal("caller's image block was mutated in place")
	}
	if got := userMsg.ContentBlocks[2].UserInputFile.Base64Data; got == "" {
		t.Fatal("caller's file block was mutated in place")
	}

	res, err := adapter.LoadEvents(ctx, "sess-demote", &adk.LoadSessionEventsRequest{})
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Message == nil {
		t.Fatalf("expected one persisted message event, got %+v", res.Events)
	}
	blocks := res.Events[0].Message.ContentBlocks
	if len(blocks) != 3 {
		t.Fatalf("persisted block count = %d, want 3", len(blocks))
	}
	if text := blocks[0].UserInputText.Text; text != "look at these" {
		t.Errorf("persisted text block = %q, want the user's own text", text)
	}
	img := blocks[1].UserInputImage
	if img == nil {
		t.Fatal("persisted image block missing")
	}
	if img.Base64Data != "" {
		t.Error("persisted image block must carry no base64 bytes")
	}
	if img.URL != url || img.MIMEType != "image/png" {
		t.Errorf("persisted image reference = (url %q, mime %q), want URL and mime preserved", img.URL, img.MIMEType)
	}
	file := blocks[2].UserInputFile
	if file == nil || file.Base64Data != "" || file.URL == "" || file.Name != "report.pdf" || file.MIMEType != "application/pdf" {
		t.Errorf("persisted file block = %+v, want URL-only reference with name and mime preserved", file)
	}

	// The stamped identity survives the serializer round-trip: references
	// preserve name/mime/size for transcript fidelity (7.2).
	meta, ok := attachmentBlockMetaOf(blocks[1])
	if !ok {
		t.Fatal("persisted image block lost its attachment identity Extra")
	}
	if meta.Name != "shot.png" || meta.Mime != "image/png" || meta.Size != int64(len(imgBlob)) {
		t.Errorf("persisted image identity = %+v, want name/mime/size preserved", meta)
	}
}

// TestSessionAdapter_NoBase64InStoredPayloads proves the demotion at the
// payload level: the serialized bytes in the store contain no base64, while
// the reference metadata does appear (attachments design D6, spec "Session
// payload stays lean").
func TestSessionAdapter_NoBase64InStoredPayloads(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws-lean")

	blob := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x42, 0x13, 0x37}
	encoded := base64.StdEncoding.EncodeToString(blob)
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputImage{URL: "/api/v1/files/att-cap/a1", Base64Data: encoded, MIMEType: "image/png"}),
		},
	}
	ev := &adk.SessionEvent[*schema.AgenticMessage]{EventID: "evt-lean", TurnID: "turn-lean", Timestamp: time.Now().UTC(), Message: msg}
	if err := adapter.AppendEvents(ctx, "sess-lean", []*adk.SessionEvent[*schema.AgenticMessage]{ev}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	rows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: "ws-lean",
		SessionID:   "sess-lean",
	})
	if err != nil {
		t.Fatalf("load raw rows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows persisted")
	}
	for _, row := range rows {
		if strings.Contains(string(row.Payload), encoded) {
			t.Fatalf("base64 payload leaked into stored event %q", row.EventID)
		}
		if !strings.Contains(string(row.Payload), "/api/v1/files/att-cap/a1") {
			t.Errorf("stored event %q lost the capability URL reference", row.EventID)
		}
	}
}

// ---------------------------------------------------------------------------
// fix-image-attachment-lane — model-bound blocks carry bytes, URLs ride meta
// ---------------------------------------------------------------------------

// TestBuildAttachmentUserMessage_BytesOnlyPayloads pins the wire shape
// (fix-image-attachment-lane D1): image and file blocks carry Base64Data +
// MIMEType and NO URL — the connector's resolveURL prefers a non-empty URL
// over the bytes, and providers cannot fetch server-relative capability URLs.
func TestBuildAttachmentUserMessage_BytesOnlyPayloads(t *testing.T) {
	imgBlob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	pdfBlob := []byte("%PDF-wire")
	blobs := newStubBlobs(map[string][]byte{"att-img": imgBlob, "att-pdf": pdfBlob})
	r := &Runner{attachmentBlobs: blobs}
	req := ExecRequest{
		WorkspaceID: "ws-wire",
		Input:       "look",
		Attachments: []AttachmentRef{
			{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(imgBlob))},
			{ID: "att-pdf", Name: "report.pdf", MimeType: "application/pdf", Lane: attLaneInlinePDF, Size: int64(len(pdfBlob))},
		},
	}

	msg, err := r.buildAttachmentUserMessage(context.Background(), req, inputModality{})
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}
	if len(msg.ContentBlocks) != 3 {
		t.Fatalf("block count = %d, want 3 (text, image, file)", len(msg.ContentBlocks))
	}

	img := msg.ContentBlocks[1].UserInputImage
	if img == nil {
		t.Fatal("image block missing")
	}
	if img.URL != "" {
		t.Errorf("image payload URL = %q, want empty (the URL must ride the meta)", img.URL)
	}
	if img.Base64Data != base64.StdEncoding.EncodeToString(imgBlob) || img.MIMEType != "image/png" {
		t.Errorf("image payload = (b64 %q, mime %q), want the bytes and image/png", img.Base64Data, img.MIMEType)
	}
	meta, ok := attachmentBlockMetaOf(msg.ContentBlocks[1])
	if !ok || meta.URL != "/api/v1/files/att-cap/att-img" {
		t.Errorf("image meta = (%+v, %v), want the capability URL in the meta", meta, ok)
	}

	file := msg.ContentBlocks[2].UserInputFile
	if file == nil {
		t.Fatal("file block missing")
	}
	if file.URL != "" {
		t.Errorf("file payload URL = %q, want empty (the URL must ride the meta)", file.URL)
	}
	if file.Base64Data != base64.StdEncoding.EncodeToString(pdfBlob) || file.MIMEType != "application/pdf" || file.Name != "report.pdf" {
		t.Errorf("file payload = %+v, want the bytes, application/pdf, report.pdf", file)
	}
	meta, ok = attachmentBlockMetaOf(msg.ContentBlocks[2])
	if !ok || meta.URL != "/api/v1/files/att-cap/att-pdf" {
		t.Errorf("file meta = (%+v, %v), want the capability URL in the meta", meta, ok)
	}
}

// TestAttachmentMetasOf_URLFromMeta pins the transcript pill URL resolution on
// the current shape: the payload URL is empty, so the meta URL wins — for both
// Extra shapes the meta can arrive in (registered round-trip struct and the
// plain-JSON map), and for both block kinds.
func TestAttachmentMetasOf_URLFromMeta(t *testing.T) {
	const url = "/api/v1/files/att-cap/meta-url"

	typed := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.UserInputImage{Base64Data: "AAEC", MIMEType: "image/png"}),
		schema.NewContentBlock(&schema.UserInputFile{Base64Data: "AAEC", MIMEType: "application/pdf", Name: "report.pdf"}),
	}}
	setAttachmentBlockMeta(typed.ContentBlocks[0], attachmentBlockMeta{ID: "a1", Name: "shot.png", Mime: "image/png", Size: 3, Lane: attLaneInlineImage, URL: url})
	setAttachmentBlockMeta(typed.ContentBlocks[1], attachmentBlockMeta{ID: "a2", Name: "report.pdf", Mime: "application/pdf", Size: 3, Lane: attLaneInlinePDF, URL: url})
	want := []AttachmentMeta{
		{Name: "shot.png", MimeType: "image/png", Size: 3, URL: url},
		{Name: "report.pdf", MimeType: "application/pdf", Size: 3, URL: url},
	}
	if got := attachmentMetasOf(typed); !reflect.DeepEqual(got, want) {
		t.Errorf("typed-shape metas = %+v, want %+v", got, want)
	}

	// The map shape is what a plain JSON boundary leaves behind (the
	// registered type degrades to map[string]any with JSON-typed scalars).
	raw, err := json.Marshal(typed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var mapped schema.AgenticMessage
	if err := json.Unmarshal(raw, &mapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, block := range mapped.ContentBlocks {
		if _, ok := block.Extra[AttachmentMetaExtraKey].(map[string]any); !ok {
			t.Fatalf("expected the meta to degrade to a map across JSON, got %T", block.Extra[AttachmentMetaExtraKey])
		}
	}
	if got := attachmentMetasOf(&mapped); !reflect.DeepEqual(got, want) {
		t.Errorf("map-shape metas = %+v, want %+v (the map branch must decode the meta url)", got, want)
	}
}

// TestAttachmentMetasOf_LegacyPayloadURLFallback pins the fallback for old
// transcripts: blocks demoted before the meta carried a URL keep their pill
// link via the payload URL.
func TestAttachmentMetasOf_LegacyPayloadURLFallback(t *testing.T) {
	const url = "/api/v1/files/att-cap/legacy"
	msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.UserInputImage{URL: url, MIMEType: "image/png"}),
		schema.NewContentBlock(&schema.UserInputFile{URL: url, MIMEType: "application/pdf", Name: "report.pdf"}),
	}}
	setAttachmentBlockMeta(msg.ContentBlocks[0], attachmentBlockMeta{ID: "a1", Name: "shot.png", Mime: "image/png", Size: 3, Lane: attLaneInlineImage})
	setAttachmentBlockMeta(msg.ContentBlocks[1], attachmentBlockMeta{ID: "a2", Name: "report.pdf", Mime: "application/pdf", Size: 3, Lane: attLaneInlinePDF})

	got := attachmentMetasOf(msg)
	if len(got) != 2 {
		t.Fatalf("metas = %+v, want 2 entries", got)
	}
	for _, meta := range got {
		if meta.URL != url {
			t.Errorf("legacy %s meta URL = %q, want the payload URL %q", meta.Name, meta.URL, url)
		}
	}
}

// TestIsStaleAttachmentBlock_MetadataAware pins the URL-only-reference
// predicate on both demotion shapes (fix-image-attachment-lane 1.3): bytes
// gone plus a surviving URL — payload-borne (legacy) or meta-borne (current)
// — is stale; byte-carrying blocks are never stale.
func TestIsStaleAttachmentBlock_MetadataAware(t *testing.T) {
	t.Run("legacy demoted: payload URL", func(t *testing.T) {
		block := schema.NewContentBlock(&schema.UserInputImage{URL: "/api/v1/files/att-cap/old", MIMEType: "image/png"})
		if !isStaleAttachmentBlock(block) {
			t.Error("a demoted payload-URL image block must classify as stale")
		}
		file := schema.NewContentBlock(&schema.UserInputFile{URL: "/api/v1/files/att-cap/old", MIMEType: "application/pdf"})
		if !isStaleAttachmentBlock(file) {
			t.Error("a demoted payload-URL file block must classify as stale")
		}
	})
	t.Run("current demoted: meta URL", func(t *testing.T) {
		block := schema.NewContentBlock(&schema.UserInputImage{MIMEType: "image/png"})
		setAttachmentBlockMeta(block, attachmentBlockMeta{ID: "a1", Name: "shot.png", Mime: "image/png", Lane: attLaneInlineImage, URL: "/api/v1/files/att-cap/new"})
		if !isStaleAttachmentBlock(block) {
			t.Error("a demoted meta-URL image block must classify as stale")
		}
		file := schema.NewContentBlock(&schema.UserInputFile{MIMEType: "application/pdf", Name: "report.pdf"})
		setAttachmentBlockMeta(file, attachmentBlockMeta{ID: "a2", Name: "report.pdf", Mime: "application/pdf", Lane: attLaneInlinePDF, URL: "/api/v1/files/att-cap/new"})
		if !isStaleAttachmentBlock(file) {
			t.Error("a demoted meta-URL file block must classify as stale")
		}
	})
	t.Run("fresh byte-carrying blocks are never stale", func(t *testing.T) {
		block := schema.NewContentBlock(&schema.UserInputImage{Base64Data: "AAEC", MIMEType: "image/png"})
		setAttachmentBlockMeta(block, attachmentBlockMeta{ID: "a1", Name: "shot.png", Mime: "image/png", Lane: attLaneInlineImage, URL: "/api/v1/files/att-cap/new"})
		if isStaleAttachmentBlock(block) {
			t.Error("a byte-carrying image block must never classify as stale")
		}
		file := schema.NewContentBlock(&schema.UserInputFile{Base64Data: "AAEC", MIMEType: "application/pdf"})
		setAttachmentBlockMeta(file, attachmentBlockMeta{ID: "a2", Name: "report.pdf", Mime: "application/pdf", Lane: attLaneInlinePDF, URL: "/api/v1/files/att-cap/new"})
		if isStaleAttachmentBlock(file) {
			t.Error("a byte-carrying file block must never classify as stale")
		}
	})
}

// TestAttachmentPlaceholderText_ResolvesFromMeta pins the placeholder's path
// resolution on the current demotion shape: the payload URL is empty, so the
// name and workspace path come from the meta.
func TestAttachmentPlaceholderText_ResolvesFromMeta(t *testing.T) {
	const url = "/api/v1/files/att-cap/meta-url"
	block := schema.NewContentBlock(&schema.UserInputImage{MIMEType: "image/png"})
	setAttachmentBlockMeta(block, attachmentBlockMeta{ID: "a1", Name: "shot.png", Mime: "image/png", Size: 3, Lane: attLaneInlineImage, URL: url})

	text := attachmentPlaceholderText(block)
	if !strings.Contains(text, "shot.png") || !strings.Contains(text, "image/png") || !strings.Contains(text, url) {
		t.Errorf("placeholder %q must name the file, its mime, and the meta URL path", text)
	}
}

// TestSessionAdapter_RoundTripKeepsPillURLs is the persist+reload regression
// for fix-image-attachment-lane 1.4/1.5: a turn built with the current wire
// shape (payload has no URL; the capability URL rides the meta) persists its
// demoted blocks and reloads with the pill URLs intact — for both the image
// and the PDF block — and the reloaded blocks classify as stale references.
func TestSessionAdapter_RoundTripKeepsPillURLs(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws-pill")

	imgBlob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	pdfBlob := []byte("%PDF-pill")
	blobs := newStubBlobs(map[string][]byte{"att-img": imgBlob, "att-pdf": pdfBlob})
	builder := &Runner{attachmentBlobs: blobs}
	msg, err := builder.buildAttachmentUserMessage(ctx, ExecRequest{
		WorkspaceID: "ws-pill",
		Input:       "review these",
		Attachments: []AttachmentRef{
			{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(imgBlob))},
			{ID: "att-pdf", Name: "report.pdf", MimeType: "application/pdf", Lane: attLaneInlinePDF, Size: int64(len(pdfBlob))},
		},
	}, inputModality{})
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}

	ev := &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   "evt-pill",
		TurnID:    "turn-pill",
		Timestamp: time.Now().UTC(),
		Message:   msg,
	}
	if err := adapter.AppendEvents(ctx, "sess-pill", []*adk.SessionEvent[*schema.AgenticMessage]{ev}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	// The stored payload carries the URLs through the meta, never the bytes.
	rows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{WorkspaceID: "ws-pill", SessionID: "sess-pill"})
	if err != nil {
		t.Fatalf("load raw rows: %v", err)
	}
	for _, row := range rows {
		payload := string(row.Payload)
		if strings.Contains(payload, base64.StdEncoding.EncodeToString(imgBlob)) {
			t.Error("base64 image bytes leaked into the stored payload")
		}
		if !strings.Contains(payload, "/api/v1/files/att-cap/att-img") || !strings.Contains(payload, "/api/v1/files/att-cap/att-pdf") {
			t.Error("stored payload lost the capability URLs")
		}
	}

	res, err := adapter.LoadEvents(ctx, "sess-pill", &adk.LoadSessionEventsRequest{})
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Message == nil {
		t.Fatalf("expected one persisted message event, got %+v", res.Events)
	}
	reloaded := res.Events[0].Message

	// Hydrated pills: both attachments keep name/mime/size and the capability
	// URL sourced from the block meta.
	want := []AttachmentMeta{
		{Name: "shot.png", MimeType: "image/png", Size: int64(len(imgBlob)), URL: "/api/v1/files/att-cap/att-img"},
		{Name: "report.pdf", MimeType: "application/pdf", Size: int64(len(pdfBlob)), URL: "/api/v1/files/att-cap/att-pdf"},
	}
	if got := attachmentMetasOf(reloaded); !reflect.DeepEqual(got, want) {
		t.Errorf("hydrated attachment metas = %+v, want %+v", got, want)
	}

	// The demoted blocks are exactly the URL-only-reference shape the
	// placeholder middleware collapses: bytes gone, URL only in the meta.
	for _, block := range reloaded.ContentBlocks {
		if block.UserInputImage != nil {
			if block.UserInputImage.Base64Data != "" || block.UserInputImage.URL != "" {
				t.Error("reloaded image block must be demoted (no bytes, no payload URL)")
			}
			if !isStaleAttachmentBlock(block) {
				t.Error("reloaded image block must classify as a stale URL reference")
			}
		}
		if block.UserInputFile != nil {
			if block.UserInputFile.Base64Data != "" || block.UserInputFile.URL != "" {
				t.Error("reloaded file block must be demoted (no bytes, no payload URL)")
			}
			if !isStaleAttachmentBlock(block) {
				t.Error("reloaded file block must classify as a stale URL reference")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 6.2 — the model-time placeholder middleware
// ---------------------------------------------------------------------------

func TestCollapseStaleAttachmentBlocks(t *testing.T) {
	staleImage := schema.NewContentBlock(&schema.UserInputImage{URL: "/api/v1/files/att-cap/att-img", MIMEType: "image/png"})
	setAttachmentBlockMeta(staleImage, attachmentBlockMeta{ID: "att-img", Name: "shot.png", Mime: "image/png", Size: 42, Lane: attLaneInlineImage})
	freshImage := schema.NewContentBlock(&schema.UserInputImage{URL: "/api/v1/files/att-cap/att-new", Base64Data: "AAEC", MIMEType: "image/png"})
	userMsg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "earlier"}), staleImage},
	}
	assistantMsg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "reply"})},
	}
	freshMsg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{freshImage}}
	msgs := []*schema.AgenticMessage{userMsg, assistantMsg, freshMsg}

	out := collapseStaleAttachmentBlocks(msgs)

	// Copy-on-write: the originals are untouched (live transcript mapping
	// reads them).
	if staleImage.UserInputImage.Base64Data != "" || staleImage.UserInputImage.URL == "" {
		t.Fatal("input block was mutated in place")
	}
	if out[0] == userMsg {
		t.Fatal("a rewritten message must be a clone, not the original")
	}
	// Untouched messages are aliased.
	if out[1] != assistantMsg {
		t.Error("an untouched assistant message must be aliased")
	}
	if out[2] != freshMsg {
		t.Error("a message without stale blocks must be aliased")
	}

	blocks := out[0].ContentBlocks
	if len(blocks) != 2 {
		t.Fatalf("rewritten block count = %d, want 2", len(blocks))
	}
	if got := blocks[1]; got.Type != schema.ContentBlockTypeUserInputText || got.UserInputText == nil {
		t.Fatalf("stale image block = %v, want a UserInputText placeholder", got.Type)
	}
	text := blocks[1].UserInputText.Text
	if !strings.Contains(text, "shot.png") || !strings.Contains(text, "image/png") || !strings.Contains(text, "/api/v1/files/att-cap/att-img") {
		t.Errorf("placeholder %q must name the file, its mime, and the workspace path", text)
	}
	if !isAttachmentPointerNote(blocks[1]) {
		t.Error("placeholder must be marked so it never reaches transcript text")
	}
	// The byte-carrying block passes untouched.
	if out[2].ContentBlocks[0].UserInputImage.Base64Data != "AAEC" {
		t.Error("byte-carrying blocks must pass through unchanged")
	}

	if got := collapseStaleAttachmentBlocks(nil); got != nil {
		t.Error("nil input must stay nil")
	}
	plain := []*schema.AgenticMessage{assistantMsg}
	if got := collapseStaleAttachmentBlocks(plain); len(got) != 1 || got[0] != assistantMsg {
		t.Error("sessions without stale blocks must come back untouched")
	}
}

// ---------------------------------------------------------------------------
// 6.3 — current-turn lifetime across the runner
// ---------------------------------------------------------------------------

// toolLoopCaptureModel replies with a tool call on the first model call and
// a final answer afterwards, recording every model input per call.
type toolLoopCaptureModel struct {
	mu     sync.Mutex
	calls  int
	inputs [][]*schema.AgenticMessage
}

func (m *toolLoopCaptureModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.calls++
	m.inputs = append(m.inputs, append([]*schema.AgenticMessage(nil), input...))
	call := m.calls
	m.mu.Unlock()
	if call == 1 {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID: "call-loop-1", Name: "test.echo", Arguments: `{}`,
				}},
			},
		}, nil
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "done"})},
	}, nil
}

func (m *toolLoopCaptureModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *toolLoopCaptureModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *toolLoopCaptureModel) snapshot() [][]*schema.AgenticMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]*schema.AgenticMessage, len(m.inputs))
	copy(out, m.inputs)
	return out
}

type echoTool struct{}

func (echoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "test.echo", Desc: "echoes"}, nil
}

func (echoTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return "echo-result", nil
}

// userImageOf returns the image payload of the last user message in a model
// input, failing the test when there is none.
func userImageOf(t *testing.T, input []*schema.AgenticMessage) *schema.UserInputImage {
	t.Helper()
	for i := len(input) - 1; i >= 0; i-- {
		msg := input[i]
		if msg.Role != schema.AgenticRoleTypeUser {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block.UserInputImage != nil {
				return block.UserInputImage
			}
		}
	}
	t.Fatalf("no image block in any user message of the input (%d messages)", len(input))
	return nil
}

// rewireModel re-points the runner's model factory at mdl (rewindProbeModel
// precedent), for tests that need a fresh probe per turn.
func rewireModel(runner *Runner, mdl Model) {
	runner.agenticFactory = func(context.Context, string, providers.Credential, string) (Model, error) {
		return mdl, nil
	}
}

// lastUserMessage returns the last user-role message of a model input (the
// current turn's message), failing the test when there is none.
func lastUserMessage(t *testing.T, msgs []*schema.AgenticMessage) *schema.AgenticMessage {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.AgenticRoleTypeUser {
			return msgs[i]
		}
	}
	t.Fatal("no user message in the model input")
	return nil
}

// TestRunner_BytesOnEveryModelCallOfTheTurn pins the carrying-turn guarantee
// (spec "Image visible across one turn's tool loop"): a multi-iteration turn
// delivers the image bytes on EVERY model call.
func TestRunner_BytesOnEveryModelCallOfTheTurn(t *testing.T) {
	blob := []byte{0x11, 0x22, 0x33, 0x44, 0x55}
	encoded := base64.StdEncoding.EncodeToString(blob)
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &toolLoopCaptureModel{}
	reg := NewToolRegistry()
	reg.Register("test.echo", func(ToolContext) (tool.BaseTool, error) { return echoTool{}, nil })
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs), WithToolRegistry(reg))
	req.AllowedTools = []string{"test.echo"}
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("unexpected stream outcome, got %+v", events)
	}
	if probe.callCount() < 2 {
		t.Fatalf("expected a multi-iteration turn (tool call then answer), got %d model calls", probe.callCount())
	}
	for i, input := range probe.snapshot() {
		if got := userImageOf(t, input); got.Base64Data != encoded {
			t.Errorf("model call %d: image bytes missing — they must ride every call of the carrying turn", i+1)
		}
	}
}

// TestRunner_NextTurnSeesPlaceholderNotBytes pins the older-turn collapse
// (spec "Older attachments collapse to placeholders"): a second turn on the
// persisted session receives a text placeholder naming the file and its
// workspace path, and no image bytes anywhere on the wire.
func TestRunner_NextTurnSeesPlaceholderNotBytes(t *testing.T) {
	blob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	encoded := base64.StdEncoding.EncodeToString(blob)
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	first := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, first, WithAttachmentBlobs(blobs))
	req.Input = "what is this?"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("turn 1 did not complete, got %+v", ev)
	}

	// Second turn on the same persisted session: no attachments carried.
	second := &captureModel{}
	rewireModel(runner, second)
	req2 := req
	req2.Input = "and now without attachments"
	req2.Attachments = nil
	stream2, err := runner.Run(context.Background(), req2)
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if ev := collectStream(t, stream2); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("turn 2 did not complete, got %+v", ev)
	}

	msgs := second.captured()
	if len(msgs) == 0 {
		t.Fatal("turn 2 never called the model")
	}
	sawPlaceholder := false
	for _, msg := range msgs {
		payload, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		if strings.Contains(string(payload), encoded) {
			t.Fatal("turn 2 model input contains the older turn's image bytes")
		}
		if msg.Role != schema.AgenticRoleTypeUser {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block.UserInputImage != nil {
				t.Errorf("turn 2 user message still carries an image block (url %q)", block.UserInputImage.URL)
			}
			if block.UserInputText != nil && strings.Contains(block.UserInputText.Text, "shot.png") &&
				strings.Contains(block.UserInputText.Text, "/api/v1/files/att-cap/att-img") {
				sawPlaceholder = true
			}
		}
	}
	if !sawPlaceholder {
		t.Error("turn 2 model input must carry the older image as a placeholder naming the file and its workspace path")
	}
}

// TestRunner_RegeneratedTurnRedeliversBytes pins regeneration (spec
// "Regenerated turn re-expands its attachments"): re-running the same request
// is a fresh construction, so the bytes ride again without re-upload, while
// the older sibling turn stays a placeholder.
func TestRunner_RegeneratedTurnRedeliversBytes(t *testing.T) {
	blob := []byte{0x0A, 0x0B, 0x0C, 0x0D, 0x0E}
	encoded := base64.StdEncoding.EncodeToString(blob)
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	first := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, first, WithAttachmentBlobs(blobs))
	req.Input = "describe this"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("turn 1 did not complete, got %+v", ev)
	}

	regen := &captureModel{}
	rewireModel(runner, regen)
	stream2, err := runner.Run(context.Background(), req) // same refs, fresh run
	if err != nil {
		t.Fatalf("regenerated Run: %v", err)
	}
	if ev := collectStream(t, stream2); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("regenerated turn did not complete, got %+v", ev)
	}

	freshUser := lastUserMessage(t, regen.captured())
	if got := userImageOf(t, []*schema.AgenticMessage{freshUser}); got.Base64Data != encoded {
		t.Error("regenerated turn must deliver the attachment bytes to the model again")
	}
	// The earlier turn's copy in context must be a placeholder: the fresh
	// user message is the only place bytes appear.
	count := 0
	for _, msg := range regen.captured() {
		if msg.Role != schema.AgenticRoleTypeUser {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block.UserInputImage != nil && block.UserInputImage.Base64Data == encoded {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("byte-carrying image blocks in regenerated context = %d, want exactly 1 (older turn collapsed)", count)
	}
}

// ---------------------------------------------------------------------------
// 7.3 — live == hydrated transcript fidelity
// ---------------------------------------------------------------------------

// liveUserMessageOf returns the live stream's user message_completed event.
func liveUserMessageOf(t *testing.T, events []TranscriptEvent) *CompletedMessage {
	t.Helper()
	for i := range events {
		ev := events[i]
		if ev.Kind == TranscriptEventMessageCompleted && ev.Message != nil && ev.Message.Role == "user" {
			return ev.Message
		}
	}
	t.Fatalf("live stream has no user message_completed event, got %+v", events)
	return nil
}

// projectedUserMessageOf returns the hydrated transcript's user
// message_completed event.
func projectedUserMessageOf(t *testing.T, result *HistoryResult) *CompletedMessage {
	t.Helper()
	for i := range result.Events {
		ev := result.Events[i]
		if ev.Kind == TranscriptEventMessageCompleted && ev.Message != nil && ev.Message.Role == "user" {
			return ev.Message
		}
	}
	t.Fatalf("hydrated transcript has no user message_completed event, got %+v", result.Events)
	return nil
}

func TestRunner_TranscriptRoundTripImageAndPDF(t *testing.T) {
	imgBlob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	pdfBlob := []byte("%PDF-roundtrip")
	blobs := newStubBlobs(map[string][]byte{"att-img": imgBlob, "att-pdf": pdfBlob})
	probe := &captureModel{}
	runner, ws, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = "review these"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(imgBlob))},
		{ID: "att-pdf", Name: "report.pdf", MimeType: "application/pdf", Lane: "inline-pdf", Size: int64(len(pdfBlob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("turn did not complete, got %+v", events)
	}

	want := []AttachmentMeta{
		{Name: "shot.png", MimeType: "image/png", Size: int64(len(imgBlob)), URL: "/api/v1/files/att-cap/att-img"},
		{Name: "report.pdf", MimeType: "application/pdf", Size: int64(len(pdfBlob)), URL: "/api/v1/files/att-cap/att-pdf"},
	}

	live := liveUserMessageOf(t, events)
	if live.Content != "review these" {
		t.Errorf("live user content = %q, want the user's own text", live.Content)
	}
	if !reflect.DeepEqual(live.Attachments, want) {
		t.Errorf("live attachments = %+v, want %+v", live.Attachments, want)
	}

	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	got := projectedUserMessageOf(t, hist)
	if got.Content != live.Content {
		t.Errorf("hydrated user text = %q, live = %q", got.Content, live.Content)
	}
	if !reflect.DeepEqual(got.Attachments, live.Attachments) {
		t.Errorf("hydrated attachments %+v must equal live %+v (same name/mime/size/url, same order)", got.Attachments, live.Attachments)
	}
}

func TestRunner_TranscriptRoundTripAttachmentOnly(t *testing.T) {
	blob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, ws, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = "" // attachment-only send
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("turn did not complete, got %+v", events)
	}

	live := liveUserMessageOf(t, events)
	if live.Content != "" {
		t.Errorf("attachment-only live user content = %q, want empty", live.Content)
	}
	if len(live.Attachments) != 1 {
		t.Fatalf("attachment-only live user message must carry its attachment, got %+v", live.Attachments)
	}

	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	got := projectedUserMessageOf(t, hist)
	if got.Content != "" || !reflect.DeepEqual(got.Attachments, live.Attachments) {
		t.Errorf("attachment-only projection = (content %q, attachments %+v), want empty content and the live metadata %+v", got.Content, got.Attachments, live.Attachments)
	}
}

func TestRunner_TranscriptRoundTripPointerNoteHidden(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{"att-dump": []byte("dump bytes")})
	probe := &captureModel{}
	runner, ws, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = "import this dump"
	req.Attachments = []AttachmentRef{
		{ID: "att-dump", Name: "dump.sql", MimeType: "application/sql", Lane: attLaneDrop, Size: 10},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("turn did not complete, got %+v", events)
	}

	live := liveUserMessageOf(t, events)
	if live.Content != "import this dump" || strings.Contains(live.Content, "read-only") {
		t.Errorf("live user text = %q, want only the user's own text with no pointer note", live.Content)
	}

	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	got := projectedUserMessageOf(t, hist)
	if got.Content != live.Content {
		t.Errorf("hydrated user text = %q, live = %q", got.Content, live.Content)
	}
	for _, att := range got.Attachments {
		if att.Name == "dump.sql" && att.URL == "" {
			t.Error("drop-lane attachment chip must carry its capability URL")
		}
	}
	// The pointer note is model-facing plumbing: it reaches the model but is
	// absent from both text paths.
	modelMsg := probe.capturedUserMessage(t)
	raw := ""
	for _, block := range modelMsg.ContentBlocks {
		if block.UserInputText != nil {
			raw += block.UserInputText.Text
		}
	}
	if !strings.Contains(raw, "read-only") {
		t.Error("the pointer note must reach the model message")
	}
	if strings.Contains(extractAgenticText(modelMsg), "read-only") {
		t.Error("extractAgenticText must skip the pointer note block")
	}
}
