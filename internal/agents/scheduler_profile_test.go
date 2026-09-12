package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// schedulerSessionID documents the session id shape the scheduler service
// mints for one run (integrate-scheduler D4/D7): sched_<schedulerID>_<unix
// seconds>. The minter itself ships with the scheduler service (a later
// wave); this helper pins the shape the drain, the runs listing's
// sched_<schedulerID>_ prefix query, and the session-index skip rely on.
func schedulerSessionID(schedulerID string, at time.Time) string {
	return fmt.Sprintf("sched_%s_%d", schedulerID, at.Unix())
}

func TestSchedulerSessionIDShape(t *testing.T) {
	at := time.Unix(1760000000, 0).UTC()
	got := schedulerSessionID("sch-9f3a", at)
	if got != "sched_sch-9f3a_1760000000" {
		t.Fatalf("schedulerSessionID = %q, want sched_sch-9f3a_1760000000", got)
	}
	// The runs listing (D7) keys transcripts on the deterministic
	// sched_<schedulerID>_ prefix; the trailing unix timestamp gives every
	// run its own session so the one-run-per-session guard never collides.
	if prefix := "sched_sch-9f3a_"; !strings.HasPrefix(got, prefix) {
		t.Fatalf("session id %q must carry the sched_<schedulerID>_ prefix %q", got, prefix)
	}
	if ts := strings.TrimPrefix(got, "sched_sch-9f3a_"); ts != "1760000000" {
		t.Fatalf("session id %q must end with the unix timestamp, got suffix %q", got, ts)
	}
}

// recordingComposer delegates to the real DefaultInstructionComposer and
// records the instruction it composed plus the params the runner passed, so
// tests assert exactly what composeAgent wired into the ADK agent.
type recordingComposer struct {
	mu    sync.Mutex
	inner InstructionComposer
	last  string
	param ComposeParams
}

func newRecordingComposer() *recordingComposer {
	return &recordingComposer{inner: NewInstructionComposer()}
}

func (c *recordingComposer) Compose(ctx context.Context, params ComposeParams) (string, error) {
	instruction, err := c.inner.Compose(ctx, params)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = instruction
	c.param = params
	return instruction, nil
}

func (c *recordingComposer) composed() (string, ComposeParams) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last, c.param
}

// countingCheckpoints wraps the store's checkpoint port and counts writes and
// deletes so tests can assert the scheduler run's checkpoint skip (task 3.2):
// with the store skipped, the ADK never arms a checkpoint ID, so neither a
// write nor the finalize-time delete can fire.
type countingCheckpoints struct {
	inner   store.SessionCheckpointStore
	mu      sync.Mutex
	sets    int
	deletes int
}

func (c *countingCheckpoints) Get(ctx context.Context, id string) ([]byte, bool, error) {
	return c.inner.Get(ctx, id)
}

func (c *countingCheckpoints) Set(ctx context.Context, id string, data []byte) error {
	c.mu.Lock()
	c.sets++
	c.mu.Unlock()
	return c.inner.Set(ctx, id, data)
}

func (c *countingCheckpoints) Delete(ctx context.Context, id string) error {
	c.mu.Lock()
	c.deletes++
	c.mu.Unlock()
	return c.inner.Delete(ctx, id)
}

func (c *countingCheckpoints) counts() (sets, deletes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sets, c.deletes
}

// setupSchedulerRunner seeds the same fixture as setupLifecycleRunner but
// keeps the store handle, wires the real instruction composer through a
// recordingComposer, and swaps the checkpoint store for a counting spy.
// Extra options configure further runner knobs (e.g. the channel ports).
func setupSchedulerRunner(t *testing.T, sessionID string, opts ...RunnerOption) (store.Store, *Runner, *recordingComposer, *countingCheckpoints, ExecRequest) {
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

	cps := &countingCheckpoints{inner: st.SessionCheckpoints()}
	rec := newRecordingComposer()
	runnerOpts := []RunnerOption{
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return &manyDeltaModel{deltas: 1}, nil
		}),
		WithInstructionComposer(rec),
	}
	runnerOpts = append(runnerOpts, opts...)
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), cps, st.Memories(), st.AgentSessions(),
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		runnerOpts...)

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
		SessionID:   sessionID,
		UserID:      user.ID,
		Input:       "hello",
	}
	return st, runner, rec, cps, req
}

