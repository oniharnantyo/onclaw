package agents

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
)

// fakeModalityResolver is an injectable InputModalityResolver: it answers
// from a per-kind map (absent kinds default to unknown) and records the last
// consultation arguments so tests can pin the (provider, hint, model)
// threading from resolve().
type fakeModalityResolver struct {
	mu       sync.Mutex
	supports map[domain.InputKind]domain.InputSupport

	lastProviderType string
	lastHint         string
	lastModel        string
	calls            int
}

func newFakeModalityResolver(supports map[domain.InputKind]domain.InputSupport) *fakeModalityResolver {
	return &fakeModalityResolver{supports: supports}
}

func (f *fakeModalityResolver) SupportsInput(_ context.Context, providerType, catalogHint, modelID string, kind domain.InputKind) domain.InputSupport {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastProviderType, f.lastHint, f.lastModel = providerType, catalogHint, modelID
	if s, ok := f.supports[kind]; ok {
		return s
	}
	return domain.InputUnknown
}

func (f *fakeModalityResolver) lastCall() (providerType, hint, mdl string, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastProviderType, f.lastHint, f.lastModel, f.calls
}

// unsupportedImageResolver answers unsupported for images, supported for PDFs
// — the text-only-model profile the degrade scenarios run against.
func unsupportedImageResolver() *fakeModalityResolver {
	return newFakeModalityResolver(map[domain.InputKind]domain.InputSupport{
		domain.InputKindImage: domain.InputUnsupported,
		domain.InputKindPDF:   domain.InputSupported,
	})
}

// visionResolver answers supported for everything.
func visionResolver() *fakeModalityResolver {
	return newFakeModalityResolver(map[domain.InputKind]domain.InputSupport{
		domain.InputKindImage: domain.InputSupported,
		domain.InputKindPDF:   domain.InputSupported,
	})
}

// ---------------------------------------------------------------------------
// Spec "Model-modality attachment degradation" — the four scenarios
// ---------------------------------------------------------------------------

// TestRunner_DegradesImageOnTextOnlyModel pins scenario "Image degrades on a
// text-only model": the model message carries a marked pointer note (name,
// mime, size, cannot-view) instead of an image block, the note's stamped
// identity keeps the capability URL, and the degraded ref never opens its
// bytes.
func TestRunner_DegradesImageOnTextOnlyModel(t *testing.T) {
	blob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe,
		WithAttachmentBlobs(blobs), WithInputModalityResolver(unsupportedImageResolver()))
	req.Input = "what is this?"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("degraded turn must complete, got %+v", events)
	}

	msg := probe.capturedUserMessage(t)
	if got := blockTypes(msg); len(got) != 2 ||
		got[0] != schema.ContentBlockTypeUserInputText || got[1] != schema.ContentBlockTypeUserInputText {
		t.Fatalf("block types = %v, want [text, note-text] with no image block", got)
	}
	note := msg.ContentBlocks[1]
	text := note.UserInputText.Text
	for _, want := range []string{"shot.png", "image/png", "5 B", "cannot view images", "reference only"} {
		if !strings.Contains(text, want) {
			t.Errorf("pointer note %q must contain %q", text, want)
		}
	}
	if note.Extra[AttachmentPointerExtraKey] != true {
		t.Errorf("note Extra = %v, want [%s]=true so it never renders as transcript text", note.Extra, AttachmentPointerExtraKey)
	}
	meta, ok := attachmentBlockMetaOf(note)
	if !ok {
		t.Fatal("degraded note lost its attachment identity meta")
	}
	if meta.URL != "/api/v1/files/att-cap/att-img" || meta.Name != "shot.png" || meta.Mime != "image/png" || meta.Size != int64(len(blob)) {
		t.Errorf("degraded note meta = %+v, want identity with the capability URL", meta)
	}

	// Degradation skips byte resolution entirely: no OpenAttachment lookup.
	if _, attLookups := blobs.lookups(); len(attLookups) != 0 {
		t.Errorf("degraded ref must not resolve bytes, got lookups %v", attLookups)
	}
}

// TestRunner_DegradedRefNeedsNoBytes proves the degraded path never touches
// byte resolution: the blob resolver serves the capability URL but the byte
// lookup would fail — the turn must still build the note.
func TestRunner_DegradedRefNeedsNoBytes(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{})
	blobs.urls["att-img"] = "/api/v1/files/att-cap/att-img" // URL serves; bytes would fail
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = ""
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: 4096},
	}

	msg, err := runner.buildAttachmentUserMessage(context.Background(), req, inputModality{image: domain.InputUnsupported})
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}
	if len(msg.ContentBlocks) != 1 || msg.ContentBlocks[0].UserInputImage != nil {
		t.Fatalf("block types = %v, want exactly the pointer note", blockTypes(msg))
	}
	if got := msg.ContentBlocks[0].UserInputText.Text; !strings.Contains(got, "4.0 KB") {
		t.Errorf("note %q must render the human size", got)
	}
}

