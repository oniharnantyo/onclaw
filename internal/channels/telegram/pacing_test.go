package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/stretchr/testify/require"
)

// TestSendsWaitForSlot pins the shared pacing slot (design D8): sequential
// sends to one chat space at least one slot apart.
func TestSendsWaitForSlot(t *testing.T) {
	fx := newFixture(t)
	fx.svc.t.pacingSlot = 60 * time.Millisecond
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, fx.svc.sendNotice(ctx, 42, "ping"))
	}
	sends := fx.bot.callsOf("sendMessage")
	require.Len(t, sends, 3)
	for i := 1; i < len(sends); i++ {
		gap := sends[i].At.Sub(sends[i-1].At)
		require.GreaterOrEqual(t, gap, 50*time.Millisecond, "send %d must wait for the slot", i)
	}
}

// TestInterimEditSkipsWhenBusy pins skip-when-busy (design D8): an interim
// preview edit does not wait for the slot — it is dropped.
func TestInterimEditSkipsWhenBusy(t *testing.T) {
	fx := newFixture(t)
	// Occupy the slot.
	p := fx.svc.pacer(42)
	p.mu.Lock()
	p.next = time.Now().Add(500 * time.Millisecond)
	p.mu.Unlock()

	// A preview already exists (the first push is a send and waits by
	// design; the skip-when-busy rule governs interim edits).
	pmp := &pump{svc: fx.svc, chatID: 42, lastEdit: time.Now().Add(-time.Hour), preview: 5, previewX: "shown"}
	pmp.buf.WriteString("interim state")
	pmp.editPreview()

	require.Empty(t, fx.bot.callsOf("editMessageText"), "the interim edit skips while the slot is busy")
	require.Empty(t, fx.bot.callsOf("sendMessage"))
}

// TestFinalEditNeverGates pins the final-edit rule (design D8): the final
// proceeds immediately even when the slot is busy.
func TestFinalEditNeverGates(t *testing.T) {
	fx := newFixture(t)
	p := fx.svc.pacer(42)
	p.mu.Lock()
	p.next = time.Now().Add(500 * time.Millisecond)
	p.mu.Unlock()

	pmp := &pump{
		svc: fx.svc, chatID: 42,
		preview:  7,
		lastEdit: time.Now(),
		start:    time.Now(),
	}
	began := time.Now()
	pmp.finalize("the final answer")

	require.Len(t, fx.bot.callsOf("editMessageText"), 1, "the final edit goes out ungned")
	require.Less(t, time.Since(began), 300*time.Millisecond, "the final never waits for the slot")
}

// TestFloodInlineRetry pins the ≤FloodInlineMax rule (design D8): a small
// retry_after sleeps inline and retries once, without a cooldown.
func TestFloodInlineRetry(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errSeq["sendMessage"] = []error{
		&botapi.Error{Kind: botapi.KindRateLimited, Description: "flood", RetryAfter: 10 * time.Millisecond},
	}

	require.NoError(t, fx.svc.sendNotice(context.Background(), 42, "hello"))
	require.Len(t, fx.bot.callsOf("sendMessage"), 2, "one attempt plus the inline retry")
	require.False(t, fx.svc.cooling(42), "no cooldown for an inline-sized penalty")
}

// TestFloodCooldownDefersAndRedelivers pins fail-closed (design D8): a large
// retry_after enters a cooldown, refuses subsequent sends locally (parking
// them), and redelivers everything when the cooldown ends.
func TestFloodCooldownDefersAndRedelivers(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errSeq["sendMessage"] = []error{
		&botapi.Error{Kind: botapi.KindRateLimited, Description: "flood", RetryAfter: 100 * time.Millisecond},
	}

	err := fx.svc.sendNotice(context.Background(), 42, "first")
	require.ErrorIs(t, err, ErrDeferred, "the penalized send defers")
	require.True(t, fx.svc.cooling(42))

	// During the cooldown: refused locally, parked for redelivery — the
	// inbound path never sleeps on this.
	err = fx.svc.sendNotice(context.Background(), 42, "second")
	require.ErrorIs(t, err, ErrDeferred)

	eventually(t, "the parked output to redeliver", func() bool {
		sends := fx.bot.callsOf("sendMessage")
		seen := map[string]bool{}
		for _, c := range sends {
			seen[c.Text] = true
		}
		return seen["first"] && seen["second"]
	})
	require.False(t, fx.svc.cooling(42), "the cooldown clears")
}

// TestCooldownSkipsInterimButKeepsFinal pins the pump-side behavior: interim
// preview edits are skipped during a cooldown (not parked); final output
// parks and redelivers.
func TestCooldownSkipsInterimButKeepsFinal(t *testing.T) {
	fx := newFixture(t)
	p := fx.svc.pacer(42)
	fx.svc.enterCooldown(p, 100*time.Millisecond)

	pmp := &pump{svc: fx.svc, chatID: 42, lastEdit: time.Now().Add(-time.Hour), start: time.Now()}
	pmp.buf.WriteString("interim during cooldown")
	pmp.editPreview()
	require.Empty(t, fx.bot.callsOf("sendMessage"), "interim edits skip during cooldown")

	pmp.preview = 5
	pmp.finalize("the final during cooldown")
	eventually(t, "the final to redeliver after the cooldown", func() bool {
		for _, c := range fx.bot.callsOf("editMessageText") {
			if strings.Contains(c.Text, "final during cooldown") {
				return true
			}
		}
		return false
	})
}

// TestMarkdownChunksSequentially pins chunking on the send path: text over
// ChunkLimit goes out as sequential parts through the pipeline.
func TestMarkdownChunksSequentially(t *testing.T) {
	fx := newFixture(t)
	long := strings.Repeat("word ", 2000) // ~10k chars, over 4096 UTF-16 units

	require.NoError(t, fx.svc.sendMarkdown(context.Background(), 42, long, nil))
	sends := fx.bot.callsOf("sendMessage")
	require.Greater(t, len(sends), 1, "the text chunks")
	for i, c := range sends {
		require.LessOrEqual(t, len([]rune(c.Text)), ChunkLimit+16, "part %d within the cap", i)
		require.Contains(t, c.Text, "\\("+string(rune('0'+i+1))+"/", "part %d carries the escaped marker", i)
	}
	for i := 1; i < len(sends); i++ {
		require.GreaterOrEqual(t, sends[i].At.Sub(sends[i-1].At), 1*time.Millisecond, "parts pace sequentially")
	}
}