// seedPromptDocs writes the four on-disk prompt documents into the agent
// workspace directory; the WORKSPACE and USER docs are virtual and fed by
// seeded memories instead.
func seedPromptDocs(t *testing.T, runner *Runner, markers map[string]string) {
	t.Helper()
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), "acme", "atlas")
	for _, name := range []string{"AGENTS.md", "IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
		content := strings.ToUpper(strings.TrimSuffix(name, ".md")) + "-CONTENT-MARKER"
		if markers != nil {
			if override, ok := markers[name]; ok {
				content = override
			}
		}
		if err := os.WriteFile(filepath.Join(agentDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
}

const (
	unattendedContractHeader = "## Unattended run\n\nThis run executes unattended on a schedule — nobody is watching live. " +
		"Your final reply is the deliverable: report the outcome plainly and completely, including any failures."
	unattendedContractNoReply = " If there is nothing worth reporting, reply with exactly NO_REPLY and nothing else."
	unattendedContractPlain   = ` If there is nothing worth reporting, say so plainly (for example: "Nothing to report.").`
)

// TestRun_SchedulerProfileComposition covers the trimmed execution profile
// (integrate-scheduler D6, spec scenario "Trimmed composition"): with all six
// documents and workspace shared memory present, the scheduler-origin
// instruction carries AGENTS/IDENTITY/SOUL plus the workspace metadata doc,
// omits USER.md, BOOTSTRAP.md, the shared-memory subsection, and channel
// docs, and ends with the unattended contract in its NO_REPLY variant.
func TestRun_SchedulerProfileComposition(t *testing.T) {
	st, runner, rec, _, req := setupSchedulerRunner(t, schedulerSessionID("sch-1", time.Unix(1760000000, 0)))
	ctx := context.Background()

	seedPromptDocs(t, runner, nil)
	if err := st.Memories().UpsertWorkspaceMemory(ctx, req.WorkspaceID, "WORKSPACE-SHARED-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := st.Memories().UpsertUserMemory(ctx, req.WorkspaceID, req.UserID, "USER-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}

	req.Origin = OriginScheduler
	req.SchedulerNoReply = "NO_REPLY"
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (scheduler): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the scheduler run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if !params.SchedulerProfile {
		t.Fatal("composeAgent must set SchedulerProfile for a scheduler-origin run")
	}
	if params.NoReplyToken != "NO_REPLY" {
		t.Fatalf("NoReplyToken = %q, want the request's SchedulerNoReply", params.NoReplyToken)
	}

	for _, marker := range []string{
		"AGENTS-CONTENT-MARKER",
		"IDENTITY-CONTENT-MARKER",
		"SOUL-CONTENT-MARKER",
	} {
		if !strings.Contains(instruction, marker) {
			t.Fatalf("scheduler instruction must contain %s, got:\n%s", marker, instruction)
		}
	}
	if !strings.Contains(instruction, "# Workspace") || !strings.Contains(instruction, "acme") {
		t.Fatalf("scheduler instruction must carry the workspace metadata doc, got:\n%s", instruction)
	}
	for _, absent := range []string{
		"BOOTSTRAP-CONTENT-MARKER",
		"## Shared memory",
		"WORKSPACE-SHARED-MEMORY-MARKER",
		"# Current User",
		"USER-MEMORY-MARKER",
		"# Channel",
	} {
		if strings.Contains(instruction, absent) {
			t.Fatalf("scheduler instruction must omit %q, got:\n%s", absent, instruction)
		}
	}
	if want := unattendedContractHeader + unattendedContractNoReply; !strings.HasSuffix(instruction, want) {
		t.Fatalf("scheduler instruction must end with the unattended contract (NO_REPLY variant), got:\n%s", instruction)
	}
}

// TestRun_SchedulerProfileThreadContract pins the thread-target wording
// variant: with no suppression token the contract asks for a plain
// "Nothing to report." instead of the literal NO_REPLY.
func TestRun_SchedulerProfileThreadContract(t *testing.T) {
	_, runner, rec, _, req := setupSchedulerRunner(t, schedulerSessionID("sch-2", time.Unix(1760000001, 0)))
	ctx := context.Background()

	seedPromptDocs(t, runner, nil)

	req.Origin = OriginScheduler
	req.SchedulerNoReply = "" // thread target: the transcript is the product
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (scheduler, thread target): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the scheduler run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if !params.SchedulerProfile || params.NoReplyToken != "" {
		t.Fatalf("compose params = %+v, want SchedulerProfile with an empty NoReplyToken", params)
	}
	if want := unattendedContractHeader + unattendedContractPlain; !strings.HasSuffix(instruction, want) {
		t.Fatalf("scheduler instruction must end with the unattended contract (plain variant), got:\n%s", instruction)
	}
	if strings.Contains(instruction, "NO_REPLY") {
		t.Fatalf("thread-target contract must not teach the suppression token, got:\n%s", instruction)
	}
}

// TestRun_InteractiveCompositionUnchanged pins the other side of the origin
// branch: a user-origin run for the same agent composes the full ordinary
// stack — USER.md, BOOTSTRAP.md, both memory subsections — and never the
// unattended contract (integrate-scheduler D6, "Interactive runs are
// unchanged").
func TestRun_InteractiveCompositionUnchanged(t *testing.T) {
	st, runner, rec, _, req := setupSchedulerRunner(t, "sess-interactive-profile")
	ctx := context.Background()

	seedPromptDocs(t, runner, nil)
	if err := st.Memories().UpsertWorkspaceMemory(ctx, req.WorkspaceID, "WORKSPACE-SHARED-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := st.Memories().UpsertUserMemory(ctx, req.WorkspaceID, req.UserID, "USER-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (user): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if params.SchedulerProfile {
		t.Fatal("a user-origin run must not select the scheduler profile")
	}
	for _, marker := range []string{
		"AGENTS-CONTENT-MARKER",
		"IDENTITY-CONTENT-MARKER",
		"SOUL-CONTENT-MARKER",
		"BOOTSTRAP-CONTENT-MARKER",
		"# Current User",
		"USER-MEMORY-MARKER",
		"## Shared memory",
		"WORKSPACE-SHARED-MEMORY-MARKER",
	} {
		if !strings.Contains(instruction, marker) {
			t.Fatalf("interactive instruction must contain %s, got:\n%s", marker, instruction)
		}
	}
	if strings.Contains(instruction, "Unattended run") {
		t.Fatalf("interactive instruction must never carry the unattended contract, got:\n%s", instruction)
	}
}

// TestRun_SchedulerProfileSkipsChannelDocs exercises the composeAgent origin
// guard with a deliberately malformed request: a scheduler-origin run
// carrying ChannelID must compose the unattended profile, never the channel
// context docs (integrate-scheduler D6).
func TestRun_SchedulerProfileSkipsChannelDocs(t *testing.T) {
	_, runner, rec, _, req := setupSchedulerRunner(t, schedulerSessionID("sch-3", time.Unix(1760000002, 0)),
		WithChannelContext(&fakeChannelContext{
			channel: domain.Channel{ID: "ch-1", Name: "Incidents", Slug: "incidents"},
		}),
		WithChannelFeed(&fakeChannelFeed{}),
	)
	ctx := context.Background()

	seedPromptDocs(t, runner, nil)

	req.Origin = OriginScheduler
	req.ChannelID = "ch-1"
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (scheduler with malformed ChannelID): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the scheduler run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if !params.SchedulerProfile {
		t.Fatal("the malformed request must still select the scheduler profile")
	}
	if len(params.ChannelDocs) != 0 {
		t.Fatalf("channel docs must never compose for a scheduler run, got %+v", params.ChannelDocs)
	}
	if strings.Contains(instruction, "# Channel") {
		t.Fatalf("scheduler instruction must omit channel docs, got:\n%s", instruction)
	}
	if want := unattendedContractHeader + unattendedContractPlain; !strings.HasSuffix(instruction, want) {
		t.Fatalf("scheduler instruction must end with the unattended contract, got:\n%s", instruction)
	}
}

// TestRun_SchedulerSessionNotIndexedAndCheckpointFree covers D7 and the
// checkpoint skip (task 3.2): a persistent scheduler-origin run persists its
// transcript events but never writes the agent_sessions index and never
// arms the checkpoint machinery; an interactive run on the same runner still
// does both.
func TestRun_SchedulerSessionNotIndexedAndCheckpointFree(t *testing.T) {
	_, runner, _, cps, req := setupSchedulerRunner(t, schedulerSessionID("sch-1", time.Unix(1760000000, 0)))
	ctx := context.Background()

	req.Origin = OriginScheduler
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (scheduler): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the scheduler run to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err := runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("scheduler run must never write the agent_sessions index, got %d rows", len(rows))
	}

	// Run sessions are artifacts (D7): the transcript itself hydrates like any
	// session's — events without an index row.
	hist, err := runner.History(ctx, HistoryRequest{WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if !hasKind(hist.Events, TranscriptEventTurnCompleted) {
		t.Fatalf("scheduler transcript must persist and complete, got %+v", hist.Events)
	}

	// Checkpoint skip (task 3.2): the run never arms a checkpoint ID, so the
	// spy saw neither a write nor the finalize-time delete.
	if sets, deletes := cps.counts(); sets != 0 || deletes != 0 {
		t.Fatalf("scheduler run checkpoint activity = (%d sets, %d deletes), want (0, 0)", sets, deletes)
	}

	// The interactive control: same runner, user origin — indexes its session
	// and keeps the checkpoint machinery (the finalize-time delete proves the
	// checkpoint ID was armed).
	ireq := req
	ireq.Origin = ""
	ireq.SessionID = "sess-interactive"
	istream, err := runner.Run(ctx, ireq)
	if err != nil {
		t.Fatalf("Run (interactive): %v", err)
	}
	if ev := collectStream(t, istream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the interactive run to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(ireq))

	rows, err = runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions after interactive run: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionID != "sess-interactive" {
		t.Fatalf("interactive run must index its session, got %+v", rows)
	}
	if _, deletes := cps.counts(); deletes == 0 {
		t.Fatal("interactive run must keep the checkpoint machinery (finalize delete never fired)")
	}
}
