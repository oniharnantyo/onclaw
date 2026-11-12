package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/groups"
	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// fakeNativeNotifier records the notifications engine's built payloads so
// the completion-announce tests can assert the enqueue result.
type fakeNativeNotifier struct {
	mu       sync.Mutex
	payloads []notifications.Payload
	err      error
}

func (n *fakeNativeNotifier) Send(p notifications.Payload) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.payloads = append(n.payloads, p)
	return nil
}

func (n *fakeNativeNotifier) sent() []notifications.Payload {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]notifications.Payload(nil), n.payloads...)
}

// newTestNotifEngine builds a real notifications engine over the recording
// notifier with an agent resolver keyed by agent id: resolving a session id
// to a title proves the webhook candidate's name key is the agent.
func newTestNotifAgentEngine(agent *fakeNativeNotifier) *notifications.Engine {
	names := map[string]string{"agt-1": "Scout"}
	return notifications.NewEngine(agent, nil, notifications.NameResolvers{
		Agent: func(id string) string { return names[id] },
	})
}

// newWebhookRuns wires the webhook adapter over the fake manager and the
// real sessions service over the fake store.
func newWebhookRuns(t *testing.T) (*webhookRuns, *fakeAgentsManager) {
	t.Helper()
	svc := sessions.NewService(fake.New())
	mgr := &fakeAgentsManager{sessions: svc}
	return &webhookRuns{runtime: mgr, sessions: svc}, mgr
}

// newNotifyingWebhookRuns binds the notifications engine and creates the
// titled webhook fire session the dispatch path's Create produces, returning
// its id.
func newNotifyingWebhookRuns(t *testing.T) (*webhookRuns, *fakeAgentsManager, *fakeNativeNotifier, string) {
	t.Helper()
	runs, mgr := newWebhookRuns(t)
	notifier := &fakeNativeNotifier{}
	runs.notifEngine = newTestNotifAgentEngine(notifier)
	id, err := runs.Create(context.Background(), "agt-1", "CI — 2026-11-09 12:00")
	require.NoError(t, err)
	return runs, mgr, notifier, id
}

// awaitPayload returns the engine's first delivered payload or fails the test
// when none arrives.
func awaitPayload(t *testing.T, notifier *fakeNativeNotifier) notifications.Payload {
	t.Helper()
	require.Eventually(t, func() bool { return len(notifier.sent()) > 0 },
		2*time.Second, 5*time.Millisecond, "the completion notification never arrived")
	return notifier.sent()[0]
}

func TestWebhookRuns_Create_TitlesSessionAndReturnsID(t *testing.T) {
	runs, mgr := newWebhookRuns(t)

	id, err := runs.Create(context.Background(), "agt-1", "CI — 2026-11-09 12:00")
	require.NoError(t, err)
	require.Equal(t, id, mgr.created[0])

	// The title was set BEFORE returning (the receiver builds the URL and
	// answers 202 right after Submit), so the persisted session carries it.
	sess, err := runs.sessions.Session(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "agt-1", sess.AgentID)
	require.Equal(t, "CI — 2026-11-09 12:00", sess.Title)
}

func TestWebhookRuns_Submit_StampsWebhookOrigin(t *testing.T) {
	runs, mgr := newWebhookRuns(t)

	turn, err := runs.Submit(context.Background(), "sess-1", "build finished, deploy it", hooks.OriginWebhook)
	require.NoError(t, err)
	require.NotNil(t, turn)
	require.Len(t, mgr.submits, 1)
	req := mgr.submits[0]
	require.Equal(t, "sess-1", req.SessionID)
	require.Equal(t, "build finished, deploy it", req.Message)
	require.Equal(t, hooks.OriginWebhook, req.Origin, "the receiver's origin rides the send request")
}

func TestWebhookRuns_Submit_WrapsErrors(t *testing.T) {
	runs, mgr := newWebhookRuns(t)
	mgr.submitErr = errInject

	turn, err := runs.Submit(context.Background(), "sess-1", "prompt", hooks.OriginWebhook)
	require.Nil(t, turn)
	require.ErrorIs(t, err, errInject)
	require.Contains(t, err.Error(), fmt.Sprintf("submit webhook fire for session %s", "sess-1"))
}

func TestWebhookRuns_Submit_AnnouncesCompletedFire(t *testing.T) {
	runs, mgr, notifier, id := newNotifyingWebhookRuns(t)
	finish := mgr.armTurn(t, "turn-1")
	require.NoError(t, runs.sessions.Append(context.Background(), id, []sessions.Event{
		{TurnID: "turn-1", Kind: sessions.KindAssistantMessage, Text: "deploy finished"},
	}))

	_, err := runs.Submit(context.Background(), id, "prompt", hooks.OriginWebhook)
	require.NoError(t, err)

	// The turn has not resolved yet, so nothing may enqueue before finish.
	require.Empty(t, notifier.sent())

	finish(nil)

	p := awaitPayload(t, notifier)
	require.Equal(t, notifications.CategoryWebhooks, p.Category)
	require.Equal(t, "Scout", p.Title, "the title resolves through the agent resolver — the name key is the fire's agent")
	require.Equal(t, "completed\ndeploy finished", p.Body)
	require.Equal(t, id, p.ThreadID)

	// The click target is the fire session (spec "Agent-target fire
	// completes").
	route, ok := runs.notifEngine.ResolveRoute(notifications.WebhookNotificationID(id, "turn-1"))
	require.True(t, ok)
	require.Equal(t, notifications.Route{Kind: notifications.RouteSession, ID: id}, route)
}

