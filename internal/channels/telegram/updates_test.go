package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/stretchr/testify/require"
)

// attachAndRun starts the runtime against a Running gateway and waits past
// the cold-boot sync (poll #1) so staged updates hit the regular loop.
func attachAndRun(t *testing.T, fx *fixture) {
	t.Helper()
	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the regular poll loop to start", func() bool { return fx.bot.pollCount() >= 2 })
}

// TestRedeliveryDedupe pins design D9: a Bot API redelivery of an already
// processed update is dispatched once and receipted in the store.
func TestRedeliveryDedupe(t *testing.T) {
	fx := newFixture(t)
	attachAndRun(t, fx)

	fx.bot.push(dmUpdate(1, 42, "once"))
	eventually(t, "the first dispatch", func() bool { return len(fx.rt.submittedReqs()) == 1 })

	// The same update id arrives again (unacked-window redelivery).
	fx.bot.push(dmUpdate(1, 42, "once"))
	time.Sleep(80 * time.Millisecond)
	require.Len(t, fx.rt.submittedReqs(), 1, "the redelivered update must be deduped")

	done, err := fx.st.Channels().HasUpdateReceipt(context.Background(), "buddies_bot", 1)
	require.NoError(t, err)
	require.True(t, done, "the processed update must carry a store receipt")
}

// TestOffsetAcksAfterBatch pins ack-after-processing: the next getUpdates
// carries lastUpdateID+1 only once the batch was admitted.
func TestOffsetAcksAfterBatch(t *testing.T) {
	fx := newFixture(t)
	attachAndRun(t, fx)

	fx.bot.push(dmUpdate(5, 42, "a"), groupUpdate(6, 42, "@buddies_bot b"))
	eventually(t, "the offset to advance past the batch", func() bool {
		return fx.bot.lastOffset() == 7
	})
}

// TestColdBootBacklogDropped pins the cold-boot rule (design D9): the
// process's first poll acks-and-drops the backlog that accumulated while the
// process was down; only later updates dispatch.
func TestColdBootBacklogDropped(t *testing.T) {
	fx := newFixture(t)
	fx.bot.push(dmUpdate(1, 42, "old one"), groupUpdate(2, 42, "@buddies_bot old two"))

	attachAndRun(t, fx)
	time.Sleep(80 * time.Millisecond)

	require.Empty(t, fx.rt.submittedReqs(), "cold-boot backlog must be dropped")
	done, err := fx.st.Channels().HasUpdateReceipt(context.Background(), "buddies_bot", 1)
	require.NoError(t, err)
	require.False(t, done, "dropped backlog must not be receipted as processed")
	require.Equal(t, int64(3), fx.bot.lastOffset(), "the backlog must be acked")
}

// TestHoldQueueCapDropsOldest pins the hold queue bound (design D9): over 64
// held updates the oldest drops.
func TestHoldQueueCapDropsOldest(t *testing.T) {
	fx := newFixture(t)
	attachAndRun(t, fx)

	fx.gw.transition(gateway.StatePaused)
	for i := 1; i <= 70; i++ {
		fx.bot.push(dmUpdate(int64(i), 42, "m"))
	}
	eventually(t, "the hold queue to fill to the cap", func() bool {
		fx.svc.mu.Lock()
		defer fx.svc.mu.Unlock()
		return len(fx.svc.hold) == HoldQueueCap
	})
	fx.svc.mu.Lock()
	first := fx.svc.hold[0].ID
	fx.svc.mu.Unlock()
	require.Equal(t, int64(7), first, "the oldest held updates must drop first")
}
