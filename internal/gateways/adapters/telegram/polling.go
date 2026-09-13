package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strconv"
	"time"
)

// pollBackoffMax caps the exponential sleep between failed getUpdates calls
// so a flaky network cannot silence a poller forever — it retries forever,
// but never busier than every 30 s.
const pollBackoffMax = 30 * time.Second

// pollLoop is the long-polling ingestion goroutine (design D11): one
// getUpdates hang at a time, offsets advancing over confirmed updates. On
// start the backlog is skipped (offset -1): unconfirmed updates from a
// previous process are dropped — outbound at-least-once is the outbox's
// contract; inbound replays would double-mint runs whose sends already
// happened. Network errors retry with exponential backoff and only exit
// when the context is cancelled.
func (a *Adapter) pollLoop(ctx context.Context) {
	defer a.wg.Done()

	// The priming call uses offset -1 — Telegram's documented skip-backlog
	// trick: it returns only the LAST update, so the poller can confirm past
	// everything unconfirmed from a previous process without processing it.
	// Unconfirmed updates from a previous process are dropped: outbound
	// at-least-once is the outbox's contract; inbound replays would
	// double-mint runs whose sends already happened.
	offset := int64(-1)
	primed := false
	var consecutiveErrors int

	for {
		if ctx.Err() != nil {
			return
		}

		params := url.Values{}
		params.Set("timeout", strconv.Itoa(int(a.pollTimeout.Seconds())))
		params.Set("limit", "100")
		params.Set("offset", strconv.FormatInt(offset, 10))

		raw, err := a.transport.Call(ctx, "getUpdates", params)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			consecutiveErrors++
			sleep := pollBackoff(consecutiveErrors)
			slog.Warn("telegram getUpdates failed", "gateway_id", a.gatewayID,
				"err", err, "retry_in", sleep.String())
			if sleepErr := sleepContext(ctx, sleep); sleepErr != nil {
				return
			}
			continue
		}
		consecutiveErrors = 0

		updates, err := decodeUpdates(raw)
		if err != nil {
			slog.Warn("telegram getUpdates: undecodable batch dropped",
				"gateway_id", a.gatewayID, "err", err)
			// Treat an undecodable batch like an empty one: the next call
			// carries the same offset, so nothing is skipped.
			updates = nil
		}

		if !primed {
			primed = true
			if len(updates) > 0 {
				offset = updates[len(updates)-1].UpdateID + 1
			} else {
				offset = 0
			}
			continue
		}

		for i := range updates {
			upd := &updates[i]
			a.processUpdate(ctx, upd)
			// Confirm everything through the highest update id in the batch.
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}
		}
	}
}

// decodeUpdates decodes the getUpdates result array.
func decodeUpdates(raw json.RawMessage) ([]apiUpdate, error) {
	var updates []apiUpdate
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// pollBackoff doubles the retry sleep per consecutive failure, capped.
func pollBackoff(consecutive int) time.Duration {
	sleep := time.Second
	for i := 1; i < consecutive && sleep < pollBackoffMax; i++ {
		sleep *= 2
	}
	if sleep > pollBackoffMax {
		sleep = pollBackoffMax
	}
	return sleep
}
