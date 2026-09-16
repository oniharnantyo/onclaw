package whatsappcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// FakeTransport is the in-memory Transport for tests (tasks.md 3.4): calls
// are recorded, operations resolve through scriptable handlers, sends get
// incrementing wamid-shaped ids by default, and media downloads come from a
// map. Zero network.
type FakeTransport struct {
	mu sync.Mutex

	// Calls records every call in order (operation + payload/path).
	Calls []FakeCall

	// Handlers maps operation → result builder; an operation with no
	// handler gets the built-in default (send → incrementing message id,
	// phone number → metadata stub, media → URL stub, mark read → empty).
	Handlers map[string]func(call FakeCall) (any, error)

	// Files maps media URL → body for Download.
	Files map[string][]byte

	// Err, when set, fails every subsequent call (sticky — clear it to
	// resume).
	Err error

	// nextMessageID feeds the default send response.
	nextMessageID int
}

// FakeCall is one recorded transport call.
type FakeCall struct {
	Op   string
	Body []byte // JSON request payload (message sends, mark-read calls)
	Path string // resource path (phone number, media id, download URL)
}

// NewFakeTransport builds an empty fake.
func NewFakeTransport() *FakeTransport {
	return &FakeTransport{
		Handlers: make(map[string]func(FakeCall) (any, error)),
		Files:    make(map[string][]byte),
	}
}

// Call implements the Transport seams: record, resolve the scripted result,
// else fall back to the per-operation default.
func (t *FakeTransport) call(_ context.Context, op string, body []byte, path string) (json.RawMessage, error) {
	t.mu.Lock()
	t.Calls = append(t.Calls, FakeCall{Op: op, Body: body, Path: path})
	if t.Err != nil {
		err := t.Err
		t.mu.Unlock()
		return nil, err
	}
	handler, scripted := t.Handlers[op]
	if !scripted {
		switch op {
		case OpSendMessage:
			t.nextMessageID++
			handler = func(FakeCall) (any, error) {
				return map[string]any{
					"messages": []map[string]string{{"id": fmt.Sprintf("wamid.fake-%04d", t.nextMessageID)}},
				}, nil
			}
		case OpMarkRead:
			handler = func(FakeCall) (any, error) { return struct{}{}, nil }
		case OpPhoneNumber:
			handler = func(call FakeCall) (any, error) {
				return map[string]string{
					"id":            call.Path,
					"verified_name": "OnClaw Test Number",
				}, nil
			}
		case OpMedia:
			handler = func(call FakeCall) (any, error) {
				return map[string]string{
					"id":        call.Path,
					"url":       "https://media.fake/" + call.Path,
					"mime_type": "application/octet-stream",
				}, nil
			}
		case OpDownload:
			// The Files map governs the outcome (missing paths error in
			// Download); the recorded call still needs a response.
			handler = func(FakeCall) (any, error) { return struct{}{}, nil }
		default:
			t.mu.Unlock()
			return nil, fmt.Errorf("fake transport: no handler for operation %q", op)
		}
	}
	t.mu.Unlock()

	result, err := handler(FakeCall{Op: op, Body: body, Path: path})
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// SendMessage implements Transport.
func (t *FakeTransport) SendMessage(ctx context.Context, payload []byte) (json.RawMessage, error) {
	return t.call(ctx, OpSendMessage, payload, "")
}

// MarkRead implements Transport.
func (t *FakeTransport) MarkRead(ctx context.Context, payload []byte) error {
	_, err := t.call(ctx, OpMarkRead, payload, "")
	return err
}

// PhoneNumber implements Transport.
func (t *FakeTransport) PhoneNumber(ctx context.Context) (json.RawMessage, error) {
	return t.call(ctx, OpPhoneNumber, nil, "phone-number-id")
}

// Media implements Transport.
func (t *FakeTransport) Media(ctx context.Context, mediaID string) (json.RawMessage, error) {
	return t.call(ctx, OpMedia, nil, mediaID)
}

// Download implements Transport from the Files map.
func (t *FakeTransport) Download(ctx context.Context, mediaURL string) ([]byte, error) {
	if _, err := t.call(ctx, OpDownload, nil, mediaURL); err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	data, ok := t.Files[mediaURL]
	if !ok {
		return nil, fmt.Errorf("fake transport: no file at %q", mediaURL)
	}
	return data, nil
}

// SetResult scripts one operation's result.
func (t *FakeTransport) SetResult(op string, result any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Handlers[op] = func(FakeCall) (any, error) { return result, nil }
}

// SetError scripts one operation's failure (per-operation, not sticky).
func (t *FakeTransport) SetError(op string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Handlers[op] = func(FakeCall) (any, error) { return nil, err }
}

// SetHandler scripts one operation with request access.
func (t *FakeTransport) SetHandler(op string, handler func(call FakeCall) (any, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Handlers[op] = handler
}

// CallsTo returns every recorded call for one operation.
func (t *FakeTransport) CallsTo(op string) []FakeCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []FakeCall
	for _, c := range t.Calls {
		if c.Op == op {
			out = append(out, c)
		}
	}
	return out
}
