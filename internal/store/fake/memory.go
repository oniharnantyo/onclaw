package fake

import (
	"context"
	"sort"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// memoryNoteKey is the (agent, note name) pair a retention row is keyed by.
type memoryNoteKey struct{ agentID, name string }

// memoryStagingKey is the (agent, source ref) pair a staging row is keyed by.
type memoryStagingKey struct{ agentID, sourceRef string }

type memoryStore struct{ s *Store }

func (m memoryStore) AppendTrace(_ context.Context, t store.MemoryTrace) (store.MemoryTrace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if t.ID == "" {
		t.ID = nextID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = nowUTC()
	}
	if t.Hits == nil {
		t.Hits = []store.MemoryTraceHit{}
	}
	m.s.memoryTraces[t.ID] = cloneTrace(t)
	return cloneTrace(t), nil
}

func (m memoryStore) ListTracesSince(_ context.Context, agentID string, since time.Time) ([]store.MemoryTrace, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	out := make([]store.MemoryTrace, 0)
	for _, t := range m.s.memoryTraces {
		if t.AgentID == agentID && !t.CreatedAt.Before(since) {
			out = append(out, cloneTrace(t))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m memoryStore) UpsertNoteState(_ context.Context, st store.MemoryNoteState) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if st.FirstSeen.IsZero() {
		st.FirstSeen = nowUTC()
	}
	m.s.memoryNotes[memoryNoteKey{st.AgentID, st.Name}] = cloneNoteState(st)
	return nil
}

func (m memoryStore) ListNoteStates(_ context.Context, agentID string) ([]store.MemoryNoteState, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	out := make([]store.MemoryNoteState, 0)
	for key, st := range m.s.memoryNotes {
		if key.agentID == agentID {
			out = append(out, cloneNoteState(st))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m memoryStore) UpsertStaging(_ context.Context, st store.MemoryStaging) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if st.FirstStaged.IsZero() {
		st.FirstStaged = nowUTC()
	}
	m.s.memoryStaging[memoryStagingKey{st.AgentID, st.SourceRef}] = st
	return nil
}

func (m memoryStore) ListStaging(_ context.Context, agentID string) ([]store.MemoryStaging, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	out := make([]store.MemoryStaging, 0)
	for key, st := range m.s.memoryStaging {
		if key.agentID == agentID {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceRef < out[j].SourceRef })
	return out, nil
}

func (m memoryStore) ExpireStaging(_ context.Context, agentID string, before time.Time) (int, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	expired := 0
	for key, st := range m.s.memoryStaging {
		if key.agentID == agentID && st.FirstStaged.Before(before) {
			delete(m.s.memoryStaging, key)
			expired++
		}
	}
	return expired, nil
}

func (m memoryStore) GetSweep(_ context.Context, agentID string) (store.MemorySweep, bool, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	sw, ok := m.s.memorySweeps[agentID]
	if !ok {
		return store.MemorySweep{}, false, nil
	}
	return cloneSweep(sw), true, nil
}

func (m memoryStore) PutSweep(_ context.Context, sw store.MemorySweep) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	m.s.memorySweeps[sw.AgentID] = cloneSweep(sw)
	return nil
}

// deleteMemory removes every memory row of the agent — the cascade on the
// memory foreign keys (add-agent-memory design D1). Callers hold s.mu.
func (s *Store) deleteMemory(agentID string) {
	for id, t := range s.memoryTraces {
		if t.AgentID == agentID {
			delete(s.memoryTraces, id)
		}
	}
	for key := range s.memoryNotes {
		if key.agentID == agentID {
			delete(s.memoryNotes, key)
		}
	}
	for key := range s.memoryStaging {
		if key.agentID == agentID {
			delete(s.memoryStaging, key)
		}
	}
	delete(s.memorySweeps, agentID)
}

// cloneTrace copies a trace value, copying the hits slice so the stored value
// never aliases a caller's slice.
func cloneTrace(in store.MemoryTrace) store.MemoryTrace {
	hits := make([]store.MemoryTraceHit, len(in.Hits))
	copy(hits, in.Hits)
	in.Hits = hits
	return in
}

// cloneNoteState copies a note-state value, normalizing the nullable time to
// UTC through a fresh pointer (mirroring cloneSchedule).
func cloneNoteState(in store.MemoryNoteState) store.MemoryNoteState {
	in.LastRecall = cloneTimePtr(in.LastRecall)
	return in
}

// cloneSweep copies a sweep value, normalizing the nullable times to UTC
// through fresh pointers (mirroring cloneSchedule).
func cloneSweep(in store.MemorySweep) store.MemorySweep {
	in.LastStart = cloneTimePtr(in.LastStart)
	in.LastFinish = cloneTimePtr(in.LastFinish)
	return in
}
