package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/require"
)

// approvalTap builds an inline-keyboard tap update.
func approvalTap(id int64, userID int64, sessionID, interruptID, decision string) *botapi.CallbackQuery {
	return &botapi.CallbackQuery{
		ID:   "cbq-1",
		From: &botapi.User{ID: userID, FirstName: "Alice"},
		Data: "appr:" + sessionID + ":" + interruptID + ":" + decision,
	}
}

// TestApprovalCardSent pins design D12: an approval_required event on the
// channel's turn sends the command card with both decisions' callback data.
func TestApprovalCardSent(t *testing.T) {
	fx := newFixture(t)
	sess := startTurn(t, fx, "clean the tmp dir")

	fx.rt.emit(sess, fx.rt.approvalRequired(sess, "int-1", "rm -rf /tmp/scratch"))
	eventually(t, "the approval card", func() bool {
		for _, c := range fx.bot.callsOf("sendMessage") {
			if strings.Contains(c.Markup, "inline_keyboard") {
				return true
			}
		}
		return false
	})

	var markup string
	for _, c := range fx.bot.callsOf("sendMessage") {
		if strings.Contains(c.Markup, "inline_keyboard") {
			markup = c.Markup
		}
	}
	require.Contains(t, markup, "appr:"+sess+":int-1:approve")
	require.Contains(t, markup, "appr:"+sess+":int-1:deny")
	require.Contains(t, sentTexts(fx)[len(sentTexts(fx))-1], "rm -rf /tmp/scratch")
}

// TestApprovalTaps pins the tap path: allowlisted taps resolve through the
// runtime and answer the callback; stale taps change nothing.
func TestApprovalTaps(t *testing.T) {
	newStarted := func(t *testing.T) (*fixture, string) {
		fx := newFixture(t)
		sess := startTurn(t, fx, "dangerous thing")
		return fx, sess
	}

	t.Run("approve", func(t *testing.T) {
		fx, sess := newStarted(t)
		fx.rt.mu.Lock()
		fx.rt.pendingApprovals[sess+":int-1"] = true
		fx.rt.mu.Unlock()

		fx.svc.handleCallback(context.Background(), approvalTap(2, 42, sess, "int-1", "approve"))
		require.Len(t, fx.rt.approvals, 1)
		require.True(t, fx.rt.approvals[0].Approved)
		require.Equal(t, sess, fx.rt.approvals[0].SessionID)
		require.Len(t, fx.bot.callsOf("answerCallbackQuery"), 1, "the tap is acknowledged")
	})

	t.Run("deny", func(t *testing.T) {
		fx, sess := newStarted(t)
		fx.rt.mu.Lock()
		fx.rt.pendingApprovals[sess+":int-1"] = true
		fx.rt.mu.Unlock()

		fx.svc.handleCallback(context.Background(), approvalTap(2, 42, sess, "int-1", "deny"))
		require.Len(t, fx.rt.approvals, 1)
		require.False(t, fx.rt.approvals[0].Approved)
	})

	t.Run("unauthorized tap resolves nothing", func(t *testing.T) {
		fx, sess := newStarted(t)
		fx.rt.mu.Lock()
		fx.rt.pendingApprovals[sess+":int-1"] = true
		fx.rt.mu.Unlock()

		fx.svc.handleCallback(context.Background(), approvalTap(2, 77, sess, "int-1", "approve"))
		require.Empty(t, fx.rt.approvals, "the stranger's tap never resolves")
		require.Len(t, fx.bot.callsOf("answerCallbackQuery"), 1, "still answered")
	})

	t.Run("stale tap answers and changes nothing", func(t *testing.T) {
		fx, sess := newStarted(t)
		// Nothing pending: ResolveApproval reports ErrApprovalNotPending.
		fx.svc.handleCallback(context.Background(), approvalTap(2, 42, sess, "int-9", "approve"))
		require.Len(t, fx.rt.approvals, 1, "the attempt is recorded but refused")
		require.Len(t, fx.bot.callsOf("answerCallbackQuery"), 1)

		// The interrupt stays resolvable by a legitimate later tap.
		fx.rt.mu.Lock()
		fx.rt.pendingApprovals[sess+":int-1"] = true
		fx.rt.mu.Unlock()
		fx.svc.handleCallback(context.Background(), approvalTap(3, 42, sess, "int-1", "approve"))
		require.True(t, fx.rt.approvals[1].Approved, "the stale tap changed no state")
	})

	t.Run("lookup failure fails closed", func(t *testing.T) {
		fx, sess := newStarted(t)
		fx.svc.chans = failingChans{fx.st.Channels()}
		fx.rt.mu.Lock()
		fx.rt.pendingApprovals[sess+":int-1"] = true
		fx.rt.mu.Unlock()

		fx.svc.handleCallback(context.Background(), approvalTap(2, 42, sess, "int-1", "approve"))
		require.Empty(t, fx.rt.approvals, "an untrustworthy lookup never resolves")
	})
}

