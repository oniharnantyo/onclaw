package agents

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The regenerate tests (task 7.1, design D7, spec chat-view "Regenerate and
// branch variants"): a regeneration is a real turn — the original prompt is
// re-sent exactly once in the log, the reply events carry the variant_of
// marker, and a failed regeneration leaves the previous reply intact.

// firstUserTurn runs one scripted turn and returns its turn id.
func firstUserTurn(t *testing.T, f *fixture, sess sessions.Session, message string) string {
	t.Helper()
	turn, err := f.mgr.Send(t.Context(), sess.ID, message)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	return turn.TurnID()
}

// TestRegenerate_RunsANewTurnOnTheOriginalPrompt pins the shape: exactly one
// user message stays in the log, the new turn's events carry the variant_of
// marker, and the model re-received the original prompt (without the old
// answer in context).
func TestRegenerate_RunsANewTurnOnTheOriginalPrompt(t *testing.T) {
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		return fakeReply{text: "answer", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)
	originalTurn := firstUserTurn(t, f, sess, "the original question")

	newTurn, err := f.mgr.Regenerate(t.Context(), sess.ID, originalTurn)
	require.NoError(t, err)
	require.NotEqual(t, originalTurn, newTurn.TurnID(), "a regeneration is a new turn")
	require.NoError(t, newTurn.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)

	// Exactly one user message: the duplicate prompt was suppressed.
	users := eventsOfKind(events, sessions.KindUserMessage)
	require.Len(t, users, 1)
	require.Equal(t, "the original question", users[0].Text)
	require.Empty(t, users[0].VariantOf, "the original prompt carries no marker")

	// The new turn's reply events group under the marker.
	marked := 0
	for _, ev := range events {
		if ev.TurnID == newTurn.TurnID() {
			require.Equal(t, originalTurn, ev.VariantOf, "every reply event carries variant_of")
			marked++
		}
	}
	require.GreaterOrEqual(t, marked, 2, "the reply (assistant message + completion) is marked")

	// The model re-received the original prompt as fresh input, and the
	// replayed context cut before the old answer.
	input := f.model.calls()[1]
	require.Equal(t, schema.User, input[len(input)-1].Role)
	require.Equal(t, "the original question", input[len(input)-1].Content)
	for _, msg := range input {
		require.NotContains(t, msg.Content, "answer", "the old answer is not in the fresh attempt's context")
	}
}

// TestRegenerate_FailureKeepsThePreviousReply pins the failure path (spec:
// a regeneration that fails leaves the previous alternative intact): the
// turn_error carries the marker and the previous assistant message stands
// untouched.
func TestRegenerate_FailureKeepsThePreviousReply(t *testing.T) {
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return fakeReply{text: "the good answer", promptTokens: 10, completionTokens: 2}
		}
		return fakeReply{err: errString("provider exploded mid-regeneration")}
	})
	sess := f.newSession(t)
	originalTurn := firstUserTurn(t, f, sess, "ask me anything")

	regen, err := f.mgr.Regenerate(t.Context(), sess.ID, originalTurn)
	require.NoError(t, err)
	require.Error(t, regen.Wait(), "the failed regeneration reports its error")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)

	// The previous reply is intact: exactly one assistant message, unmarked.
	answers := eventsOfKind(events, sessions.KindAssistantMessage)
	require.Len(t, answers, 1)
	require.Equal(t, "the good answer", answers[0].Text)
	require.Empty(t, answers[0].VariantOf)

	// The turn_error is persisted and carries the marker so the UI can drop
	// the empty variant without orphaning the previous reply.
	var sawMarkedError bool
	for _, ev := range eventsOfKind(events, sessions.KindTurnError) {
		if ev.TurnID == regen.TurnID() {
			require.Equal(t, originalTurn, ev.VariantOf)
			require.Contains(t, ev.Error, "provider exploded")
			sawMarkedError = true
		}
	}
	require.True(t, sawMarkedError, "the failed regeneration emits its turn error")

	// Still exactly one user message.
	require.Len(t, eventsOfKind(events, sessions.KindUserMessage), 1)
}

