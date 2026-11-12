package webhooks

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// testNow is the fixed delivery clock: session titles and recorded
// timestamps are deterministic under it.
func testNow() time.Time {
	return time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
}

// testAddr is the ephemeral listener address Service tests configure via
// WithListenAddr: the suite must never contend for the production
// DefaultAddr, which a live app instance may hold. The resolved port reads
// back through ListenerState.BoundAddr.
const testAddr = "127.0.0.1:0"

// fakeTurn is the TurnView the Runs fake hands back: instantly terminal.
type fakeTurn struct {
	id  string
	err error
}

func (t *fakeTurn) TurnID() string        { return t.id }
func (t *fakeTurn) Done() <-chan struct{} { return closedDone }
func (t *fakeTurn) Wait() error           { return t.err }

var closedDone = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// submitCall is one recorded Runs.Submit.
type submitCall struct {
	SessionID string
	Prompt    string
	Origin    string
}

// createCall is one recorded Runs.Create.
type createCall struct {
	AgentID string
	Title   string
}

// fakeRuns records the Runs seam's calls (add-webhook-triggers tasks 5.1).
type fakeRuns struct {
	mu        sync.Mutex
	creates   []createCall
	submits   []submitCall
	createErr error
	submitErr error
}

func (f *fakeRuns) Create(_ context.Context, agentID, title string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	f.creates = append(f.creates, createCall{AgentID: agentID, Title: title})
	return fmt.Sprintf("session-%d", len(f.creates)), nil
}

func (f *fakeRuns) Submit(_ context.Context, sessionID, prompt, origin string) (TurnView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	f.submits = append(f.submits, submitCall{SessionID: sessionID, Prompt: prompt, Origin: origin})
	return &fakeTurn{id: fmt.Sprintf("turn-%d", len(f.submits))}, nil
}

func (f *fakeRuns) createdCalls() []createCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]createCall(nil), f.creates...)
}

func (f *fakeRuns) submitCalls() []submitCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]submitCall(nil), f.submits...)
}

// sendCall is one recorded GroupSender.Send.
type sendCall struct {
	GroupID string
	Text    string
}

// fakeGroups records the GroupSender seam's calls (add-webhook-triggers
// tasks 5.2).
type fakeGroups struct {
	mu   sync.Mutex
	sent []sendCall
	err  error
}

func (f *fakeGroups) Send(_ context.Context, groupID, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.sent = append(f.sent, sendCall{GroupID: groupID, Text: text})
	return fmt.Sprintf("message-%d", len(f.sent)), nil
}

func (f *fakeGroups) sentCalls() []sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sendCall(nil), f.sent...)
}

// pipe is one pipeline test fixture: a fake store seeded with a live agent
// and group, both dispatch seams, and a real gateway manager held Running.
type pipe struct {
	st     store.Store
	runs   *fakeRuns
	groups *fakeGroups
	gw     gateway.Manager
	rec    *Receiver
}

// newPipe builds the fixture. The gateway starts Running, so deliveries
// dispatch unless a test pauses or stops it.
func newPipe(t *testing.T, opts ...Option) *pipe {
	t.Helper()
	st := fake.New()
	ctx := context.Background()
	require.NoError(t, st.Agents().Create(ctx, &store.Agent{Name: "Scout", Slug: "scout"}))
	agents, err := st.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.NoError(t, st.Groups().CreateGroup(ctx, &store.AgentGroup{Name: "ops"}))
	groups, err := st.Groups().ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 1)

	runs := &fakeRuns{}
	grps := &fakeGroups{}
	gw := gateway.NewManager()
	require.NoError(t, gw.Start(ctx))

	return &pipe{
		st:     st,
		runs:   runs,
		groups: grps,
		gw:     gw,
		rec:    NewReceiver(st, runs, grps, gw, append([]Option{WithClock(testNow)}, opts...)...),
	}
}

// agentID returns the fixture agent's id.
func (p *pipe) agentID(t *testing.T) string {
	t.Helper()
	agents, err := p.st.Agents().List(context.Background())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	return agents[0].ID
}

// groupID returns the fixture group's id.
func (p *pipe) groupID(t *testing.T) string {
	t.Helper()
	groups, err := p.st.Groups().ListGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	return groups[0].ID
}

// createEndpoint stores an endpoint with the given verifiers and returns it
// with its assigned id.
func (p *pipe) createEndpoint(t *testing.T, ep store.WebhookEndpoint, verifiers []store.WebhookVerifier) store.WebhookEndpoint {
	t.Helper()
	require.NoError(t, p.st.Webhooks().SaveEndpoint(context.Background(), &ep))
	if len(verifiers) > 0 {
		require.NoError(t, p.st.Webhooks().SaveVerifiers(context.Background(), ep.ID, verifiers))
	}
	saved, err := p.st.Webhooks().GetEndpoint(context.Background(), ep.ID)
	require.NoError(t, err)
	return *saved
}

// deliver POSTs one delivery to the receiver and returns the response. body
// may be nil; headers may be nil.
func (p *pipe) deliver(endpointID string, body []byte, headers http.Header, pathToken string) *httptest.ResponseRecorder {
	url := "/webhooks/" + endpointID
	if pathToken != "" {
		url += "/" + pathToken
	}
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header = headers
	rec := httptest.NewRecorder()
	p.rec.ServeHTTP(rec, req)
	return rec
}

// deliveries reads the endpoint's delivery log newest-first.
func (p *pipe) deliveries(t *testing.T, endpointID string) []store.WebhookDelivery {
	t.Helper()
	rows, err := p.st.Webhooks().ListDeliveries(context.Background(), endpointID)
	require.NoError(t, err)
	return rows
}

// headerVerifier is the simplest credential: one static header equality row.
func headerVerifier(secret string) store.WebhookVerifier {
	return store.WebhookVerifier{Kind: store.WebhookVerifierHeader, Preset: "header", HeaderName: "X-Webhook-Token", Secret: secret}
}

// tokenHeaders builds the credential headers for the header-verifier fixture.
func tokenHeaders(secret string) http.Header {
	h := http.Header{}
	h.Set("X-Webhook-Token", secret)
	return h
}
