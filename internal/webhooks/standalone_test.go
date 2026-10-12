package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// The standalone tests run in-package (the queue_test.go privilege): the
// fixture wires the real Queue to the unexported ProcessDelivery, and the
// fail-closed render assertions read the shared ErrRenderFailed sentinel.

// standaloneTestKey is the instance master key for the test cipher (32
// bytes, the AES-256-GCM requirement).
var standaloneTestKey = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef")

// gitlabMRPayload is a GitLab-shaped merge-request delivery payload.
const gitlabMRPayload = `{"object_kind":"merge_request","project":{"path_with_namespace":"team/api"},"object_attributes":{"iid":7,"title":"Fix the login race","url":"https://gitlab.example/team/api/-/merge_requests/7"}}`

// fakeStandaloneRunner records the runs the standalone pipeline routes and
// signals each one; it returns a fixed run id so the submitted log row's
// link is assertable.
type fakeStandaloneRunner struct {
	mu    sync.Mutex
	runs  []StandaloneRunRequest
	runID string
	err   error
	notif chan struct{}
}

func newFakeStandaloneRunner() *fakeStandaloneRunner {
	return &fakeStandaloneRunner{runID: "run-42", notif: make(chan struct{}, 32)}
}

func (r *fakeStandaloneRunner) RunWebhookEvent(_ context.Context, req StandaloneRunRequest) (string, error) {
	r.mu.Lock()
	r.runs = append(r.runs, req)
	runID, err := r.runID, r.err
	r.mu.Unlock()
	select {
	case r.notif <- struct{}{}:
	default:
	}
	return runID, err
}

func (r *fakeStandaloneRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func (r *fakeStandaloneRunner) last() StandaloneRunRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[len(r.runs)-1]
}

// waitRun waits for one more run to arrive and returns it.
func (r *fakeStandaloneRunner) waitRun(t *testing.T) StandaloneRunRequest {
	t.Helper()
	select {
	case <-r.notif:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the routed run")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[len(r.runs)-1]
}

// waitNoRun fails if any run arrives within the settle window — the
// no-enqueue assertions for filtered/duplicate/rejected verdicts.
func (r *fakeStandaloneRunner) waitNoRun(t *testing.T, baseline int) {
	t.Helper()
	time.Sleep(100 * time.Millisecond)
	if got := r.count(); got != baseline {
		t.Fatalf("expected no runs beyond the %d baseline, got %d", baseline, got)
	}
}

// standaloneFixture is one seeded standalone endpoint with the full pipeline
// wired: the fake store, the test cipher, a generous rate limiter, a
// standalone-lane queue feeding ProcessDelivery, and the recording runner.
type standaloneFixture struct {
	ws      *domain.Workspace
	wh      *domain.WorkspaceWebhook
	secret  string
	stores  store.WorkspaceWebhookStore
	runner  *fakeStandaloneRunner
	limiter *RateLimiter
	queue   *Queue
	ing     *StandaloneIngress
}

// newStandaloneFixture seeds a workspace and an hmac_sha256 endpoint bound
// to a channel, applying mutate before the create (scheme, filter, template,
// disabled-state overrides in the tests).
func newStandaloneFixture(t *testing.T, mutate func(*domain.WorkspaceWebhook)) *standaloneFixture {
	t.Helper()
	s := fake.New()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "standalone", Name: "WS standalone"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	cipher := NewAESGCMCipher(standaloneTestKey)
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	envelope, err := cipher.EncryptSecret(ws.ID, []byte(secret))
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}

	wh := &domain.WorkspaceWebhook{
		WorkspaceID:      ws.ID,
		Name:             "gitlab-mr-review",
		Scheme:           domain.WorkspaceWebhookSchemeHmacSha256,
		SecretCiphertext: envelope,
		SecretHint:       SecretHint(secret),
		EventSource: domain.WorkspaceWebhookEventSource{
			Kind: domain.WorkspaceWebhookEventSourceHeader,
			Name: "X-Gitlab-Event",
		},
		DeliveryIDSource: "X-Delivery-Id",
		TargetAgentID:    uuid.NewString(),
		TargetKind:       domain.ConnectionWebhookTargetChannel,
		TargetID:         "chan-incidents",
		Enabled:          true,
	}
	if mutate != nil {
		mutate(wh)
	}
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	f := &standaloneFixture{
		ws:     ws,
		wh:     wh,
		secret: secret,
		stores: s.WorkspaceWebhooks(),
	}
	f.runner = newFakeStandaloneRunner()

	var ing *StandaloneIngress
	f.limiter = NewRateLimiter(1000, time.Minute)
	f.queue = NewQueue(16, func(ctx context.Context, job *Delivery) {
		ing.ProcessDelivery(ctx, job)
	})
	ing = NewStandaloneIngress(f.stores, cipher, f.limiter, f.queue, f.runner)
	f.ing = ing
	t.Cleanup(f.queue.Close)
	return f
}

