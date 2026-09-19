package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// stubBlobs is a fixed AttachmentBlobs: it serves the configured blobs by
// attachment ID and records every lookup for tenancy assertions.
type stubBlobs struct {
	mu     sync.Mutex
	blobs  map[string][]byte
	urls   map[string]string
	wsIDs  []string
	attIDs []string
}

func newStubBlobs(blobs map[string][]byte) *stubBlobs {
	return &stubBlobs{blobs: blobs, urls: map[string]string{}}
}

func (s *stubBlobs) OpenAttachment(_ context.Context, workspaceID, attachmentID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsIDs = append(s.wsIDs, workspaceID)
	s.attIDs = append(s.attIDs, attachmentID)
	blob, ok := s.blobs[attachmentID]
	if !ok {
		return nil, fmt.Errorf("attachment %q not found", attachmentID)
	}
	return blob, nil
}

// AttachmentURL serves a deterministic capability URL per attachment. It
// deliberately does not record into the shared lookup log: the OpenAttachment
// lookups are the byte-resolution tenancy surface, and URL derivation is a
// separate per-ref read.
func (s *stubBlobs) AttachmentURL(_ context.Context, _ string, attachmentID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if url, ok := s.urls[attachmentID]; ok {
		return url, nil
	}
	if _, ok := s.blobs[attachmentID]; !ok {
		return "", fmt.Errorf("attachment %q not found", attachmentID)
	}
	return "/api/v1/files/att-cap/" + attachmentID, nil
}

func (s *stubBlobs) lookups() (wsIDs, attIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.wsIDs...), append([]string(nil), s.attIDs...)
}

// PublishCreatedDocument satisfies the document.create delivery seam
// (add-document-create-tool D6): a deterministic capability URL per name,
// recording the workspace for tenancy assertions alongside the lookups.
func (s *stubBlobs) PublishCreatedDocument(_ context.Context, workspaceID, name, sourcePath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsIDs = append(s.wsIDs, workspaceID)
	if _, err := os.Stat(sourcePath); err != nil {
		return "", fmt.Errorf("created document %q not found", sourcePath)
	}
	return "/api/v1/files/doc-cap/" + name, nil
}

// dropProbeModel completes the turn on its first call, first checking the
// expected drop-lane file on disk mid-run and capturing its bytes — the
// in-run existence and content proof.
type dropProbeModel struct {
	mu      sync.Mutex
	path    string
	content string
	readErr string
}

func (m *dropProbeModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	data, err := os.ReadFile(m.path)
	m.mu.Lock()
	if err != nil {
		m.readErr = err.Error()
	} else {
		m.content = string(data)
	}
	readErr := m.readErr
	m.mu.Unlock()
	text := "attachment found"
	if readErr != "" {
		text = "attachment read failed"
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: text}},
		},
	}, nil
}

func (m *dropProbeModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// setupAttachmentsRunner seeds the fake store (workspace, provider, user,
// role, member, fs-tooled agent), seeds the on-disk jail root, and wires a
// runner over the given model with the given extra options.
func setupAttachmentsRunner(t *testing.T, mdl Model, opts ...RunnerOption) (*Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{
		WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID,
	}); err != nil {
		t.Fatalf("create member: %v", err)
	}
	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Tools:       []string{"read_file", "write_file"},
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	onClawDir := t.TempDir()
	allOpts := append([]RunnerOption{
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return mdl, nil
		}),
		WithInstructionComposer(stubComposer{}),
	}, opts...)
	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		onClawDir,
		allOpts...,
	)

	// Seed the agent's on-disk workspace directory (the jail root).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   "sess-attach",
		UserID:      user.ID,
		Input:       "summarize the attachment",
	}
	return runner, ws, ag, req
}

// rewindProbeModel re-wires the runner's model factory to a probe carrying
// the run-scoped path, which depends on the seeded identities only known
// after setup.
func rewindProbeModel(runner *Runner, probe *dropProbeModel) {
	runner.agenticFactory = func(context.Context, string, providers.Credential, string) (Model, error) {
		return probe, nil
	}
}

