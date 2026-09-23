package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// heartbeatSessionID documents the session id shape the heartbeat service
// mints (add-agent-heartbeat D2): every tick of one heartbeat appends to the
// same persistent shared session hb_<agentID> — continuity is the point, and
// the runner's session-index skip keys on the heartbeat origin, not the id.
func heartbeatSessionID(agentID string) string {
	return "hb_" + agentID
}

// heartbeatContractPinned pins the closing silence contract word-for-word
// (add-agent-heartbeat D7, spec agent-heartbeat "Silence contract"): the
// entire final reply must be the NO_REPLY token or the report goes to a human.
const heartbeatContractPinned = "## Heartbeat\n\nThis run is an unattended periodic self-check — nobody is waiting for a report, and silence is the expected outcome. If nothing needs attention, your ENTIRE final reply must be exactly NO_REPLY (capitalization does not matter) and nothing else. Any other reply is delivered to a human: keep reports short and actionable."

// TestRun_HeartbeatProfileComposition covers the heartbeat execution profile
// (add-agent-heartbeat D9, spec agent-runtime "Heartbeat composition"): with
// all six documents and workspace shared memory present, the heartbeat-origin
// instruction carries AGENTS/IDENTITY/SOUL, the workspace metadata doc, the
// HEARTBEAT checklist, and the activity digest, and omits USER.md,
// BOOTSTRAP.md, the shared-memory subsection, and channel docs.
func TestRun_HeartbeatProfileComposition(t *testing.T) {
	st, runner, rec, _, req := setupSchedulerRunner(t, "sess-hb-placeholder")
	ctx := context.Background()

	req.SessionID = heartbeatSessionID(req.AgentID)

	seedPromptDocs(t, runner, nil)
	if err := st.Memories().UpsertWorkspaceMemory(ctx, req.WorkspaceID, "WORKSPACE-SHARED-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := st.Memories().UpsertUserMemory(ctx, req.WorkspaceID, req.UserID, "USER-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}

	req.Origin = OriginHeartbeat
	req.HeartbeatChecklist = "HEARTBEAT-CHECKLIST-MARKER\n- check the disk\n- greet new members"
	req.HeartbeatDigest = "DIGEST-BODY-MARKER\n- #incidents: disk alarm cleared"
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (heartbeat): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the heartbeat run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if !params.HeartbeatProfile {
		t.Fatal("composeAgent must set HeartbeatProfile for a heartbeat-origin run")
	}
	if params.SchedulerProfile {
		t.Fatal("a heartbeat-origin run must not select the scheduler profile")
	}
	if params.HeartbeatChecklist != req.HeartbeatChecklist || params.HeartbeatDigest != req.HeartbeatDigest {
		t.Fatalf("compose params = %+v, want the request's checklist and digest passed through", params)
	}

	for _, marker := range []string{
		promptdocs.BasePrompt,
		"## Rich cards",
		"IDENTITY-CONTENT-MARKER",
		"SOUL-CONTENT-MARKER",
		"# Workspace",
		"acme",
		"## HEARTBEAT checklist",
		"HEARTBEAT-CHECKLIST-MARKER",
		"check the disk",
		"## Workspace activity",
		"DIGEST-BODY-MARKER",
	} {
		if !strings.Contains(instruction, marker) {
			t.Fatalf("heartbeat instruction must contain the base prompt and %s, got:\n%s", marker, instruction)
		}
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
			t.Fatalf("heartbeat instruction must omit %q, got:\n%s", absent, instruction)
		}
	}
	if !strings.HasSuffix(instruction, heartbeatContractPinned) {
		t.Fatalf("heartbeat instruction must end with the silence contract, got:\n%s", instruction)
	}
}

