package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// compactModel is a scripted summarizer model: it records every input (so
// tests can assert the window and the focus instruction it received) and
// returns a fixed summary carrying the configured provider usage. An optional
// gate holds Generate open so tests can hold a run live.
type compactModel struct {
	mu     sync.Mutex
	calls  int
	inputs [][]*schema.AgenticMessage
	text   string
	usage  *schema.TokenUsage
	gate   chan struct{}
}

func (m *compactModel) Generate(ctx context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.calls++
	m.inputs = append(m.inputs, input)
	gate := m.gate
	m.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: m.text}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{TokenUsage: m.usage},
	}, nil
}

func (m *compactModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *compactModel) callCount(t *testing.T) int {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// lastInputUserText returns the text of the last message the summarizer
// received — where the focus instruction must surface.
func (m *compactModel) lastInputUserText(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inputs) == 0 {
		t.Fatal("summarizer was never called")
	}
	input := m.inputs[len(m.inputs)-1]
	if len(input) == 0 {
		t.Fatal("summarizer received an empty input")
	}
	return extractAgenticText(input[len(input)-1])
}

// setupCompactRunner seeds a workspace, agent, user, and provider, and wires
// the runner to the given summarizer model over the fake store — the same
// harness shape as setupHooksRunner, plus optional workspace hooks.
func setupCompactRunner(t *testing.T, mdl *compactModel, hookList ...*domain.WorkspaceHook) (store.Store, *Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("create member: %v", err)
	}
	for _, h := range hookList {
		h.WorkspaceID = ws.ID
		if err := st.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
			t.Fatalf("create hook %q: %v", h.Name, err)
		}
	}

	onClawDir := t.TempDir()
	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(), st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		onClawDir,
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return mdl, nil
		}),
		WithInstructionComposer(stubComposer{}),
		WithHooks(hooks.NewDispatcher(st.Hooks(), hooks.NewRegistry())),
	)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   "sess-compact",
		UserID:      user.ID,
		Input:       "focus on the incident discussion",
		Command:     CommandCompact,
	}
	return st, runner, ws, ag, req
}

// seedWindow appends a two-message window to the session log. The texts are
// long enough that the compaction estimate shrinks past the middleware's
// fixed summary preamble, yet far below any trigger threshold — the manual
// command must compact regardless.
func seedWindow(t *testing.T, adapter *ADKSessionAdapter, sessionID string) {
	t.Helper()
	err := adapter.AppendEvents(context.Background(), sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{
		{
			EventID: "m1",
			TurnID:  "turn-1",
			Message: schema.UserAgenticMessage("the database failed over at 03:14 and paged the on-call. " + strings.Repeat("timeline detail entry. ", 55)),
		},
		{
			EventID: "m2",
			TurnID:  "turn-1",
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "acknowledged, failover completed in four minutes. " + strings.Repeat("follow-up note entry. ", 55)}),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("seed window: %v", err)
	}
}

func compactRequest(req ExecRequest, focus string) ExecRequest {
	req.Command = CommandCompact
	req.Input = focus
	return req
}

func findCompacted(events []TranscriptEvent) *TranscriptEvent {
	for i := range events {
		if events[i].Kind == TranscriptEventContextCompacted {
			return &events[i]
		}
	}
	return nil
}

