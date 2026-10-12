package server_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// -----------------------------------------------------------------------------
// The standalone webhook runtime's runner adapter (add-workspace-webhooks D6):
// the composition seam that turns a verified delivery into an
// OriginService agent run carrying the endpoint's attribution. Fake run
// seam and poster — no live runner, no network.
// -----------------------------------------------------------------------------

// fakeExecRunner records the ExecRequests the submitter builds and hands
// back a stream pre-loaded with the final assistant text (the drain path's
// input). failRun streams a terminal error event instead, exercising the
// failed-status derivation.
type fakeExecRunner struct {
	mu      sync.Mutex
	reqs    []agents.ExecRequest
	failRun bool
}

func (r *fakeExecRunner) Run(_ context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	stream := agents.NewEventStream(8)
	if r.failRun {
		stream.Send(&agents.TranscriptEvent{
			Kind:  agents.TranscriptEventError,
			Error: "provider exploded",
		})
	} else {
		stream.Send(&agents.TranscriptEvent{
			Kind: agents.TranscriptEventMessageCompleted,
			Message: &agents.CompletedMessage{
				Role:    "assistant",
				Content: "the run's final text",
			},
		})
	}
	stream.Close()

	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return stream, nil
}

func (r *fakeExecRunner) last(t *testing.T) agents.ExecRequest {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.reqs)
		var req agents.ExecRequest
		if n > 0 {
			req = r.reqs[n-1]
		}
		r.mu.Unlock()
		if n > 0 {
			return req
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the submitted ExecRequest")
	return agents.ExecRequest{}
}

// fakeChannelPoster records the chokepoint posts the drain performs.
type fakeChannelPoster struct {
	mu    sync.Mutex
	posts []string
}

func (p *fakeChannelPoster) PostFromAgent(_ context.Context, _, _, _, body string) (domain.ChannelMessage, error) {
	p.mu.Lock()
	p.posts = append(p.posts, body)
	p.mu.Unlock()
	return domain.ChannelMessage{}, nil
}

func (p *fakeChannelPoster) last(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		n := len(p.posts)
		var body string
		if n > 0 {
			body = p.posts[n-1]
		}
		p.mu.Unlock()
		if n > 0 {
			return body
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the channel post")
	return ""
}

// runtimeFixture seeds a workspace (owner + optional creator member, bound
// agent) and the standalone runtime over the fakes.
type runtimeFixture struct {
	st      store.Store
	ws      *domain.Workspace
	agent   *domain.Agent
	ownerID string
	secret  string
	rt      *server.WorkspaceWebhookRuntime
	runner  *fakeExecRunner
	poster  *fakeChannelPoster
}

var runtimeTestKey = []byte("01234567890123456789012345678901")

// newRuntimeFixture seeds the store; withCreator also mints a creator user
// (a plain Member) the bound agent carries as CreatedBy — CreatedBy is
// immutable after create, so the fixture sets it at creation time. The
// returned creatorID is "" without withCreator.
func newRuntimeFixture(t *testing.T, withCreator bool) (*runtimeFixture, string) {
	t.Helper()
	st := storefake.New()
	ctx := context.Background()

	owner := &domain.User{Email: "rt-owner@example.com", Name: "Owner"}
	if err := st.Users().Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	ws := &domain.Workspace{Slug: "rt", Name: "WS rt"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner member: %v", err)
	}

	creatorID := ""
	var createdBy *string
	if withCreator {
		creator := &domain.User{Email: "rt-creator@example.com", Name: "Creator"}
		if err := st.Users().Create(ctx, creator); err != nil {
			t.Fatalf("create creator: %v", err)
		}
		memberRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, Permissions: domain.MemberPermissions, BuiltIn: true}
		if err := st.Roles().Create(ctx, memberRole); err != nil {
			t.Fatalf("create member role: %v", err)
		}
		if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: creator.ID, RoleID: memberRole.ID}); err != nil {
			t.Fatalf("add creator member: %v", err)
		}
		creatorID = creator.ID
		createdBy = &creatorID
	}

	agent := &domain.Agent{WorkspaceID: ws.ID, Name: "Atlas", Slug: "atlas", CreatedBy: createdBy}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	f := &runtimeFixture{st: st, ws: ws, agent: agent, ownerID: owner.ID}
	f.runner = &fakeExecRunner{}
	f.poster = &fakeChannelPoster{}
	f.rt = server.NewWorkspaceWebhookRuntime(
		st.WorkspaceWebhooks(),
		st.Connections(),
		webhooks.NewService(st.Connections(), st.ConnectionWebhooks(), st.Agents(), st.Channels(), webhooks.NewAESGCMCipher(runtimeTestKey)),
		st.Agents(),
		st.Members(),
		st.Roles(),
		f.runner,
		f.poster,
		runtimeTestKey,
	)
	t.Cleanup(f.rt.Queue.Close)
	return f, creatorID
}

