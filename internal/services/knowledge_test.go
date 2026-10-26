package services

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// knowledgeFixture wires the knowledge service over a fake store's document
// registry, one seeded agent, and a recording emitter.
type knowledgeFixture struct {
	svc     *KnowledgeService
	st      *fake.Store
	emitter *recordingEmitter
	agent   store.Agent
}

func newKnowledgeFixture(t *testing.T) *knowledgeFixture {
	t.Helper()
	st := fake.New()
	know := knowledge.NewStore(t.TempDir(), st.Documents())
	agent := store.Agent{Name: "scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(context.Background(), &agent))
	emitter := &recordingEmitter{}
	return &knowledgeFixture{
		svc:     NewKnowledgeService(know, st.Agents(), emitter),
		st:      st,
		emitter: emitter,
		agent:   agent,
	}
}

// b64 encodes an upload payload the way the frontend's FileReader flow does.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// TestKnowledgeService_UploadContentRoundtrip pins the base64 upload and
// serving pattern (design D8, the chat-attachments payload shape).
func TestKnowledgeService_UploadContentRoundtrip(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	doc, err := f.svc.Upload(ctx, "notes.txt", "text/plain", b64("hello"), knowledge.ScopeGlobal, "", "meeting notes")
	require.NoError(t, err)
	require.Equal(t, "notes.txt", doc.Filename)
	require.Equal(t, knowledge.ScopeGlobal, doc.Scope)
	require.Empty(t, doc.AgentID)
	require.Equal(t, knowledge.SourceUser, doc.Source)
	require.Equal(t, "meeting notes", doc.Description)
	require.Equal(t, int64(5), doc.Size)

	content, err := f.svc.Content(ctx, doc.ID)
	require.NoError(t, err)
	require.Equal(t, "hello", string(mustDecode(t, content)), "content round-trips base64")
}

// TestKnowledgeService_UploadAgentScopeResolvesSlug pins the agent resolution:
// an agent scope resolves the id to the slug; an unknown agent is refused.
func TestKnowledgeService_UploadAgentScopeResolvesSlug(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	doc, err := f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeAgent, f.agent.ID, "")
	require.NoError(t, err)
	require.Equal(t, f.agent.ID, doc.AgentID)
	require.Equal(t, "scout", doc.AgentSlug)

	_, err = f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeAgent, "ffffffffffffffffffffffffffffffff", "")
	require.ErrorIs(t, err, store.ErrNotFound, "an unknown agent must be refused")

	_, err = f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), "bogus", "", "")
	require.ErrorContains(t, err, "unknown document scope")
}

// TestKnowledgeService_SetScope pins promote and demote through the service:
// promotion ignores the agent id, demotion resolves the target agent.
func TestKnowledgeService_SetScope(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	doc, err := f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeAgent, f.agent.ID, "")
	require.NoError(t, err)

	// Promote: agentID is ignored.
	got, err := f.svc.SetScope(ctx, doc.ID, knowledge.ScopeGlobal, "ignored")
	require.NoError(t, err)
	require.Equal(t, knowledge.ScopeGlobal, got.Scope)
	require.Empty(t, got.AgentSlug)

	// Demote to the seeded agent.
	got, err = f.svc.SetScope(ctx, doc.ID, knowledge.ScopeAgent, f.agent.ID)
	require.NoError(t, err)
	require.Equal(t, knowledge.ScopeAgent, got.Scope)
	require.Equal(t, "scout", got.AgentSlug)

	// Demoting to an unknown agent is refused.
	_, err = f.svc.SetScope(ctx, doc.ID, knowledge.ScopeAgent, "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestKnowledgeService_SetDescription pins the description edit (the primary
// PDF discovery handle).
func TestKnowledgeService_SetDescription(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	doc, err := f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeGlobal, "", "")
	require.NoError(t, err)

	got, err := f.svc.SetDescription(ctx, doc.ID, "quarterly report")
	require.NoError(t, err)
	require.Equal(t, "quarterly report", got.Description)

	list, err := f.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "quarterly report", list[0].Description, "the edit persists in the catalog")
}

// TestKnowledgeService_Delete pins the delete path and the unknown-id
// sentinel.
func TestKnowledgeService_Delete(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	doc, err := f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeGlobal, "", "")
	require.NoError(t, err)

	require.NoError(t, f.svc.Delete(ctx, doc.ID))
	list, err := f.svc.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)

	err = f.svc.Delete(ctx, "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestKnowledgeService_ListExcludesIndex pins that the scope indexes never
// surface in the management lists (D11).
func TestKnowledgeService_ListExcludesIndex(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	_, err := f.svc.Upload(ctx, "INDEX.md", "text/plain", b64("x"), knowledge.ScopeGlobal, "", "")
	require.Error(t, err, "the index filename is reserved at ingestion")

	_, err = f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeGlobal, "", "")
	require.NoError(t, err)

	list, err := f.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "notes.txt", list[0].Filename)
}

// TestKnowledgeService_MutationsEmitKnowledgeChanged pins the live-update
// event (design D8): every mutation emits knowledge:changed.
func TestKnowledgeService_MutationsEmitKnowledgeChanged(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	after := len(f.emitter.recorded())
	doc, err := f.svc.Upload(ctx, "notes.txt", "text/plain", b64("x"), knowledge.ScopeGlobal, "", "")
	require.NoError(t, err)
	events := drainEvents(t, f.emitter, after, 1)
	require.Equal(t, EventKnowledgeChanged, events[0].Name)
	_, ok := events[0].Data[0].(KnowledgeChangedEvent)
	require.True(t, ok, "the payload is a KnowledgeChangedEvent")

	after = len(f.emitter.recorded())
	_, err = f.svc.SetDescription(ctx, doc.ID, "d")
	require.NoError(t, err)
	events = drainEvents(t, f.emitter, after, 1)
	require.Equal(t, EventKnowledgeChanged, events[0].Name)

	after = len(f.emitter.recorded())
	_, err = f.svc.SetScope(ctx, doc.ID, knowledge.ScopeAgent, f.agent.ID)
	require.NoError(t, err)
	events = drainEvents(t, f.emitter, after, 1)
	require.Equal(t, EventKnowledgeChanged, events[0].Name)

	after = len(f.emitter.recorded())
	require.NoError(t, f.svc.Delete(ctx, doc.ID))
	events = drainEvents(t, f.emitter, after, 1)
	require.Equal(t, EventKnowledgeChanged, events[0].Name)
}

// TestKnowledgeService_RejectionsEmitNothing pins that gate rejections emit
// no event: no mutation happened.
func TestKnowledgeService_RejectionsEmitNothing(t *testing.T) {
	f := newKnowledgeFixture(t)
	ctx := context.Background()

	_, err := f.svc.Upload(ctx, "clip.mp4", "video/mp4", b64("x"), knowledge.ScopeGlobal, "", "")
	require.Error(t, err)
	require.Empty(t, f.emitter.recorded(), "a rejected upload is not a mutation")

	_, err = f.svc.Upload(ctx, "notes.txt", "text/plain", "not base64!", knowledge.ScopeGlobal, "", "")
	require.Error(t, err)
	require.Empty(t, f.emitter.recorded())
}

// mustDecode decodes one base64 payload, failing the test on garbage.
func mustDecode(t *testing.T, data string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(data)
	require.NoError(t, err)
	return raw
}