// TestRunner_SendsImageWholeOnVisionCapableModel pins scenario "Image is sent
// whole on a vision-capable model": the bytes path, no note.
func TestRunner_SendsImageWholeOnVisionCapableModel(t *testing.T) {
	blob := []byte{0x11, 0x22, 0x33, 0x44, 0x55}
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	resolver := visionResolver()
	runner, _, _, req := setupAttachmentsRunner(t, probe,
		WithAttachmentBlobs(blobs), WithInputModalityResolver(resolver))
	req.Input = "describe this"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("turn did not complete, got %+v", ev)
	}

	msg := probe.capturedUserMessage(t)
	if got := blockTypes(msg); len(got) != 2 || got[1] != schema.ContentBlockTypeUserInputImage {
		t.Fatalf("block types = %v, want [text, image]", got)
	}
	if got := msg.ContentBlocks[1].UserInputImage.Base64Data; got != base64.StdEncoding.EncodeToString(blob) {
		t.Error("vision-capable model must receive the image bytes")
	}
	for _, block := range msg.ContentBlocks {
		if block.UserInputText != nil && strings.Contains(block.UserInputText.Text, "cannot view") {
			t.Error("no pointer note may appear on a vision-capable model")
		}
	}
	// Capability resolution is (provider, hint, model)-scoped.
	providerType, hint, mdl, calls := resolver.lastCall()
	if calls == 0 || providerType != "openai" || hint != "" || mdl != "gpt-4o" {
		t.Errorf("resolver consulted with (%q, %q, %q) x%d, want (openai, \"\", gpt-4o)", providerType, hint, mdl, calls)
	}
}

// TestRunner_RegeneratedTurnRebuildsImageAfterModelSwitch pins scenario
// "Regeneration after a model switch sees the image again": degradation is
// per build, so re-running the same turn with a different resolver answer
// delivers the image block.
func TestRunner_RegeneratedTurnRebuildsImageAfterModelSwitch(t *testing.T) {
	blob := []byte{0x0A, 0x0B, 0x0C, 0x0D, 0x0E}
	encoded := base64.StdEncoding.EncodeToString(blob)
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	first := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, first,
		WithAttachmentBlobs(blobs), WithInputModalityResolver(unsupportedImageResolver()))
	req.Input = "describe this"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("degraded Run: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("degraded turn did not complete, got %+v", ev)
	}

	// The agent is reconfigured to a vision-capable model: the resolver now
	// answers supported, and the rebuilt turn sends the image.
	runner.inputModalityResolver = visionResolver()
	regen := &captureModel{}
	rewireModel(runner, regen)
	stream2, err := runner.Run(context.Background(), req) // same refs, fresh run
	if err != nil {
		t.Fatalf("regenerated Run: %v", err)
	}
	if ev := collectStream(t, stream2); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("regenerated turn did not complete, got %+v", ev)
	}

	fresh := lastUserMessage(t, regen.captured())
	if got := userImageOf(t, []*schema.AgenticMessage{fresh}); got.Base64Data != encoded {
		t.Error("rebuilt turn must deliver the image bytes after the model switch")
	}
	for _, block := range fresh.ContentBlocks {
		if block.UserInputText != nil && strings.Contains(block.UserInputText.Text, "cannot view") {
			t.Error("rebuilt turn on a vision-capable model must not degrade")
		}
	}
}

// TestRunner_UnknownCapabilityFailsOpen pins scenario "Unknown capability
// fails open": no catalog evidence sends the block as-is.
func TestRunner_UnknownCapabilityFailsOpen(t *testing.T) {
	blob := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x42}
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe,
		WithAttachmentBlobs(blobs),
		WithInputModalityResolver(newFakeModalityResolver(nil))) // unknown for every kind
	req.Input = ""
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("turn did not complete, got %+v", ev)
	}

	msg := probe.capturedUserMessage(t)
	if got := blockTypes(msg); len(got) != 1 || got[0] != schema.ContentBlockTypeUserInputImage {
		t.Fatalf("block types = %v, want exactly the image block (fail-open)", got)
	}
	if got := msg.ContentBlocks[0].UserInputImage.Base64Data; got != base64.StdEncoding.EncodeToString(blob) {
		t.Error("unknown capability must send the bytes unchanged")
	}
}

