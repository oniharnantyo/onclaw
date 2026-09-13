package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
)

// FakeTransport is the in-memory Transport for tests (tasks.md 2.2): calls
// are recorded, methods resolve through scriptable handlers, getUpdates
// drains a queued update list honoring the offset, and file downloads come
// from a map. Zero network.
type FakeTransport struct {
	mu sync.Mutex

	// Calls records every Call in order (method + params).
	Calls []FakeCall

	// Handlers maps method → result builder; a method with no handler
	// returns an empty JSON object result. getUpdates has built-in behavior
	// (drains Updates honoring the request offset) unless overridden here.
	Handlers map[string]func(params url.Values) (any, error)

	// Files maps file_path → body for Download.
	Files map[string][]byte

	// Err, when set, fails every subsequent Call (sticky — clear it to
	// resume).
	Err error

	// Updates is the getUpdates queue.
	Updates []apiUpdate
}

// FakeCall is one recorded transport call.
type FakeCall struct {
	Method string
	Params url.Values
}

// NewFakeTransport builds an empty fake.
func NewFakeTransport() *FakeTransport {
	return &FakeTransport{
		Handlers: make(map[string]func(params url.Values) (any, error)),
		Files:    make(map[string][]byte),
	}
}

// Call implements Transport: record, resolve the scripted result, drain the
// getUpdates queue when no handler overrides it.
func (t *FakeTransport) Call(_ context.Context, method string, params url.Values) (json.RawMessage, error) {
	t.mu.Lock()
	t.Calls = append(t.Calls, FakeCall{Method: method, Params: params})
	if t.Err != nil {
		err := t.Err
		t.mu.Unlock()
		return nil, err
	}
	handler, scripted := t.Handlers[method]
	var updates []apiUpdate
	if !scripted && method == "getUpdates" {
		offset := int64(0)
		if raw := params.Get("offset"); raw != "" {
			fmt.Sscanf(raw, "%d", &offset)
		}
		kept := make([]apiUpdate, 0, len(t.Updates))
		for _, upd := range t.Updates {
			if offset < 0 || upd.UpdateID >= offset {
				updates = append(updates, upd)
			} else {
				kept = append(kept, upd)
			}
		}
		t.Updates = kept
	}
	t.mu.Unlock()

	if scripted {
		result, err := handler(params)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
	if method == "getUpdates" {
		return json.Marshal(updates)
	}
	return json.Marshal(struct{}{})
}

// Download implements Transport from the Files map.
func (t *FakeTransport) Download(_ context.Context, filePath string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	body, ok := t.Files[filePath]
	if !ok {
		return nil, fmt.Errorf("fake transport: no file at %q", filePath)
	}
	return body, nil
}

// PushUpdates queues updates for getUpdates to drain.
func (t *FakeTransport) PushUpdates(updates ...apiUpdate) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Updates = append(t.Updates, updates...)
}

// SetResult scripts one method's result.
func (t *FakeTransport) SetResult(method string, result any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Handlers[method] = func(url.Values) (any, error) { return result, nil }
}

// SetError scripts one method's failure (per-method, not sticky).
func (t *FakeTransport) SetError(method string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Handlers[method] = func(url.Values) (any, error) { return nil, err }
}

// CallsTo returns every recorded call for one method.
func (t *FakeTransport) CallsTo(method string) []FakeCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []FakeCall
	for _, c := range t.Calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}