// seedEndpoint stores an enabled channel-bound hmac endpoint.
func (f *runtimeFixture) seedEndpoint(t *testing.T) *domain.WorkspaceWebhook {
	t.Helper()
	cipher := webhooks.NewAESGCMCipher(runtimeTestKey)
	secret, err := webhooks.GenerateSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	envelope, err := cipher.EncryptSecret(f.ws.ID, []byte(secret))
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	wh := &domain.WorkspaceWebhook{
		WorkspaceID:      f.ws.ID,
		Name:             "gitlab-mr-review",
		Scheme:           domain.WorkspaceWebhookSchemeHmacSha256,
		SecretCiphertext: envelope,
		SecretHint:       webhooks.SecretHint(secret),
		EventSource: domain.WorkspaceWebhookEventSource{
			Kind: domain.WorkspaceWebhookEventSourceHeader,
			Name: "X-Gitlab-Event",
		},
		DeliveryIDSource: "X-Gitlab-Event-UUID",
		TargetAgentID:    f.agent.ID,
		TargetKind:       domain.ConnectionWebhookTargetChannel,
		TargetID:         "chan-incidents",
		Enabled:          true,
	}
	if err := f.st.WorkspaceWebhooks().Create(context.Background(), wh); err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	f.secret = secret
	return wh
}

// deliver posts one signed delivery through the runtime's ingress.
func (f *runtimeFixture) deliver(t *testing.T, wh *domain.WorkspaceWebhook, event, deliveryID, payload string) webhooks.StandaloneOutcome {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(f.secret))
	mac.Write([]byte(payload))
	header := http.Header{}
	header.Set("X-Hub-Signature-256", hex.EncodeToString(mac.Sum(nil)))
	header.Set("X-Gitlab-Event", event)
	header.Set("X-Gitlab-Event-UUID", deliveryID)
	outcome, err := f.rt.Ingress.Deliver(context.Background(), webhooks.StandaloneDeliverInput{
		WorkspaceID: f.ws.ID,
		WebhookID:   wh.ID,
		Header:      header,
		Body:        []byte(payload),
	})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	return outcome
}

