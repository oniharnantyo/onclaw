package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// newAttentionChatService wires a ChatService with a vault over the given
// fake store, a sessions fake carrying the staged events, and a fake agent
// manager. The vault is returned so tests can assert its rows.
func newAttentionChatService(t *testing.T, st *fake.Store, fakes *fakeSessionsService) (*ChatService, *fakeAgentManager) {
	t.Helper()
	turns := newFakeAgentManager()
	svc := NewChatService(turns, fakes, &recordingEmitter{}, st, nil, nil, WithChatVault(newTestVault(t)))
	return svc, turns
}

// stagePendingSecret returns a sessions fake whose log carries one pending
// secret request (with origin and username) for interrupt "int-1".
func stagePendingSecret(st *fake.Store) *fakeSessionsService {
	return &fakeSessionsService{store: st, events: []sessions.Event{{
		ID:         "ev1",
		TurnID:     "turn1",
		Kind:       sessions.KindInputRequired,
		OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Input: &sessions.Input{
			InterruptID:   "int-1",
			AttentionKind: sessions.AttentionSecret,
			Method:        "password",
			Origin:        "https://app.example.com/login",
			Username:      "ada",
		},
	}}}
}

// TestChatService_ResolveInput_SavesThenResolves proves the save-then-resolve
// ordering: with save-to-vault checked and the pending payload naming an
// origin and username, the runtime receives savedAs=true and the vault row's
// id, addedBy the agent's display name.
func TestChatService_ResolveInput_SavesThenResolves(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	agent := &store.Agent{ID: "agent1", Name: "Scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))
	require.NoError(t, st.Sessions().Create(ctx, &store.Session{ID: "s1", AgentID: agent.ID}))

	svc, turns := newAttentionChatService(t, st, stagePendingSecret(st))

	require.NoError(t, svc.ResolveInput(ctx, "s1", "int-1", "s3cret", "", true))
	require.Len(t, turns.resolvedInputs, 1)
	require.Equal(t, "s1|int-1|s3cret||true|", turns.resolvedInputs[0][:len("s1|int-1|s3cret||true|")])

	// The receipt carries a non-empty saved login id (the vault row landed
	// BEFORE the resolve ran — the delegate saw its id).
	require.Greater(t, len(turns.resolvedInputs[0]), len("s1|int-1|s3cret||true|"))

	// The saved login row: named from the origin host, addedBy the agent.
	logins, err := svc.vault.Logins(ctx)
	require.NoError(t, err)
	require.Len(t, logins, 1)
	require.Equal(t, "app.example.com", logins[0].Name)
	require.Equal(t, "Scout", logins[0].AddedBy)
	require.Equal(t, "https://app.example.com", logins[0].Origin) // NormalizeOrigin strips the path
	require.Equal(t, "ada", logins[0].Username)
}

// TestChatService_ResolveInput_NoSaveWhenUnchecked proves an unchecked
// save-to-vault resolves unsaved: the runtime receives savedAs=false and no
// vault row lands.
func TestChatService_ResolveInput_NoSaveWhenUnchecked(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	require.NoError(t, st.Sessions().Create(ctx, &store.Session{ID: "s1", AgentID: "agent1"}))

	svc, turns := newAttentionChatService(t, st, stagePendingSecret(st))

	require.NoError(t, svc.ResolveInput(ctx, "s1", "int-1", "s3cret", "", false))
	require.Equal(t, []string{"s1|int-1|s3cret||false|"}, turns.resolvedInputs)

	logins, err := svc.vault.Logins(ctx)
	require.NoError(t, err)
	require.Empty(t, logins)
}

// TestChatService_ResolveInput_SaveFailureDoesNotBlock proves the spec's
// non-fatal save: an unwired vault (the save path fails) logs and continues —
// the sign-in still resolves, with savedAs=false.
func TestChatService_ResolveInput_SaveFailureDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	require.NoError(t, st.Sessions().Create(ctx, &store.Session{ID: "s1", AgentID: "agent1"}))

	turns := newFakeAgentManager()
	// The service under test has NO vault wired: the save path fails.
	svc := NewChatService(turns, stagePendingSecret(st), &recordingEmitter{}, st, nil, nil)

	require.NoError(t, svc.ResolveInput(ctx, "s1", "int-1", "s3cret", "", true))
	require.Equal(t, []string{"s1|int-1|s3cret||false|"}, turns.resolvedInputs)
}

// TestChatService_ResolveInput_TakeoverPayloadNeverSaves proves a
// save-to-vault request on a takeover request (no secret payload) resolves
// unsaved — there is nothing to vault.
func TestChatService_ResolveInput_TakeoverPayloadNeverSaves(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	require.NoError(t, st.Sessions().Create(ctx, &store.Session{ID: "s1", AgentID: "agent1"}))

	fakes := &fakeSessionsService{store: fake.New(), events: []sessions.Event{{
		ID:     "ev1",
		Kind:   sessions.KindInputRequired,
		TurnID: "turn1",
		Input: &sessions.Input{
			InterruptID:   "int-1",
			AttentionKind: sessions.AttentionTakeover,
			Origin:        "https://app.example.com/login",
			Username:      "ada",
		},
	}}}

	svc, turns := newAttentionChatService(t, st, fakes)

	require.NoError(t, svc.ResolveInput(ctx, "s1", "int-1", "", "", true))
	require.Equal(t, []string{"s1|int-1|||false|"}, turns.resolvedInputs)

	logins, err := svc.vault.Logins(ctx)
	require.NoError(t, err)
	require.Empty(t, logins)
}

