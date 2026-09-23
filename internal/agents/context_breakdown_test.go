package agents

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// seedCompactedWindow appends a window whose bulk sits BEFORE a compaction
// boundary: one huge pre-compaction message, a MessagesReplaced record
// carrying a small summary, and one small tail message. loadSessionWindow
// (and the summarization middleware's window rules) restart at the boundary —
// the full log does not.
func seedCompactedWindow(t *testing.T, ctx context.Context, adapter *ADKSessionAdapter, sessionID string) {
	t.Helper()
	bulk := schema.UserAgenticMessage(strings.Repeat("pre-compaction bulk entry. ", 600))
	summary := schema.UserAgenticMessage("summary of everything before the divider")
	tail := schema.UserAgenticMessage("tail message after the divider")
	err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "m0", TurnID: "turn-0", Message: bulk},
		{EventID: "mr", TurnID: "turn-0", Kind: adk.SessionEventMessagesReplaced, MessagesReplaced: &[]*schema.AgenticMessage{summary}},
		{EventID: "m1", TurnID: "turn-0", Message: tail},
	})
	if err != nil {
		t.Fatalf("seed compacted window: %v", err)
	}
}

func TestMeasureContextBreakdown_SegmentMath(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")
	seedCompactedWindow(t, ctx, adapter, "sess")

	sizes := &contextSizes{}
	// 3200 bytes of instruction → 800 tokens; no tools resolved.
	sizes.recordCompose(3200, 0)

	window, err := loadSessionWindow(ctx, adapter, "sess")
	if err != nil {
		t.Fatalf("load window: %v", err)
	}
	wantConversation := estimateWindowTokens(window)

	usage := UsagePayload{InputTokens: 4000, OutputTokens: 100, TotalTokens: 4100, FinalInputTokens: 4000}
	cb := measureContextBreakdown(ctx, adapter, "sess", sizes, usage)
	if cb == nil {
		t.Fatal("breakdown = nil, want measured segments")
	}
	if cb.Instructions != 800 {
		t.Fatalf("instructions = %d, want 3200/4 = 800", cb.Instructions)
	}
	if cb.Tools != 0 {
		t.Fatalf("tools = %d, want omitted (none resolved)", cb.Tools)
	}
	if cb.Conversation != wantConversation || wantConversation <= 0 {
		t.Fatalf("conversation = %d, want the true-window estimate %d", cb.Conversation, wantConversation)
	}
	// The seeded window is text-only: the files segment must not appear.
	if cb.Files != 0 {
		t.Fatalf("files = %d, want omitted on a text-only window", cb.Files)
	}
	// server = final-call input minus the measured segments.
	wantServer := 4000 - 800 - wantConversation
	if wantServer < 0 {
		wantServer = 0
	}
	if cb.Server != wantServer {
		t.Fatalf("server = %d, want %d", cb.Server, wantServer)
	}
	// Spec scenario: the labeled sections sum to no more than the final
	// call's input.
	if sum := cb.Instructions + cb.Tools + cb.Conversation + cb.Files + cb.Server; sum > usage.FinalInputTokens {
		t.Fatalf("segments sum %d exceeds final-call input %d", sum, usage.FinalInputTokens)
	}
}

func TestMeasureContextBreakdown_TrueWindowAfterCompaction(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")
	seedCompactedWindow(t, ctx, adapter, "sess")

	window, err := loadSessionWindow(ctx, adapter, "sess")
	if err != nil {
		t.Fatalf("load window: %v", err)
	}
	trueEstimate := estimateWindowTokens(window)

	full, err := adapter.LoadEvents(ctx, "sess", &adk.LoadSessionEventsRequest{})
	if err != nil {
		t.Fatalf("load full log: %v", err)
	}
	var fullMsgs []*schema.AgenticMessage
	for _, ev := range full.Events {
		if ev != nil && ev.Message != nil {
			fullMsgs = append(fullMsgs, ev.Message)
		}
	}
	fullEstimate := estimateWindowTokens(fullMsgs)

	usage := UsagePayload{InputTokens: 90000, OutputTokens: 10, TotalTokens: 90010, FinalInputTokens: 90000}
	cb := measureContextBreakdown(ctx, adapter, "sess", nil, usage)
	if cb == nil {
		t.Fatal("breakdown = nil, want measured segments")
	}
	// The conversation segment measures the compacted window, not the full
	// transcript: the pre-boundary bulk is summarized away server-side.
	if cb.Conversation != trueEstimate {
		t.Fatalf("conversation = %d, want the compacted window's %d", cb.Conversation, trueEstimate)
	}
	if fullEstimate <= trueEstimate {
		t.Fatalf("test setup: full-log estimate %d must exceed the true window's %d", fullEstimate, trueEstimate)
	}
}