// signedDelivery builds a validly signed delivery for the fixture's hmac
// endpoint: the conventional signature header, the declared event header,
// and the declared delivery-id header.
func (f *standaloneFixture) signedDelivery(t *testing.T, event, deliveryID, payload string) StandaloneDeliverInput {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(f.secret))
	mac.Write([]byte(payload))
	header := http.Header{}
	header.Set(StandaloneSignatureHeaderGithubHmac, hex.EncodeToString(mac.Sum(nil)))
	header.Set("X-Gitlab-Event", event)
	header.Set("X-Delivery-Id", deliveryID)
	return StandaloneDeliverInput{
		WorkspaceID: f.ws.ID,
		WebhookID:   f.wh.ID,
		Header:      header,
		Body:        []byte(payload),
	}
}

// logEntries reads the endpoint's full delivery log.
func (f *standaloneFixture) logEntries(t *testing.T) []domain.WebhookDeliveryLogEntry {
	t.Helper()
	entries, err := f.stores.ListDeliveryLog(context.Background(), f.ws.ID, f.wh.ID, 0)
	if err != nil {
		t.Fatalf("list delivery log: %v", err)
	}
	return entries
}

// findLogOutcome returns the first entry with the outcome, or fails.
func findLogOutcome(t *testing.T, entries []domain.WebhookDeliveryLogEntry, outcome string) domain.WebhookDeliveryLogEntry {
	t.Helper()
	for _, e := range entries {
		if e.Outcome == outcome {
			return e
		}
	}
	t.Fatalf("no %q row in the delivery log (%d rows)", outcome, len(entries))
	return domain.WebhookDeliveryLogEntry{}
}

