package webhooks_test

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

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// fakeRunner records the runs the pipeline routes and signals each one.
type fakeRunner struct {
	mu    sync.Mutex
	runs  []webhooks.RunRequest
	err   error
	notif chan struct{}
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{notif: make(chan struct{}, 32)}
}

func (r *fakeRunner) RunEvent(_ context.Context, req webhooks.RunRequest) error {
	r.mu.Lock()
	runs, err := r.runs, r.err
	r.runs = append(r.runs, req)
	r.mu.Unlock()
	select {
	case r.notif <- struct{}{}:
	default:
	}
	_ = runs
	return err
}

func (r *fakeRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func (r *fakeRunner) last() webhooks.RunRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[len(r.runs)-1]
}

// waitRun waits for one more run to arrive.
func (r *fakeRunner) waitRun(t *testing.T) webhooks.RunRequest {
	t.Helper()
	select {
	case <-r.notif:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the routed run")
	}
	return r.last()
}

// deliveryFixture bundles the pieces of one seeded, enabled connection.
type deliveryFixture struct {
	sd     *seed
	runner *fakeRunner
	queue  *webhooks.Queue
	svc    *webhooks.Service
	ing    *webhooks.Ingress
	secret []byte
}

// newDeliveryFixture seeds a workspace, enables webhooks on its github
// connection (channel target), and wires the full pipeline.
func newDeliveryFixture(t *testing.T, slug string, targetKind, targetID string) *deliveryFixture {
	t.Helper()
	sd := seedWorkspace(t, slug)
	svc := sd.newService(t)

	req := webhooks.EnableRequest{AgentID: sd.Agent.ID, Events: []string{"pull_request.opened", "issues.assigned", "push"}}
	switch targetKind {
	case domain.ConnectionWebhookTargetThread:
		req.TargetKind = targetKind
		req.TargetID = targetID
	default:
		req.TargetKind = domain.ConnectionWebhookTargetChannel
		req.TargetID = sd.Channel.ID
	}
	state, secret, err := svc.Enable(context.Background(), sd.WS.ID, sd.Connection.ID, req)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	_ = state

	df := &deliveryFixture{
		sd:     sd,
		runner: newFakeRunner(),
		svc:    svc,
		secret: []byte(secret),
	}
	var ing *webhooks.Ingress
	df.queue = webhooks.NewQueue(16, func(ctx context.Context, job *webhooks.Delivery) {
		ing.ProcessDelivery(ctx, job)
	})
	ing = webhooks.NewIngress(sd.webhooks, sd.connections, webhooks.NewAESGCMCipher(testKey), df.queue, df.runner)
	df.ing = ing
	t.Cleanup(df.queue.Close)
	return df
}

// signedGithub builds a signed GitHub-style delivery for the fixture.
func (df *deliveryFixture) signedGithub(t *testing.T, eventType, deliveryID, payload string) webhooks.DeliverInput {
	t.Helper()
	mac := hmac.New(sha256.New, df.secret)
	mac.Write([]byte(payload))
	header := http.Header{}
	header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	header.Set("X-GitHub-Event", eventType)
	header.Set("X-GitHub-Delivery", deliveryID)
	return webhooks.DeliverInput{
		WorkspaceID:  df.sd.WS.ID,
		ConnectionID: df.sd.Connection.ID,
		Header:       header,
		Body:         []byte(payload),
	}
}

const prOpenedPayload = `{
	"action": "opened",
	"repository": {"full_name": "acme/api"},
	"pull_request": {"number": 42, "title": "Fix the login race", "html_url": "https://github.com/acme/api/pull/42"},
	"sender": {"login": "alice"}
}`

