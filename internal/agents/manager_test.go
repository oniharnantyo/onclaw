package agents

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// TestManager_CreateSession covers session creation: unknown agents are
// rejected, created sessions bind the agent and start untitled (4.1).
func TestManager_CreateSession(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{} })

	_, err := f.mgr.CreateSession(t.Context(), "nope")
	require.ErrorIs(t, err, store.ErrNotFound)

	sess := f.newSession(t)
	require.Equal(t, f.agent.ID, sess.AgentID)
	require.Empty(t, sess.Title)
	require.False(t, sess.CreatedAt.IsZero())
}

// TestManager_Send_MultiTurnContext covers 4.1's core: a turn executes on
// the agent's configured model with the session context assembled (the
// composed persona leads as the system message), and a follow-up turn sees
// the prior turns' messages — deterministic multi-turn behavior driven by
// the fake model. The persona documents are re-read for every turn (design
// D11), so an edit between turns applies to the next one.
func TestManager_Send_MultiTurnContext(t *testing.T) {
	const (
		identity1  = "You are Tester, a precise assistant."
		soul1      = "You value directness."
		bootstrap1 = "Greet the user warmly."
		soul2      = "You value brevity."
	)
	docs := &fakePromptDocs{identity: identity1, soul: soul1, bootstrap: bootstrap1}
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		// Deterministic script: the answer depends on what the model saw.
		for _, msg := range input {
			if msg.Role == schema.User && strings.Contains(msg.Content, "again?") {
				// Only reachable if the prior assistant reply is in context.
				sawPrior := false
				for _, prior := range input {
					if prior.Role == schema.Assistant && prior.Content == "first answer" {
						sawPrior = true
					}
				}
				if sawPrior {
					return fakeReply{text: "you asked before", promptTokens: 40, completionTokens: 5}
				}
				return fakeReply{text: "I do not remember", promptTokens: 40, completionTokens: 5}
			}
		}
		return fakeReply{text: "first answer", promptTokens: 20, completionTokens: 5}
	}, WithPromptDocs(docs))
	sess := f.newSession(t)

	turn1, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn1.Wait())
	require.NotEmpty(t, turn1.TurnID())

	// Turn 1's input: the composed persona leads as the system message —
	// identity, soul and bootstrap in that order (design D12) — followed by
	// the user message alone.
	calls := f.model.calls()
	require.Len(t, calls, 1)
	require.Len(t, calls[0], 2)
	require.Equal(t, schema.System, calls[0][0].Role)
	require.Equal(t, identity1+"\n\n"+soul1+"\n\n"+bootstrap1, calls[0][0].Content)
	require.Equal(t, schema.User, calls[0][1].Role)
	require.Equal(t, "hello", calls[0][1].Content)

	// The user edits SOUL.md between turns; the next turn must see it.
	docs.soul = soul2

	// Turn 2 asks a follow-up that requires turn 1's context.
	turn2, err := f.mgr.Send(t.Context(), sess.ID, "did I say hello again?")
	require.NoError(t, err)
	require.NoError(t, turn2.Wait())

	calls = f.model.calls()
	require.Len(t, calls, 2)
	roles := make([]string, 0, len(calls[1]))
	for _, msg := range calls[1] {
		roles = append(roles, string(msg.Role))
	}
	// Context assembly: [system, user1, assistant1, user2] — the persona is
	// recomposed for the second turn, then the replayed history.
	require.Equal(t, []string{
		string(schema.System), string(schema.User), string(schema.Assistant), string(schema.User),
	}, roles)
	require.Equal(t, identity1+"\n\n"+soul2+"\n\n"+bootstrap1, calls[1][0].Content,
		"the persona is composed fresh per turn")
	require.Equal(t, "hello", calls[1][1].Content)
	require.Equal(t, "first answer", calls[1][2].Content)
	require.Equal(t, "did I say hello again?", calls[1][3].Content)
	require.Equal(t, 2, docs.readCount(), "the documents are re-read every turn, never cached")

	// The persisted transcript carries both turns' durable events.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, "first answer", events[1].Text)
	require.Equal(t, "you asked before", events[4].Text, "the model must have seen the prior turn")
}

// TestManager_Send_MissingDocumentsStillChats covers the spec's degradation
// scenario: an agent whose persona documents are absent (a failed
// generation, an empty workspace) still runs turns — the model is called
// with the bare session context and the transcript records it normally.
func TestManager_Send_MissingDocumentsStillChats(t *testing.T) {
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		// Reachable only when no system persona is composed from the absent
		// documents.
		for _, msg := range input {
			if msg.Role == schema.System {
				return fakeReply{err: errors.New("persona must be absent")}
			}
		}
		return fakeReply{text: "plain answer", promptTokens: 10, completionTokens: 2}
	}, WithPromptDocs(&fakePromptDocs{}))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	calls := f.model.calls()
	require.Len(t, calls, 1)
	require.Len(t, calls[0], 1, "no system message may ride an absent persona")
	require.Equal(t, schema.User, calls[0][0].Role)

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events), "the transcript records the turn normally")
}

// TestManager_Send_DocumentReadErrorFailsTurn pins the read-failure contract:
// a real IO failure on the persona documents omits them entirely and fails
// the turn through the error path — the transcript records the failure after
// the user message and is never corrupted with half-read persona content.
func TestManager_Send_DocumentReadErrorFailsTurn(t *testing.T) {
	boom := errors.New("workspaces: read SOUL.md: disk on fire")
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "unreachable", promptTokens: 1, completionTokens: 1}
	}, WithPromptDocs(&fakePromptDocs{err: boom}))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err, "Send must not fail for a persona read failure")
	require.ErrorIs(t, turn.Wait(), boom)

	// The model was never called: no turn runs on a half-read persona.
	require.Empty(t, f.model.calls())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindTurnError),
	}, kindsOf(events))
	require.Equal(t, boom.Error(), events[1].Error)
}