// TestArtifactDelivery pins design D12's artifact half: turn_completed
// artifacts go out via sendDocument under the caps.
func TestArtifactDelivery(t *testing.T) {
	t.Run("sent", func(t *testing.T) {
		fx := newFixture(t)
		fx.arts.files["artifacts/report.md"] = []byte("# report")
		sess := startTurn(t, fx, "make a report")

		fx.rt.emit(sess, fx.rt.assistant(sess, "done"))
		fx.rt.emit(sess, fx.rt.completed(sess,
			sessions.Artifact{Path: "artifacts/report.md", Size: 9},
		))
		fx.rt.endTurn(sess)

		eventually(t, "the document to send", func() bool { return len(fx.bot.callsOf("sendDocument")) == 1 })
		require.Equal(t, "report.md", fx.bot.callsOf("sendDocument")[0].Document)
	})

	t.Run("oversize skipped with a note", func(t *testing.T) {
		fx := newFixture(t)
		sess := startTurn(t, fx, "big output")

		fx.rt.emit(sess, fx.rt.assistant(sess, "done"))
		fx.rt.emit(sess, fx.rt.completed(sess,
			sessions.Artifact{Path: "artifacts/huge.bin", Size: MaxArtifactBytes + 1},
			sessions.Artifact{Path: "artifacts/small.txt", Size: 10},
		))
		fx.rt.endTurn(sess)

		eventually(t, "the small artifact to send", func() bool {
			return len(fx.bot.callsOf("sendDocument")) == 1
		})
		require.Equal(t, "small.txt", fx.bot.callsOf("sendDocument")[0].Document)
		eventually(t, "the oversize note", func() bool {
			for _, c := range fx.bot.callsOf("sendMessage") {
				if strings.Contains(c.Text, "Skipped artifacts/huge.bin") {
					return true
				}
			}
			return false
		})
	})

	t.Run("capped at ten", func(t *testing.T) {
		fx := newFixture(t)
		sess := startTurn(t, fx, "many files")

		var list []sessions.Artifact
		for i := 0; i < MaxArtifactsPerTurn+2; i++ {
			list = append(list, sessions.Artifact{Path: artifactPath(i), Size: 5})
		}
		fx.rt.emit(sess, fx.rt.assistant(sess, "done"))
		fx.rt.emit(sess, fx.rt.completed(sess, list...))
		fx.rt.endTurn(sess)

		eventually(t, "ten documents", func() bool {
			return len(fx.bot.callsOf("sendDocument")) == MaxArtifactsPerTurn
		})
		eventually(t, "the cap note", func() bool {
			for _, c := range fx.bot.callsOf("sendMessage") {
				if strings.Contains(c.Text, "per-turn cap") {
					return true
				}
			}
			return false
		})
	})
}

func artifactPath(i int) string { return "artifacts/f" + strings.Repeat("i", i+1) }