func TestCompact_RewritesWindowAndOrdersEvents(t *testing.T) {
	mdl := &compactModel{
		text:  "the session covered a database failover at 03:14",
		usage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}
	st, runner, ws, ag, req := setupCompactRunner(t, mdl)
	ctx := context.Background()

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedWindow(t, adapter, req.SessionID)

	stream, err := runner.Run(ctx, compactRequest(req, req.Input))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	events := collectStream(t, stream)

	// Event order contract: context_compacted THEN turn_completed, a
	// well-formed turn around them.
	if len(events) != 3 ||
		events[0].Kind != TranscriptEventTurnStarted ||
		events[1].Kind != TranscriptEventContextCompacted ||
		events[2].Kind != TranscriptEventTurnCompleted {
		t.Fatalf("event kinds = %v, want [turn_started context_compacted turn_completed]", kindsOf(events))
	}
	compacted := events[1]
	if compacted.Compaction == nil {
		t.Fatal("context_compacted carries no CompactionPayload")
	}
	if compacted.Compaction.TokensBefore <= 0 {
		t.Fatalf("tokens_before = %d, want a positive estimate", compacted.Compaction.TokensBefore)
	}
	if compacted.Compaction.TokensAfter <= 0 || compacted.Compaction.TokensAfter >= compacted.Compaction.TokensBefore {
		t.Fatalf("tokens %d → %d, want a shrinking window", compacted.Compaction.TokensBefore, compacted.Compaction.TokensAfter)
	}

	// The summarizer call's usage lands on turn_completed.
	done := events[2]
	if done.Usage == nil || done.Usage.InputTokens != 100 || done.Usage.OutputTokens != 50 || done.Usage.TotalTokens != 150 {
		t.Fatalf("turn_completed usage = %+v, want in=100 out=50 total=150", done.Usage)
	}

	// The window was rewritten in the session store: the log's head record is
	// the replacement, and the runner-scoped replay sees only the summary.
	repl, err := adapter.LoadEvents(ctx, req.SessionID, &adk.LoadSessionEventsRequest{})
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	var replacement *adk.SessionEvent[*schema.AgenticMessage]
	for _, ev := range repl.Events {
		if ev.MessagesReplaced != nil {
			replacement = ev
		}
	}
	if replacement == nil {
		t.Fatalf("no MessagesReplaced record persisted, got %d events", len(repl.Events))
	}
	if replacement.TurnID != events[0].TurnID {
		t.Fatalf("replacement turn %q, want the compact turn %q", replacement.TurnID, events[0].TurnID)
	}
	window, err := loadSessionWindow(ctx, adapter, req.SessionID)
	if err != nil {
		t.Fatalf("reload window: %v", err)
	}
	// The middleware's finalizer wraps the summary with its preamble and
	// inlined user messages — one message whose content carries the summary.
	if len(window) != 1 || !strings.Contains(extractAgenticText(window[0]), mdl.text) {
		t.Fatalf("rewritten window = %+v, want the single summary message carrying %q", window, mdl.text)
	}

	// Hydrated replay fills the same payload the live stream delivered.
	hist, err := runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, AgentID: ag.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	hydrated := findCompacted(hist.Events)
	if hydrated == nil || hydrated.Compaction == nil {
		t.Fatalf("hydrated transcript has no context_compacted, got %+v", hist.Events)
	}
	if hydrated.Compaction.TokensBefore != compacted.Compaction.TokensBefore ||
		hydrated.Compaction.TokensAfter != compacted.Compaction.TokensAfter {
		t.Fatalf("hydrated tokens %d → %d, want the live %d → %d",
			hydrated.Compaction.TokensBefore, hydrated.Compaction.TokensAfter,
			compacted.Compaction.TokensBefore, compacted.Compaction.TokensAfter)
	}
}

func TestCompact_FocusReachesSummarizerInstruction(t *testing.T) {
	mdl := &compactModel{text: "summary"}
	_, runner, _, _, req := setupCompactRunner(t, mdl)
	adapter := NewADKSessionAdapter(runner.sessionEvents, runner.checkpoints, req.WorkspaceID)
	seedWindow(t, adapter, req.SessionID)

	stream, err := runner.Run(context.Background(), compactRequest(req, "focus on the incident discussion"))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	collectStream(t, stream)

	if got := mdl.callCount(t); got != 1 {
		t.Fatalf("summarizer calls = %d, want 1", got)
	}
	// The middleware appends the user-level instruction last.
	if got := mdl.lastInputUserText(t); got != "focus on the incident discussion" {
		t.Fatalf("summarizer instruction = %q, want the focus text verbatim", got)
	}
}

