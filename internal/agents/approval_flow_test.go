package agents

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// stubComposer returns a fixed instruction so composition doesn't depend on
// prompt files on disk.
type stubComposer struct{}

func (stubComposer) Compose(_ context.Context, _ ComposeParams) (string, error) {
	return "test instruction", nil
}

// collectStream drains an EventStream until closed and returns the events.
func collectStream(t *testing.T, stream *EventStream) []TranscriptEvent {
	t.Helper()
	var out []TranscriptEvent
	for {
		ev, err := stream.Recv()
		if err != nil {
			return out
		}
		out = append(out, *ev)
	}
}

func newScriptedToolCallModel() *scriptedToolCallModel {
	return &scriptedToolCallModel{}
}

func hasKind(events []TranscriptEvent, kind TranscriptEventKind) bool {
	for _, e := range events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// setupApprovalRunner seeds a workspace/agent (shell allow-listed) and a
// runner wired to the scripted tool-call model. Each test supplies the
// dangerous command via newScriptedModel.
func setupApprovalRunner(t *testing.T, sessionID string) (store.Store, *Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	const agSlug = "atlas"
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
	if err := st.Members().Add(ctx, &domain.Member{
		WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID,
	}); err != nil {
		t.Fatalf("create member: %v", err)
	}

	model := newScriptedToolCallModel()
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return model, nil
		}),
		WithInstructionComposer(stubComposer{}),
	)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
		Autonomy:    domain.AutonomyApproval,
		Tools:       []string{ReservedShellTool},
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Seed the agent's on-disk workspace directory (the jail root).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, agSlug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   sessionID,
		UserID:      user.ID,
		Input:       "clean up the build directory",
	}
	return st, runner, ws, ag, req
}

func TestApprovalFlow_InterruptThenApprove(t *testing.T) {
	_, runner, ws, ag, req := setupApprovalRunner(t, "sess-approve")

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	if !hasKind(events, TranscriptEventApprovalRequired) {
		t.Fatalf("expected approval_required event, got %+v", events)
	}
	if hasKind(events, TranscriptEventTurnCompleted) || hasKind(events, TranscriptEventError) || hasKind(events, TranscriptEventCancelled) {
		t.Fatal("no terminal event should follow an approval interrupt")
	}
	var approval *ApprovalPayload
	for i := range events {
		if events[i].Approval != nil {
			approval = events[i].Approval
		}
	}
	if approval == nil || approval.InterruptID == "" || approval.Command == "" {
		t.Fatalf("approval payload incomplete: %+v", approval)
	}

	// Pending state is derivable from history.
	pending, err := runner.PendingApproval(context.Background(), ws.ID, req.SessionID)
	if err != nil {
		t.Fatalf("PendingApproval: %v", err)
	}
	if pending == nil || pending.InterruptID != approval.InterruptID {
		t.Fatalf("pending approval = %+v, want interrupt %q", pending, approval.InterruptID)
	}

	// Approve: the command executes and the turn completes.
	resumeStream, err := runner.Resume(context.Background(), req, approval, true)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumeEvents := collectStream(t, resumeStream)
	if !hasKind(resumeEvents, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed after approval, got %+v", resumeEvents)
	}
	if hasKind(resumeEvents, TranscriptEventApprovalRequired) {
		t.Fatal("approved command must not re-interrupt")
	}

	// After resolution, no approval is pending.
	pending, err = runner.PendingApproval(context.Background(), ws.ID, req.SessionID)
	if err != nil {
		t.Fatalf("PendingApproval after resume: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected no pending approval after resume, got %+v", pending)
	}
	_ = ag
}

func TestApprovalFlow_Deny(t *testing.T) {
	_, runner, ws, _, req := setupApprovalRunner(t, "sess-deny")

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	var approval *ApprovalPayload
	for i := range events {
		if events[i].Approval != nil {
			approval = events[i].Approval
		}
	}
	if approval == nil {
		t.Fatal("expected approval interrupt")
	}

	resumeStream, err := runner.Resume(context.Background(), req, approval, false)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumeEvents := collectStream(t, resumeStream)
	if !hasKind(resumeEvents, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed after denial, got %+v", resumeEvents)
	}
	pending, err := runner.PendingApproval(context.Background(), ws.ID, req.SessionID)
	if err != nil {
		t.Fatalf("PendingApproval: %v", err)
	}
	if pending != nil {
		t.Fatalf("denial should resolve the pending approval, got %+v", pending)
	}
}

func TestApprovalFlow_ResumeSurvivesRunnerRestart(t *testing.T) {
	st, runner, ws, ag, req := setupApprovalRunner(t, "sess-restart")

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	var approval *ApprovalPayload
	for i := range events {
		if events[i].Approval != nil {
			approval = events[i].Approval
		}
	}
	if approval == nil {
		t.Fatal("expected approval interrupt")
	}

	// A fresh Runner over the same durable store resumes the persisted
	// checkpoint — checkpoints survive process restarts.
	restarted := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		[]byte("test-key-32-bytes-long-12345678"),
		runner.onClawDir,
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return newScriptedToolCallModel(), nil
		}),
		WithInstructionComposer(stubComposer{}),
	)

	resumeStream, err := restarted.Resume(context.Background(), req, approval, true)
	if err != nil {
		t.Fatalf("Resume after restart: %v", err)
	}
	resumeEvents := collectStream(t, resumeStream)
	for _, e := range resumeEvents {
		if e.Approval != nil {
			t.Logf("re-interrupted: target=%q new=%q", approval.InterruptID, e.Approval.InterruptID)
		}
	}
	if !hasKind(resumeEvents, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed after restart resume, got %+v", resumeEvents)
	}
	_ = ws
	_ = ag
	_ = time.Now
}
