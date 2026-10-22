package fake

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/store"
)

type sessionEventStore struct{ s *Store }

func (e sessionEventStore) AppendEvents(_ context.Context, sessionID string, events []store.SessionEvent) error {
	if len(events) == 0 {
		return nil
	}

	e.s.mu.Lock()
	defer e.s.mu.Unlock()

	log := e.s.sessionEvents[sessionID]
	known := make(map[string]struct{}, len(log))
	for _, ev := range log {
		known[ev.EventID] = struct{}{}
	}

	nextSeq := int64(1)
	if len(log) > 0 {
		nextSeq = log[len(log)-1].Seq + 1
	}
	for i := range events {
		if _, dup := known[events[i].EventID]; dup {
			continue
		}
		known[events[i].EventID] = struct{}{}
		ev := cloneEvent(events[i])
		ev.SessionID = sessionID
		ev.Seq = nextSeq
		nextSeq++
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = nowUTC()
		}
		log = append(log, ev)
	}
	e.s.sessionEvents[sessionID] = log
	return nil
}

func (e sessionEventStore) LoadEventsPage(_ context.Context, sessionID string, beforeSeq int64, limit int) ([]store.SessionEvent, error) {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()

	log := e.s.sessionEvents[sessionID]
	// Newest window first (seq < beforeSeq when paging backward), replayed
	// in ascending order.
	upper := int64(0)
	if len(log) > 0 {
		upper = log[len(log)-1].Seq + 1
	}
	if beforeSeq > 0 {
		upper = beforeSeq
	}
	lower := upper - int64(limit)
	if lower < 0 {
		lower = 0
	}
	out := make([]store.SessionEvent, 0)
	for _, ev := range log {
		if ev.Seq >= lower && ev.Seq < upper {
			out = append(out, cloneEvent(ev))
		}
	}
	return out, nil
}

func (e sessionEventStore) DeleteAll(_ context.Context, sessionID string) error {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()

	delete(e.s.sessionEvents, sessionID)
	return nil
}

func (e sessionEventStore) LoadEvents(_ context.Context, sessionID string) ([]store.SessionEvent, error) {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()

	log := e.s.sessionEvents[sessionID]
	out := make([]store.SessionEvent, len(log))
	copy(out, log)
	return out, nil
}