// waitSubmitted waits for the endpoint's submitted delivery-log row.
func (f *runtimeFixture) waitSubmitted(t *testing.T, webhookID string) domain.WebhookDeliveryLogEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := f.st.WorkspaceWebhooks().ListDeliveryLog(context.Background(), f.ws.ID, webhookID, 10)
		if err != nil {
			t.Fatalf("list delivery log: %v", err)
		}
		for _, e := range entries {
			if e.Outcome == domain.WorkspaceWebhookDeliveryOutcomeSubmitted {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the submitted delivery-log row")
	return domain.WebhookDeliveryLogEntry{}
}

// waitFinished waits for the submitted delivery-log row to carry a terminal
// run status — the drain's telemetry finish (design.md D5).
func (f *runtimeFixture) waitFinished(t *testing.T, webhookID string) domain.WebhookDeliveryLogEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := f.st.WorkspaceWebhooks().ListDeliveryLog(context.Background(), f.ws.ID, webhookID, 10)
		if err != nil {
			t.Fatalf("list delivery log: %v", err)
		}
		for _, e := range entries {
			if e.Outcome == domain.WorkspaceWebhookDeliveryOutcomeSubmitted && e.Status != "" && e.Status != domain.WorkspaceWebhookRunStatusRunning {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the run telemetry finish")
	return domain.WebhookDeliveryLogEntry{}
}

// TestWorkspaceWebhookRuntime_ServiceAttribution is the spec scenario: the
// run's request names the endpoint and resolved event instead of a requesting
// user — Origin service, WebhookID/WebhookName stamped, acting identity the
// bound agent's live creator, the channel target drained through the
// chokepoint.
func TestWorkspaceWebhookRuntime_ServiceAttribution(t *testing.T) {
	f, _ := newRuntimeFixture(t, false)
	wh := f.seedEndpoint(t)

	if outcome := f.deliver(t, wh, "merge_request", "d-attr-1", `{"object_kind":"merge_request"}`); outcome != webhooks.StandaloneOutcomeSubmitted {
		t.Fatalf("expected submitted, got %v", outcome)
	}

	req := f.runner.last(t)
	if req.Origin != "service" {
		t.Errorf("origin = %q, want the service-authority origin", req.Origin)
	}
	if req.WebhookID != wh.ID {
		t.Errorf("webhook id = %q, want the endpoint id", req.WebhookID)
	}
	if req.WebhookName != "gitlab-mr-review" {
		t.Errorf("webhook name = %q, want the endpoint name", req.WebhookName)
	}
	if req.Event != "merge_request" {
		t.Errorf("event = %q, want the resolved event", req.Event)
	}
	if req.WorkspaceID != f.ws.ID || req.AgentID != f.agent.ID {
		t.Errorf("workspace/agent wrong: %+v", req)
	}
	if req.UserID != f.ownerID {
		t.Errorf("acting user = %q, want the workspace owner fallback %q", req.UserID, f.ownerID)
	}
	if req.SessionID == "" || req.SessionID == wh.TargetID {
		t.Error("a channel-target run needs its own per-delivery session")
	}
	// The connection lane's fields stay empty on the standalone lane.
	if req.ConnectionID != "" || req.ConnectionService != "" {
		t.Errorf("connection attribution must stay empty, got %q/%q", req.ConnectionID, req.ConnectionService)
	}
	if !strings.Contains(req.Input, "Webhook event merge_request from the gitlab-mr-review webhook endpoint.") {
		t.Errorf("the turn input must name the endpoint and event:\n%s", req.Input)
	}

	// The channel target drains the final text through the poster.
	if body := f.poster.last(t); body != "the run's final text" {
		t.Errorf("channel post body = %q", body)
	}

	// The submitted delivery-log row links the run via the session the
	// submitter returned (design.md D8).
	entry := f.waitSubmitted(t, wh.ID)
	if entry.RunID != req.SessionID {
		t.Errorf("submitted row run id = %q, want the run's session %q", entry.RunID, req.SessionID)
	}
}

// TestWorkspaceWebhookRuntime_ActingUserPrefersCreator: the bound agent's
// live creator acts — not the workspace owner — the creator-preferred
// resolution the scheduler and the connection lane share.
func TestWorkspaceWebhookRuntime_ActingUserPrefersCreator(t *testing.T) {
	f, creatorID := newRuntimeFixture(t, true)
	wh := f.seedEndpoint(t)

	if outcome := f.deliver(t, wh, "push", "d-creator-1", `{"object_kind":"push"}`); outcome != webhooks.StandaloneOutcomeSubmitted {
		t.Fatalf("expected submitted, got %v", outcome)
	}
	req := f.runner.last(t)
	if req.UserID != creatorID {
		t.Errorf("acting user = %q, want the bound agent's creator %q", req.UserID, creatorID)
	}
}

// TestWorkspaceWebhookRuntime_RunTelemetry: the drain finishes the
// submitted row's run telemetry for every target (design.md D5) — the tap
// ends without a terminal event here, so success-by-default completes the
// run — and the workspace-wide run listing projects it with the endpoint
// and bound agent (4.4).
func TestWorkspaceWebhookRuntime_RunTelemetry(t *testing.T) {
	f, _ := newRuntimeFixture(t, false)
	wh := f.seedEndpoint(t)

	if outcome := f.deliver(t, wh, "merge_request", "d-telemetry-1", `{"object_kind":"merge_request"}`); outcome != webhooks.StandaloneOutcomeSubmitted {
		t.Fatalf("expected submitted, got %v", outcome)
	}
	req := f.runner.last(t)
	_ = f.poster.last(t) // the channel post rides the same drain

	entry := f.waitFinished(t, wh.ID)
	if entry.RunID != req.SessionID {
		t.Fatalf("finished row run id = %q, want %q", entry.RunID, req.SessionID)
	}
	if entry.Status != domain.WorkspaceWebhookRunStatusCompleted {
		t.Errorf("status = %q, want completed (success-by-default)", entry.Status)
	}

	runs, err := f.st.WorkspaceWebhooks().ListRuns(context.Background(), f.ws.ID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want the one submitted delivery", len(runs))
	}
	run := runs[0]
	if run.WebhookName != "gitlab-mr-review" || run.Event != "merge_request" {
		t.Errorf("run projection = %s/%s, want the endpoint name and event", run.WebhookName, run.Event)
	}
	if run.RunID != req.SessionID || run.AgentID != f.agent.ID || run.AgentName != f.agent.Name {
		t.Errorf("run linkage = %s/%s/%s, want session, agent id and name", run.RunID, run.AgentID, run.AgentName)
	}
	if run.Status != domain.WorkspaceWebhookRunStatusCompleted {
		t.Errorf("run status = %q, want completed", run.Status)
	}

	// A rejected delivery is not a run: it never appears in the feed.
	header := http.Header{}
	header.Set("X-Gitlab-Event", "merge_request")
	header.Set("X-Gitlab-Event-UUID", "d-telemetry-2")
	outcome, err := f.rt.Ingress.Deliver(context.Background(), webhooks.StandaloneDeliverInput{
		WorkspaceID: f.ws.ID, WebhookID: wh.ID, Header: header, Body: []byte(`{}`),
	})
	if err == nil && outcome != webhooks.StandaloneOutcomeRejected {
		t.Fatalf("unsigned delivery outcome = %v, want rejected", outcome)
	}
	runs, err = f.st.WorkspaceWebhooks().ListRuns(context.Background(), f.ws.ID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("runs = %d after the rejection, want still 1", len(runs))
	}
}

// TestWorkspaceWebhookRuntime_RunTelemetryFailed: a terminal error event
// finishes the run failed — the failed status the run-history screen shows.
func TestWorkspaceWebhookRuntime_RunTelemetryFailed(t *testing.T) {
	f, _ := newRuntimeFixture(t, false)
	f.runner.failRun = true
	wh := f.seedEndpoint(t)

	if outcome := f.deliver(t, wh, "push", "d-telemetry-3", `{"object_kind":"push"}`); outcome != webhooks.StandaloneOutcomeSubmitted {
		t.Fatalf("expected submitted, got %v", outcome)
	}
	entry := f.waitFinished(t, wh.ID)
	if entry.Status != domain.WorkspaceWebhookRunStatusFailed {
		t.Errorf("status = %q, want failed", entry.Status)
	}
	// A failed channel run delivers no final text.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		f.poster.mu.Lock()
		n := len(f.poster.posts)
		f.poster.mu.Unlock()
		if n > 0 {
			t.Fatalf("a failed run must not post to the channel")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