func TestCompact_BelowThresholdStillCompacts(t *testing.T) {
	mdl := &compactModel{text: "tiny summary"}
	_, runner, _, _, req := setupCompactRunner(t, mdl)
	adapter := NewADKSessionAdapter(runner.sessionEvents, runner.checkpoints, req.WorkspaceID)
	seedWindow(t, adapter, req.SessionID)

	// A two-short-message window sits far below any trigger threshold, yet
	// the manual command compacts: Summarize bypasses the threshold gate.
	stream, err := runner.Run(context.Background(), compactRequest(req, "compact please"))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	events := collectStream(t, stream)
	if findCompacted(events) == nil {
		t.Fatalf("below-threshold manual compaction must still execute, got %v", kindsOf(events))
	}
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("compact turn must complete, got %v", kindsOf(events))
	}
}

func TestCompact_EmptyHistoryQuietNoOp(t *testing.T) {
	mdl := &compactModel{text: "never produced"}
	st, runner, ws, ag, req := setupCompactRunner(t, mdl)

	stream, err := runner.Run(context.Background(), compactRequest(req, "compact"))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	events := collectStream(t, stream)

	if len(events) != 2 || events[0].Kind != TranscriptEventTurnStarted || events[1].Kind != TranscriptEventTurnCompleted {
		t.Fatalf("events = %v, want a quiet [turn_started turn_completed]", kindsOf(events))
	}
	if findCompacted(events) != nil {
		t.Fatal("a quiet no-op must not emit context_compacted")
	}
	if events[1].Usage != nil {
		t.Fatalf("no-op usage = %+v, want nil (nothing spent)", events[1].Usage)
	}
	if mdl.callCount(t) != 0 {
		t.Fatal("an empty session must never reach the summarizer")
	}

	// Nothing persisted either.
	hist, err := runner.History(context.Background(), HistoryRequest{WorkspaceID: ws.ID, AgentID: ag.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist.Events) != 0 {
		t.Fatalf("hydrated events = %+v, want none", hist.Events)
	}
	_ = st
}

func TestCompact_TranscriptOffloadedToAgentDir(t *testing.T) {
	mdl := &compactModel{text: "summary"}
	st, runner, ws, ag, req := setupCompactRunner(t, mdl)
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedWindow(t, adapter, req.SessionID)

	stream, err := runner.Run(context.Background(), compactRequest(req, "compact"))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	if events := collectStream(t, stream); findCompacted(events) == nil {
		t.Fatalf("compaction did not execute: %v", kindsOf(events))
	}

	// The shared transcript-offload callback wrote the pre-compaction history
	// into the agent dir (the same contract as the automatic path).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	raw, err := os.ReadFile(filepath.Join(agentDir, "transcript.md"))
	if err != nil {
		t.Fatalf("read transcript offload: %v", err)
	}
	if !strings.Contains(string(raw), "the database failed over at 03:14") {
		t.Fatalf("offloaded transcript missing the pre-compaction history: %q", raw)
	}
}

func TestCompact_HooksFireRunLifecycleNotPromptGate(t *testing.T) {
	mdl := &compactModel{text: "summary"}
	// A user_prompt_submit hook that blocks every normal prompt — a compact
	// run carries no user prompt, so it must sail through. Observer hooks on
	// run_started/run_finished prove the lifecycle deliveries fire (audit
	// rows are only written for hooks matching the delivered event).
	blocker := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "prompt-gate",
			Event:       domain.HookEventUserPromptSubmit,
			Matcher:     "*",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo prompt denied >&2; exit 2"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	startedHook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "run-started-audit",
			Event:       domain.HookEventRunStarted,
			Matcher:     "*",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "true"}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	finishedHook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "run-finished-audit",
			Event:       domain.HookEventRunFinished,
			Matcher:     "*",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "true"}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	st, runner, ws, _, req := setupCompactRunner(t, mdl, blocker, startedHook, finishedHook)
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedWindow(t, adapter, req.SessionID)

	stream, err := runner.Run(context.Background(), compactRequest(req, req.Input))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	events := collectStream(t, stream)

	if findPromptBlocked(events) != nil {
		t.Fatalf("compact runs must not evaluate user_prompt_submit, got %+v", events)
	}
	if findCompacted(events) == nil || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("compact turn must compact and complete, got %v", kindsOf(events))
	}

	// run_started and run_finished fired (status completed); no prompt gate row.
	deadline := time.Now().Add(3 * time.Second)
	for {
		execs, err := st.Hooks().ListHookExecutions(context.Background(), req.WorkspaceID, nil, 20)
		if err != nil {
			t.Fatalf("list executions: %v", err)
		}
		var started, finished, prompts int
		for _, e := range execs {
			switch e.Event {
			case domain.HookEventRunStarted:
				started++
			case domain.HookEventRunFinished:
				finished++
			case domain.HookEventUserPromptSubmit:
				prompts++
			}
		}
		if started >= 1 && finished >= 1 {
			if prompts != 0 {
				t.Fatalf("user_prompt_submit fired %d times on compact runs, want 0", prompts)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run lifecycle hooks missing: started=%d finished=%d prompts=%d (%+v)", started, finished, prompts, execs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCompact_EphemeralQuietNoOp(t *testing.T) {
	mdl := &compactModel{text: "never produced"}
	st, runner, ws, _, req := setupCompactRunner(t, mdl)
	// The session has a persistable window, but an ephemeral compact run is
	// bound to no session: it loads nothing and persists nothing.
	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	seedWindow(t, adapter, req.SessionID)

	stream, err := runner.RunEphemeral(context.Background(), compactRequest(req, "compact"))
	if err != nil {
		t.Fatalf("RunEphemeral (compact): %v", err)
	}
	events := collectStream(t, stream)
	if findCompacted(events) != nil || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("ephemeral compact must be a quiet no-op, got %v", kindsOf(events))
	}
	if mdl.callCount(t) != 0 {
		t.Fatal("an ephemeral compact must never reach the summarizer")
	}
}

func TestCompact_ActiveRunConflict(t *testing.T) {
	gate := make(chan struct{})
	mdl := &compactModel{text: "blocked until released", gate: gate}
	_, runner, _, _, req := setupCompactRunner(t, mdl)

	// A normal turn holds the session's single run slot.
	live, err := runner.Run(context.Background(), ExecRequest{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		UserID:      req.UserID,
		Input:       "long running turn",
	})
	if err != nil {
		t.Fatalf("Run (normal): %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !runner.IsRunActive(RunKey{WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID}) {
		if time.Now().After(deadline) {
			t.Fatal("normal run never registered as live")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The compact turn enters the same guard: conflict, not a queue (D1).
	_, err = runner.Run(context.Background(), compactRequest(req, "compact"))
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("compact during a live run: err = %v, want domain.ErrConflict", err)
	}

	close(gate)
	collectStream(t, live)
}

func TestDrain_MessagesReplacedCarriesCompactionEstimates(t *testing.T) {
	// The automatic threshold path: the Callback records estimates before the
	// middleware forwards the replacement event, and the drain seam stamps
	// them onto the emitted context_compacted.
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	go func() {
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			SessionEventVariant: &adk.SessionEventVariant[*schema.AgenticMessage]{
				Event: &adk.SessionEvent[*schema.AgenticMessage]{
					Kind:             adk.SessionEventMessagesReplaced,
					TurnID:           "turn-1",
					MessagesReplaced: &[]*schema.AgenticMessage{},
				},
			},
		})
		gen.Close()
	}()

	stream := NewEventStream(16)
	r := &Runner{runMgr: newRunManager(context.Background(), 0), ingestWorker: newQueuedMemoryWorker()}
	estimates := &compactionState{}
	estimates.record(154000, 9200)
	r.drainAgentEvents(t.Context(), iter, stream, RunKey{}, "turn-1", "", nil, hooks.Event{}, estimates, nil, nil, nil, ExecRequest{}, false, NewEphemeralSessionAdapter())
	stream.Close()

	var events []TranscriptEvent
	for {
		ev, err := stream.Recv()
		if err != nil {
			break
		}
		events = append(events, *ev)
	}
	compacted := findCompacted(events)
	if compacted == nil || compacted.Compaction == nil {
		t.Fatalf("no context_compacted emitted, got %+v", events)
	}
	if compacted.Compaction.TokensBefore != 154000 || compacted.Compaction.TokensAfter != 9200 {
		t.Fatalf("compaction tokens = %d → %d, want 154000 → 9200", compacted.Compaction.TokensBefore, compacted.Compaction.TokensAfter)
	}
}

func TestNewCompactionCallback_OffloadsAndReportsEstimates(t *testing.T) {
	dir := t.TempDir()
	var gotBefore, gotAfter int
	observed := false
	cb := newCompactionCallback(dir, func(before, after int) {
		observed = true
		gotBefore, gotAfter = before, after
	})

	// 400 chars → 100 estimated tokens before; 40 chars → 10 after.
	before := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.UserAgenticMessage(strings.Repeat("a", 400)),
		},
	}
	after := adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.UserAgenticMessage(strings.Repeat("s", 40)),
		},
	}

	if err := cb(context.Background(), before, after); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !observed || gotBefore != 100 || gotAfter != 10 {
		t.Fatalf("observer = (%d, %d, seen=%v), want (100, 10, true)", gotBefore, gotAfter, observed)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "transcript.md"))
	if err != nil {
		t.Fatalf("read offloaded transcript: %v", err)
	}
	if !strings.Contains(string(raw), strings.Repeat("a", 400)) {
		t.Fatal("offloaded transcript must contain the pre-compaction window")
	}
}