// TestRun_HeartbeatProfileEmptyDigestAndChecklist pins the graceful empty
// cases (add-agent-heartbeat D3/D9): an empty digest renders the
// no-recent-activity fallback wording, and a whitespace-only checklist renders
// the explicit empty-checklist placeholder — neither section ever renders as
// bare whitespace.
func TestRun_HeartbeatProfileEmptyDigestAndChecklist(t *testing.T) {
	_, runner, rec, _, req := setupSchedulerRunner(t, "sess-hb-empty")
	ctx := context.Background()

	req.SessionID = heartbeatSessionID(req.AgentID)
	seedPromptDocs(t, runner, nil)

	req.Origin = OriginHeartbeat
	req.HeartbeatChecklist = "   \n  "
	req.HeartbeatDigest = ""
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (heartbeat, empty checklist and digest): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the heartbeat run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if !params.HeartbeatProfile {
		t.Fatal("composeAgent must set HeartbeatProfile for a heartbeat-origin run")
	}
	if !strings.Contains(instruction, "## HEARTBEAT checklist") {
		t.Fatalf("the checklist section must render even when empty, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, "(The checklist is empty. This tick has nothing specific to check — report only on anything that clearly needs attention, or stay silent.)") {
		t.Fatalf("empty checklist must render the placeholder, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, "## Workspace activity") {
		t.Fatalf("the digest section must render even when empty, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, "No recent workspace activity.") {
		t.Fatalf("empty digest must render the no-activity fallback, got:\n%s", instruction)
	}
	if !strings.HasSuffix(instruction, heartbeatContractPinned) {
		t.Fatalf("heartbeat instruction must still end with the silence contract, got:\n%s", instruction)
	}
}

// TestRun_HeartbeatKeepsCheckpointsAndOutOfIndex covers the two runner-side
// session branches (add-agent-heartbeat D2, spec "Session index stays
// human-only"): a persistent heartbeat-origin run keeps the checkpoint
// machinery armed for cross-tick continuity — the opposite of the scheduler
// skip — while never writing the agent_sessions index; an interactive run on
// the same runner still indexes its session.
func TestRun_HeartbeatKeepsCheckpointsAndOutOfIndex(t *testing.T) {
	_, runner, _, cps, req := setupSchedulerRunner(t, "sess-hb-placeholder")
	ctx := context.Background()

	req.SessionID = heartbeatSessionID(req.AgentID)
	req.Origin = OriginHeartbeat
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (heartbeat): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the heartbeat run to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err := runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("heartbeat run must never write the agent_sessions index, got %d rows", len(rows))
	}

	// The transcript itself hydrates like any session's — events without an
	// index row.
	hist, err := runner.History(ctx, HistoryRequest{WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if !hasKind(hist.Events, TranscriptEventTurnCompleted) {
		t.Fatalf("heartbeat transcript must persist and complete, got %+v", hist.Events)
	}

	// Checkpoint continuity (D2): the shared hb_ session stays resumable
	// across ticks, so the checkpoint machinery must have been exercised —
	// the finalize-time delete proves the checkpoint ID was armed.
	if _, deletes := cps.counts(); deletes == 0 {
		t.Fatal("heartbeat run must keep the checkpoint machinery (finalize delete never fired)")
	}

	// The interactive control: same runner, user origin — indexes its session.
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
}

// TestResolve_HeartbeatToolStrip covers the runner-level strip for heartbeat
// origin (add-agent-heartbeat D10): a heartbeat tick resolves neither the
// schedule tool nor the memory tools even when the agent allowlists them and
// the registry registers them, while ordinary allowed tools survive. The
// scheduler control pins the shared branch, the user control the unchanged
// ordinary surface.
func TestResolve_HeartbeatToolStrip(t *testing.T) {
	st := fake.New()
	reg := NewDefaultToolRegistry(st.Memories(), WithSchedulerTools(st.Schedulers(), st.Channels()))
	_, runner, _, _, req := setupSchedulerRunner(t, "sess-toolstrip-hb", WithToolRegistry(reg))
	ctx := context.Background()

	ws, err := runner.workspaces.ByID(ctx, req.WorkspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	agent, err := runner.agents.ByID(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	agent.Tools = []string{tools.NameSchedule, tools.NameMemory, tools.NameDeleteFile, tools.NameWebFetch}

	namesOf := func(origin string) map[string]bool {
		t.Helper()
		r := req
		r.Origin = origin
		_, resolved, err := runner.resolve(ctx, r, ws, agent, nil)
		if err != nil {
			t.Fatalf("resolve (%s): %v", origin, err)
		}
		names := make(map[string]bool, len(resolved))
		for _, tl := range resolved {
			info, err := tl.Info(ctx)
			if err != nil {
				t.Fatalf("tool info: %v", err)
			}
			names[info.Name] = true
		}
		return names
	}

	// Heartbeat origin: the shared anti-runaway strip removes schedule and the
	// memory tools regardless of the allowlist; the ordinary allowed tool
	// survives.
	hbNames := namesOf(OriginHeartbeat)
	for _, excluded := range []string{tools.NameSchedule, tools.NameMemory, tools.NameDeleteFile} {
		if hbNames[excluded] {
			t.Fatalf("heartbeat run resolved excluded tool %q (resolved: %v)", excluded, hbNames)
		}
	}
	if !hbNames[tools.NameWebFetch] {
		t.Fatalf("heartbeat run must keep the ordinary allowed tool %q (resolved: %v)", tools.NameWebFetch, hbNames)
	}

	// Scheduler control: the same strip applies (scheduler behavior must not
	// have moved with the shared branch).
	schedNames := namesOf(OriginScheduler)
	if schedNames[tools.NameSchedule] || schedNames[tools.NameMemory] || schedNames[tools.NameDeleteFile] {
		t.Fatalf("scheduler run must strip the same set (resolved: %v)", schedNames)
	}
	if !schedNames[tools.NameWebFetch] {
		t.Fatalf("scheduler run must keep the ordinary allowed tool (resolved: %v)", schedNames)
	}

	// User control: the same allowlist resolves everything the registry
	// registered — the strip applies only to the unattended origins.
	userNames := namesOf(OriginUser)
	for _, kept := range []string{tools.NameSchedule, tools.NameMemory, tools.NameDeleteFile, tools.NameWebFetch} {
		if !userNames[kept] {
			t.Fatalf("user-origin run must keep %q (resolved: %v)", kept, userNames)
		}
	}
}

// TestRun_HeartbeatProfileOtherOriginsUnchanged guards the shared-helper
// refactor (spec agent-runtime "Other origins are unchanged"): for the same
// agent with heartbeat checklist and digest fields set on the request,
// scheduler-origin composition is byte-for-byte the old unattended profile and
// user-origin composition the full ordinary stack — neither carries any
// heartbeat section.
func TestRun_HeartbeatProfileOtherOriginsUnchanged(t *testing.T) {
	st, runner, rec, _, req := setupSchedulerRunner(t, "sess-hb-other-placeholder")
	ctx := context.Background()

	seedPromptDocs(t, runner, nil)
	if err := st.Memories().UpsertWorkspaceMemory(ctx, req.WorkspaceID, "WORKSPACE-SHARED-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := st.Memories().UpsertUserMemory(ctx, req.WorkspaceID, req.UserID, "USER-MEMORY-MARKER"); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}

	req.HeartbeatChecklist = "HEARTBEAT-SHOULD-NOT-APPEAR"
	req.HeartbeatDigest = "DIGEST-SHOULD-NOT-APPEAR"

	// Scheduler origin: unchanged trimmed profile, no heartbeat sections.
	sreq := req
	sreq.SessionID = "sess-sched-other"
	sreq.Origin = OriginScheduler
	sreq.SchedulerNoReply = "NO_REPLY"
	stream, err := runner.Run(ctx, sreq)
	if err != nil {
		t.Fatalf("Run (scheduler): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the scheduler run to complete, got %+v", ev)
	}
	sInstruction, sParams := rec.composed()
	if !sParams.SchedulerProfile || sParams.HeartbeatProfile {
		t.Fatalf("scheduler compose params = %+v, want SchedulerProfile only", sParams)
	}
	for _, marker := range []string{
		promptdocs.BasePrompt,
		"IDENTITY-CONTENT-MARKER",
		"SOUL-CONTENT-MARKER",
		"# Workspace",
	} {
		if !strings.Contains(sInstruction, marker) {
			t.Fatalf("scheduler instruction must contain %s, got:\n%s", marker, sInstruction)
		}
	}
	for _, absent := range []string{
		"BOOTSTRAP-CONTENT-MARKER",
		"## Shared memory",
		"# Current User",
		"## HEARTBEAT checklist",
		"## Workspace activity",
		"## Heartbeat",
		"No recent workspace activity.",
		"HEARTBEAT-SHOULD-NOT-APPEAR",
		"DIGEST-SHOULD-NOT-APPEAR",
	} {
		if strings.Contains(sInstruction, absent) {
			t.Fatalf("scheduler instruction must omit %q, got:\n%s", absent, sInstruction)
		}
	}
	if want := unattendedContractHeader + unattendedContractNoReply; !strings.HasSuffix(sInstruction, want) {
		t.Fatalf("scheduler instruction must end with the unattended contract unchanged, got:\n%s", sInstruction)
	}

	// User origin: full ordinary stack, no unattended or heartbeat contract.
	ureq := req
	ureq.SessionID = "sess-user-other"
	ureq.Origin = ""
	ustream, err := runner.Run(ctx, ureq)
	if err != nil {
		t.Fatalf("Run (user): %v", err)
	}
	if ev := collectStream(t, ustream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the user run to complete, got %+v", ev)
	}
	uInstruction, uParams := rec.composed()
	if uParams.SchedulerProfile || uParams.HeartbeatProfile {
		t.Fatalf("user compose params = %+v, want neither unattended profile", uParams)
	}
	for _, marker := range []string{
		promptdocs.BasePrompt,
		"## Rich cards",
		"IDENTITY-CONTENT-MARKER",
		"SOUL-CONTENT-MARKER",
		"BOOTSTRAP-CONTENT-MARKER",
		"# Current User",
		"USER-MEMORY-MARKER",
		"## Shared memory",
		"WORKSPACE-SHARED-MEMORY-MARKER",
	} {
		if !strings.Contains(uInstruction, marker) {
			t.Fatalf("user instruction must contain %s, got:\n%s", marker, uInstruction)
		}
	}
	for _, absent := range []string{
		"Unattended run",
		"## HEARTBEAT checklist",
		"## Workspace activity",
		"## Heartbeat",
		"HEARTBEAT-SHOULD-NOT-APPEAR",
		"DIGEST-SHOULD-NOT-APPEAR",
	} {
		if strings.Contains(uInstruction, absent) {
			t.Fatalf("user instruction must omit %q, got:\n%s", absent, uInstruction)
		}
	}
}

// TestRun_HeartbeatProfileSkipsChannelDocs exercises the composeAgent origin
// guard with a deliberately malformed request: a heartbeat-origin run carrying
// ChannelID must compose the heartbeat profile, never the channel context docs
// (add-agent-heartbeat D9 — the unattended profiles have no room to catch up
// on).
func TestRun_HeartbeatProfileSkipsChannelDocs(t *testing.T) {
	_, runner, rec, _, req := setupSchedulerRunner(t, "sess-hb-channel",
		WithChannelContext(&fakeChannelContext{
			channel: domain.Channel{ID: "ch-1", Name: "Incidents", Slug: "incidents"},
		}),
		WithChannelFeed(&fakeChannelFeed{}),
	)
	ctx := context.Background()

	seedPromptDocs(t, runner, nil)

	req.SessionID = heartbeatSessionID(req.AgentID)
	req.Origin = OriginHeartbeat
	req.ChannelID = "ch-1"
	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (heartbeat with malformed ChannelID): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the heartbeat run to complete, got %+v", ev)
	}

	instruction, params := rec.composed()
	if !params.HeartbeatProfile {
		t.Fatal("the malformed request must still select the heartbeat profile")
	}
	if len(params.ChannelDocs) != 0 {
		t.Fatalf("channel docs must never compose for a heartbeat run, got %+v", params.ChannelDocs)
	}
	if strings.Contains(instruction, "# Channel") {
		t.Fatalf("heartbeat instruction must omit channel docs, got:\n%s", instruction)
	}
	if !strings.HasSuffix(instruction, heartbeatContractPinned) {
		t.Fatalf("heartbeat instruction must end with the silence contract, got:\n%s", instruction)
	}
}