// dropRunDir computes the expected run-scoped materialization dir for req.
func dropRunDir(runner *Runner, req ExecRequest) string {
	return runner.dropLaneRunDir(runKeyOf(req))
}

func TestRunner_MaterializesDropLaneDuringRun(t *testing.T) {
	const content = "drop-lane payload bytes"
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte(content)})
	runner, ws, _, req := setupAttachmentsRunner(t, &dropProbeModel{}, WithAttachmentBlobs(blobs))
	probe := &dropProbeModel{
		path: filepath.Join(dropRunDir(runner, req), "att-1", "report.md"),
	}
	rewindProbeModel(runner, probe)
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "report.md", MimeType: "text/markdown", Lane: attLaneDrop, Size: int64(len(content))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if hasKind(events, TranscriptEventError) {
		t.Fatalf("unexpected error event, got %+v", events)
	}
	if probe.readErr != "" {
		t.Fatalf("drop-lane file not readable mid-run at %s: %s", probe.path, probe.readErr)
	}
	if probe.content != content {
		t.Errorf("materialized content = %q, want %q", probe.content, content)
	}
	gotWS, gotAtt := blobs.lookups()
	if len(gotAtt) != 1 || gotAtt[0] != "att-1" || gotWS[0] != ws.ID {
		t.Errorf("OpenAttachment lookups = (%v, %v), want workspace %q / attachment att-1", gotWS, gotAtt, ws.ID)
	}
}

func TestRunner_RemovesDropLaneDirAfterTerminal(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte("bytes")})
	runner, _, _, req := setupAttachmentsRunner(t, &dropProbeModel{}, WithAttachmentBlobs(blobs))
	probe := &dropProbeModel{path: filepath.Join(dropRunDir(runner, req), "att-1", "report.md")}
	rewindProbeModel(runner, probe)
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "report.md", MimeType: "text/markdown", Lane: attLaneDrop, Size: 5},
	}

	runDir := dropRunDir(runner, req)
	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Errorf("run dir %s must be removed after the terminal event (stat err: %v)", runDir, err)
	}
}

func TestRunner_JailMountsDropLaneReadOnly(t *testing.T) {
	const content = "read-only payload"
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte(content)})
	runner, ws, ag, req := setupAttachmentsRunner(t, &dropProbeModel{}, WithAttachmentBlobs(blobs))
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "report.md", MimeType: "text/markdown", Lane: attLaneDrop, Size: int64(len(content))},
	}

	cfg, _, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	runDir := dropRunDir(runner, req)
	mounted := false
	for _, root := range cfg.Filesystem.ReadOnlyRoots {
		if root == runDir {
			mounted = true
		}
	}
	if !mounted {
		t.Fatalf("ReadOnlyRoots = %v, want it to contain the run dir %s", cfg.Filesystem.ReadOnlyRoots, runDir)
	}

	// The jail is built exactly as agent.go's buildMiddlewares builds it for
	// a non-channel run: primary agent dir + the read-only roots.
	jail, err := backend.NewFilesystemJailedWithRoots(cfg.Filesystem.AgentDir, cfg.Filesystem.ReadOnlyRoots...)
	if err != nil {
		t.Fatalf("build jail: %v", err)
	}

	file := filepath.Join(runDir, "att-1", "report.md")
	read, err := jail.Read(context.Background(), &einofs.ReadRequest{FilePath: file})
	if err != nil {
		t.Fatalf("read drop-lane file through the jail: %v", err)
	}
	if read.Content != content {
		t.Errorf("jail read content = %q, want %q", read.Content, content)
	}

	err = jail.Write(context.Background(), &einofs.WriteRequest{
		FilePath: filepath.Join(runDir, "att-1", "evil.txt"),
		Content:  "malicious",
	})
	if err == nil {
		t.Fatal("expected write into the drop-lane mount to be rejected, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(runDir, "att-1", "evil.txt")); statErr == nil {
		t.Error("write must not create files under the drop-lane mount")
	}
}