func TestNormalizeCommand(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"compact":  CommandCompact,
		"COMPACT":  "",
		"/compact": "",
		"delete":   "",
	}
	for in, want := range cases {
		if got := normalizeCommand(in); got != want {
			t.Errorf("normalizeCommand(%q) = %q, want %q", in, got, want)
		}
	}
	// Command is optional: validation never demands it.
	if err := (ExecRequest{
		WorkspaceID: "ws", AgentID: "ag", SessionID: "sess", UserID: "u",
		Command: CommandCompact,
	}).Validate(); err != nil {
		t.Fatalf("Validate rejected an optional Command: %v", err)
	}
}

func kindsOf(events []TranscriptEvent) []TranscriptEventKind {
	out := make([]TranscriptEventKind, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func TestCompact_ExpandsStaleAttachmentBlocks(t *testing.T) {
	mdl := &compactModel{
		text:  "summarized discussion of the PDF and image attachments",
		usage: &schema.TokenUsage{PromptTokens: 80, CompletionTokens: 40, TotalTokens: 120},
	}
	st, runner, ws, _, req := setupCompactRunner(t, mdl)
	ctx := context.Background()

	adapter := NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)

	// Seed session with a user message containing reference-form (URL-only) image and PDF blocks
	imgURL := "/api/v1/files/att-cap/att-img-1"
	pdfURL := "/api/v1/files/att-cap/att-doc-1"
	imgBlock := schema.NewContentBlock(&schema.UserInputImage{
		URL:      imgURL,
		MIMEType: "image/png",
	})
	setAttachmentBlockMeta(imgBlock, attachmentBlockMeta{
		ID:   "att-img-1",
		Name: "diagram.png",
		Mime: "image/png",
		Size: 1024,
		URL:  imgURL,
	})
	fileBlock := schema.NewContentBlock(&schema.UserInputFile{
		URL:      pdfURL,
		MIMEType: "application/pdf",
		Name:     "spec.pdf",
	})
	setAttachmentBlockMeta(fileBlock, attachmentBlockMeta{
		ID:   "att-doc-1",
		Name: "spec.pdf",
		Mime: "application/pdf",
		Size: 2048,
		URL:  pdfURL,
	})

	userMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: "Please analyze these attachments: " + strings.Repeat("extra details. ", 40)}),
			imgBlock,
			fileBlock,
		},
	}
	assistantMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "I have reviewed both documents. " + strings.Repeat("analysis summary. ", 40)}),
		},
	}

	err := adapter.AppendEvents(ctx, req.SessionID, []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "ev-1", TurnID: "turn-1", Message: userMsg},
		{EventID: "ev-2", TurnID: "turn-1", Message: assistantMsg},
	})
	if err != nil {
		t.Fatalf("seed session events: %v", err)
	}

	// Provider is "openai" from setupCompactRunner; run compaction
	stream, err := runner.Run(ctx, compactRequest(req, "compact attachments"))
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("compaction turn failed: %+v", events)
	}
	if findCompacted(events) == nil {
		t.Fatalf("expected context_compacted event, got %+v", events)
	}

	// Verify that the summarizer model received collapsed text blocks (no UserInputImage or UserInputFile blocks)
	mdl.mu.Lock()
	if len(mdl.inputs) == 0 {
		t.Fatal("summarizer was never called")
	}
	summarizerInput := mdl.inputs[0]
	mdl.mu.Unlock()

	for _, msg := range summarizerInput {
		for _, block := range msg.ContentBlocks {
			if block.UserInputImage != nil {
				t.Errorf("summarizer received unconverted UserInputImage block: %+v", block.UserInputImage)
			}
			if block.UserInputFile != nil {
				t.Errorf("summarizer received unconverted UserInputFile block: %+v", block.UserInputFile)
			}
		}
	}

	// Verify that placeholder text is present in the summarizer input
	foundImgPlaceholder := false
	foundPDFPlaceholder := false
	for _, msg := range summarizerInput {
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			var text string
			if block.UserInputText != nil {
				text = block.UserInputText.Text
			} else if block.AssistantGenText != nil {
				text = block.AssistantGenText.Text
			} else if block.Reasoning != nil {
				text = block.Reasoning.Text
			}
			if text != "" {
				if strings.Contains(text, "diagram.png") && strings.Contains(text, imgURL) {
					foundImgPlaceholder = true
				}
				if strings.Contains(text, "spec.pdf") && strings.Contains(text, pdfURL) {
					foundPDFPlaceholder = true
				}
			}
		}
	}
	if !foundImgPlaceholder {
		t.Error("summarizer input missing image placeholder note")
	}
	if !foundPDFPlaceholder {
		t.Error("summarizer input missing PDF placeholder note")
	}

	// Verify persisted session events before compaction are untouched in store
	loaded, err := adapter.LoadEvents(ctx, req.SessionID, &adk.LoadSessionEventsRequest{})
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	// There should be 3 events: ev-1, ev-2, and the compaction MessagesReplaced event
	if len(loaded.Events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(loaded.Events))
	}
	ev1 := loaded.Events[0]
	if ev1.EventID != "ev-1" || ev1.Message == nil {
		t.Fatalf("first event corrupted: %+v", ev1)
	}
	if len(ev1.Message.ContentBlocks) != 3 {
		t.Fatalf("first event blocks altered: len = %d, want 3", len(ev1.Message.ContentBlocks))
	}
	if ev1.Message.ContentBlocks[1].UserInputImage == nil || ev1.Message.ContentBlocks[1].UserInputImage.URL != imgURL {
		t.Errorf("first event image block altered: %+v", ev1.Message.ContentBlocks[1])
	}
	if ev1.Message.ContentBlocks[2].UserInputFile == nil || ev1.Message.ContentBlocks[2].UserInputFile.URL != pdfURL {
		t.Errorf("first event file block altered: %+v", ev1.Message.ContentBlocks[2])
	}
}