// waitForLogOutcome polls until a row with the outcome appears — the async
// process-func writes (submitted/failed) land after the queue drains.
func waitForLogOutcome(t *testing.T, f *standaloneFixture, outcome string) domain.WebhookDeliveryLogEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range f.logEntries(t) {
			if e.Outcome == outcome {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a %q delivery log row", outcome)
	return domain.WebhookDeliveryLogEntry{}
}

// TestStandaloneDeliver_EndToEnd_ChannelTarget is the task 4.2 end-to-end:
// a signed delivery enqueues, the process func renders the default labeled
// turn, the fake runner receives the correct attribution, and the delivery
// log links the submitted outcome to the runner's run id; the redelivery is
// a duplicate ack with its own log row and no second run.
func TestStandaloneDeliver_EndToEnd_ChannelTarget(t *testing.T) {
	f := newStandaloneFixture(t, nil)
	ctx := context.Background()

	outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "delivery-1", gitlabMRPayload))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if outcome != StandaloneOutcomeSubmitted {
		t.Fatalf("expected StandaloneOutcomeSubmitted, got %v", outcome)
	}

	run := f.runner.waitRun(t)
	if run.WorkspaceID != f.ws.ID || run.WebhookID != f.wh.ID {
		t.Errorf("run workspace/webhook wrong: %+v", run)
	}
	if run.WebhookName != "gitlab-mr-review" {
		t.Errorf("run webhook name = %q, want the endpoint name", run.WebhookName)
	}
	if run.Event != "merge_request" {
		t.Errorf("run event = %q, want the resolved header value", run.Event)
	}
	if run.AgentID != f.wh.TargetAgentID {
		t.Errorf("run agent = %q, want the bound agent", run.AgentID)
	}
	if run.TargetKind != domain.ConnectionWebhookTargetChannel || run.TargetID != "chan-incidents" {
		t.Errorf("run target wrong: %+v", run)
	}
	if run.SessionID == "" || run.SessionID == f.wh.TargetID {
		t.Errorf("a channel-target run needs its own per-delivery session, got %q", run.SessionID)
	}
	for _, want := range []string{
		"Webhook event merge_request from the gitlab-mr-review webhook endpoint.",
		"-----BEGIN WEBHOOK EVENT DATA (merge_request)-----",
		"-----END WEBHOOK EVENT DATA (merge_request)-----",
		"untrusted event data",
		"team/api",
		"Fix the login race",
	} {
		if !strings.Contains(run.Input, want) {
			t.Errorf("run input missing %q:\n%s", want, run.Input)
		}
	}

	// The submitted log row links the run id the port returned.
	entry := waitForLogOutcome(t, f, domain.WorkspaceWebhookDeliveryOutcomeSubmitted)
	if entry.RunID != "run-42" {
		t.Errorf("submitted log row run id = %q, want the runner's id", entry.RunID)
	}
	if entry.Event != "merge_request" {
		t.Errorf("submitted log row event = %q", entry.Event)
	}

	// The same delivery id redelivered → acked, no second turn, duplicate
	// log row.
	outcome, err = f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "delivery-1", gitlabMRPayload))
	if err != nil || outcome != StandaloneOutcomeDuplicate {
		t.Fatalf("redelivery must ack as a duplicate, got %v (%v)", outcome, err)
	}
	f.runner.waitNoRun(t, 1)
	entry = findLogOutcome(t, f.logEntries(t), domain.WorkspaceWebhookDeliveryOutcomeDuplicate)
	if entry.Event != "merge_request" {
		t.Errorf("duplicate log row event = %q", entry.Event)
	}
}

// TestStandaloneDeliver_ThreadTargetRunsInBoundSession pins the thread-side
// routing: a thread-bound endpoint's run happens in the bound session.
func TestStandaloneDeliver_ThreadTargetRunsInBoundSession(t *testing.T) {
	f := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) {
		wh.TargetKind = domain.ConnectionWebhookTargetThread
		wh.TargetID = "sess_bound_thread_1"
	})

	outcome, err := f.ing.Deliver(context.Background(), f.signedDelivery(t, "merge_request", "delivery-t1", gitlabMRPayload))
	if err != nil || outcome != StandaloneOutcomeSubmitted {
		t.Fatalf("deliver: %v (%v)", outcome, err)
	}
	run := f.runner.waitRun(t)
	if run.SessionID != "sess_bound_thread_1" {
		t.Fatalf("a thread-target run must happen in the bound session, got %q", run.SessionID)
	}
	if run.TargetKind != domain.ConnectionWebhookTargetThread || run.TargetID != "sess_bound_thread_1" {
		t.Fatalf("thread target wrong: %+v", run)
	}
}