func TestRunner_SweepsStaleDropLaneDirsAtConstruction(t *testing.T) {
	onClawDir := t.TempDir()
	stale := filepath.Join(dropLaneRoot(onClawDir), "stale-run", "att-9")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatalf("seed stale dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, "leftover.bin"), []byte("crash leftover"), 0o644); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}

	NewRunner(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
		[]byte("dummy-key"),
		onClawDir,
	)

	if _, err := os.Stat(filepath.Join(dropLaneRoot(onClawDir), "stale-run")); !os.IsNotExist(err) {
		t.Errorf("stale run dir must be swept at construction (stat err: %v)", err)
	}
}

func TestRunner_NilAttachmentsCreateNoDropLaneDir(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{})
	runner, _, _, req := setupAttachmentsRunner(t, &dropProbeModel{}, WithAttachmentBlobs(blobs))
	probe := &dropProbeModel{path: filepath.Join(dropRunDir(runner, req), "att-1", "report.md")}
	rewindProbeModel(runner, probe)
	// req.Attachments stays nil — the cron/channel/compact caller shape.

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if probe.readErr == "" {
		t.Error("probe must not have found a drop-lane file on a nil-attachment run")
	}
	if _, err := os.Stat(dropLaneRoot(runner.onClawDir)); !os.IsNotExist(err) {
		t.Errorf("drop-lane root must never be created for nil attachments (stat err: %v)", err)
	}
}

func TestRunner_InlineLaneRefsNotMaterialized(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{"att-2": []byte("inline bytes")})
	runner, _, _, req := setupAttachmentsRunner(t, &dropProbeModel{}, WithAttachmentBlobs(blobs))
	probe := &dropProbeModel{path: filepath.Join(dropRunDir(runner, req), "att-2", "shot.png")}
	rewindProbeModel(runner, probe)
	req.Attachments = []AttachmentRef{
		{ID: "att-2", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: 12},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if _, statErr := os.Stat(dropLaneRoot(runner.onClawDir)); !os.IsNotExist(statErr) {
		t.Errorf("inline-only refs must not create the drop-lane root (stat err: %v)", statErr)
	}
	// Inline lanes resolve through the blob resolver at message-construction
	// time (attachments design D5/D9) — exactly once, never through drop-lane
	// materialization.
	wsIDs, attLookups := blobs.lookups()
	if len(attLookups) != 1 || attLookups[0] != "att-2" || wsIDs[0] != req.WorkspaceID {
		t.Errorf("inline lanes must resolve through the blob resolver once, got lookups (ws=%v, att=%v)", wsIDs, attLookups)
	}
}

func TestRunner_DropLaneWithoutBlobsFailsBeforeModelCall(t *testing.T) {
	probe := &dropProbeModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe)
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "report.md", MimeType: "text/markdown", Lane: attLaneDrop, Size: 5},
	}

	_, err := runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected Run to fail without a configured blob resolver, got nil")
	}
	if !strings.Contains(err.Error(), "WithAttachmentBlobs") {
		t.Errorf("error must name the missing capability, got: %v", err)
	}
	if probe.content != "" || probe.readErr != "" {
		t.Error("model must not have been called")
	}
	if _, statErr := os.Stat(dropLaneRoot(runner.onClawDir)); !os.IsNotExist(statErr) {
		t.Errorf("failed materialization must not leave the drop-lane root behind (stat err: %v)", statErr)
	}
}