// TestIngress_EndToEnd_SignedDeliveryToBoundChannel is the task 5.4
// end-to-end: a signed delivery becomes a bound-agent run in the bound
// channel with labeled event data and service-authority attribution; the
// redelivery is deduped; disabling stops ingestion.
func TestIngress_EndToEnd_SignedDeliveryToBoundChannel(t *testing.T) {
	df := newDeliveryFixture(t, "wh-e2e", domain.ConnectionWebhookTargetChannel, "")
	ctx := context.Background()

	// 1. A signed pull_request.opened delivery → a run for the bound agent.
	outcome, err := df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "delivery-1", prOpenedPayload))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if outcome != webhooks.OutcomeDelivered {
		t.Fatalf("expected OutcomeDelivered, got %v", outcome)
	}

	// 2. The run reaches the bound agent in the bound channel with the
	// service-authority attribution and labeled event data.
	run := df.runner.waitRun(t)
	if webhooks.RunAuthorityService != "service" {
		t.Fatalf("RunAuthorityService = %q, want the literal service origin", webhooks.RunAuthorityService)
	}
	if run.Authority != webhooks.RunAuthorityService {
		t.Errorf("run authority = %q, want %q", run.Authority, webhooks.RunAuthorityService)
	}
	if run.ConnectionID != df.sd.Connection.ID || run.ConnectionService != "github" || run.Event != "pull_request.opened" {
		t.Errorf("run attribution wrong: %+v", run)
	}
	if run.AgentID != df.sd.Agent.ID {
		t.Errorf("run agent = %q, want the bound agent", run.AgentID)
	}
	if run.TargetKind != domain.ConnectionWebhookTargetChannel || run.TargetID != df.sd.Channel.ID {
		t.Errorf("run target wrong: %+v", run)
	}
	if run.SessionID == "" || run.SessionID == df.sd.Channel.ID {
		t.Errorf("a channel-target run needs its own per-delivery session, got %q", run.SessionID)
	}
	for _, want := range []string{
		"pull_request.opened",
		df.sd.Connection.ID,
		"-----BEGIN GITHUB EVENT DATA (pull_request.opened)-----",
		"acme/api",
		"Fix the login race",
	} {
		if !strings.Contains(run.Input, want) {
			t.Errorf("run input missing %q:\n%s", want, run.Input)
		}
	}

	// 3. The same delivery id redelivered → acked, no second turn.
	outcome, err = df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "delivery-1", prOpenedPayload))
	if err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	if outcome != webhooks.OutcomeAcked {
		t.Fatalf("expected the replay to ack, got %v", outcome)
	}
	time.Sleep(100 * time.Millisecond)
	if got := df.runner.count(); got != 1 {
		t.Fatalf("replay produced a second turn (%d runs)", got)
	}

	// 4. Disabling stops ingestion: even a signed, fresh delivery gets the
	// generic rejection.
	if _, err := df.svc.Disable(ctx, df.sd.WS.ID, df.sd.Connection.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	outcome, err = df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "delivery-2", prOpenedPayload))
	if err != nil || outcome != webhooks.OutcomeUnknown {
		t.Fatalf("a disabled connection must reject generically, got %v (%v)", outcome, err)
	}
	if df.runner.count() != 1 {
		t.Fatal("a disabled connection must produce no runs")
	}
}

func TestIngress_EndToEnd_ThreadTargetRunsInTheBoundSession(t *testing.T) {
	df := newDeliveryFixture(t, "wh-e2e-thread", domain.ConnectionWebhookTargetThread, "sess_bound_thread_1")
	ctx := context.Background()

	outcome, err := df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "delivery-t1", prOpenedPayload))
	if err != nil || outcome != webhooks.OutcomeDelivered {
		t.Fatalf("deliver: %v (%v)", outcome, err)
	}
	run := df.runner.waitRun(t)
	if run.SessionID != "sess_bound_thread_1" {
		t.Fatalf("a thread-target run must happen in the bound session, got %q", run.SessionID)
	}
	if run.TargetKind != domain.ConnectionWebhookTargetThread || run.TargetID != "sess_bound_thread_1" {
		t.Fatalf("thread target wrong: %+v", run)
	}
}