// TestStandaloneDeliver_Rejections is the D5 posture: unknown, paused, bad
// signature, missing material, and over-cap deliveries all reject with a
// classified (transport-collapsible) error, and verification failures leave
// no dedupe trace.
func TestStandaloneDeliver_Rejections(t *testing.T) {
	f := newStandaloneFixture(t, nil)
	ctx := context.Background()

	t.Run("unknown endpoint", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "d-unknown", gitlabMRPayload)
		in.WebhookID = "no-such-endpoint"
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrEndpointNotFound) {
			t.Fatalf("expected rejected/ErrEndpointNotFound, got %v (%v)", outcome, err)
		}
	})

	t.Run("foreign workspace is unknown", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "d-foreign", gitlabMRPayload)
		in.WorkspaceID = "no-such-workspace"
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrEndpointNotFound) {
			t.Fatalf("expected rejected/ErrEndpointNotFound, got %v (%v)", outcome, err)
		}
	})

	t.Run("paused endpoint", func(t *testing.T) {
		paused := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) { wh.Enabled = false })
		outcome, err := paused.ing.Deliver(ctx, paused.signedDelivery(t, "merge_request", "d-paused", gitlabMRPayload))
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrEndpointDisabled) {
			t.Fatalf("expected rejected/ErrEndpointDisabled, got %v (%v)", outcome, err)
		}
		if paused.runner.count() != 0 {
			t.Fatal("a paused endpoint must produce no runs")
		}
		// The paused rejection is visible in the log (the admin's "why is
		// nothing happening" surface), classified rejected.
		entry := findLogOutcome(t, paused.logEntries(t), domain.WorkspaceWebhookDeliveryOutcomeRejected)
		if entry.Detail != "endpoint paused" {
			t.Errorf("paused log detail = %q", entry.Detail)
		}
	})

	t.Run("tampered body", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "d-tamper", gitlabMRPayload)
		in.Body = []byte(`{"object_kind":"merge_request","project":{"path_with_namespace":"evil/evil"}}`)
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrEndpointUnverified) {
			t.Fatalf("expected rejected/ErrEndpointUnverified, got %v (%v)", outcome, err)
		}
		entry := findLogOutcome(t, f.logEntries(t), domain.WorkspaceWebhookDeliveryOutcomeRejected)
		if entry.Detail != "signature verification failed" {
			t.Errorf("rejected log detail = %q", entry.Detail)
		}
	})

	t.Run("missing signature header", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "d-unsigned", gitlabMRPayload)
		in.Header.Del(StandaloneSignatureHeaderGithubHmac)
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrEndpointUnverified) {
			t.Fatalf("expected rejected/ErrEndpointUnverified, got %v (%v)", outcome, err)
		}
	})

	t.Run("missing delivery id header", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "", gitlabMRPayload)
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrDeliveryMalformed) {
			t.Fatalf("expected rejected/ErrDeliveryMalformed, got %v (%v)", outcome, err)
		}
	})

	t.Run("over-cap body", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "d-cap", gitlabMRPayload)
		in.Body = make([]byte, StandaloneMaxBodyBytes+1)
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("expected rejected/ErrBodyTooLarge, got %v (%v)", outcome, err)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		in := f.signedDelivery(t, "merge_request", "d-empty", gitlabMRPayload)
		in.Body = nil
		outcome, err := f.ing.Deliver(ctx, in)
		if outcome != StandaloneOutcomeRejected {
			t.Fatalf("expected rejected for an empty body, got %v (%v)", outcome, err)
		}
	})

	// Nothing rejected above recorded a dedupe row: a valid delivery
	// reusing a previously-rejected delivery id processes fresh.
	f.runner.waitNoRun(t, 0)
	outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-tamper", gitlabMRPayload))
	if err != nil || outcome != StandaloneOutcomeSubmitted {
		t.Fatalf("a rejected delivery must leave no dedupe trace, got %v (%v)", outcome, err)
	}
	f.runner.waitRun(t)
}

// TestStandaloneDeliver_RateLimited pins the one enumerable rejection: the
// second delivery inside an exhausted window answers with the RateLimitError
// class carrying its retry-after, a rejected log row, and no run.
func TestStandaloneDeliver_RateLimited(t *testing.T) {
	f := newStandaloneFixture(t, nil)
	f.limiter = NewRateLimiter(1, time.Minute)
	// Rebuild the ingress with the tight limiter (the fixture built its own).
	cipher := NewAESGCMCipher(standaloneTestKey)
	queue := NewQueue(16, func(ctx context.Context, job *Delivery) { f.ing.ProcessDelivery(ctx, job) })
	defer queue.Close()
	f.ing = NewStandaloneIngress(f.stores, cipher, f.limiter, queue, f.runner)
	ctx := context.Background()

	if outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-rl-1", gitlabMRPayload)); err != nil || outcome != StandaloneOutcomeSubmitted {
		t.Fatalf("first delivery must pass the fresh window, got %v (%v)", outcome, err)
	}
	f.runner.waitRun(t)

	outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-rl-2", gitlabMRPayload))
	if outcome != StandaloneOutcomeRejected || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rejected/ErrRateLimited, got %v (%v)", outcome, err)
	}
	var rle *RateLimitError
	if !errors.As(err, &rle) || rle.RetryAfter <= 0 || rle.RetryAfter > time.Minute {
		t.Fatalf("the rate-limit error must carry a positive in-window retry-after, got %+v", rle)
	}
	f.runner.waitNoRun(t, 1)
	entry := findLogOutcome(t, f.logEntries(t), domain.WorkspaceWebhookDeliveryOutcomeRejected)
	if !strings.Contains(entry.Detail, "rate limited") {
		t.Errorf("rate-limited log detail = %q", entry.Detail)
	}
}

