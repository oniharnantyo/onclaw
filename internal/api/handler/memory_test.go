package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// hFakeStagedWriteStore implements memory.StagedWriteStore for testing.
type hFakeStagedWriteStore struct {
	mu     sync.Mutex
	writes []*memory.StagedWrite
	nextID int64
}

func (f *hFakeStagedWriteStore) StageWrite(_ context.Context, agent, op, target, content string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.writes = append(f.writes, &memory.StagedWrite{
		ID: f.nextID, Agent: agent, Operation: op, Target: target, Content: content, Status: "pending",
	})
	return f.nextID, nil
}

func (f *hFakeStagedWriteStore) ListStaged(_ context.Context, agent string) ([]*memory.StagedWrite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if agent == "" {
		var all []*memory.StagedWrite
		for _, w := range f.writes {
			if w.Status == "pending" {
				all = append(all, w)
			}
		}
		return all, nil
	}
	var filtered []*memory.StagedWrite
	for _, w := range f.writes {
		if w.Agent == agent && w.Status == "pending" {
			filtered = append(filtered, w)
		}
	}
	return filtered, nil
}

func (f *hFakeStagedWriteStore) GetStagedWrite(_ context.Context, id int64) (*memory.StagedWrite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.writes {
		if w.ID == id {
			return w, nil
		}
	}
	return nil, nil
}

func (f *hFakeStagedWriteStore) ApproveWrite(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.writes {
		if w.ID == id {
			w.Status = "approved"
			break
		}
	}
	return nil
}

func (f *hFakeStagedWriteStore) RejectWrite(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.writes {
		if w.ID == id {
			w.Status = "rejected"
			break
		}
	}
	return nil
}

func newHFakeStagedWriteStore() *hFakeStagedWriteStore {
	return &hFakeStagedWriteStore{
		writes: []*memory.StagedWrite{
			{ID: 1, Agent: "agent-a", Operation: "add", Content: "Test memory fact", Status: "pending"},
			{ID: 2, Agent: "agent-b", Operation: "add", Content: "Another fact", Status: "pending"},
		},
		nextID: 2,
	}
}

func TestListDreamSweeps_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/memory/dreams", "")
	w := httptest.NewRecorder()
	f.h.ListDreamSweeps(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp []*memory.DreamSweepRecord
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp == nil {
		t.Error("expected empty array, got nil")
	}
}

func TestListStagedWrites_Success(t *testing.T) {
	f := newHFixture(t)
	f.svc.SetStagedWriteStore(newHFakeStagedWriteStore())

	req := makeReq(http.MethodGet, "/api/memory/staged", "")
	w := httptest.NewRecorder()
	f.h.ListStagedWrites(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp []*memory.StagedWrite
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("expected 2 staged writes, got %d", len(resp))
	}
}

func TestApproveStagedWrite(t *testing.T) {
	f := newHFixture(t)
	sws := newHFakeStagedWriteStore()
	f.svc.SetStagedWriteStore(sws)

	req := makeReq(http.MethodPost, "/api/memory/staged/1/approve", "")
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	f.h.ApproveStagedWrite(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	sw, _ := sws.GetStagedWrite(context.Background(), 1)
	if sw == nil || sw.Status != "approved" {
		t.Errorf("expected staged write 1 to be approved, got status %q", sw.Status)
	}
}

func TestApproveStagedWrite_InvalidID(t *testing.T) {
	f := newHFixture(t)
	f.svc.SetStagedWriteStore(newHFakeStagedWriteStore())

	req := makeReq(http.MethodPost, "/api/memory/staged/abc/approve", "")
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	f.h.ApproveStagedWrite(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRejectStagedWrite(t *testing.T) {
	f := newHFixture(t)
	sws := newHFakeStagedWriteStore()
	f.svc.SetStagedWriteStore(sws)

	req := makeReq(http.MethodPost, "/api/memory/staged/2/reject", "")
	req.SetPathValue("id", "2")
	w := httptest.NewRecorder()
	f.h.RejectStagedWrite(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	sw, _ := sws.GetStagedWrite(context.Background(), 2)
	if sw == nil || sw.Status != "rejected" {
		t.Errorf("expected staged write 2 to be rejected, got status %q", sw.Status)
	}
}

func TestRejectStagedWrite_InvalidID(t *testing.T) {
	f := newHFixture(t)
	f.svc.SetStagedWriteStore(newHFakeStagedWriteStore())

	req := makeReq(http.MethodPost, "/api/memory/staged/-1/reject", "")
	req.SetPathValue("id", "-1")
	w := httptest.NewRecorder()
	f.h.RejectStagedWrite(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