// TestRegenerate_ReSendsStoredAttachments pins the attachment re-send (task
// 5.3): the regeneration re-sends the original turn's stored content from
// disk under the same capability key — never a re-upload.
func TestRegenerate_ReSendsStoredAttachments(t *testing.T) {
	f, atts := newAttachmentFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "seen", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)
	att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "look", att.Key)
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	regen, err := f.mgr.Regenerate(t.Context(), sess.ID, turn.TurnID())
	require.NoError(t, err)
	require.NoError(t, regen.Wait())

	// The fresh input carried the same stored content, under the original
	// key — a re-upload would have minted a new capability key.
	input := f.model.calls()[1]
	last := input[len(input)-1]
	var sawStoredImage bool
	for _, part := range last.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeImageURL &&
			part.Image != nil && part.Image.Base64Data != nil &&
			decodeB64(t, *part.Image.Base64Data) == "png-bytes" {
			sawStoredImage = true
		}
	}
	require.True(t, sawStoredImage, "the regenerated input carries the stored image content")

	// The key still resolves to the same metadata: nothing replaced it.
	resolved, err := atts.Resolve(t.Context(), att.Key)
	require.NoError(t, err)
	require.Equal(t, att, resolved)

	// The marker rides the regenerated turn's events.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, ev := range events {
		if ev.TurnID == regen.TurnID() {
			require.Equal(t, turn.TurnID(), ev.VariantOf)
		}
	}
}

// TestRegenerate_CancelledRegenerationKeepsTheMarker pins the marker on the
// cancelled terminal: a regeneration cancelled mid-stream persists its
// turn_cancelled with the variant_of marker, like every other reply event.
func TestRegenerate_CancelledRegenerationKeepsTheMarker(t *testing.T) {
	shortenEscalation(t)
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return fakeReply{text: "the first answer", promptTokens: 5, completionTokens: 1}
		}
		return fakeReply{text: "one two three"}
	})
	sess := f.newSession(t)
	originalTurn := firstUserTurn(t, f, sess, "ask again")

	// The regeneration pauses mid-stream like a provider stream; the cancel
	// must tear it down from there.
	gate := make(chan struct{})
	f.model.midStream = func() { <-gate }

	regen, err := f.mgr.Regenerate(t.Context(), sess.ID, originalTurn)
	require.NoError(t, err)

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, regen.Wait(), ErrTurnCancelled)
	close(gate)

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	var sawMarkedCancel bool
	for _, ev := range events {
		if ev.TurnID == regen.TurnID() && ev.Kind == sessions.KindTurnCancelled {
			require.Equal(t, originalTurn, ev.VariantOf, "the cancelled terminal carries the marker")
			sawMarkedCancel = true
		}
	}
	require.True(t, sawMarkedCancel, "the cancelled regeneration's terminal is on the log")
}

// TestRegenerate_Refusals pins the guards: an unknown turn id fails, a busy
// session fails with ErrTurnInProgress, and a user-message-less turn id
// fails.
func TestRegenerate_Refusals(t *testing.T) {
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok"}
	})
	f.model.midStream = func() { <-gate }
	sess := f.newSession(t)

	_, err := f.mgr.Regenerate(t.Context(), sess.ID, "no-such-turn")
	require.Error(t, err, "an unknown turn id fails")

	// While a turn is in flight the session is busy.
	turn, err := f.mgr.Send(t.Context(), sess.ID, "in flight")
	require.NoError(t, err)
	_, err = f.mgr.Regenerate(t.Context(), sess.ID, "any-turn")
	require.ErrorIs(t, err, ErrTurnInProgress)
	close(gate)
	require.NoError(t, turn.Wait())
}