// TestStandaloneDeliver_Filtered covers the filter gate (task 4.1): a
// non-match and an eval error both ack with a filtered log row and no run;
// a match proceeds to a run.
func TestStandaloneDeliver_Filtered(t *testing.T) {
	const filter = `event == "merge_request" && project.path_with_namespace == "team/api"`
	f := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) {
		wh.FilterExpression = filter
	})
	ctx := context.Background()

	// A matching delivery proceeds.
	outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-match", gitlabMRPayload))
	if err != nil || outcome != StandaloneOutcomeSubmitted {
		t.Fatalf("a matching delivery must submit, got %v (%v)", outcome, err)
	}
	f.runner.waitRun(t)

	// A non-matching delivery (right event, wrong project) is an explicit
	// ignored ack with a filtered row and no second run.
	other := strings.Replace(gitlabMRPayload, "team/api", "team/frontend", 1)
	outcome, err = f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-other", other))
	if err != nil || outcome != StandaloneOutcomeFiltered {
		t.Fatalf("a non-matching delivery must ack filtered, got %v (%v)", outcome, err)
	}
	f.runner.waitNoRun(t, 1)
	entry := findLogOutcome(t, f.logEntries(t), domain.WorkspaceWebhookDeliveryOutcomeFiltered)
	if entry.Detail != "filter expression evaluated false" {
		t.Errorf("filtered log detail = %q", entry.Detail)
	}

	// An eval error (missing field) fails closed the same way: ignored ack,
	// no run. The replacement body is what gets signed — verification must
	// pass so the delivery reaches the filter.
	in := f.signedDelivery(t, "merge_request", "d-evalerr", `{"object_kind":"merge_request"}`)
	outcome, err = f.ing.Deliver(ctx, in)
	if err != nil || outcome != StandaloneOutcomeFiltered {
		t.Fatalf("an eval error must ack filtered, got %v (%v)", outcome, err)
	}
	f.runner.waitNoRun(t, 1)
	entry = waitForLogOutcome(t, f, domain.WorkspaceWebhookDeliveryOutcomeFiltered)
	if !strings.Contains(entry.Detail, "filter evaluation failed") {
		t.Errorf("eval-error log detail = %q", entry.Detail)
	}
}

// TestStandaloneDeliver_TemplateFailClosed pins the spec scenario: a
// delivery whose payload is missing a template field drops with a `failed`
// log row and no run — never a half-filled turn (task 4.2).
func TestStandaloneDeliver_TemplateFailClosed(t *testing.T) {
	f := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) {
		wh.TurnTemplate = "MR !{object_attributes.iid}: {object_attributes.title} — {missing.field}"
		wh.SamplePayload = gitlabMRPayload
	})
	ctx := context.Background()

	// The ingress accepts (verified, deduped, unfiltered); the fail-closed
	// drop happens in the process func.
	outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-tmpl", gitlabMRPayload))
	if err != nil || outcome != StandaloneOutcomeSubmitted {
		t.Fatalf("deliver: %v (%v)", outcome, err)
	}

	entry := waitForLogOutcome(t, f, domain.WorkspaceWebhookDeliveryOutcomeFailed)
	if !strings.Contains(entry.Detail, "missing.field") {
		t.Errorf("failed log detail = %q, want the missing field named", entry.Detail)
	}
	f.runner.waitNoRun(t, 0)
}