func TestMeasureContextBreakdown_NoProviderUsageOmitsBlock(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")
	seedCompactedWindow(t, ctx, adapter, "sess")

	sizes := &contextSizes{}
	sizes.recordCompose(3200, 800)

	if cb := measureContextBreakdown(ctx, adapter, "sess", sizes, UsagePayload{}); cb != nil {
		t.Fatalf("breakdown = %+v, want nil when the provider reported no usage", cb)
	}
}

func TestMeasureContextBreakdown_ServerFlooredAtZero(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")
	seedCompactedWindow(t, ctx, adapter, "sess")

	// Estimator skew: the measured segments overshoot the final-call input.
	// The server share floors at zero and the overshoot stays at face value.
	sizes := &contextSizes{}
	sizes.recordCompose(8000, 0)
	usage := UsagePayload{InputTokens: 100, OutputTokens: 0, TotalTokens: 100, FinalInputTokens: 100}
	cb := measureContextBreakdown(ctx, adapter, "sess", sizes, usage)
	if cb == nil {
		t.Fatal("breakdown = nil, want measured segments")
	}
	if cb.Server != 0 {
		t.Fatalf("server = %d, want floored at 0", cb.Server)
	}
}

func TestMeasureContextBreakdown_FilesFromInWindowAttachments(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")

	image := schema.NewContentBlock(&schema.UserInputImage{MIMEType: "image/png"})
	setAttachmentBlockMeta(image, attachmentBlockMeta{ID: "a1", Name: "shot.png", Mime: "image/png", Size: 900, Lane: "inline-image"})
	file := schema.NewContentBlock(&schema.UserInputFile{MIMEType: "application/pdf", Name: "report.pdf"})
	setAttachmentBlockMeta(file, attachmentBlockMeta{ID: "a2", Name: "report.pdf", Mime: "application/pdf", Size: 4000, Lane: "inline-pdf"})
	msg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "see attached"}), image, file},
	}
	if err := adapter.AppendEvents(ctx, "sess", []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "m1", TurnID: "turn-1", Message: msg},
	}); err != nil {
		t.Fatalf("seed attachment window: %v", err)
	}

	// The files estimate follows the web estimator's constants: 1100 flat per
	// image, ceil(4000/4)=1000 for the byte-derived PDF, text excluded.
	wantFiles := 1100 + 1000
	usage := UsagePayload{InputTokens: 90000, OutputTokens: 0, TotalTokens: 90000, FinalInputTokens: 90000}
	cb := measureContextBreakdown(ctx, adapter, "sess", nil, usage)
	if cb == nil {
		t.Fatal("breakdown = nil, want measured segments")
	}
	if cb.Files != wantFiles {
		t.Fatalf("files = %d, want %d (1100 image + 1000 pdf)", cb.Files, wantFiles)
	}
}

func TestEstimateWindowFileTokens(t *testing.T) {
	image := schema.NewContentBlock(&schema.UserInputImage{MIMEType: "image/jpeg"})
	setAttachmentBlockMeta(image, attachmentBlockMeta{ID: "a1", Mime: "image/jpeg", Size: 1, Lane: "inline-image"})
	// A degraded image is a text pointer note — its bytes never reach the
	// model, so it must not count as an image payload.
	degraded := schema.NewContentBlock(&schema.UserInputText{Text: "the model cannot view images"})
	setAttachmentBlockMeta(degraded, attachmentBlockMeta{ID: "a2", Mime: "image/png", Size: 9999, Lane: "inline-image"})
	// A payload file below the byte floor still costs the minimum.
	smallFile := schema.NewContentBlock(&schema.UserInputFile{MIMEType: "application/pdf"})
	setAttachmentBlockMeta(smallFile, attachmentBlockMeta{ID: "a3", Mime: "application/pdf", Size: 8, Lane: "inline-pdf"})
	// An unidentified payload (no stamped meta) contributes nothing.
	bare := schema.NewContentBlock(&schema.UserInputFile{MIMEType: "application/pdf"})

	msgs := []*schema.AgenticMessage{{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{image, degraded, smallFile, bare, schema.NewContentBlock(&schema.UserInputText{Text: "plain text"}), nil},
	}}
	// 1100 image + 64 floored PDF; the degraded note, the bare block, and the
	// plain text contribute nothing here (text is the conversation estimator's
	// share).
	if got := estimateWindowFileTokens(msgs); got != 1100+64 {
		t.Fatalf("estimateWindowFileTokens = %d, want %d", got, 1100+64)
	}
	if got := estimateWindowFileTokens(nil); got != 0 {
		t.Fatalf("estimateWindowFileTokens(nil) = %d, want 0", got)
	}
}

