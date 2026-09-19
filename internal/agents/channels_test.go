package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ----- fakes -----

// stubChannelHandles resolves handles from a fixed map keyed
// "user:<id>" / "agent:<id>" — the shared handle rules without store reads.
type stubChannelHandles map[string]string

func (s stubChannelHandles) UserHandle(_ context.Context, userID string) (string, error) {
	if handle, ok := s["user:"+userID]; ok {
		return handle, nil
	}
	return "", fmt.Errorf("no handle for user %q", userID)
}

func (s stubChannelHandles) AgentHandle(_ context.Context, _, agentID string) (string, error) {
	if handle, ok := s["agent:"+agentID]; ok {
		return handle, nil
	}
	return "", fmt.Errorf("no handle for agent %q", agentID)
}

// fakeChannelContext is an in-memory ChannelContext. tail is kept
// oldest→newest, matching the store's feed order. active is the channel's
// active work session (nil = none).
type fakeChannelContext struct {
	mu        sync.Mutex
	channel   domain.Channel
	members   []domain.ChannelMember
	tail      []domain.ChannelMessage
	tailLimit int
	active    *domain.WorkSession
	err       error
}

func (f *fakeChannelContext) GetChannel(_ context.Context, _, _ string) (domain.Channel, error) {
	if f.err != nil {
		return domain.Channel{}, f.err
	}
	return f.channel, nil
}

func (f *fakeChannelContext) ListChannelMembers(_ context.Context, _, _ string) ([]domain.ChannelMember, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.members, nil
}

func (f *fakeChannelContext) ChannelTail(_ context.Context, _, _ string, limit int) ([]domain.ChannelMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	f.tailLimit = limit
	f.mu.Unlock()
	if len(f.tail) > limit {
		return f.tail[len(f.tail)-limit:], nil
	}
	return f.tail, nil
}

func (f *fakeChannelContext) ChannelMessagesAfter(_ context.Context, _, _ string, beforeSeq int64, limit int) ([]domain.ChannelMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	var older []domain.ChannelMessage
	for _, msg := range f.tail {
		if beforeSeq == 0 || msg.Seq < beforeSeq {
			older = append(older, msg)
		}
	}
	if len(older) > limit {
		older = older[len(older)-limit:]
	}
	return older, nil
}

func (f *fakeChannelContext) ActiveWorkSession(_ context.Context, _, _ string) (*domain.WorkSession, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.active, nil
}

// fakeChannelFeed records PostFromAgent calls and returns a synthesized
// persisted message, mirroring the chokepoint's seq-assigning insert.
type fakeChannelFeed struct {
	mu    sync.Mutex
	posts []struct {
		workspaceID string
		channelID   string
		agentID     string
		body        string
	}
	nextSeq int64
	err     error
}

