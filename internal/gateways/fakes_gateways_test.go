package gateways

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

// domainWorkspace / domainUser build minimal valid fixtures for the fake
// store's workspace-existence and user-existence checks.
func domainWorkspace(id string) *domain.Workspace {
	return &domain.Workspace{ID: id, Slug: "slug-" + id, Name: "Workspace " + id}
}

func domainUser(id string) *domain.User {
	return &domain.User{ID: id, Email: id + "@example.com", Name: "User " + id}
}

// Test-only doubles shared by the gateways package unit tests. Names carry
// the testPlatform/testRun/testMem/testStub prefixes to stay distinct from
// any other doubles in the package.

// -------------------------------------------------------------------------
// PlatformAdapter double
// -------------------------------------------------------------------------

type testSentMessage struct {
	ChatID string
	HTML   string
	// Flavor is the wire-format tag the send carried
	// (add-whatsapp-gateway design D9).
	Flavor string
	Opts   SendOptions
}

type testEditMessage struct {
	ChatID    string
	MessageID string
	HTML      string
	Flavor    string
}

// testPlatformAdapter records every outbound call and serves canned
// downloads. Send/edit failures are injectable per call. Its capability
// matrix (CanEdit × CanButton, design D2) is constructor-parameterized so
// the same double drives every platform cell; the default constructor is
// the Telegram cell (both true).
type testPlatformAdapter struct {
	mu        sync.Mutex
	canEdit   bool
	canButton bool
	nextID    int
	sent      []testSentMessage
	edits     []testEditMessage
	typings   int
	cards     []agents.ApprovalPayload
	downloads map[string][]byte
	dlErr     error
	sendErrs  []error // consumed per SendMessage call; nil entries succeed
	editErrs  []error // consumed per EditMessage call; nil entries succeed
}

func newTestPlatformAdapter() *testPlatformAdapter {
	return newTestPlatformAdapterWithCaps(true, true)
}

// newTestPlatformAdapterWithCaps builds the double for one capability-matrix
// cell (add-whatsapp-gateway design D2).
func newTestPlatformAdapterWithCaps(canEdit, canButton bool) *testPlatformAdapter {
	return &testPlatformAdapter{
		canEdit:   canEdit,
		canButton: canButton,
		nextID:    100,
		downloads: map[string][]byte{},
	}
}

func (a *testPlatformAdapter) Start(ctx context.Context) error { return nil }
func (a *testPlatformAdapter) Stop(ctx context.Context) error  { return nil }

func (a *testPlatformAdapter) Capabilities() AdapterCapabilities {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AdapterCapabilities{CanEdit: a.canEdit, CanButton: a.canButton}
}

func (a *testPlatformAdapter) SendMessage(ctx context.Context, chatID, body, flavor string, opts SendOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sendErrs) > 0 {
		err := a.sendErrs[0]
		a.sendErrs = a.sendErrs[1:]
		if err != nil {
			return "", err
		}
	}
	a.nextID++
	id := "msg-" + strconv.Itoa(a.nextID)
	a.sent = append(a.sent, testSentMessage{ChatID: chatID, HTML: body, Flavor: flavor, Opts: opts})
	return id, nil
}

func (a *testPlatformAdapter) EditMessage(ctx context.Context, chatID, messageID, body, flavor string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.editErrs) > 0 {
		err := a.editErrs[0]
		a.editErrs = a.editErrs[1:]
		if err != nil {
			return err
		}
	}
	a.edits = append(a.edits, testEditMessage{ChatID: chatID, MessageID: messageID, HTML: body, Flavor: flavor})
	return nil
}

func (a *testPlatformAdapter) SendTyping(ctx context.Context, chatID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.typings++
	return nil
}

func (a *testPlatformAdapter) SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cards = append(a.cards, interrupt)
	a.nextID++
	return "card-" + strconv.Itoa(a.nextID), nil
}

func (a *testPlatformAdapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dlErr != nil {
		return nil, a.dlErr
	}
	data, ok := a.downloads[fileID]
	if !ok {
		return nil, io.EOF
	}
	return append([]byte(nil), data...), nil
}

// --- accessors ---

func (a *testPlatformAdapter) sentMessages() []testSentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]testSentMessage(nil), a.sent...)
}

func (a *testPlatformAdapter) editedMessages() []testEditMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]testEditMessage(nil), a.edits...)
}

func (a *testPlatformAdapter) typingCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.typings
}

func (a *testPlatformAdapter) approvalCards() []agents.ApprovalPayload {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agents.ApprovalPayload(nil), a.cards...)
}

// -------------------------------------------------------------------------
// RunSubmitter double
// -------------------------------------------------------------------------

type testResumeCall struct {
	Req      agents.ExecRequest
	Approval agents.ApprovalPayload
	Approved bool
}

type testRunSubmitter struct {
	mu           sync.Mutex
	resumeCalls  []testResumeCall
	resumeStream *agents.EventStream
	resumeErr    error
}

func (s *testRunSubmitter) Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	return nil, nil
}

func (s *testRunSubmitter) Resume(ctx context.Context, req agents.ExecRequest, approval agents.ApprovalPayload, approved bool) (*agents.EventStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resumeCalls = append(s.resumeCalls, testResumeCall{Req: req, Approval: approval, Approved: approved})
	if s.resumeErr != nil {
		return nil, s.resumeErr
	}
	return s.resumeStream, nil
}

func (s *testRunSubmitter) calls() []testResumeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]testResumeCall(nil), s.resumeCalls...)
}

// -------------------------------------------------------------------------
// storage.Storage double
// -------------------------------------------------------------------------

type testMemFile struct {
	*bytes.Reader
	contentType string
}

func (f *testMemFile) ContentType() string { return f.contentType }
func (f *testMemFile) Close() error        { return nil }

var _ storage.File = (*testMemFile)(nil)

// testMemStorage is an in-memory storage.Storage.
type testMemStorage struct {
	mu    sync.Mutex
	blobs map[string][]byte
	mimes map[string]string
}

func newTestMemStorage() *testMemStorage {
	return &testMemStorage{blobs: map[string][]byte{}, mimes: map[string]string{}}
}

func (s *testMemStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.blobs[key] = data
	s.mimes[key] = contentType
	return nil
}

func (s *testMemStorage) Open(ctx context.Context, key string) (storage.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.blobs[key]
	if !ok {
		return nil, io.EOF
	}
	return &testMemFile{Reader: bytes.NewReader(data), contentType: s.mimes[key]}, nil
}

func (s *testMemStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key)
	return nil
}

func (s *testMemStorage) URL(key string) string { return "mem://" + key }

var _ storage.Storage = (*testMemStorage)(nil)

// testStorageResolver adapts testMemStorage to the ingress StorageResolver.
type testStorageResolver struct {
	storage *testMemStorage
	backend string
	err     error
}

func (r *testStorageResolver) ForWorkspace(ctx context.Context, workspaceID string) (storage.Storage, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.storage, nil
}

func (r *testStorageResolver) DriverName(ctx context.Context, workspaceID string) (string, error) {
	return r.backend, r.err
}

// -------------------------------------------------------------------------
// Transcriber double
// -------------------------------------------------------------------------

type testTranscriber struct {
	transcript string
	err        error
	calls      int
}

func (t *testTranscriber) Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error) {
	t.calls++
	return t.transcript, t.err
}
