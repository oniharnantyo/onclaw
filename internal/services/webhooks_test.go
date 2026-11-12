package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/webhooks"
)

// fakeWebhookRuntime is the WebhookRuntime fake: the listener state is
// settable, reconfigure requests arrive on a channel (the service kicks it
// asynchronously), and Test calls are recorded.
type fakeWebhookRuntime struct {
	state        webhooks.ListenerState
	reconfigured chan string
	testErr      error
	tested       chan string
}

func newFakeWebhookRuntime() *fakeWebhookRuntime {
	return &fakeWebhookRuntime{reconfigured: make(chan string, 8), tested: make(chan string, 8)}
}

func (f *fakeWebhookRuntime) State() webhooks.ListenerState { return f.state }

func (f *fakeWebhookRuntime) Reconfigure(addr string) { f.reconfigured <- addr }

func (f *fakeWebhookRuntime) Test(_ context.Context, endpointID string) error {
	f.tested <- endpointID
	return f.testErr
}

// webhookFixture wires a bound WebhookService over the fake store, a seeded
// agent and group (the two legal targets), and the fake runtime.
type webhookFixture struct {
	st      *fake.Store
	svc     *WebhookService
	rt      *fakeWebhookRuntime
	emitter *recordingEmitter
}

func newWebhookFixture(t *testing.T) *webhookFixture {
	t.Helper()
	f := &webhookFixture{
		st:      fake.New(),
		rt:      newFakeWebhookRuntime(),
		emitter: &recordingEmitter{},
	}
	f.svc = NewWebhookService(f.st, f.emitter, f.rt)
	ctx := context.Background()
	require.NoError(t, f.st.Agents().Create(ctx, &store.Agent{ID: "agt-1", Name: "Scout"}))
	require.NoError(t, f.st.Groups().CreateGroup(ctx, &store.AgentGroup{ID: "grp-1", Name: "Ops"}))
	return f
}

// gitLabInput is a valid agent-target input with one stdweb verifier — the
// canonical save payload the tests mutate.
func gitLabInput() WebhookEndpointInput {
	return WebhookEndpointInput{
		Name:    "CI alerts",
		AgentID: "agt-1",
		Prompt:  "A webhook arrived. Payload:",
		Enabled: true,
		Verifiers: []WebhookVerifierInput{
			{Kind: store.WebhookVerifierStdWeb, Preset: "gitlab", Secret: "whsec_supersecret9999"},
		},
	}
}

// createEndpoint stores gitLabInput through the service and drains its
// change event, so later event-count snapshots are stable.
func (f *webhookFixture) createEndpoint(t *testing.T, in WebhookEndpointInput) WebhookEndpointView {
	t.Helper()
	before := len(f.emitter.recorded())
	view, err := f.svc.CreateEndpoint(context.Background(), in)
	require.NoError(t, err)
	drainEvents(t, f.emitter, before, 1)
	return view
}

// mustStoredVerifiers reads the raw verifier rows straight from the store —
// the one place a test may look at secrets, to verify they survived.
func mustStoredVerifiers(t *testing.T, f *webhookFixture, id string) []store.WebhookVerifier {
	t.Helper()
	ep, err := f.st.Webhooks().GetEndpoint(context.Background(), id)
	require.NoError(t, err)
	return ep.Verifiers
}

func TestWebhookService_SecretsNeverLeakIntoViews(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	secret := "whsec_supersecret9999"
	view := f.createEndpoint(t, gitLabInput())

	v := view.Verifiers[0]
	require.True(t, v.SecretSet, "SecretSet reports presence")
	require.Equal(t, "9999", v.SecretHint, "the hint is the secret's last four")
	require.NotContains(t, v.SecretHint, secret[:12])

	// No view — endpoint list, single read, deliveries, presets, and the
	// whole bound JSON surface — may carry any fragment of the secret.
	endpoints, err := f.svc.ListEndpoints(ctx)
	require.NoError(t, err)
	for _, source := range []any{endpoints, view, f.svc.Presets()} {
		blob, err := json.Marshal(source)
		require.NoError(t, err)
		require.NotContains(t, string(blob), secret, "the raw secret must never cross the boundary")
		require.NotContains(t, string(blob), "supersecret")
	}

	stored := mustStoredVerifiers(t, f, view.ID)
	require.Len(t, stored, 1)
	require.Equal(t, secret, stored[0].Secret, "the stored secret survives verbatim for verification")
}