// TestIngress_Rejections is the 2.1 posture: unknown targets, disabled
// ingestion, bad signatures, and malformed deliveries all collapse to one
// generic non-enumerating outcome with zero side effects.
func TestIngress_Rejections(t *testing.T) {
	df := newDeliveryFixture(t, "wh-reject", domain.ConnectionWebhookTargetChannel, "")
	ctx := context.Background()

	runsAtStart := df.runner.count()

	assertUnknown := func(name string, in webhooks.DeliverInput) {
		t.Run(name, func(t *testing.T) {
			outcome, err := df.ing.Deliver(ctx, in)
			if err != nil {
				t.Fatalf("deliver: %v", err)
			}
			if outcome != webhooks.OutcomeUnknown {
				t.Fatalf("expected OutcomeUnknown, got %v", outcome)
			}
		})
	}

	// Unknown workspace and unknown connection.
	assertUnknown("unknown workspace", func() webhooks.DeliverInput {
		in := df.signedGithub(t, "pull_request", "d-ws", prOpenedPayload)
		in.WorkspaceID = "no-such-workspace"
		return in
	}())
	assertUnknown("unknown connection", func() webhooks.DeliverInput {
		in := df.signedGithub(t, "pull_request", "d-conn", prOpenedPayload)
		in.ConnectionID = "no-such-connection"
		return in
	}())

	// A connection whose recipe declares no webhooks.
	figma := &domain.Connection{WorkspaceID: df.sd.WS.ID, Service: "figma", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := df.sd.connections.Create(ctx, figma); err != nil {
		t.Fatalf("create figma connection: %v", err)
	}
	header := http.Header{}
	header.Set("X-Hub-Signature-256", "sha256=abcd")
	header.Set("X-GitHub-Event", "push")
	header.Set("X-GitHub-Delivery", "d-figma")
	assertUnknown("non-webhook recipe", webhooks.DeliverInput{
		WorkspaceID: df.sd.WS.ID, ConnectionID: figma.ID, Header: header, Body: []byte("{}"),
	})

	// Signature failures — tampered, unsigned, wrong-header scheme.
	tampered := df.signedGithub(t, "pull_request", "d-tamper", prOpenedPayload)
	tampered.Body = []byte(`{"action":"opened","repository":{"full_name":"evil/evil"},"pull_request":{"number":1,"title":"pwn","html_url":"https://evil.example"},"sender":{"login":"mallory"}}`)
	assertUnknown("tampered body", tampered)

	unsigned := df.signedGithub(t, "pull_request", "d-unsigned", prOpenedPayload)
	unsigned.Header.Del("X-Hub-Signature-256")
	assertUnknown("unsigned", unsigned)

	// A GitLab-style verbatim-token header does not satisfy the GitHub
	// scheme: the verifier is recipe-driven, the names never hardcoded.
	wrongScheme := df.signedGithub(t, "pull_request", "d-scheme", prOpenedPayload)
	wrongScheme.Header.Del("X-Hub-Signature-256")
	wrongScheme.Header.Set("X-Gitlab-Token", string(df.secret))
	assertUnknown("secret token on an hmac scheme", wrongScheme)

	// Missing provider headers.
	missingDelivery := df.signedGithub(t, "pull_request", "", prOpenedPayload)
	assertUnknown("missing delivery id header", missingDelivery)
	missingEvent := df.signedGithub(t, "", "d-noevent", prOpenedPayload)
	assertUnknown("missing event type header", missingEvent)

	// Empty body.
	empty := df.signedGithub(t, "pull_request", "d-empty", prOpenedPayload)
	empty.Body = nil
	assertUnknown("empty body", empty)

	// Nothing above may have recorded a delivery or routed a run: a valid
	// delivery with the same ids still processes fresh.
	time.Sleep(50 * time.Millisecond)
	if got := df.runner.count(); got != runsAtStart {
		t.Fatalf("rejections must never route runs: %d runs appeared", got-runsAtStart)
	}
	outcome, err := df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "d-tamper", prOpenedPayload))
	if err != nil || outcome != webhooks.OutcomeDelivered {
		t.Fatalf("a rejected delivery must leave no dedupe trace, got %v (%v)", outcome, err)
	}
	df.runner.waitRun(t)
}

// TestIngress_UnselectedEventAcksWithoutTurn pins the spec scenario: a
// verified delivery for an event the connection did not select is
// acknowledged without producing an agent turn.
func TestIngress_UnselectedEventAcksWithoutTurn(t *testing.T) {
	df := newDeliveryFixture(t, "wh-unselected", domain.ConnectionWebhookTargetChannel, "")
	ctx := context.Background()

	// release.published is in the github catalog but not in the fixture's
	// selected events.
	releasePayload := `{"action":"published","repository":{"full_name":"acme/api"},"release":{"tag_name":"v1.0","name":"One","html_url":"https://github.com/acme/api/releases/v1.0"},"sender":{"login":"alice"}}`
	outcome, err := df.ing.Deliver(ctx, df.signedGithub(t, "release", "d-release", releasePayload))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if outcome != webhooks.OutcomeAcked {
		t.Fatalf("expected an ack for the unselected event, got %v", outcome)
	}
	time.Sleep(100 * time.Millisecond)
	if df.runner.count() != 0 {
		t.Fatal("an unselected event must not produce a turn")
	}

	// The unselected delivery id was not recorded: a later selected
	// delivery reusing the id still processes.
	outcome, err = df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "d-release", prOpenedPayload))
	if err != nil || outcome != webhooks.OutcomeDelivered {
		t.Fatalf("expected delivery after the unselected ack, got %v (%v)", outcome, err)
	}
	df.runner.waitRun(t)
}