func TestUsagePayloadJSON_BreakdownOmitBehavior(t *testing.T) {
	// Without a breakdown the key is absent entirely.
	b, err := json.Marshal(UsagePayload{InputTokens: 10, OutputTokens: 2, TotalTokens: 12})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "context_breakdown") {
		t.Fatalf("payload without a breakdown serialized the key: %s", b)
	}

	// With one, the key names its measured segments; zero segments never
	// serialize (never zero — omitted).
	b, err = json.Marshal(UsagePayload{
		InputTokens: 10, OutputTokens: 2, TotalTokens: 12, FinalInputTokens: 10,
		ContextBreakdown: &ContextBreakdown{Instructions: 4, Conversation: 6},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"context_breakdown":{"instructions":4,"conversation":6}`) {
		t.Fatalf("breakdown JSON = %s, want measured segments only", b)
	}
	if strings.Contains(string(b), `"tools":0`) || strings.Contains(string(b), `"files":0`) || strings.Contains(string(b), `"server":0`) {
		t.Fatalf("zero segments leaked into the wire form: %s", b)
	}
}

// TestDrainAgentEvents_BreakdownRidesTerminalUsageOnly drives the drain loop
// against a store-backed session: a turn whose provider reported usage
// carries the breakdown on its terminal event, measured from the compose
// sizes and the true window; a turn without provider usage carries no usage
// and no breakdown at all.
func TestDrainAgentEvents_BreakdownRidesTerminalUsageOnly(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")
	seedCompactedWindow(t, ctx, adapter, "sess")

	sizes := &contextSizes{}
	sizes.recordCompose(400, 0) // 100 tokens of instructions

	drain := func(withUsage bool) []TranscriptEvent {
		iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
		go func() {
			if withUsage {
				sendSpanModelEnd(gen, 4000, 100, 4100)
			}
			gen.Close()
		}()
		stream := NewEventStream(16)
		r := &Runner{runMgr: newRunManager(context.Background(), 0), memoryWorker: newQueuedMemoryWorker()}
		req := ExecRequest{SessionID: "sess"}
		r.drainAgentEvents(ctx, iter, stream, RunKey{}, "turn-1", "", nil, hooks.Event{}, &compactionState{}, sizes, nil, nil, req, false, adapter)
		stream.Close()
		return collectStream(t, stream)
	}

	terminal := terminalEvent(drain(true))
	if terminal == nil || terminal.Kind != TranscriptEventTurnCompleted {
		t.Fatalf("terminal = %+v, want turn_completed", terminal)
	}
	if terminal.Usage == nil {
		t.Fatal("terminal usage is nil")
	}
	cb := terminal.Usage.ContextBreakdown
	if cb == nil {
		t.Fatal("terminal usage carries no context breakdown")
	}
	if cb.Instructions != 100 {
		t.Fatalf("instructions = %d, want 100", cb.Instructions)
	}
	window, err := loadSessionWindow(ctx, adapter, "sess")
	if err != nil {
		t.Fatalf("load window: %v", err)
	}
	if cb.Conversation != estimateWindowTokens(window) {
		t.Fatalf("conversation = %d, want the true-window estimate %d", cb.Conversation, estimateWindowTokens(window))
	}

	// No provider usage → no usage block, no breakdown.
	terminal = terminalEvent(drain(false))
	if terminal == nil || terminal.Usage != nil {
		t.Fatalf("terminal usage = %+v, want nil when the provider reported none", terminal.Usage)
	}
}

// TestComposeAgent_StampsContextMeasure pins the compose-time
// instrumentation (D7 2.1): composeAgent reports the byte length of the real
// composed instruction and of the marshaled tool schemas onto the per-run
// measure, without disturbing the rest of the composition.
func TestComposeAgent_StampsContextMeasure(t *testing.T) {
	ctx := context.Background()
	mdl := &compactModel{text: "ok"}
	st, runner, ws, ag, req := setupCompactRunner(t, mdl)

	// No tools resolved: only the instruction share is stamped.
	loadedWs, loadedAgent, user, role, err := runner.load(ctx, req)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg, resolvedTools, err := runner.resolve(ctx, req, loadedWs, loadedAgent, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolvedTools) != 0 {
		t.Fatalf("resolved tools = %d, want none for a toolless agent", len(resolvedTools))
	}
	sizes := &contextSizes{}
	cfg.ContextMeasure = sizes
	if _, err := runner.composeAgent(ctx, req, &cfg, loadedWs, user, role, resolvedTools, loadedAgent); err != nil {
		t.Fatalf("composeAgent: %v", err)
	}
	if sizes.instructionBytes != len("test instruction") {
		t.Fatalf("instructionBytes = %d, want the stub composition's %d", sizes.instructionBytes, len("test instruction"))
	}
	if sizes.toolSchemaBytes != 0 {
		t.Fatalf("toolSchemaBytes = %d, want 0 without tools", sizes.toolSchemaBytes)
	}

	// An agent exposing web.fetch stamps the marshaled schema bytes too.
	chart := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "beacon",
		Name:        "Beacon",
		ProviderID:  ag.ProviderID,
		Model:       ag.Model,
		Tools:       []string{tools.NameWebFetch},
	}
	if err := st.Agents().Create(ctx, chart); err != nil {
		t.Fatalf("create chart agent: %v", err)
	}
	chartDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, chart.Slug)
	if err := os.MkdirAll(chartDir, 0o755); err != nil {
		t.Fatalf("seed chart agent dir: %v", err)
	}
	req.AgentID = chart.ID
	loadedWs, loadedAgent, user, role, err = runner.load(ctx, req)
	if err != nil {
		t.Fatalf("load chart agent: %v", err)
	}
	cfg, resolvedTools, err = runner.resolve(ctx, req, loadedWs, loadedAgent, nil)
	if err != nil {
		t.Fatalf("resolve chart agent: %v", err)
	}
	sizes = &contextSizes{}
	cfg.ContextMeasure = sizes
	if _, err := runner.composeAgent(ctx, req, &cfg, loadedWs, user, role, resolvedTools, loadedAgent); err != nil {
		t.Fatalf("composeAgent chart: %v", err)
	}
	if sizes.instructionBytes != len("test instruction") {
		t.Fatalf("instructionBytes = %d, want %d", sizes.instructionBytes, len("test instruction"))
	}
	if want := toolSchemaBytes(ctx, resolvedTools); sizes.toolSchemaBytes == 0 || sizes.toolSchemaBytes != want {
		t.Fatalf("toolSchemaBytes = %d, want the marshaled schemas' %d (> 0)", sizes.toolSchemaBytes, want)
	}
}

// TestContextBreakdown_DisplayOnlyByContract is the grep consumers asked for
// by D7 (task 2.3): the breakdown must flow nowhere except the usage payload
// it rides. Only the producers (events.go type, runner.go accumulation seam,
// context_breakdown.go measurement) and the openresponses wire mapper may
// name it — billing (scheduler), trigger math and summarization (the
// estimator and compaction state), history hydration, and the gateway
// aggregator never read it.
func TestContextBreakdown_DisplayOnlyByContract(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	allowed := map[string]bool{
		filepath.Join("internal", "agents", "events.go"):            true,
		filepath.Join("internal", "agents", "runner.go"):            true,
		filepath.Join("internal", "agents", "context_breakdown.go"): true,
		filepath.Join("internal", "openresponses", "translate.go"):  true,
	}

	var offenders []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		if allowed[rel] {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), "ContextBreakdown") || strings.Contains(string(data), "context_breakdown") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk sources: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("context breakdown consumed outside the usage payload surface (display-only contract): %v", offenders)
	}
}

// TestContextBreakdown_CancelledTurnStillMeasures pins the cancel-path seam:
// a cancelled turn that already spent provider usage carries the breakdown on
// its cancelled terminal, measured with a read detached from the cancelled
// run context.
func TestContextBreakdown_CancelledTurnStillMeasures(t *testing.T) {
	st := fake.New()
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), "ws")
	seedCompactedWindow(t, context.Background(), adapter, "sess")

	sizes := &contextSizes{}
	sizes.recordCompose(400, 0)

	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	go func() {
		sendSpanModelEnd(gen, 4000, 100, 4100)
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{Err: context.Canceled})
		gen.Close()
	}()
	stream := NewEventStream(16)
	r := &Runner{runMgr: newRunManager(context.Background(), 0), memoryWorker: newQueuedMemoryWorker()}
	runCtx, cancel := context.WithCancel(context.Background())
	cancel() // the run context is already dead when the seam fires
	r.drainAgentEvents(runCtx, iter, stream, RunKey{}, "turn-1", "", nil, hooks.Event{}, &compactionState{}, sizes, nil, nil, ExecRequest{SessionID: "sess"}, false, adapter)
	stream.Close()

	terminal := terminalEvent(collectStream(t, stream))
	if terminal == nil || terminal.Kind != TranscriptEventCancelled {
		t.Fatalf("terminal = %+v, want cancelled", terminal)
	}
	if terminal.Usage == nil || terminal.Usage.ContextBreakdown == nil {
		t.Fatalf("cancelled terminal usage = %+v, want a breakdown on spent usage", terminal.Usage)
	}
	if terminal.Usage.ContextBreakdown.Conversation <= 0 {
		t.Fatalf("conversation = %d, want the true-window measure despite the cancelled context", terminal.Usage.ContextBreakdown.Conversation)
	}
}