func TestRunner_MaterializeDropLaneNameSanitization(t *testing.T) {
	ctx := context.Background()
	onClawDir := t.TempDir()
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte("x")})
	r := &Runner{onClawDir: onClawDir, attachmentBlobs: blobs}

	t.Run("traversal name rejected", func(t *testing.T) {
		req := ExecRequest{
			WorkspaceID: "ws", AgentID: "ag", SessionID: "sess",
			Attachments: []AttachmentRef{{ID: "att-1", Name: "..", Lane: attLaneDrop}},
		}
		if _, err := r.materializeDropLane(ctx, req); err == nil {
			t.Fatal("expected '..' file name to be rejected")
		}
	})
	t.Run("empty name rejected", func(t *testing.T) {
		req := ExecRequest{
			WorkspaceID: "ws", AgentID: "ag", SessionID: "sess",
			Attachments: []AttachmentRef{{ID: "att-1", Name: "", Lane: attLaneDrop}},
		}
		if _, err := r.materializeDropLane(ctx, req); err == nil {
			t.Fatal("expected an empty file name to be rejected")
		}
	})
	t.Run("nested name collapses to base", func(t *testing.T) {
		req := ExecRequest{
			WorkspaceID: "ws", AgentID: "ag", SessionID: "sess",
			Attachments: []AttachmentRef{{ID: "att-1", Name: "sub/dir/data.csv", Lane: attLaneDrop}},
		}
		dir, err := r.materializeDropLane(ctx, req)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "att-1", "data.csv"))
		if err != nil {
			t.Fatalf("read materialized file: %v", err)
		}
		if string(data) != "x" {
			t.Errorf("content = %q, want %q", data, "x")
		}
	})
	t.Run("hostile session id cannot escape the root", func(t *testing.T) {
		req := ExecRequest{
			WorkspaceID: "ws", AgentID: "ag", SessionID: "../../escape",
			Attachments: []AttachmentRef{{ID: "att-1", Name: "f.bin", Lane: attLaneDrop}},
		}
		dir, err := r.materializeDropLane(ctx, req)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if filepath.Dir(filepath.Dir(filepath.Dir(dir))) != dropLaneRoot(onClawDir) {
			t.Errorf("run dir %q escaped the drop-lane root %q", dir, dropLaneRoot(onClawDir))
		}
	})
	t.Run("dot-only session id cannot traverse via path cleaning", func(t *testing.T) {
		req := ExecRequest{
			WorkspaceID: "..", AgentID: "..", SessionID: "..",
			Attachments: []AttachmentRef{{ID: "att-1", Name: "f.bin", Lane: attLaneDrop}},
		}
		dir, err := r.materializeDropLane(ctx, req)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if filepath.Dir(filepath.Dir(filepath.Dir(dir))) != dropLaneRoot(onClawDir) {
			t.Errorf("run dir %q escaped the drop-lane root %q", dir, dropLaneRoot(onClawDir))
		}
	})
	t.Run("download failure fails materialization", func(t *testing.T) {
		req := ExecRequest{
			WorkspaceID: "ws", AgentID: "ag", SessionID: "sess-err",
			Attachments: []AttachmentRef{{ID: "att-missing", Name: "f.bin", Lane: attLaneDrop}},
		}
		if _, err := r.materializeDropLane(ctx, req); err == nil {
			t.Fatal("expected download failure to fail materialization")
		}
	})
}

// captureModel records every model input so tests can assert the constructed
// multimodal user message, replying with a fixed assistant text.
type captureModel struct {
	mu     sync.Mutex
	inputs []*schema.AgenticMessage
}

func (m *captureModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	m.inputs = append(m.inputs, input...)
	m.mu.Unlock()
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "ok"}},
		},
	}, nil
}

func (m *captureModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *captureModel) captured() []*schema.AgenticMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*schema.AgenticMessage(nil), m.inputs...)
}

// capturedUserMessage returns the first user-role message the model saw,
// failing the test when the model was never called or never got one.
func (m *captureModel) capturedUserMessage(t *testing.T) *schema.AgenticMessage {
	t.Helper()
	msgs := m.captured()
	if len(msgs) == 0 {
		t.Fatal("model was never called")
	}
	for _, msg := range msgs {
		if msg.Role == schema.AgenticRoleTypeUser {
			return msg
		}
	}
	t.Fatalf("no user message reached the model, inputs: %+v", msgs)
	return nil
}

// blockTypes flattens a message's content block types for ordering asserts.
func blockTypes(msg *schema.AgenticMessage) []schema.ContentBlockType {
	out := make([]schema.ContentBlockType, 0, len(msg.ContentBlocks))
	for _, b := range msg.ContentBlocks {
		out = append(out, b.Type)
	}
	return out
}