func TestWebhookService_SecretHintDerivation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		secret string
		hint   string
	}{
		{"long secret", "123456:AAHx-token-abcd", "abcd"},
		{"four chars", "abcd", "abcd"},
		{"short secret", "ab", "ab"},
		{"empty secret", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newWebhookFixture(t)
			in := gitLabInput()
			in.Verifiers[0].Secret = tt.secret
			view := f.createEndpoint(t, in)

			v := view.Verifiers[0]
			require.Equal(t, tt.hint, v.SecretHint)
			require.Equal(t, tt.secret != "", v.SecretSet)
		})
	}
}

func TestWebhookService_Update_BlankSecretKeepsStored(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	view := f.createEndpoint(t, gitLabInput())

	// An edit that leaves the secret field untouched (blank) keeps the
	// stored secret while the prompt and target move.
	updated, err := f.svc.UpdateEndpoint(ctx, view.ID, WebhookEndpointInput{
		Name:    "CI alerts",
		AgentID: "agt-1",
		Prompt:  "rewritten prompt",
		Enabled: false,
		Verifiers: []WebhookVerifierInput{
			{Kind: store.WebhookVerifierStdWeb, Preset: "gitlab"}, // blank secret
		},
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, "rewritten prompt", updated.Prompt)
	require.True(t, updated.Verifiers[0].SecretSet, "the blank save must keep the stored secret")
	require.Equal(t, "9999", updated.Verifiers[0].SecretHint)
	require.Equal(t, "whsec_supersecret9999", mustStoredVerifiers(t, f, view.ID)[0].Secret)

	// A non-blank secret replaces it (rotation).
	rotated, err := f.svc.UpdateEndpoint(ctx, view.ID, WebhookEndpointInput{
		Name:    "CI alerts",
		AgentID: "agt-1",
		Prompt:  "rewritten prompt",
		Enabled: true,
		Verifiers: []WebhookVerifierInput{
			{Kind: store.WebhookVerifierStdWeb, Preset: "gitlab", Secret: "whsec_rotated8888"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "8888", rotated.Verifiers[0].SecretHint)
	require.Equal(t, "whsec_rotated8888", mustStoredVerifiers(t, f, view.ID)[0].Secret)

	// A brand-new row's blank secret stays blank — there is nothing to keep
	// at that position.
	grown, err := f.svc.UpdateEndpoint(ctx, view.ID, WebhookEndpointInput{
		Name:    "CI alerts",
		AgentID: "agt-1",
		Prompt:  "p",
		Enabled: true,
		Verifiers: []WebhookVerifierInput{
			{Kind: store.WebhookVerifierStdWeb, Preset: "gitlab"}, // blank, keeps
			{Kind: store.WebhookVerifierPath, Preset: ""},         // new row, blank
		},
	})
	require.NoError(t, err)
	require.True(t, grown.Verifiers[0].SecretSet)
	require.False(t, grown.Verifiers[1].SecretSet, "a new row's blank secret has nothing to inherit")
}

func TestWebhookService_Create_DuplicateNameMapsToFriendlyError(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	f.createEndpoint(t, gitLabInput())

	in := gitLabInput()
	in.AgentID = ""
	in.GroupID = "grp-1"
	_, err := f.svc.CreateEndpoint(ctx, in)
	require.ErrorIs(t, err, store.ErrDuplicate, "the sentinel stays reachable for callers")
	require.Contains(t, err.Error(), "already in use", "the message names the collision, not the store")

	// Case-insensitive uniqueness, mirroring the store's NOCASE index.
	in.Name = "ci ALERTS"
	_, err = f.svc.CreateEndpoint(ctx, in)
	require.ErrorIs(t, err, store.ErrDuplicate)
}

func TestWebhookService_Create_RequiresVerifier(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	in := gitLabInput()
	in.Verifiers = nil
	_, err := f.svc.CreateEndpoint(ctx, in)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one verifier")

	// A structurally impossible row (unknown kind) is rejected too.
	in.Verifiers = []WebhookVerifierInput{{Kind: "nope"}}
	_, err = f.svc.CreateEndpoint(ctx, in)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestWebhookService_Create_TargetMustResolve(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	both := gitLabInput()
	both.GroupID = "grp-1"
	_, err := f.svc.CreateEndpoint(ctx, both)
	require.Error(t, err, "exactly one target is required")

	neither := gitLabInput()
	neither.AgentID = ""
	_, err = f.svc.CreateEndpoint(ctx, neither)
	require.Error(t, err)

	missing := gitLabInput()
	missing.AgentID = "ghost"
	_, err = f.svc.CreateEndpoint(ctx, missing)
	require.ErrorIs(t, err, store.ErrNotFound)

	missingGroup := gitLabInput()
	missingGroup.AgentID = ""
	missingGroup.GroupID = "ghost"
	_, err = f.svc.CreateEndpoint(ctx, missingGroup)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestWebhookService_Delete_CascadesViewWise(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	view := f.createEndpoint(t, gitLabInput())
	before := len(f.emitter.recorded())

	require.NoError(t, f.svc.DeleteEndpoint(ctx, view.ID))
	drainEvents(t, f.emitter, before, 1)

	endpoints, err := f.svc.ListEndpoints(ctx)
	require.NoError(t, err)
	require.Empty(t, endpoints, "the deleted endpoint leaves the list")
	_, err = f.svc.GetEndpoint(ctx, view.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	// The verifiers and delivery log went with it (the store's cascade,
	// visible view-wise: the endpoint id no longer resolves anywhere).
	_, err = f.svc.Deliveries(ctx, view.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// Deleting an unknown id is the sentinel.
	require.ErrorIs(t, f.svc.DeleteEndpoint(ctx, view.ID), store.ErrNotFound)
}

func TestWebhookService_SetEnabled(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	view := f.createEndpoint(t, gitLabInput())
	before := len(f.emitter.recorded())

	toggled, err := f.svc.SetEnabled(ctx, view.ID, false)
	require.NoError(t, err)
	require.False(t, toggled.Enabled)
	// Toggling keeps the definition and the stored secrets.
	require.True(t, toggled.Verifiers[0].SecretSet)
	require.Equal(t, "9999", toggled.Verifiers[0].SecretHint)
	stored := mustStoredVerifiers(t, f, view.ID)
	require.Len(t, stored, 1)

	events := drainEvents(t, f.emitter, before, 1)
	require.Equal(t, EventWebhooksChanged, events[0].Name)
	require.Equal(t, WebhooksChangedEvent{EndpointID: view.ID}, events[0].Data[0])

	_, err = f.svc.SetEnabled(ctx, "ghost", true)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestWebhookService_DeliveriesAndLastDeliverySummary(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	view := f.createEndpoint(t, gitLabInput())

	now := time.Now().UTC()
	require.NoError(t, f.st.Webhooks().AppendDelivery(ctx, &store.WebhookDelivery{
		EndpointID: view.ID, AttemptedAt: now.Add(-time.Minute), Status: 202,
		Outcome: store.WebhookOutcomeAccepted, SessionID: "sess-1", Via: "gitlab",
	}))
	require.NoError(t, f.st.Webhooks().AppendDelivery(ctx, &store.WebhookDelivery{
		EndpointID: view.ID, AttemptedAt: now, Status: 401,
		Outcome: store.WebhookOutcomeAuthFailed, Via: "gitlab", Note: "signature mismatch",
	}))

	rows, err := f.svc.Deliveries(ctx, view.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, store.WebhookOutcomeAuthFailed, rows[0].Outcome, "newest first")
	require.Equal(t, 401, rows[0].Status)
	require.Equal(t, "signature mismatch", rows[0].Note)
	require.Equal(t, store.WebhookOutcomeAccepted, rows[1].Outcome)
	require.Equal(t, "sess-1", rows[1].SessionID)

	// The endpoint view summarizes the newest row — the list's last-delivery
	// column.
	fresh, err := f.svc.GetEndpoint(ctx, view.ID)
	require.NoError(t, err)
	require.Equal(t, store.WebhookOutcomeAuthFailed, fresh.LastOutcome)
	require.Equal(t, 401, fresh.LastStatus)
	require.Equal(t, now, fresh.LastAttemptedAt)

	// Nothing delivered yet: the summary stays zero-valued.
	other := f.createEndpoint(t, func() WebhookEndpointInput {
		in := gitLabInput()
		in.Name = "Other"
		return in
	}())
	require.Empty(t, other.LastOutcome)
	require.Zero(t, other.LastStatus)
	require.True(t, other.LastAttemptedAt.IsZero())
}

func TestWebhookService_SetListenerAddress(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	kicks := make(chan string, 8)
	f.svc.OnAddrChange = func(addr string) { kicks <- addr }

	// The address persists on the gateway settings row, next to the other
	// gateway configuration.
	require.NoError(t, f.st.GatewaySettings().Save(ctx, &store.GatewaySettings{Enabled: true, DefaultAgentID: "agt-1"}))
	require.NoError(t, f.svc.SetListenerAddress(ctx, "0.0.0.0:9000"))

	saved, err := f.st.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9000", saved.WebhookAddr)
	require.True(t, saved.Enabled, "the other settings columns survive the save")
	require.Equal(t, "agt-1", saved.DefaultAgentID)

	// The runtime rebind kicked, applying without a restart.
	require.Equal(t, "0.0.0.0:9000", awaitAddrKick(t, kicks))

	// A cleared address stores empty (unset = default) and kicks the
	// resolved loopback default, so the listener does not keep the old bind.
	require.NoError(t, f.svc.SetListenerAddress(ctx, ""))
	saved, err = f.st.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.Empty(t, saved.WebhookAddr)
	require.Equal(t, webhooks.DefaultAddr, awaitAddrKick(t, kicks))
}

// awaitAddrKick reads one OnAddrChange kick or fails the test.
func awaitAddrKick(t *testing.T, kicks <-chan string) string {
	t.Helper()
	select {
	case addr := <-kicks:
		return addr
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the reconfigure kick")
		return ""
	}
}

func TestWebhookService_URLAndListenerState(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	// Unset address: the generated URL rides the loopback default.
	view := f.createEndpoint(t, gitLabInput())
	require.True(t, strings.HasPrefix(view.URL, "http://"+webhooks.DefaultAddr+"/webhooks/"+view.ID),
		"the URL is the delivery route on the configured address, got %q", view.URL)

	// A configured address is used verbatim.
	require.NoError(t, f.st.GatewaySettings().Save(ctx, &store.GatewaySettings{WebhookAddr: "10.0.0.5:9000"}))
	withAddr, err := f.svc.GetEndpoint(ctx, view.ID)
	require.NoError(t, err)
	require.Contains(t, withAddr.URL, "http://10.0.0.5:9000/webhooks/")

	// A path-token verifier appends its secret segment — the credential is
	// part of the URL by design (it is the only place it is surfaced).
	pathInput := func() WebhookEndpointInput {
		in := gitLabInput()
		in.Name = "Bare sender"
		in.Verifiers = []WebhookVerifierInput{{Kind: store.WebhookVerifierPath, Secret: "tok-1234"}}
		return in
	}
	pathView := f.createEndpoint(t, pathInput())
	require.True(t, strings.HasSuffix(pathView.URL, "/webhooks/"+pathView.ID+"/tok-1234"))

	// Listener state passes through the runtime seam verbatim.
	f.rt.state = webhooks.ListenerState{Serving: true, BoundAddr: "127.0.0.1:8787"}
	state := f.svc.ListenerState()
	require.True(t, state.Serving)
	require.Equal(t, "127.0.0.1:8787", state.BoundAddr)

	f.rt.state = webhooks.ListenerState{BindError: "address already in use"}
	state = f.svc.ListenerState()
	require.False(t, state.Serving)
	require.Equal(t, "address already in use", state.BindError)
}

func TestWebhookService_Test(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	view := f.createEndpoint(t, gitLabInput())
	before := len(f.emitter.recorded())

	require.NoError(t, f.svc.Test(ctx, view.ID))
	require.Equal(t, view.ID, <-f.rt.tested)
	// The completed self-test refreshes the deliveries view.
	drainEvents(t, f.emitter, before, 1)

	f.rt.testErr = errServiceInject
	err := f.svc.Test(ctx, view.ID)
	require.ErrorIs(t, err, errServiceInject, "a failed test surfaces its error")
}

// errServiceInject is the marker error the fake runtime returns.
var errServiceInject = errors.New("injected test failure")

func TestWebhookService_Presets(t *testing.T) {
	f := newWebhookFixture(t)

	presets := f.svc.Presets()
	require.Len(t, presets, 8, "the catalog mirrors the backend presets")
	byName := map[string]WebhookPresetView{}
	for _, p := range presets {
		byName[p.Name] = p
	}
	gitlab := byName["gitlab"]
	require.Equal(t, "GitLab", gitlab.Title)
	require.NotEmpty(t, gitlab.Hint, "the wizard renders the where-to-find-it hint")
	require.Len(t, gitlab.Verifiers, 1)
	require.False(t, gitlab.Verifiers[0].SecretSet, "presets seed no secrets")
	require.Equal(t, "gitlab", gitlab.Verifiers[0].Preset)

	// The preset cards carry only non-secret configuration.
	blob, err := json.Marshal(presets)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "Secret\":", "no secret field rides the preset view")
}