// TestStandaloneDeliver_EventSources covers the declared event source kinds
// (task 4.1): payload-field and fixed resolution, and the missing-event →
// empty-string rule the filter sees.
func TestStandaloneDeliver_EventSources(t *testing.T) {
	ctx := context.Background()

	t.Run("payload field source", func(t *testing.T) {
		f := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) {
			wh.EventSource = domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourcePayloadField, Path: "object_kind"}
			wh.FilterExpression = `event == "merge_request"`
		})
		in := f.signedDelivery(t, "", "d-field", gitlabMRPayload)
		outcome, err := f.ing.Deliver(ctx, in)
		if err != nil || outcome != StandaloneOutcomeSubmitted {
			t.Fatalf("deliver: %v (%v)", outcome, err)
		}
		if run := f.runner.waitRun(t); run.Event != "merge_request" {
			t.Fatalf("event = %q, want the payload field's value", run.Event)
		}
	})

	t.Run("fixed source", func(t *testing.T) {
		f := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) {
			wh.EventSource = domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourceFixed, Value: "pipeline"}
			wh.FilterExpression = `event == "pipeline"`
		})
		outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "", "d-fixed", gitlabMRPayload))
		if err != nil || outcome != StandaloneOutcomeSubmitted {
			t.Fatalf("deliver: %v (%v)", outcome, err)
		}
		if run := f.runner.waitRun(t); run.Event != "pipeline" {
			t.Fatalf("event = %q, want the fixed value", run.Event)
		}
	})

	t.Run("missing payload field resolves to the empty event", func(t *testing.T) {
		f := newStandaloneFixture(t, func(wh *domain.WorkspaceWebhook) {
			wh.EventSource = domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourcePayloadField, Path: "not.there"}
			wh.FilterExpression = `event == "merge_request"`
		})
		// The empty event fails the filter: filtered ack, not a rejection.
		outcome, err := f.ing.Deliver(ctx, f.signedDelivery(t, "merge_request", "d-noevent", gitlabMRPayload))
		if err != nil || outcome != StandaloneOutcomeFiltered {
			t.Fatalf("a missing event must ack through the filter, got %v (%v)", outcome, err)
		}
		f.runner.waitNoRun(t, 0)
	})
}

// TestRenderStandaloneTurn_Default covers the default (template-less) turn:
// endpoint name + event, labeled delimited payload, truncation at the cap.
func TestRenderStandaloneTurn_Default(t *testing.T) {
	wh := &domain.WorkspaceWebhook{Name: "gitlab-mr-review"}

	// A small payload rides whole, delimited, with no truncation marker.
	turn, err := RenderStandaloneTurn(wh, "merge_request", []byte(gitlabMRPayload))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(turn, "Webhook event merge_request from the gitlab-mr-review webhook endpoint.") {
		t.Errorf("the turn must name the endpoint and event:\n%s", turn)
	}
	if !strings.Contains(turn, "-----BEGIN WEBHOOK EVENT DATA (merge_request)-----") ||
		!strings.Contains(turn, "-----END WEBHOOK EVENT DATA (merge_request)-----") {
		t.Errorf("the payload must ride between the labeled markers:\n%s", turn)
	}
	if strings.Contains(turn, "…[truncated]") {
		t.Error("a small payload must not truncate")
	}
	if !strings.Contains(turn, "team/api") {
		t.Errorf("the payload content must be present:\n%s", turn)
	}

	// An oversized payload truncates at the cap with the explicit marker.
	big := `{"blob":"` + strings.Repeat("a", 6000) + `"}`
	turn, err = RenderStandaloneTurn(wh, "push", []byte(big))
	if err != nil {
		t.Fatalf("render big: %v", err)
	}
	if !strings.Contains(turn, "…[truncated]") {
		t.Error("an oversized payload must carry the explicit truncation marker")
	}
	begin := strings.Index(turn, "-----BEGIN WEBHOOK EVENT DATA (push)-----")
	end := strings.Index(turn, "-----END WEBHOOK EVENT DATA (push)-----")
	if begin < 0 || end < 0 || end <= begin {
		t.Fatalf("markers missing from the turn:\n%s", turn)
	}
	embedded := turn[begin:end]
	if len(embedded) > standaloneTurnPayloadCap+len("\n…[truncated]\n")+64 {
		t.Errorf("embedded payload must sit at the %d-byte cap, got %d bytes", standaloneTurnPayloadCap, len(embedded))
	}
	// The embedded content must be a verbatim prefix of the original payload,
	// never a re-encoded or partial-rune mangle.
	const beginMarker = "-----BEGIN WEBHOOK EVENT DATA (push)-----\n"
	payloadPart, _, _ := strings.Cut(embedded[len(beginMarker):], "\n…[truncated]")
	if !strings.HasPrefix(big, payloadPart) {
		t.Error("the embedded payload must be a prefix of the original payload")
	}
}