func TestRunner_AttachmentOnlyMessageCarriesImageBlockAlone(t *testing.T) {
	blob := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D} // 5 fake PNG bytes
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = "" // attachment-only send: no user text
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("attachment-only turn must complete like any turn, got %+v", events)
	}
	if hasKind(events, TranscriptEventError) {
		t.Fatalf("unexpected error event, got %+v", events)
	}

	msg := probe.capturedUserMessage(t)
	if got := blockTypes(msg); len(got) != 1 || got[0] != schema.ContentBlockTypeUserInputImage {
		t.Fatalf("block types = %v, want exactly [%s]", got, schema.ContentBlockTypeUserInputImage)
	}
	img := msg.ContentBlocks[0].UserInputImage
	if img == nil {
		t.Fatal("image block payload missing")
	}
	if want := base64.StdEncoding.EncodeToString(blob); img.Base64Data != want {
		t.Errorf("Base64Data = %q, want %q", img.Base64Data, want)
	}
	if img.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want image/png", img.MIMEType)
	}
}

func TestRunner_MixedAttachmentsOrderBlocks(t *testing.T) {
	imgBlob := []byte{1, 2, 3, 4, 5}
	pdfBlob := []byte("%PDF-fake")
	csvBlob := []byte("A,B\n1,2")
	blobs := newStubBlobs(map[string][]byte{
		"att-img": imgBlob,
		"att-pdf": pdfBlob,
		"att-txt": csvBlob,
		"att-drp": []byte("dump bytes"),
	})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	// Refs deliberately scrambled: construction groups blocks by lane in spec
	// order (text, fenced text, image, pdf, pointer), not by ref order.
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(imgBlob))},
		{ID: "att-txt", Name: "notes.csv", MimeType: "text/csv", Lane: "inline-text", Size: int64(len(csvBlob))},
		{ID: "att-pdf", Name: "report.pdf", MimeType: "application/pdf", Lane: "inline-pdf", Size: int64(len(pdfBlob))},
		{ID: "att-drp", Name: "dump.sql", MimeType: "application/sql", Lane: attLaneDrop, Size: 10},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("unexpected stream outcome, got %+v", events)
	}

	msg := probe.capturedUserMessage(t)
	wantTypes := []schema.ContentBlockType{
		schema.ContentBlockTypeUserInputText, // user's own text
		schema.ContentBlockTypeUserInputText, // fenced inline-text
		schema.ContentBlockTypeUserInputImage,
		schema.ContentBlockTypeUserInputFile,
		schema.ContentBlockTypeUserInputText, // pointer note
	}
	if got := blockTypes(msg); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("block types = %v, want %v", got, wantTypes)
	}
	if text := msg.ContentBlocks[0].UserInputText.Text; text != "summarize the attachment" {
		t.Errorf("user text block = %q, want the turn input", text)
	}
	wantFence := "```notes.csv\n" + string(csvBlob) + "\n```"
	if fenced := msg.ContentBlocks[1].UserInputText.Text; fenced != wantFence {
		t.Errorf("fenced block = %q, want %q", fenced, wantFence)
	}
	if got := msg.ContentBlocks[2].UserInputImage.Base64Data; got != base64.StdEncoding.EncodeToString(imgBlob) {
		t.Errorf("image Base64Data = %q, want %q", got, base64.StdEncoding.EncodeToString(imgBlob))
	}
	file := msg.ContentBlocks[3].UserInputFile
	if file.Base64Data != base64.StdEncoding.EncodeToString(pdfBlob) || file.MIMEType != "application/pdf" || file.Name != "report.pdf" {
		t.Errorf("file block = %+v, want pdf bytes, application/pdf, report.pdf", file)
	}
	note := msg.ContentBlocks[4]
	wantPath := filepath.Join(dropRunDir(runner, req), dropLaneSeg("att-drp"), "dump.sql")
	if !strings.Contains(note.UserInputText.Text, "dump.sql") || !strings.Contains(note.UserInputText.Text, wantPath) {
		t.Errorf("pointer note %q must name the file and path %q", note.UserInputText.Text, wantPath)
	}
	if note.Extra[AttachmentPointerExtraKey] != true {
		t.Errorf("pointer note Extra = %v, want [%s]=true", note.Extra, AttachmentPointerExtraKey)
	}
}