// TestChatService_ResolveInput_StaleInterruptPropagates proves a stale
// interrupt id surfaces agents.ErrInputNotPending.
func TestChatService_ResolveInput_StaleInterruptPropagates(t *testing.T) {
	svc, turns := newAttentionChatService(t, fake.New(), stagePendingSecret(fake.New()))
	turns.resolveInputErr = agents.ErrInputNotPending

	err := svc.ResolveInput(context.Background(), "s1", "int-1", "s3cret", "", false)
	require.ErrorIs(t, err, agents.ErrInputNotPending)
}

// TestChatService_BeginBrowserTakeover_Delegates proves the takeover approval
// reaches the runtime's BeginTakeover with session and interrupt, and runtime
// errors propagate.
func TestChatService_BeginBrowserTakeover_Delegates(t *testing.T) {
	svc, turns := newAttentionChatService(t, fake.New(), stagePendingSecret(fake.New()))

	require.NoError(t, svc.BeginBrowserTakeover(context.Background(), "s1", "int-1"))
	require.Equal(t, []string{"s1|int-1"}, turns.begunTakeovers)

	turns.beginTakeoverErr = agents.ErrInputNotPending
	err := svc.BeginBrowserTakeover(context.Background(), "s1", "int-1")
	require.ErrorIs(t, err, agents.ErrInputNotPending)
}

// TestChatService_ResolveTakeover_Delegates proves the takeover resolution
// reaches the runtime's ResolveAttention with the outcome, and runtime errors
// propagate.
func TestChatService_ResolveTakeover_Delegates(t *testing.T) {
	svc, turns := newAttentionChatService(t, fake.New(), stagePendingSecret(fake.New()))

	require.NoError(t, svc.ResolveTakeover(context.Background(), "s1", "int-1", sessions.InputCompleted))
	require.Equal(t, []string{"s1|int-1|completed"}, turns.resolvedAttention)

	require.NoError(t, svc.ResolveTakeover(context.Background(), "s1", "int-2", sessions.InputCancelled))
	require.Equal(t, []string{"s1|int-1|completed", "s1|int-2|cancelled"}, turns.resolvedAttention)

	turns.resolveAttentionErr = agents.ErrInputNotPending
	err := svc.ResolveTakeover(context.Background(), "s1", "int-1", sessions.InputCompleted)
	require.ErrorIs(t, err, agents.ErrInputNotPending)
}

// TestChatService_CancelInput_Delegates proves the secret card's Cancel
// button reaches the runtime's ResolveAttention with the cancelled outcome,
// and runtime errors propagate.
func TestChatService_CancelInput_Delegates(t *testing.T) {
	svc, turns := newAttentionChatService(t, fake.New(), stagePendingSecret(fake.New()))

	require.NoError(t, svc.CancelInput(context.Background(), "s1", "int-1"))
	require.Equal(t, []string{"s1|int-1|cancelled"}, turns.resolvedAttention)

	turns.resolveAttentionErr = agents.ErrInputNotPending
	err := svc.CancelInput(context.Background(), "s1", "int-1")
	require.ErrorIs(t, err, agents.ErrInputNotPending)
}

// TestChatService_ResolveInput_TypedIdentifierWinsForVault proves the card's
// typed identifier (username or email) overrides the agent-supplied username
// for the vault entry, while the delivery keeps both values untouched.
func TestChatService_ResolveInput_TypedIdentifierWinsForVault(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	agent := &store.Agent{ID: "agent1", Name: "Scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))
	require.NoError(t, st.Sessions().Create(ctx, &store.Session{ID: "s1", AgentID: agent.ID}))

	vaultSvc := newTestVault(t)
	turns := newFakeAgentManager()
	svc := NewChatService(turns, stagePendingSecret(st), &recordingEmitter{}, st, nil, nil, WithChatVault(vaultSvc))

	require.NoError(t, svc.ResolveInput(ctx, "s1", "int-1", "s3cret", "ada@corp.example", true))
	require.Len(t, turns.resolvedInputs, 1)
	// The runtime delivery: (text, identifier, savedAs, savedLoginID…).
	require.True(t, strings.HasPrefix(turns.resolvedInputs[0], "s1|int-1|s3cret|ada@corp.example|true|"),
		"delivery record: %q", turns.resolvedInputs[0])

	logins, err := vaultSvc.Logins(ctx)
	require.NoError(t, err)
	require.Len(t, logins, 1)
	require.Equal(t, "ada@corp.example", logins[0].Username)
	require.Equal(t, "https://app.example.com", logins[0].Origin)
}