// TestRenderStandaloneTurn_Template covers template rendering (task 4.2):
// placeholders fill from the payload inside the labeled wrapper; a missing
// field fails closed with ErrRenderFailed.
func TestRenderStandaloneTurn_Template(t *testing.T) {
	wh := &domain.WorkspaceWebhook{
		Name:         "gitlab-mr-review",
		TurnTemplate: "MR !{object_attributes.iid} in {project.path_with_namespace}: {object_attributes.title}",
	}

	turn, err := RenderStandaloneTurn(wh, "merge_request", []byte(gitlabMRPayload))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"MR !7 in team/api: Fix the login race", "-----BEGIN WEBHOOK EVENT DATA (merge_request)-----"} {
		if !strings.Contains(turn, want) {
			t.Errorf("turn missing %q:\n%s", want, turn)
		}
	}

	bad := &domain.WorkspaceWebhook{
		Name:         "gitlab-mr-review",
		TurnTemplate: "MR {does.not_exist}",
	}
	_, err = RenderStandaloneTurn(bad, "merge_request", []byte(gitlabMRPayload))
	if !errors.Is(err, ErrRenderFailed) {
		t.Fatalf("a missing template field must fail closed with ErrRenderFailed, got %v", err)
	}
}

// TestValidateTemplateForSave is the save-time whitelist discipline
// (design.md D7): every failure wraps domain.ErrInvalid with the renderer's
// reason; a blank template validates trivially.
func TestValidateTemplateForSave(t *testing.T) {
	t.Run("blank template is trivially valid", func(t *testing.T) {
		wh := &domain.WorkspaceWebhook{}
		if err := ValidateTemplateForSave(wh); err != nil {
			t.Fatalf("a blank template must validate, got %v", err)
		}
	})

	t.Run("template without sample payload", func(t *testing.T) {
		wh := &domain.WorkspaceWebhook{TurnTemplate: "MR {object_attributes.iid}"}
		err := ValidateTemplateForSave(wh)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected domain.ErrInvalid, got %v", err)
		}
	})

	t.Run("non-JSON sample payload", func(t *testing.T) {
		wh := &domain.WorkspaceWebhook{TurnTemplate: "MR {a}", SamplePayload: "not json"}
		err := ValidateTemplateForSave(wh)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected domain.ErrInvalid, got %v", err)
		}
	})

	t.Run("placeholder missing from the sample", func(t *testing.T) {
		wh := &domain.WorkspaceWebhook{
			TurnTemplate:  "MR {object_attributes.iid} in {project.path_with_namespace}",
			SamplePayload: `{"object_attributes":{"iid":7}}`,
		}
		err := ValidateTemplateForSave(wh)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected domain.ErrInvalid, got %v", err)
		}
		if !strings.Contains(err.Error(), "project.path_with_namespace") {
			t.Errorf("the rejection must name the unresolvable placeholder, got %q", err.Error())
		}
	})

	t.Run("every placeholder resolvable", func(t *testing.T) {
		wh := &domain.WorkspaceWebhook{
			TurnTemplate:  "MR {object_attributes.iid}: {object_attributes.title}",
			SamplePayload: gitlabMRPayload,
		}
		if err := ValidateTemplateForSave(wh); err != nil {
			t.Fatalf("a covered template must validate, got %v", err)
		}
	})

	t.Run("malformed placeholder", func(t *testing.T) {
		wh := &domain.WorkspaceWebhook{TurnTemplate: "unclosed {field", SamplePayload: `{"field":"x"}`}
		if err := ValidateTemplateForSave(wh); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected domain.ErrInvalid for a malformed placeholder, got %v", err)
		}
	})
}