func TestRunner_DropLanePointerNoteMarkedWithPath(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte("bytes")})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = "" // isolate the pointer note as the message's only block
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "report.md", MimeType: "text/markdown", Lane: attLaneDrop, Size: 5},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("unexpected stream outcome, got %+v", events)
	}

	msg := probe.capturedUserMessage(t)
	if got := blockTypes(msg); len(got) != 1 || got[0] != schema.ContentBlockTypeUserInputText {
		t.Fatalf("block types = %v, want exactly one text block", got)
	}
	note := msg.ContentBlocks[0]
	wantPath := filepath.Join(dropRunDir(runner, req), dropLaneSeg("att-1"), "report.md")
	if !strings.Contains(note.UserInputText.Text, "report.md") {
		t.Errorf("pointer note %q must name the file", note.UserInputText.Text)
	}
	if !strings.Contains(note.UserInputText.Text, wantPath) {
		t.Errorf("pointer note %q must contain the materialized path %q", note.UserInputText.Text, wantPath)
	}
	if !strings.Contains(note.UserInputText.Text, "filesystem tools") {
		t.Errorf("non-document drop ref pointer note %q must name the filesystem tools", note.UserInputText.Text)
	}
	if strings.Contains(note.UserInputText.Text, "document.read") {
		t.Errorf("non-document drop ref pointer note %q must not name the document.read tool", note.UserInputText.Text)
	}
	if note.Extra[AttachmentPointerExtraKey] != true {
		t.Errorf("pointer note Extra = %v, want [%s]=true", note.Extra, AttachmentPointerExtraKey)
	}
}

func TestRunner_DropLanePointerNoteDocumentNamesDocumentReadTool(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte("bytes")})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = "" // isolate the pointer note as the message's only block
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "brief.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Lane: attLaneDrop, Size: 5},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("unexpected stream outcome, got %+v", events)
	}

	msg := probe.capturedUserMessage(t)
	if got := blockTypes(msg); len(got) != 1 || got[0] != schema.ContentBlockTypeUserInputText {
		t.Fatalf("block types = %v, want exactly one text block", got)
	}
	note := msg.ContentBlocks[0]
	wantPath := filepath.Join(dropRunDir(runner, req), dropLaneSeg("att-1"), "brief.docx")
	if !strings.Contains(note.UserInputText.Text, "brief.docx") {
		t.Errorf("pointer note %q must name the document", note.UserInputText.Text)
	}
	if !strings.Contains(note.UserInputText.Text, wantPath) {
		t.Errorf("pointer note %q must contain the materialized path %q", note.UserInputText.Text, wantPath)
	}
	if !strings.Contains(note.UserInputText.Text, "document.read tool") {
		t.Errorf("document drop ref pointer note %q must name the document.read tool", note.UserInputText.Text)
	}
	if strings.Contains(note.UserInputText.Text, "filesystem tools") {
		t.Errorf("document drop ref pointer note %q must not name the filesystem tools", note.UserInputText.Text)
	}
	if note.Extra[AttachmentPointerExtraKey] != true {
		t.Errorf("pointer note Extra = %v, want [%s]=true", note.Extra, AttachmentPointerExtraKey)
	}
}