// TestManager_Send_SessionBusy covers the per-session turn guard: a second
// Send while a turn runs fails with ErrTurnInProgress, and after the turn
// completes the session accepts a new turn immediately.
func TestManager_Send_SessionBusy(t *testing.T) {
	release := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-release
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	first, err := f.mgr.Send(t.Context(), sess.ID, "one")
	require.NoError(t, err)

	_, err = f.mgr.Send(t.Context(), sess.ID, "two")
	require.ErrorIs(t, err, ErrTurnInProgress)

	close(release)
	require.NoError(t, first.Wait())

	// The busy flag clears before Wait unblocks: sending again works.
	second, err := f.mgr.Send(t.Context(), sess.ID, "three")
	require.NoError(t, err)
	require.NoError(t, second.Wait())
}

// TestManager_Send_Validation covers the request-level rejections.
func TestManager_Send_Validation(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{} })
	sess := f.newSession(t)

	_, err := f.mgr.Send(t.Context(), sess.ID, "   ")
	require.Error(t, err, "a blank message is rejected")

	_, err = f.mgr.Send(t.Context(), "nope", "hi")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = f.mgr.Subscribe(t.Context(), "nope")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = f.mgr.ContextUsage(t.Context(), "nope")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestManager_Send_AutoTitle covers the untitled-session rename: the first
// message's first line becomes the title.
func TestManager_Send_AutoTitle(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "help me plan\na trip")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	got, err := f.sessions.Session(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "help me plan", got.Title)
}

// TestManager_Send_ModelFailureRecordsError covers the spec's failure path:
// when the model errors mid-call the turn fails, and the failure is recorded
// as a persisted turn_error after the user message (spec agent-runtime:
// unreachable provider records an error event, chat UI shows the failure).
func TestManager_Send_ModelFailureRecordsError(t *testing.T) {
	boom := errors.New("provider exploded")
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{err: boom}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err, "Send must not fail for provider-side failures")
	require.ErrorIs(t, turn.Wait(), boom)

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindTurnError),
	}, kindsOf(events))
	// The ADK wraps the model failure with its node context; the failure
	// message itself must reach the transcript.
	require.Contains(t, events[1].Error, boom.Error())
	require.NotEmpty(t, events[1].TurnID)
	require.Equal(t, events[0].TurnID, events[1].TurnID, "the failure carries the turn id")
}

// TestManager_Send_SessionOverrideWins covers design D1's resolution: a
// session's stored model and effort overrides reach the model build for the
// turn, replacing the agent's defaults per field.
func TestManager_Send_SessionOverrideWins(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok"}
	})
	sess := f.newSession(t)
	require.NoError(t, f.sessions.SetOverrides(t.Context(), sess.ID, "claude-opus-4-1", "high"))

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.Equal(t, ModelOverride{Model: "claude-opus-4-1", Effort: "high"},
		f.registry.overrideAt(0), "the session row's overrides must win")
}

// TestManager_Send_UnsetOverridesInheritAgentDefaults covers the inherit
// half: an untouched session resolves the agent's stored effort default (the
// model falls back at build time — the override stays empty), and both empty
// mean no override at all.
func TestManager_Send_UnsetOverridesInheritAgentDefaults(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok"}
	})
	agent := f.agent
	agent.Effort = "medium"
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))

	sess := f.newSession(t)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.Equal(t, ModelOverride{Model: "", Effort: "medium"},
		f.registry.overrideAt(0),
		"the agent's effort default applies when the session carries none; the model inherits at build")
}

// TestManager_Send_RejectedEffortFailsTurnWithError covers the spec's
// unsupported-effort scenario against the real registry: a session override
// the effective model does not support fails the turn with a clear
// turn_error event instead of running at some other effort.
func TestManager_Send_RejectedEffortFailsTurnWithError(t *testing.T) {
	fs := fake.New()
	provider := &store.Provider{Name: "openai", Type: ProviderOpenAI, APIKey: "key-test", Enabled: true}
	require.NoError(t, fs.Providers().Create(t.Context(), provider))
	require.NoError(t, fs.ProviderModels().Create(t.Context(), &store.ProviderModel{
		ProviderID:    provider.ID,
		ModelID:       "gpt-4o",
		ContextWindow: 128000,
		Enabled:       true,
	}))
	agent := &store.Agent{Name: "tester", ProviderID: provider.ID, Model: "gpt-4o"}
	require.NoError(t, fs.Agents().Create(t.Context(), agent))

	svc := sessions.NewService(fs)
	// The browser is in the catalog: an open-allowlist turn constructs it.
	m := newManager(fs, svc, newModelRegistry(fs), WithBrowserDir(t.TempDir()))
	sess, err := m.CreateSession(t.Context(), agent.ID)
	require.NoError(t, err)
	// The stored gpt-4o row declares no efforts, so it offers only high
	// (design D3) — the minimal override must be rejected.
	require.NoError(t, svc.SetOverrides(t.Context(), sess.ID, "", "minimal"))

	turn, err := m.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err, "the rejection is the turn's outcome, not Send's")
	require.Error(t, turn.Wait())

	events, err := svc.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindTurnError),
	}, kindsOf(events))
	require.Contains(t, events[1].Error, "reasoning effort")
	require.Contains(t, events[1].Error, "gpt-4o", "the error names the model that cannot honor it")
}