// TestRunner_UnwiredResolverFailsOpen pins the construction default: a runner
// without WithInputModalityResolver behaves exactly as before — bytes path.
func TestRunner_UnwiredResolverFailsOpen(t *testing.T) {
	blob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = ""
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(blob))},
	}

	msg, err := runner.buildAttachmentUserMessage(context.Background(), req, inputModality{})
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}
	if got := blockTypes(msg); len(got) != 1 || got[0] != schema.ContentBlockTypeUserInputImage {
		t.Fatalf("block types = %v, want the image block on the default resolver", got)
	}
	if NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), t.TempDir()).inputModalityResolver == nil {
		t.Fatal("NewRunner must default the input-modality resolver (fail-open unknown)")
	}
}

// ---------------------------------------------------------------------------
// Mixed turn + transcript projection
// ---------------------------------------------------------------------------

// TestRunner_MixedTurnDegradesOnlyUnsupportedKind pins the per-kind decision:
// text + degraded image + supported PDF yields text, fenced text, pointer
// note, file block — in lane order — and only the PDF resolves bytes.
func TestRunner_MixedTurnDegradesOnlyUnsupportedKind(t *testing.T) {
	imgBlob := []byte{1, 2, 3, 4, 5}
	pdfBlob := []byte("%PDF-degrade")
	csvBlob := []byte("A,B\n1,2")
	blobs := newStubBlobs(map[string][]byte{"att-img": imgBlob, "att-pdf": pdfBlob, "att-txt": csvBlob})
	probe := &captureModel{}
	runner, ws, ag, req := setupAttachmentsRunner(t, probe,
		WithAttachmentBlobs(blobs), WithInputModalityResolver(unsupportedImageResolver()))
	req.Input = "mixed turn"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(imgBlob))},
		{ID: "att-txt", Name: "notes.csv", MimeType: "text/csv", Lane: attLaneInlineText, Size: int64(len(csvBlob))},
		{ID: "att-pdf", Name: "report.pdf", MimeType: "application/pdf", Lane: attLaneInlinePDF, Size: int64(len(pdfBlob))},
	}

	// The full resolve → build thread, exactly what run() does.
	cfg, _, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	msg, err := runner.buildAttachmentUserMessage(context.Background(), req, cfg.InputModality)
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}
	wantTypes := []schema.ContentBlockType{
		schema.ContentBlockTypeUserInputText, // user's own text
		schema.ContentBlockTypeUserInputText, // fenced inline-text
		schema.ContentBlockTypeUserInputText, // degraded image pointer note
		schema.ContentBlockTypeUserInputFile, // supported PDF bytes
	}
	if got := blockTypes(msg); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("block types = %v, want %v", got, wantTypes)
	}
	if text := msg.ContentBlocks[1].UserInputText.Text; !strings.Contains(text, "```notes.csv") {
		t.Errorf("fenced block = %q, want the csv fence", text)
	}
	note := msg.ContentBlocks[2]
	if !strings.Contains(note.UserInputText.Text, "shot.png") || note.Extra[AttachmentPointerExtraKey] != true {
		t.Errorf("block 2 = %q (extra %v), want the marked image pointer note", note.UserInputText.Text, note.Extra)
	}
	file := msg.ContentBlocks[3].UserInputFile
	if file == nil || file.Base64Data != base64.StdEncoding.EncodeToString(pdfBlob) || file.Name != "report.pdf" {
		t.Errorf("file block = %+v, want the pdf bytes untouched", file)
	}
	// The degraded image never resolves bytes; the fenced text and the
	// supported PDF each open exactly once.
	wsIDs, attLookups := blobs.lookups()
	wantLookups := []string{"att-txt", "att-pdf"}
	if !reflect.DeepEqual(attLookups, wantLookups) {
		t.Errorf("OpenAttachment lookups = %v, want %v (the degraded image must not resolve bytes)", attLookups, wantLookups)
	}
	if len(wsIDs) != len(wantLookups) {
		t.Errorf("lookup workspaces = %v, want one scoped lookup per open", wsIDs)
	}
}