func TestWebhookRuns_Submit_AnnouncesFailedAndCancelledOutcomes(t *testing.T) {
	t.Run("failed turn announces the failure text as detail", func(t *testing.T) {
		runs, mgr, notifier, id := newNotifyingWebhookRuns(t)
		finish := mgr.armTurn(t, "turn-1")

		_, err := runs.Submit(context.Background(), id, "prompt", hooks.OriginWebhook)
		require.NoError(t, err)
		finish(errInject)

		p := awaitPayload(t, notifier)
		require.Equal(t, "failed", outcomeWord(t, p.Body))
		// No assistant message exists; the terminal error is the only
		// content a failed fire can carry.
		require.Equal(t, "failed\n"+errInject.Error(), p.Body)
	})

	t.Run("cancelled turn announces cancelled", func(t *testing.T) {
		runs, mgr, notifier, id := newNotifyingWebhookRuns(t)
		finish := mgr.armTurn(t, "turn-1")

		_, err := runs.Submit(context.Background(), id, "prompt", hooks.OriginWebhook)
		require.NoError(t, err)
		finish(agents.ErrTurnCancelled)

		p := awaitPayload(t, notifier)
		require.Equal(t, "cancelled", p.Body)
	})
}

// outcomeWord reads the body's leading outcome word (the engine composes
// "<outcome>\n<detail>").
func outcomeWord(t *testing.T, body string) string {
	t.Helper()
	word, _, _ := strings.Cut(body, "\n")
	return word
}

func TestWebhookRuns_Submit_NilEngineIsInert(t *testing.T) {
	runs, mgr := newWebhookRuns(t)
	id, err := runs.Create(context.Background(), "agt-1", "CI — 12:00")
	require.NoError(t, err)
	finish := mgr.armTurn(t, "turn-1")

	_, err = runs.Submit(context.Background(), id, "prompt", hooks.OriginWebhook)
	require.NoError(t, err)
	require.NotPanics(t, func() { finish(nil) }, "no engine bound: resolving the turn must not announce or panic")
}

func TestWebhookRuns_Submit_NotificationFailureIsBestEffort(t *testing.T) {
	runs, mgr, notifier, id := newNotifyingWebhookRuns(t)
	notifier.err = errInject
	finish := mgr.armTurn(t, "turn-1")

	_, err := runs.Submit(context.Background(), id, "prompt", hooks.OriginWebhook)
	require.NoError(t, err)
	require.NotPanics(t, func() { finish(nil) },
		"a failed notification must not affect anything — the delivery was already answered 202")
	require.Empty(t, notifier.sent())
}

// fakeGroupsEvents records the groups service's UI pushes; nothing in the
// group-sender tests asserts on them, but the service requires the seam.
type fakeGroupsEvents struct{}

func (fakeGroupsEvents) GroupChanged() {}

func (fakeGroupsEvents) GroupEvent(string, groups.GroupEvent) {}

// newGroupSender wires the group adapter over the real groups service (the
// fake manager satisfies its Manager seam) and the fake store's transcript.
func newGroupSender(t *testing.T) (groupSender, *fake.Store, *fakeNativeNotifier) {
	t.Helper()
	st := fake.New()
	sessSvc := sessions.NewService(st)
	mgr := &fakeAgentsManager{sessions: sessSvc}
	groupSvc := groups.New(groups.Deps{
		Store:    st,
		Sessions: sessSvc,
		Manager:  mgr,
		Ref:      agents.NewGroupTurnRef(),
		Events:   fakeGroupsEvents{},
	})
	notifier := &fakeNativeNotifier{}
	return groupSender{groups: groupSvc, msgs: st.Groups()}, st, notifier
}

func TestGroupSender_Send_ReturnsAppendedMessageID(t *testing.T) {
	sender, st, notifier := newGroupSender(t)
	ctx := context.Background()

	g, err := sender.groups.CreateGroup(ctx, "Ops", "", "", nil)
	require.NoError(t, err)

	// Seed one earlier transcript row so the snapshot-and-filter path has a
	// before-bound to skip past.
	require.NoError(t, st.Groups().AppendMessage(ctx, &store.AgentGroupMessage{
		GroupID: g.ID, Kind: store.GroupMessageKindChange, AuthorKind: store.GroupAuthorKindCoordinator,
	}))

	messageID, err := sender.Send(ctx, g.ID, "CI failed on main — @Lead please look")
	require.NoError(t, err)
	require.NotEmpty(t, messageID, "the delivery log needs the produced transcript row's id")

	// The id is the one user-authored message row — not the coordinator
	// change line seeded above.
	rows, err := st.Groups().ListMessages(ctx, g.ID, 0, 10)
	require.NoError(t, err)
	var user *store.AgentGroupMessage
	for i := range rows {
		if rows[i].MessageID == messageID {
			user = &rows[i]
		}
	}
	require.NotNil(t, user, "the returned id names a transcript row")
	require.Equal(t, store.GroupMessageKindMessage, user.Kind)
	require.Equal(t, store.GroupAuthorKindUser, user.AuthorKind)

	// Group deliveries raise nothing (spec: they surface in the room).
	require.Empty(t, notifier.sent())
}

func TestGroupSender_Send_VanishedGroupFails(t *testing.T) {
	sender, _, _ := newGroupSender(t)

	_, err := sender.Send(context.Background(), "ghost", "text")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Contains(t, err.Error(), "webhook group delivery")
}
