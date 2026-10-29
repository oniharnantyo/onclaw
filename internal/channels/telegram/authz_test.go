package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// failingChans wraps a ChannelStore whose allowlist listing always fails —
// the fail-closed table's lookup-error branch.
type failingChans struct {
	store.ChannelStore
}

func (f failingChans) ListChannelUsers(context.Context) ([]store.ChannelUser, error) {
	return nil, errTestRejected
}

// TestAuthorizationTable walks every design-D4 branch. Each case runs one
// message through handleMessage and asserts what the pipeline did.
func TestAuthorizationTable(t *testing.T) {
	allowlisted := int64(42)
	stranger := int64(77)

	newSvc := func(t *testing.T) *fixture {
		return newFixture(t)
	}
	failClosed := func(t *testing.T) *fixture {
		fx := newFixture(t)
		fx.svc.chans = failingChans{fx.st.Channels()}
		return fx
	}

	tests := []struct {
		name        string
		make        func(t *testing.T) *fixture
		update      botapi.Update
		wantSubmit  bool
		wantPairing bool
		wantIDReply bool
	}{
		{
			name:       "dm allowlisted sender dispatches",
			make:       newSvc,
			update:     dmUpdate(1, allowlisted, "hello"),
			wantSubmit: true,
		},
		{
			name:        "dm unknown sender gets id reply and pairing",
			make:        newSvc,
			update:      dmUpdate(1, stranger, "hi"),
			wantIDReply: true,
			wantPairing: true,
		},
		{
			name:   "dm lookup failure fails closed",
			make:   failClosed,
			update: dmUpdate(1, allowlisted, "hello"),
		},
		{
			name:   "group not enrolled is ignored",
			make:   newSvc,
			update: groupUpdate(1, allowlisted, "@buddies_bot run"),
		},
		{
			name: "group non-allowlisted sender is ignored",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, false)
				return fx
			},
			update: groupUpdate(1, stranger, "@buddies_bot run"),
		},
		{
			name: "group mention dispatches",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, false)
				return fx
			},
			update:     groupUpdate(1, allowlisted, "@buddies_bot run the numbers"),
			wantSubmit: true,
		},
		{
			name: "group unaddressed chatter with free_response off is ignored",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, false)
				return fx
			},
			update: groupUpdate(1, allowlisted, "casual chatter"),
		},
		{
			name: "group unaddressed chatter with free_response on dispatches",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, true)
				return fx
			},
			update:     groupUpdate(1, allowlisted, "casual chatter"),
			wantSubmit: true,
		},
		{
			name: "group foreign-bot mention is ignored",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, true) // even free_response cannot override the foreign-bot filter
				return fx
			},
			update: groupUpdate(1, allowlisted, "hey @other_bot do a thing"),
		},
		{
			name: "group foreign-bot command is ignored",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, true)
				return fx
			},
			update: groupUpdate(1, allowlisted, "/run@other_bot now"),
		},
		{
			name: "group reply to the bot dispatches",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, false)
				return fx
			},
			update: func() botapi.Update {
				u := groupUpdate(1, allowlisted, "thanks, one more thing")
				u.Message.ReplyToMessage = &botapi.Message{
					MessageID: 999,
					From:      &botapi.User{ID: 900, Username: "buddies_bot"},
				}
				return u
			}(),
			wantSubmit: true,
		},
		{
			name: "group bot-suffixed command passes as text",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, false)
				return fx
			},
			update:     groupUpdate(1, allowlisted, "/run@buddies_bot report"),
			wantSubmit: true,
		},
		{
			name: "group status command resolves without a turn",
			make: func(t *testing.T) *fixture {
				fx := newFixture(t)
				seedGroup(t, fx, false)
				return fx
			},
			update:     groupUpdate(1, allowlisted, "/status@buddies_bot"),
			wantSubmit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := tt.make(t)
			fx.svc.handleMessage(context.Background(), tt.update.Message)
			time.Sleep(60 * time.Millisecond) // debounce + async pairing reply

			submits := fx.rt.submittedReqs()
			if tt.wantSubmit {
				require.Len(t, submits, 1, "expected one dispatched turn")
				require.Equal(t, "telegram", submits[0].Origin)
			} else {
				require.Empty(t, submits, "expected no dispatch")
			}

			if tt.wantPairing {
				pairings, err := fx.st.Channels().ListPendingPairings(context.Background())
				require.NoError(t, err)
				require.Len(t, pairings, 1)
				require.Equal(t, stranger, pairings[0].TelegramUserID)
			}
			if tt.wantIDReply {
				sends := fx.bot.callsOf("sendMessage")
				require.NotEmpty(t, sends)
				require.True(t, strings.Contains(sends[0].Text, "77"), "the reply must carry the numeric id")
			}
		})
	}
}

// TestPairingOnePerUserAndCap pins the pairing constraints (design D4): one
// pending pairing per user, total cap of 5.
func TestPairingOnePerUserAndCap(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	// Two messages from the same unknown user: one pairing.
	fx.svc.handleMessage(ctx, dmUpdate(1, 77, "a").Message)
	fx.svc.handleMessage(ctx, dmUpdate(2, 77, "b").Message)
	time.Sleep(40 * time.Millisecond)
	pairings, err := fx.st.Channels().ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Len(t, pairings, 1)

	// With 77 already pending, fill the cap to 5, then a sixth requester
	// is dropped.
	for i := int64(101); i <= 104; i++ {
		require.NoError(t, fx.st.Channels().AddChannelPairing(ctx, store.ChannelPairing{TelegramUserID: i}))
	}
	fx.svc.handleMessage(ctx, dmUpdate(3, 106, "me too").Message)
	time.Sleep(40 * time.Millisecond)
	pairings, err = fx.st.Channels().ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Len(t, pairings, 5, "the pairing cap must hold")

	// The id reply still went out to the sixth requester (they must learn
	// their id even when the pairing cannot be recorded).
	require.NotEmpty(t, fx.bot.callsOf("sendMessage"))
}

// TestGroupFailClosed pins the group lookup-error branch: a store failure
// denies even for an allowlisted sender.
func TestGroupFailClosed(t *testing.T) {
	fx := newFixture(t)
	seedGroup(t, fx, true)
	fx.svc.chans = failingChans{fx.st.Channels()}

	fx.svc.handleMessage(context.Background(), groupUpdate(1, 42, "@buddies_bot run").Message)
	time.Sleep(60 * time.Millisecond)
	require.Empty(t, fx.rt.submittedReqs())
}
