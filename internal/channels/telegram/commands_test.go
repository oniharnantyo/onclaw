package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseCommandTable pins the command grammar: known commands, the group
// @bot suffix, and pass-through for everything else.
func TestParseCommandTable(t *testing.T) {
	tests := []struct {
		text  string
		isCmd bool
		name  string
	}{
		{"/new", true, "new"},
		{"/new@buddies_bot", true, "new"},
		{"/STOP", true, "stop"},
		{"/stop@buddies_bot ", true, "stop"},
		{"/status", true, "status"},
		{"/new@other_bot", false, ""},
		{"hello", false, ""},
		{"/unknown", false, ""},
		{"run /new", false, ""},
		{"@buddies_bot hi", false, ""},
	}
	for _, tt := range tests {
		cmd, ok := parseCommand(tt.text, "buddies_bot")
		require.Equal(t, tt.isCmd, ok, tt.text)
		if tt.isCmd {
			require.Equal(t, tt.name, cmd.name, tt.text)
		}
	}
}

// TestStopCommandBypassesQueue pins /stop: it cancels the in-flight turn
// immediately, without queueing (design D5).
func TestStopCommandBypassesQueue(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "long running"))
	eventually(t, "the turn", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	sess := fx.rt.firstSession()

	fx.svc.handleMessage(context.Background(), dmUpdate(2, 42, "/stop").Message)

	eventually(t, "the cancel", func() bool { return len(fx.rt.cancels) == 1 })
	require.Equal(t, sess, fx.rt.cancels[0])
	// The cancelled turn's notice rides the pump.
	eventually(t, "the stop confirmation", func() bool {
		for _, c := range fx.bot.callsOf("sendMessage") {
			if strings.Contains(c.Text, "Stopped.") {
				return true
			}
		}
		return false
	})
}

// TestStopWithoutSession pins the no-op branch.
func TestStopWithoutSession(t *testing.T) {
	fx := newFixture(t)

	fx.svc.handleMessage(context.Background(), dmUpdate(1, 42, "/stop").Message)
	eventually(t, "the notice", func() bool { return len(fx.bot.callsOf("sendMessage")) == 1 })
	require.Empty(t, fx.rt.cancels)
	require.Contains(t, fx.bot.callsOf("sendMessage")[0].Text, "Nothing is running.")
}

// TestStatusCommand pins /status: agent, pin, model, busy — no turn starts.
func TestStatusCommand(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "hello"))
	eventually(t, "the turn", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	// Wait for the session binding to persist before /status reads it.
	chat, err := fx.st.Channels().GetChannelChat(context.Background(), 42)
	require.NoError(t, err)
	require.NotEmpty(t, chat.SessionID)

	fx.svc.handleMessage(context.Background(), dmUpdate(2, 42, "/status").Message)
	eventually(t, "the status reply", func() bool {
		for _, c := range fx.bot.callsOf("sendMessage") {
			if strings.Contains(c.Text, "Agent:") {
				return true
			}
		}
		return false
	})
	var status string
	for _, c := range fx.bot.callsOf("sendMessage") {
		if strings.Contains(c.Text, "Agent:") {
			status = c.Text
		}
	}
	require.Contains(t, status, "Agent: agent-default")
	require.Contains(t, status, "Pinned: none")
	require.Contains(t, status, "Busy: yes")
	require.Len(t, fx.rt.submittedReqs(), 1, "a command never starts a turn")
}

// TestUnknownCommandPassesThrough pins the pass-through rule: an unknown
// command is ordinary text for the agent.
func TestUnknownCommandPassesThrough(t *testing.T) {
	fx := newFixture(t)

	fx.svc.handleMessage(context.Background(), dmUpdate(1, 42, "/whatever do a thing").Message)
	eventually(t, "the pass-through dispatch", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	require.Equal(t, "/whatever do a thing", fx.rt.submittedReqs()[0].Message)
}