func TestRunner_DropLanePointerNotePDFMimeNamesDocumentReadTool(t *testing.T) {
	// Legacy inline-pdf lane values may flow through the drop lane without a
	// usable extension; the mime alone must select the document.read copy.
	blobs := newStubBlobs(map[string][]byte{"att-1": []byte("%PDF-fake")})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Input = ""
	req.Attachments = []AttachmentRef{
		{ID: "att-1", Name: "report", MimeType: "application/pdf", Lane: attLaneDrop, Size: 9},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if hasKind(events, TranscriptEventError) || !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("unexpected stream outcome, got %+v", events)
	}

	msg := probe.capturedUserMessage(t)
	note := msg.ContentBlocks[0]
	if !strings.Contains(note.UserInputText.Text, "document.read tool") {
		t.Errorf("pdf-mime drop ref pointer note %q must name the document.read tool", note.UserInputText.Text)
	}
}

func TestRunner_AttachmentBytesAbsentFromStreamEvents(t *testing.T) {
	blob := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x42} // 5-byte fake image
	encoded := base64.StdEncoding.EncodeToString(blob)
	blobs := newStubBlobs(map[string][]byte{"att-img": blob})
	probe := &captureModel{}
	runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
	req.Attachments = []AttachmentRef{
		{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: int64(len(blob))},
	}

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	for _, ev := range events {
		payload, mErr := json.Marshal(ev)
		if mErr != nil {
			t.Fatalf("marshal event: %v", mErr)
		}
		if strings.Contains(string(payload), encoded) {
			t.Fatalf("attachment base64 %q leaked into event %s", encoded, payload)
		}
		if strings.Contains(string(payload), string(blob)) {
			t.Fatalf("attachment raw bytes leaked into event %s", payload)
		}
	}
}

func TestRunner_InlineResolutionFailureFailsRunBeforeStream(t *testing.T) {
	t.Run("unknown attachment id", func(t *testing.T) {
		blobs := newStubBlobs(map[string][]byte{})
		probe := &captureModel{}
		runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
		req.Attachments = []AttachmentRef{
			{ID: "att-missing", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: 5},
		}

		stream, err := runner.Run(context.Background(), req)
		if err == nil {
			t.Fatal("expected Run to fail on unknown attachment id, got nil")
		}
		if !strings.Contains(err.Error(), "att-missing") {
			t.Errorf("error must name the attachment, got: %v", err)
		}
		if stream != nil {
			t.Errorf("failed construction must not start a stream, got %+v", stream)
		}
		if n := len(probe.captured()); n != 0 {
			t.Errorf("model must not be called, got %d inputs", n)
		}
	})
	t.Run("empty bytes", func(t *testing.T) {
		blobs := newStubBlobs(map[string][]byte{"att-empty": {}})
		probe := &captureModel{}
		runner, _, _, req := setupAttachmentsRunner(t, probe, WithAttachmentBlobs(blobs))
		req.Attachments = []AttachmentRef{
			{ID: "att-empty", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: 0},
		}

		stream, err := runner.Run(context.Background(), req)
		if err == nil {
			t.Fatal("expected Run to fail on empty attachment bytes, got nil")
		}
		if !strings.Contains(err.Error(), "empty bytes") {
			t.Errorf("error must name the failure, got: %v", err)
		}
		if stream != nil {
			t.Errorf("failed construction must not start a stream, got %+v", stream)
		}
		if n := len(probe.captured()); n != 0 {
			t.Errorf("model must not be called, got %d inputs", n)
		}
	})
	t.Run("runner without blob resolver", func(t *testing.T) {
		probe := &captureModel{}
		runner, _, _, req := setupAttachmentsRunner(t, probe) // no WithAttachmentBlobs
		req.Attachments = []AttachmentRef{
			{ID: "att-img", Name: "shot.png", MimeType: "image/png", Lane: "inline-image", Size: 5},
		}

		stream, err := runner.Run(context.Background(), req)
		if err == nil {
			t.Fatal("expected Run to fail without a configured blob resolver, got nil")
		}
		if !strings.Contains(err.Error(), "WithAttachmentBlobs") {
			t.Errorf("error must name the missing capability, got: %v", err)
		}
		if stream != nil {
			t.Errorf("failed construction must not start a stream, got %+v", stream)
		}
		if n := len(probe.captured()); n != 0 {
			t.Errorf("model must not be called, got %d inputs", n)
		}
	})
}