// TestRunner_DegradedTurnRecordsPillInTranscript is the 4.5 integration
// coverage (fake-store round-trip): a full turn carrying an inline image on a
// text-only model completes, and both the live and hydrated transcript
// projections record the user pill for the attachment.
func TestRunner_DegradedTurnRecordsPillInTranscript(t *testing.T) {
	blob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D}
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, ws, _, req := setupAttachmentsRunner(t, probe,
		WithAttachmentBlobs(blobs), WithInputModalityResolver(unsupportedImageResolver()))
	req.Input = "what is this?"
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: attLaneInlineImage, Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("degraded turn must complete successfully, got %+v", events)
	}

	wantPills := []AttachmentMeta{
		{Name: "shot.png", MimeType: "image/png", Size: int64(len(blob)), URL: "/api/v1/files/att-cap/att-img"},
	}
	live := liveUserMessageOf(t, events)
	if live.Content != "what is this?" {
		t.Errorf("live user content = %q, want only the user's own text (no note text)", live.Content)
	}
	if !reflect.DeepEqual(live.Attachments, wantPills) {
		t.Errorf("live attachments = %+v, want the pill %+v", live.Attachments, wantPills)
	}

	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	got := projectedUserMessageOf(t, hist)
	if !reflect.DeepEqual(got.Attachments, live.Attachments) {
		t.Errorf("hydrated attachments %+v must equal live %+v", got.Attachments, live.Attachments)
	}

	// The model-facing message carries the marked note, not the image.
	modelMsg := probe.capturedUserMessage(t)
	for _, block := range modelMsg.ContentBlocks {
		if block.UserInputImage != nil {
			t.Error("degraded turn's model message must carry no image block")
		}
	}
	sawNote := false
	for _, block := range modelMsg.ContentBlocks {
		if block.UserInputText != nil && strings.Contains(block.UserInputText.Text, "cannot view images") {
			sawNote = isAttachmentPointerNote(block)
		}
	}
	if !sawNote {
		t.Error("degraded turn's model message must carry the marked cannot-view note")
	}
}

// ---------------------------------------------------------------------------
// resolve() plumbing
// ---------------------------------------------------------------------------

// credentialCaptureFactory records the credential the factory received.
type credentialCaptureFactory struct {
	mu   sync.Mutex
	cred providers.Credential
}

func (c *credentialCaptureFactory) factory(_ context.Context, _ string, cred providers.Credential, _ string) (Model, error) {
	c.mu.Lock()
	c.cred = cred
	c.mu.Unlock()
	return &captureModel{}, nil
}

func (c *credentialCaptureFactory) captured() providers.Credential {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cred
}

// TestResolve_ThreadsCatalogHintAndModality pins the resolve-time wiring
// (fix-image-attachment-lane D4 step 2): the effective catalog hint reaches
// both the resolver consultation and the model factory credential, and the
// resolved supports ride the agentConfig.
func TestResolve_ThreadsCatalogHintAndModality(t *testing.T) {
	capture := &credentialCaptureFactory{}
	runner, ws, ag, req := setupAttachmentsRunner(t, &captureModel{}, WithAgenticModelFactory(capture.factory))

	// Reconfigure the seeded provider as a compatible gateway with an
	// explicit catalog hint.
	provs, err := runner.providers.ListForWorkspace(context.Background(), ws.ID)
	if err != nil || len(provs) != 1 {
		t.Fatalf("list providers: %v (n=%d)", err, len(provs))
	}
	prov := &provs[0]
	prov.Type = "openai-compatible"
	prov.BaseURL = "https://gw.example.com"
	prov.CatalogProvider = "zai-coding-plan"
	if err := runner.providers.Update(context.Background(), prov); err != nil {
		t.Fatalf("update provider: %v", err)
	}

	resolver := newFakeModalityResolver(nil) // unknown everywhere; args are what matters
	runner.inputModalityResolver = resolver

	cfg, _, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if cfg.InputModality.providerType != "openai-compatible" || cfg.InputModality.catalogHint != "zai-coding-plan" || cfg.InputModality.model != "gpt-4o" {
		t.Errorf("cfg.InputModality = %+v, want provider openai-compatible, hint zai-coding-plan, model gpt-4o", cfg.InputModality)
	}
	if cfg.InputModality.image != domain.InputUnknown || cfg.InputModality.pdf != domain.InputUnknown {
		t.Errorf("supports = (%q, %q), want unknown/unknown from the empty resolver", cfg.InputModality.image, cfg.InputModality.pdf)
	}
	providerType, hint, mdl, calls := resolver.lastCall()
	if calls < 2 || providerType != "openai-compatible" || hint != "zai-coding-plan" || mdl != "gpt-4o" {
		t.Errorf("resolver consulted with (%q, %q, %q) x%d, want the compatible provider + hint + model", providerType, hint, mdl, calls)
	}
	if cred := capture.captured(); cred.CatalogHint != "zai-coding-plan" {
		t.Errorf("factory credential CatalogHint = %q, want the effective hint", cred.CatalogHint)
	}
}

// ---------------------------------------------------------------------------
// Size formatting
// ---------------------------------------------------------------------------

func TestHumanAttachmentSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{4096, "4.0 KB"},
		{1536, "1.5 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}
	for _, tc := range cases {
		if got := humanAttachmentSize(tc.in); got != tc.want {
			t.Errorf("humanAttachmentSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