func (f *fakeChannelFeed) PostFromAgent(_ context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error) {
	if f.err != nil {
		return domain.ChannelMessage{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextSeq++
	f.posts = append(f.posts, struct {
		workspaceID string
		channelID   string
		agentID     string
		body        string
	}{workspaceID, channelID, agentID, body})
	return domain.ChannelMessage{
		ID:          fmt.Sprintf("msg-%d", f.nextSeq),
		WorkspaceID: workspaceID,
		ChannelID:   channelID,
		Seq:         f.nextSeq,
		Body:        body,
		CreatedAt:   time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (f *fakeChannelFeed) posted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

// ----- ExecRequest validation (task 4.1) -----

func TestExecRequest_ChannelValidation(t *testing.T) {
	base := ExecRequest{
		WorkspaceID: "ws",
		AgentID:     "ag",
		SessionID:   "sess",
		UserID:      "u",
	}

	if err := (base).Validate(); err != nil {
		t.Fatalf("user-origin request must validate: %v", err)
	}

	channelReq := base
	channelReq.Origin = OriginChannel
	if err := channelReq.Validate(); err == nil || !strings.Contains(err.Error(), "channel_id") {
		t.Fatalf("channel origin without ChannelID must fail validation, got %v", err)
	}

	channelReq.ChannelID = "ch-1"
	channelReq.RootMessageID = "root-1"
	channelReq.ChainDepth = 2
	if err := channelReq.Validate(); err != nil {
		t.Fatalf("channel request with ChannelID must validate: %v", err)
	}
}

// ----- Composition renderers (task 4.1 / design D8) -----

func TestRenderChannelDoc(t *testing.T) {
	channel := domain.Channel{
		ID:          "ch-1",
		WorkspaceID: "ws-1",
		Name:        "Production Ops",
		Slug:        "incidents",
		Purpose:     "Coordinate production incident response end to end.",
		Conventions: "Be terse. Link dashboards.",
	}
	members := []domain.ChannelMember{
		{WorkspaceID: "ws-1", ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeUser, UserID: "u-1", Specialization: "incident commander"},
		{WorkspaceID: "ws-1", ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeAgent, AgentID: "ag-2", Specialization: "metrics & dashboards"},
	}
	handles := stubChannelHandles{"user:u-1": "sarah-chen", "agent:ag-2": "beacon"}

	doc, err := renderChannelDoc(context.Background(), handles, channel, members, "atlas")
	if err != nil {
		t.Fatalf("renderChannelDoc: %v", err)
	}
	want := strings.Join([]string{
		"# Channel: #incidents — Production Ops",
		"",
		"Purpose: Coordinate production incident response end to end.",
		"",
		"Members:",
		"  - @sarah-chen (user) — incident commander",
		"  - @beacon (agent) — metrics & dashboards",
		"",
		"You are @atlas.",
		"",
		"## Conventions",
		"Be terse. Link dashboards.",
	}, "\n")
	if doc != want {
		t.Fatalf("CHANNEL.md doc =\n%s\nwant\n%s", doc, want)
	}

	// Blank optionals omit their lines; the fixed skeleton remains.
	minimal, err := renderChannelDoc(context.Background(), handles, domain.Channel{Name: "Ops", Slug: "ops"}, nil, "atlas")
	if err != nil {
		t.Fatalf("renderChannelDoc minimal: %v", err)
	}
	if strings.Contains(minimal, "Purpose") || strings.Contains(minimal, "Conventions") || strings.Contains(minimal, "Members") {
		t.Fatalf("blank optionals must be omitted, got:\n%s", minimal)
	}
	if !strings.Contains(minimal, "# Channel: #ops — Ops") || !strings.Contains(minimal, "You are @atlas.") {
		t.Fatalf("minimal doc =\n%s", minimal)
	}
}

func TestRenderChannelCatchUp(t *testing.T) {
	handles := stubChannelHandles{"user:u-1": "sarah-chen", "agent:ag-2": "beacon", "agent:atlas-id": "atlas"}
	channel := domain.Channel{WorkspaceID: "ws-1", Name: "Production Ops", Slug: "incidents"}
	tail := []domain.ChannelMessage{
		{WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 1, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: "u-1", Body: "kickoff"},
		{WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 2, AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: "ag-2",
			Body: "p99 is spiking", RunSummary: &domain.RunSummary{Tools: map[string]int{"grafana.query": 2}, DurationMS: 1200}},
		{WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 3, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: "u-1", Body: "@atlas can you analyze the payment part?"},
	}

	// The triggering message (seq 3) rides the tail and the turn input; the
	// composer must drop the trailing duplicate.
	req := ExecRequest{
		WorkspaceID: "ws-1",
		AgentID:     "atlas-id",
		Input:       "[#incidents] @sarah-chen: @atlas can you analyze the payment part?",
	}
	got, err := renderChannelCatchUp(context.Background(), handles, channel, tail, req)
	if err != nil {
		t.Fatalf("renderChannelCatchUp: %v", err)
	}
	want := strings.Join([]string{
		"# Channel catch-up",
		"",
		"You have not spoken in this channel yet.",
		"",
		"@sarah-chen: kickoff",
		"@beacon: p99 is spiking (@beacon — grafana.query ×2 · 1.2s)",
	}, "\n")
	if got != want {
		t.Fatalf("catch-up =\n%s\nwant\n%s", got, want)
	}

	// A warm summon — the agent already spoke — drops the cold note.
	agentTail := append([]domain.ChannelMessage{}, tail...)
	agentTail = append(agentTail, domain.ChannelMessage{
		WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 4,
		AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: "atlas-id", Body: "on it",
	})
	warm, err := renderChannelCatchUp(context.Background(), handles, channel, agentTail, req)
	if err != nil {
		t.Fatalf("renderChannelCatchUp warm: %v", err)
	}
	if strings.Contains(warm, channelColdSummonNote) {
		t.Fatalf("warm summon must not carry the cold note:\n%s", warm)
	}
	if !strings.Contains(warm, "@atlas: on it") {
		t.Fatalf("warm summon tail =\n%s", warm)
	}

	// An empty feed is just the cold note.
	empty, err := renderChannelCatchUp(context.Background(), handles, channel, nil, req)
	if err != nil {
		t.Fatalf("renderChannelCatchUp empty: %v", err)
	}
	if empty != "# Channel catch-up\n\n"+channelColdSummonNote {
		t.Fatalf("empty feed catch-up =\n%s", empty)
	}
}

func TestComposeChannelDocs(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "s@example.com", Name: "Sarah Chen"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	fcc := &fakeChannelContext{
		channel: domain.Channel{ID: "ch-1", WorkspaceID: ws.ID, Name: "Production Ops", Slug: "incidents", Purpose: "Incident response."},
		members: []domain.ChannelMember{
			{WorkspaceID: ws.ID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeUser, UserID: user.ID, Specialization: "incident commander"},
			{WorkspaceID: ws.ID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeAgent, AgentID: agent.ID, Specialization: "metrics"},
		},
		tail: []domain.ChannelMessage{
			{WorkspaceID: ws.ID, ChannelID: "ch-1", Seq: 1, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: user.ID, Body: "kickoff"},
			{WorkspaceID: ws.ID, ChannelID: "ch-1", Seq: 2, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: user.ID, Body: "hello room"},
		},
	}

	runner := NewRunner(nil, st.Agents(), st.Users(), nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
		[]byte("k"), "/tmp/onclaw")
	// Splice the channel ports directly: composeChannelDocs reads them (the
	// constructor options are covered by the wiring tests below).
	runner.channelContext = fcc

	docs, err := runner.composeChannelDocs(ctx, ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		ChannelID:   "ch-1",
		Input:       "[#incidents] @sarah-chen: hello room",
	}, agent)
	if err != nil {
		t.Fatalf("composeChannelDocs: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected CHANNEL.md doc + catch-up tail, got %d docs", len(docs))
	}
	if !strings.Contains(docs[0], "# Channel: #incidents — Production Ops") ||
		!strings.Contains(docs[0], "@sarah-chen (user) — incident commander") ||
		!strings.Contains(docs[0], "You are @atlas.") {
		t.Fatalf("CHANNEL.md doc =\n%s", docs[0])
	}
	if !strings.Contains(docs[1], "@sarah-chen: kickoff") {
		t.Fatalf("catch-up doc =\n%s", docs[1])
	}
	// The triggering message rides the tail and must be trimmed: the turn
	// input carries it (L3).
	if strings.Contains(docs[1], "hello room") {
		t.Fatalf("triggering message must be excluded from the catch-up tail:\n%s", docs[1])
	}
	if fcc.tailLimit != ChannelTailLimit {
		t.Fatalf("ChannelTail limit = %d, want %d", fcc.tailLimit, ChannelTailLimit)
	}
}

// ----- Work session block in the channel doc (channel-teams task 4 / D8) -----

func TestRenderWorkSessionBlock(t *testing.T) {
	base := domain.WorkSession{
		ID:          "wsess-1",
		WorkspaceID: "ws-1",
		ChannelID:   "ch-1",
		Goal:        "Add dark mode to settings",
		Status:      domain.WorkSessionOpen,
		Budget:      12,
		HopsUsed:    7,
	}

	// Open session, plain member: the pinned shape without the facilitator line.
	open := renderWorkSessionBlock(base, false)
	want := strings.Join([]string{
		"## Work session",
		"Goal: Add dark mode to settings",
		"Status: open",
		"Hops remaining: 5 of 12",
		"Shared project space: /project (PLAN.md is the tracker — claim your area before writing)",
	}, "\n")
	if open != want {
		t.Fatalf("open block =\n%s\nwant\n%s", open, want)
	}

	// Paused on the human gate.
	awaiting := base
	awaiting.Status = domain.WorkSessionPaused
	awaiting.PauseReason = "awaiting-human"
	if got := renderWorkSessionBlock(awaiting, false); !strings.Contains(got, "Status: paused (awaiting-human)") {
		t.Fatalf("awaiting-human block =\n%s", got)
	}

	// Paused on the exhausted budget: no hops remain.
	exhausted := base
	exhausted.Status = domain.WorkSessionPaused
	exhausted.PauseReason = "budget-exhausted"
	exhausted.HopsUsed = exhausted.Budget
	got := renderWorkSessionBlock(exhausted, false)
	if !strings.Contains(got, "Status: paused (budget-exhausted)") || !strings.Contains(got, "Hops remaining: 0 of 12") {
		t.Fatalf("budget-exhausted block =\n%s", got)
	}

	// The facilitator gets its closing power announced.
	facilitator := renderWorkSessionBlock(base, true)
	if !strings.Contains(facilitator, "You are the facilitator: you may close this session with session.close once the goal is met.") {
		t.Fatalf("facilitator block =\n%s", facilitator)
	}
	if strings.Contains(open, "facilitator") {
		t.Fatalf("plain member block must not carry the facilitator line:\n%s", open)
	}
}

func TestComposeChannelDocs_SessionBlock(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "s@example.com", Name: "Sarah Chen"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	fcc := &fakeChannelContext{
		channel: domain.Channel{ID: "ch-1", WorkspaceID: ws.ID, Name: "Production Ops", Slug: "incidents"},
		members: []domain.ChannelMember{
			{WorkspaceID: ws.ID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeAgent, AgentID: agent.ID},
		},
	}
	runner := NewRunner(nil, st.Agents(), st.Users(), nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
		[]byte("k"), "/tmp/onclaw")
	runner.channelContext = fcc

	req := ExecRequest{WorkspaceID: ws.ID, AgentID: agent.ID, ChannelID: "ch-1"}

	// No active session: the doc has no session block.
	docs, err := runner.composeChannelDocs(ctx, req, agent)
	if err != nil {
		t.Fatalf("composeChannelDocs (no session): %v", err)
	}
	if strings.Contains(docs[0], "Work session") {
		t.Fatalf("session-less channel doc must not carry the block:\n%s", docs[0])
	}

	// An active session appends the block; a non-facilitator member gets no
	// closing line.
	fcc.active = &domain.WorkSession{Goal: "Ship the retry queue", Status: domain.WorkSessionOpen, Budget: 12, HopsUsed: 2}
	docs, err = runner.composeChannelDocs(ctx, req, agent)
	if err != nil {
		t.Fatalf("composeChannelDocs (session): %v", err)
	}
	if !strings.Contains(docs[0], "## Work session") ||
		!strings.Contains(docs[0], "Goal: Ship the retry queue") ||
		!strings.Contains(docs[0], "Hops remaining: 10 of 12") {
		t.Fatalf("session block missing from channel doc:\n%s", docs[0])
	}
	if strings.Contains(docs[0], "You are the facilitator") {
		t.Fatalf("non-facilitator doc must not carry the facilitator line:\n%s", docs[0])
	}

	// The facilitator's doc carries the line.
	fcc.members[0].Role = domain.ChannelMemberRoleFacilitator
	docs, err = runner.composeChannelDocs(ctx, req, agent)
	if err != nil {
		t.Fatalf("composeChannelDocs (facilitator): %v", err)
	}
	if !strings.Contains(docs[0], "You are the facilitator: you may close this session with session.close once the goal is met.") {
		t.Fatalf("facilitator line missing:\n%s", docs[0])
	}

	// The session block sits inside the L1 channel doc, before the catch-up.
	if strings.Contains(docs[1], "Work session") {
		t.Fatalf("session block must not leak into the catch-up doc:\n%s", docs[1])
	}

	// A failing session read fails the composition — context gates must not
	// silently disappear. The wrapper fails only the session read; the base
	// fake keeps serving the room reads.
	failing := &sessionErrContext{fakeChannelContext: fcc, err: errors.New("db down")}
	runner.channelContext = failing
	if _, err := runner.composeChannelDocs(ctx, req, agent); err == nil || !strings.Contains(err.Error(), "load active work session") {
		t.Fatalf("broken session read must fail the composition, got %v", err)
	}
}

// sessionErrContext fails only ActiveWorkSession, delegating the rest of the
// reads to the embedded fake.
type sessionErrContext struct {
	*fakeChannelContext
	err error
}

func (f *sessionErrContext) ActiveWorkSession(context.Context, string, string) (*domain.WorkSession, error) {
	return nil, f.err
}

func TestInstructionComposer_ChannelDocsPosition(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(tempDir+"/BOOTSTRAP.md", []byte("# Bootstrap: Welcome"), 0o644); err != nil {
		t.Fatalf("write BOOTSTRAP.md: %v", err)
	}

	st := fake.New()
	ws := &domain.Workspace{ID: "ws-1", Slug: "acme", Name: "Acme"}
	user := &domain.User{ID: "u-1", Name: "Sarah Chen"}

	instruction, err := NewInstructionComposer().Compose(context.Background(), ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user,
		RoleName:  "owner",
		Memories:  st.Memories(),
		ChannelDocs: []string{
			"CHANNEL-DOC",
			"CATCH-UP-DOC",
		},
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	userIdx := strings.Index(instruction, "# Current User")
	channelIdx := strings.Index(instruction, "CHANNEL-DOC")
	catchUpIdx := strings.Index(instruction, "CATCH-UP-DOC")
	bootstrapIdx := strings.Index(instruction, "# Bootstrap: Welcome")
	if userIdx < 0 || channelIdx < 0 || catchUpIdx < 0 || bootstrapIdx < 0 {
		t.Fatalf("missing docs in instruction:\n%s", instruction)
	}
	if !(userIdx < channelIdx && channelIdx < catchUpIdx && catchUpIdx < bootstrapIdx) {
		t.Fatalf("channel docs must sit between USER.md and BOOTSTRAP.md:\n%s", instruction)
	}

	// Without ChannelDocs the composition is exactly as before (no doc, no
	// extra separators).
	plain, err := NewInstructionComposer().Compose(context.Background(), ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user,
		RoleName:  "owner",
		Memories:  st.Memories(),
	})
	if err != nil {
		t.Fatalf("Compose plain: %v", err)
	}
	if strings.Contains(plain, "CHANNEL-DOC") {
		t.Fatal("plain composition must not contain channel docs")
	}
}

// ----- Resolve-time wiring (task 4.1) -----

func TestRunner_ChannelRunRequiresChannelPorts(t *testing.T) {
	mdl := &hooksModel{final: "done"}
	_, runner, ws, ag, req := setupHooksRunner(t, nil, mdl)

	channelReq := req
	channelReq.Origin = OriginChannel
	channelReq.ChannelID = "ch-1"
	channelReq.SessionID = "chan_ch-1_" + ag.ID
	_, err := runner.Run(context.Background(), channelReq)
	if err == nil || !strings.Contains(err.Error(), "WithChannelContext") {
		t.Fatalf("channel run without channel ports must fail at resolve time, got %v", err)
	}
	_ = ws
}

// ----- Channel tools (task 5) -----

func resolvedChannelTools(t *testing.T, tctx ToolContext, allowed []string) []tool.BaseTool {
	t.Helper()
	_, tools, err := ResolvedTools(tctx, NewDefaultToolRegistry(nil), allowed)
	if err != nil {
		t.Fatalf("ResolvedTools: %v", err)
	}
	return tools
}

func toolByName(t *testing.T, tools []tool.BaseTool, name string) tool.InvokableTool {
	t.Helper()
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info != nil && info.Name == name {
			inv, ok := tl.(tool.InvokableTool)
			if !ok {
				t.Fatalf("tool %q is not invokable", name)
			}
			return inv
		}
	}
	t.Fatalf("tool %q not resolved from %v", name, tools)
	return nil
}

func TestChannelPostTool(t *testing.T) {
	feed := &fakeChannelFeed{}
	handles := stubChannelHandles{}
	tctx := ToolContext{
		WorkspaceID: "ws-1", AgentID: "ag-1", ChannelID: "ch-1",
		ChannelContext: &fakeChannelContext{}, ChannelFeed: feed, ChannelHandles: handles,
	}
	tools := resolvedChannelTools(t, tctx, []string{"memory", ChannelToolPost, ChannelToolHistory})
	post := toolByName(t, tools, ChannelToolPost)

	out, err := post.InvokableRun(context.Background(), `{"body":"heads up, deploying"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var result struct {
		ID        string    `json:"id"`
		Seq       int64     `json:"seq"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("result %q is not the compact JSON confirmation: %v", out, err)
	}
	if result.ID != "msg-1" || result.Seq != 1 || result.CreatedAt.IsZero() {
		t.Fatalf("posted message result = %+v", result)
	}
	feed.mu.Lock()
	got := feed.posts
	feed.mu.Unlock()
	if len(got) != 1 || got[0].workspaceID != "ws-1" || got[0].channelID != "ch-1" || got[0].agentID != "ag-1" || got[0].body != "heads up, deploying" {
		t.Fatalf("feed post = %+v", got)
	}

	if _, err := post.InvokableRun(context.Background(), `{"body":"   "}`); err == nil {
		t.Fatal("blank body must be rejected")
	}

	// Outside a channel run the constructor fails explicitly.
	if _, err := newChannelPostTool(ToolContext{WorkspaceID: "ws-1"}); err == nil {
		t.Fatal("channel.post must not construct without a channel feed")
	}
}

func TestChannelHistoryTool(t *testing.T) {
	handles := stubChannelHandles{"user:u-1": "sarah-chen", "agent:ag-2": "beacon"}
	fcc := &fakeChannelContext{tail: []domain.ChannelMessage{
		{WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 1, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: "u-1", Body: "kickoff"},
		{WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 2, AuthorType: domain.ChannelMemberTypeAgent, AuthorAgentID: "ag-2",
			Body: "looking", RunSummary: &domain.RunSummary{Tools: map[string]int{"grafana.query": 1}, DurationMS: 800}},
		{WorkspaceID: "ws-1", ChannelID: "ch-1", Seq: 3, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: "u-1", Body: "thanks"},
	}}
	tctx := ToolContext{
		WorkspaceID: "ws-1", AgentID: "ag-1", ChannelID: "ch-1",
		ChannelContext: fcc, ChannelFeed: &fakeChannelFeed{}, ChannelHandles: handles,
	}
	history := toolByName(t, resolvedChannelTools(t, tctx, []string{ChannelToolHistory}), ChannelToolHistory)

	// Newest page (no cursor), oldest→newest, attributed + annotated.
	out, err := history.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	want := strings.Join([]string{
		"[#1] @sarah-chen: kickoff",
		"[#2] @beacon: looking (@beacon — grafana.query ×1 · 0.8s)",
		"[#3] @sarah-chen: thanks",
	}, "\n")
	if out != want {
		t.Fatalf("history =\n%s\nwant\n%s", out, want)
	}

	// Backward cursor: strictly older than seq 2.
	out, err = history.InvokableRun(context.Background(), `{"before":2}`)
	if err != nil {
		t.Fatalf("InvokableRun before=2: %v", err)
	}
	if out != "[#1] @sarah-chen: kickoff" {
		t.Fatalf("paged history = %q", out)
	}

	// An empty page is an explicit marker, not an error.
	out, err = history.InvokableRun(context.Background(), `{"before":1}`)
	if err != nil || out != channelHistoryEmptyMarker {
		t.Fatalf("empty page = %q, err %v", out, err)
	}

	// Limit clamps to the page max.
	if _, err := history.InvokableRun(context.Background(), `{"limit":500}`); err != nil {
		t.Fatalf("over-limit call must clamp, got error: %v", err)
	}

	// Outside a channel run the constructor fails explicitly.
	if _, err := newChannelHistoryTool(ToolContext{WorkspaceID: "ws-1"}); err == nil {
		t.Fatal("channel.history must not construct without channel bindings")
	}
}

func TestChannelTools_ChannelRunOnlyExposure(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/onclaw")

	// Channel runs carry the channel toolset through the gate.
	gated, err := runner.applyToolGate(ctx, "ws-1", scopeChannelToolsIn([]string{"memory"}, true))
	if err != nil {
		t.Fatalf("applyToolGate: %v", err)
	}
	for _, name := range ChannelToolNames {
		if !strings.Contains(strings.Join(gated, ","), name) {
			t.Fatalf("channel run toolset must include %s, got %v", name, gated)
		}
	}

	// The workspace gate still wins: a disabled channel tool is filtered.
	governed := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/onclaw",
		WithToolPolicy(&fakeToolPolicy{enabled: map[string]bool{ChannelToolPost: false}}))
	gated, err = governed.applyToolGate(ctx, "ws-1", scopeChannelToolsIn([]string{}, true))
	if err != nil {
		t.Fatalf("applyToolGate governed: %v", err)
	}
	for _, name := range gated {
		if name == ChannelToolPost {
			t.Fatalf("disabled channel.post must be gated out, got %v", gated)
		}
	}

	// Non-channel runs never expose the channel toolset, even when allowlisted.
	stripped := withoutChannelTools([]string{"memory", ChannelToolPost, ChannelToolHistory})
	if len(stripped) != 1 || stripped[0] != "memory" {
		t.Fatalf("non-channel toolset = %v, want [memory]", stripped)
	}
	if names := NewDefaultToolRegistry(nil).Names(); !containsString(names, ChannelToolPost) || !containsString(names, ChannelToolHistory) {
		t.Fatalf("registry must register the channel tools, got %v", names)
	}
}

func containsString(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestChannelCatalogEntries(t *testing.T) {
	for _, name := range ChannelToolNames {
		entry, ok := ToolCatalogEntryByKey(name)
		if !ok {
			t.Fatalf("catalog entry missing for %s", name)
		}
		if entry.DisplayName == "" || entry.Group != "channel" {
			t.Fatalf("catalog entry for %s = %+v", name, entry)
		}
		if !entry.AlwaysOn {
			t.Fatalf("%s must be AlwaysOn", name)
		}
	}
}

// ----- Hooks interplay (task 4.3 / design D10) -----

// captureHookCommand writes a command hook whose handler saves the delivered
// event JSON (stdin) to file — a payload observer with exit 0 (allow).
func captureHookCommand(t *testing.T, name string, event domain.HookEvent, matcher, file string) *domain.WorkspaceHook {
	t.Helper()
	return &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        name,
			Event:       event,
			Matcher:     matcher,
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "cat > " + file}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
}

// waitForHookEvent polls for a hook-capture file and returns the first
// complete event JSON in it. Deliveries are detached from the run, and the
// capture file appears before `cat` finishes writing, so the poll requires a
// payload that parses and carries its event name.
func waitForHookEvent(t *testing.T, path, event string) agenthooks.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil && len(raw) > 0 {
			var ev agenthooks.Event
			if json.Unmarshal(raw, &ev) == nil && ev.Event == event {
				return ev
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("hook event %q never fully captured in %s (last: %q, %v)", event, path, string(raw), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// channelRunContext returns the fake channel ports plus the runner options
// binding them, seeded for a channel whose only member is the setup user.
func channelRunContext() (*fakeChannelContext, *fakeChannelFeed, []RunnerOption) {
	fcc := &fakeChannelContext{
		channel: domain.Channel{ID: "ch-1", Name: "Production Ops", Slug: "incidents", Purpose: "Incident response."},
	}
	fcf := &fakeChannelFeed{}
	opts := []RunnerOption{
		WithChannelContext(fcc),
		WithChannelFeed(fcf),
	}
	// The roster/tail reference the seeded user, whose id only exists after
	// setupHooksRunner created it — the caller populates those via
	// seedChannelFixtures once the request (and user id) is known.
	return fcc, fcf, opts
}

func seedChannelFixtures(fcc *fakeChannelContext, wsID, userID string) {
	fcc.channel.WorkspaceID = wsID
	fcc.members = []domain.ChannelMember{
		{WorkspaceID: wsID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeUser, UserID: userID, Specialization: "incident commander"},
	}
	fcc.tail = []domain.ChannelMessage{
		{WorkspaceID: wsID, ChannelID: "ch-1", Seq: 1, AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: userID, Body: "kickoff"},
	}
}

func TestHooks_ChannelOriginBlockedBeforeModel(t *testing.T) {
	mdl := &hooksModel{final: "should never be produced"}
	hook := &domain.WorkspaceHook{
		HookBase: domain.HookBase{
			Name:        "channel-gate",
			Event:       domain.HookEventUserPromptSubmit,
			Matcher:     "channel",
			HandlerType: domain.HookHandlerCommand,
			Config:      mustHookJSON(t, map[string]any{"command": "sh", "args": []string{"-c", "echo channel prompts denied >&2; exit 2"}}),
			TimeoutMS:   5000,
			OnFailure:   domain.HookFailureAllow,
			Enabled:     true,
		},
	}
	fcc, fcf, opts := channelRunContext()
	_, runner, ws, ag, req := setupHooksRunnerWithOpts(t, nil, mdl, opts, hook)

	channelReq := req
	channelReq.Origin = OriginChannel
	channelReq.ChannelID = "ch-1"
	channelReq.SessionID = "chan_ch-1_" + ag.ID
	channelReq.Input = "[#incidents] @sarah-chen: summon"
	seedChannelFixtures(fcc, ws.ID, channelReq.UserID)

	stream, err := runner.Run(context.Background(), channelReq)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	// The summoning message hits user_prompt_submit BEFORE the model: the
	// channel-origin matcher blocks with zero model calls (D10).
	if mdl.callCount(t) != 0 {
		t.Fatal("a channel-blocked prompt must never reach the model")
	}
	blocked := findPromptBlocked(events)
	if blocked == nil || blocked.PromptBlocked == nil {
		t.Fatalf("expected prompt_blocked for the channel-origin run, got %+v", events)
	}

	// The same hook must not gate a user-origin run.
	userReq := req
	userReq.SessionID = "sess-user"
	userStream, err := runner.Run(context.Background(), userReq)
	if err != nil {
		t.Fatalf("Run (user): %v", err)
	}
	userEvents := collectStream(t, userStream)
	if !hasKind(userEvents, TranscriptEventTurnCompleted) || findPromptBlocked(userEvents) != nil {
		t.Fatalf("user-origin run must complete normally, got %+v", userEvents)
	}
	if got := mdl.callCount(t); got != 1 {
		t.Fatalf("model calls after user-origin run = %d, want 1", got)
	}
	if fcf.posted() != 0 {
		t.Fatal("no channel feed traffic may occur in these gated runs")
	}
}

func TestHooks_ChannelOriginRunLifecyclePayloads(t *testing.T) {
	mdl := &hooksModel{final: "done"}
	startedFile := t.TempDir() + "/run_started.json"
	finishedFile := t.TempDir() + "/run_finished.json"
	hooks := []*domain.WorkspaceHook{
		captureHookCommand(t, "channel-start-observer", domain.HookEventRunStarted, "channel", startedFile),
		// run_finished matchers select on the terminal STATUS (D8); the
		// channel origin rides the payload, which is what this asserts.
		captureHookCommand(t, "channel-finish-observer", domain.HookEventRunFinished, "*", finishedFile),
	}
	fcc, _, opts := channelRunContext()
	_, runner, ws, ag, req := setupHooksRunnerWithOpts(t, nil, mdl, opts, hooks...)

	channelReq := req
	channelReq.Origin = OriginChannel
	channelReq.ChannelID = "ch-1"
	channelReq.SessionID = "chan_ch-1_" + ag.ID
	seedChannelFixtures(fcc, ws.ID, channelReq.UserID)

	stream, err := runner.Run(context.Background(), channelReq)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("channel run must complete, got %+v", events)
	}

	started := waitForHookEvent(t, startedFile, "run_started")
	if started.Origin != OriginChannel {
		t.Fatalf("run_started payload = %+v", started)
	}

	finished := waitForHookEvent(t, finishedFile, "run_finished")
	if finished.Origin != OriginChannel || finished.Status != hookRunStatusCompleted {
		t.Fatalf("run_finished payload = %+v", finished)
	}
}

// A channel run end to end over the fake ports: composition reads the room,
// the run completes, and the ChannelTail read used the pinned catch-up bound.
func TestRunner_ChannelRunComposeAndExecute(t *testing.T) {
	mdl := &hooksModel{final: "done"}
	fcc, _, opts := channelRunContext()
	_, runner, ws, ag, req := setupHooksRunnerWithOpts(t, nil, mdl, opts)

	channelReq := req
	channelReq.Origin = OriginChannel
	channelReq.ChannelID = "ch-1"
	channelReq.SessionID = "chan_ch-1_" + ag.ID
	channelReq.Input = "[#incidents] @sarah-chen: kickoff"
	seedChannelFixtures(fcc, ws.ID, channelReq.UserID)
	fcc.tail = []domain.ChannelMessage{
		{WorkspaceID: ws.ID, ChannelID: "ch-1", Seq: 1,
			AuthorType: domain.ChannelMemberTypeUser, AuthorUserID: channelReq.UserID, Body: "kickoff"},
	}

	stream, err := runner.Run(context.Background(), channelReq)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) {
		t.Fatalf("channel run must complete cleanly, got %+v", events)
	}
	if fcc.tailLimit != ChannelTailLimit {
		t.Fatalf("catch-up tail read limit = %d, want %d", fcc.tailLimit, ChannelTailLimit)
	}

	// A failing channel context read fails the run — the room context is the
	// run's grounding. Since channel-teams task 4/5, resolve reads the room
	// (channel + roster) before composition to gate the facilitator powers
	// and the /project mount, so the failure surfaces there.
	fcc.err = errors.New("db down")
	failedReq := channelReq
	failedReq.SessionID = "chan_ch-1_alt"
	if _, err := runner.Run(context.Background(), failedReq); err == nil || !strings.Contains(err.Error(), "load channel") {
		t.Fatalf("broken channel context must fail the run, got %v", err)
	}
}
