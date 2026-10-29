package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/promptgen"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// fakeGenerator is the PromptGenerator test double: it records the calls the
// service makes and fails on demand. It commits nothing to disk — a real
// promptgen service does that through its DocStore, which the service never
// sees.
type fakeGenerator struct {
	mu          sync.Mutex
	createErr   error
	regenErr    error
	createCalls int
	regenCalls  int
	slugs       []string
	params      []promptgen.Params
}

func (f *fakeGenerator) GenerateForCreate(_ context.Context, slug string, params promptgen.Params) (promptgen.Documents, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.slugs = append(f.slugs, slug)
	f.params = append(f.params, params)
	if f.createErr != nil {
		return promptgen.Documents{}, f.createErr
	}
	return promptgen.Documents{Identity: "identity", Soul: "soul", Bootstrap: "bootstrap"}, nil
}

func (f *fakeGenerator) Regenerate(_ context.Context, slug string, params promptgen.Params) (promptgen.Documents, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.regenCalls++
	f.slugs = append(f.slugs, slug)
	f.params = append(f.params, params)
	if f.regenErr != nil {
		return promptgen.Documents{}, f.regenErr
	}
	return promptgen.Documents{Identity: "identity-2", Soul: "soul-2", Bootstrap: "bootstrap-2"}, nil
}

func (f *fakeGenerator) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls
}

func (f *fakeGenerator) regenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.regenCalls
}

func (f *fakeGenerator) lastParams() promptgen.Params {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.params[len(f.params)-1]
}

// agentsFixture bundles the service with its collaborators so each test
// starts from an isolated store, fake generator, workspace root, and
// knowledge corpus.
type agentsFixture struct {
	svc  *AgentsService
	st   *fake.Store
	gen  *fakeGenerator
	ws   *workspaces.Manager
	know *knowledge.Store
	// skills is the global skills library directory backing the
	// workspace-skill bindings (add-agent-created-skills task 2).
	skills string
}

func newAgentsFixture(t *testing.T) *agentsFixture {
	t.Helper()
	st := fake.New()
	gen := &fakeGenerator{}
	ws := workspaces.NewManager(t.TempDir())
	know := knowledge.NewStore(t.TempDir(), st.Documents())
	skills := t.TempDir()
	return &agentsFixture{
		svc:    NewAgentsService(st, gen, ws, know, skills),
		st:     st,
		gen:    gen,
		ws:     ws,
		know:   know,
		skills: skills,
	}
}

// addProvider stores one provider row under the given ID.
func (f *agentsFixture) addProvider(t *testing.T, id string) store.Provider {
	t.Helper()
	provider := store.Provider{ID: id, Type: "anthropic", Name: "Anthropic", APIKey: "sk-test-key"}
	require.NoError(t, f.st.Providers().Create(context.Background(), &provider))
	return provider
}

// validCreateInput returns an input satisfying every validation rule, with
// the fixture's provider wired in.
func (f *agentsFixture) validCreateInput() CreateAgentInput {
	return CreateAgentInput{
		Name:          "Scout",
		Slug:          "scout",
		Description:   "The scout's card summary.",
		Brief:         "Find things quickly and report back.",
		Avatar:        map[string]any{"sex": "woman"},
		ProviderID:    "p1",
		Model:         "claude-sonnet-4-5",
		Temperature:   0.7,
		MaxTokens:     4096,
		ContextWindow: 200000,
		Tools:         []string{"web"},
		Skills:        []string{"search"},
		MCPServers:    []string{"fs"},
	}
}

// seedAgent inserts a row directly through the store, bypassing the service,
// so tests can arrange arbitrary prompt statuses and slugs.
func (f *agentsFixture) seedAgent(t *testing.T, slug, status string) store.Agent {
	t.Helper()
	agent := store.Agent{
		Name:          "Seeded " + slug,
		Slug:          slug,
		Brief:         "stored brief for " + slug,
		PromptsStatus: status,
		ProviderID:    "p1",
		Model:         "claude-sonnet-4-5",
	}
	require.NoError(t, f.st.Agents().Create(context.Background(), &agent))
	return agent
}

// workspaceDir returns the workspace directory path for the slug.
func workspaceDir(t *testing.T, ws *workspaces.Manager, slug string) string {
	t.Helper()
	dir, err := ws.Dir(slug)
	require.NoError(t, err)
	return dir
}

func requireDirExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.NoError(t, err, "expected %s to exist", path)
}

func requireDirAbsent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "expected %s to be absent, got %v", path, err)
}

func TestAgentsService_Create_Success(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	agent, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	require.NotEmpty(t, agent.ID)
	require.Equal(t, "Scout", agent.Name)
	require.Equal(t, "scout", agent.Slug)
	require.Equal(t, "The scout's card summary.", agent.Description)
	require.Equal(t, "Find things quickly and report back.", agent.Brief)
	require.Equal(t, map[string]any{"sex": "woman"}, agent.Avatar)
	require.Equal(t, "p1", agent.ProviderID)
	require.Equal(t, "claude-sonnet-4-5", agent.Model)
	require.Equal(t, 0.7, agent.Temperature)
	require.Equal(t, 4096, agent.MaxTokens)
	require.Equal(t, 200000, agent.ContextWindow)
	require.Equal(t, []string{"web"}, agent.Tools)
	require.Equal(t, []string{"search"}, agent.Skills)
	require.Equal(t, []string{"fs"}, agent.MCPServers)
	require.False(t, agent.CreatedAt.IsZero())
	require.False(t, agent.UpdatedAt.IsZero())
	// The status machine settled before the call returned (design D4).
	require.Equal(t, store.PromptsStatusReady, agent.PromptsStatus)
	require.Empty(t, agent.PromptsError)
	// The workspace directory exists even though the fake generator writes
	// nothing: the service creates it before generation (design D11).
	requireDirExists(t, workspaceDir(t, f.ws, "scout"))
}

// TestAgentsService_SubagentsToggle covers the dedicated subagents capability
// toggle through the service DTO (design D3): an omitted toggle creates the
// agent with subagents enabled (the column's default), an explicit false turns
// it off without touching any other capability field, and both directions
// round-trip through Update into the stored row.
func TestAgentsService_SubagentsToggle(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	// Omitted toggle: nil means on (task 5.1's default).
	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	require.True(t, created.SubagentsEnabled, "an omitted toggle must default the capability on")

	stored, err := f.svc.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.True(t, stored.SubagentsEnabled, "the default-on value must persist through the store")

	// Turn it off through Update: the flag and only the flag moves.
	created.SubagentsEnabled = false
	updated, err := f.svc.Update(context.Background(), created)
	require.NoError(t, err)
	require.False(t, updated.SubagentsEnabled)
	require.Equal(t, []string{"web"}, updated.Tools, "the toggle must not disturb the tool allowlist")
	require.Equal(t, []string{"search"}, updated.Skills)
	require.Equal(t, []string{"fs"}, updated.MCPServers)

	stored, err = f.svc.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.False(t, stored.SubagentsEnabled, "the off state must survive a re-read")

	// And back on.
	stored.SubagentsEnabled = true
	reEnabled, err := f.svc.Update(context.Background(), stored)
	require.NoError(t, err)
	require.True(t, reEnabled.SubagentsEnabled, "the toggle must turn back on")
}

// TestAgentsService_Create_SubagentsExplicitOff covers the explicit-off create
// path: a client that wants an agent without the capability sets the pointer to
// false and the store keeps it off, with the treats-nil-as-on default never
// applied.
func TestAgentsService_Create_SubagentsExplicitOff(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	input := f.validCreateInput()
	off := false
	input.SubagentsEnabled = &off

	created, err := f.svc.Create(context.Background(), input)
	require.NoError(t, err)
	require.False(t, created.SubagentsEnabled, "an explicit false must create the agent with subagents off")

	stored, err := f.svc.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.False(t, stored.SubagentsEnabled)
}

func TestAgentsService_Create_GeneratesFromStoredProvider(t *testing.T) {
	f := newAgentsFixture(t)
	provider := f.addProvider(t, "p1")

	_, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	require.Equal(t, 1, f.gen.createCount())
	require.Equal(t, "scout", f.gen.slugs[0])
	params := f.gen.lastParams()
	require.Equal(t, provider, params.Provider)
	require.Equal(t, "claude-sonnet-4-5", params.Model)
	require.Equal(t, "Scout", params.AgentName)
	require.Equal(t, "Find things quickly and report back.", params.Brief)
}

func TestAgentsService_Create_EmptyName(t *testing.T) {
	f := newAgentsFixture(t)
	input := f.validCreateInput()
	input.Name = ""

	_, err := f.svc.Create(context.Background(), input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent name must not be empty")
	require.Equal(t, 0, f.gen.createCount())
}

func TestAgentsService_Create_EmptyBrief(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	for _, brief := range []string{"", "   "} {
		input := f.validCreateInput()
		input.Brief = brief

		_, err := f.svc.Create(context.Background(), input)
		require.Error(t, err)
		require.Contains(t, err.Error(), "agent brief must not be empty")
	}
	list, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
	require.Equal(t, 0, f.gen.createCount())
}

func TestAgentsService_Create_InvalidSlug(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	for _, slug := range []string{"", "Bad Slug", "-leading", "trailing-", "double--dash", "under_score"} {
		input := f.validCreateInput()
		input.Slug = slug

		_, err := f.svc.Create(context.Background(), input)
		require.Error(t, err, "slug %q must be rejected", slug)
		require.Contains(t, err.Error(), "invalid agent slug")
	}
	list, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
	require.Equal(t, 0, f.gen.createCount())
}

func TestAgentsService_Create_MissingProvider(t *testing.T) {
	f := newAgentsFixture(t)
	input := f.validCreateInput()
	input.ProviderID = "ghost"

	_, err := f.svc.Create(context.Background(), input)
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Contains(t, err.Error(), "provider")
	// Nothing persisted, nothing generated (design D4: validation first).
	list, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
	require.Equal(t, 0, f.gen.createCount())
}

func TestAgentsService_Create_DuplicateSlug(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	_, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	dup := f.validCreateInput()
	dup.Name = "Other Scout"
	_, err = f.svc.Create(context.Background(), dup)
	require.Error(t, err)
	require.Contains(t, err.Error(), "slug")

	list, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, 1, f.gen.createCount())
}

func TestAgentsService_Create_GenerationFails(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	f.gen.createErr = errors.New("provider exploded: 401 unauthorized")

	agent, err := f.svc.Create(context.Background(), f.validCreateInput())
	// The failure is reported as a generation failure, not a rejection.
	require.ErrorIs(t, err, ErrPromptGeneration)
	// The row survives with the failed status and the sanitized reason.
	require.NotEmpty(t, agent.ID)
	require.Equal(t, store.PromptsStatusFailed, agent.PromptsStatus)
	require.Equal(t, "provider authentication failed — check provider API key", agent.PromptsError)

	got, err := f.svc.Get(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusFailed, got.PromptsStatus)
	// The workspace directory survives the failed generation (design D11).
	requireDirExists(t, workspaceDir(t, f.ws, "scout"))
}

func TestAgentsService_Create_WaitsForGeneration(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	// Synchronous generation (design D4): by the time Create returns, the
	// generator has been invoked exactly once and its outcome committed.
	agent, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	require.Equal(t, 1, f.gen.createCount())
	require.Equal(t, store.PromptsStatusReady, agent.PromptsStatus)

	got, err := f.svc.Get(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusReady, got.PromptsStatus)
}

func TestAgentsService_Get_List(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	got, err := f.svc.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.Equal(t, "Scout", got.Name)

	list, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, created.ID, list[0].ID)
}

func TestAgentsService_Get_NotFound(t *testing.T) {
	f := newAgentsFixture(t)

	_, err := f.svc.Get(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestAgentsService_Update_Success(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	created.Name = "Scout II"
	created.Description = "Rewritten summary."
	created.ProviderID = "p1"
	created.Model = "claude-opus-4-6"
	created.Temperature = 0.2
	created.MaxTokens = 1024
	created.ContextWindow = 100000
	created.Tools = []string{"web", "files"}

	updated, err := f.svc.Update(context.Background(), created)
	require.NoError(t, err)
	require.Equal(t, "Scout II", updated.Name)
	require.Equal(t, "Rewritten summary.", updated.Description)
	require.Equal(t, "claude-opus-4-6", updated.Model)
	require.Equal(t, 0.2, updated.Temperature)
	require.Equal(t, 1024, updated.MaxTokens)
	require.Equal(t, 100000, updated.ContextWindow)
	require.Equal(t, []string{"web", "files"}, updated.Tools)

	got, err := f.svc.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.Equal(t, "Scout II", got.Name)
}

func TestAgentsService_Update_KeepsSlugBriefStatus(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	// The client cannot move the immutables: the slug names the workspace
	// directory, the brief is the stored regeneration source, and the
	// prompt status moves only through the generation paths.
	created.Slug = "renamed"
	created.Brief = "client-crafted brief"
	created.PromptsStatus = store.PromptsStatusGenerating
	created.PromptsError = "client-crafted error"

	updated, err := f.svc.Update(context.Background(), created)
	require.NoError(t, err)
	require.Equal(t, "scout", updated.Slug)
	require.Equal(t, "Find things quickly and report back.", updated.Brief)
	require.Equal(t, store.PromptsStatusReady, updated.PromptsStatus)
	require.Empty(t, updated.PromptsError)

	got, err := f.svc.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.Equal(t, "scout", got.Slug)
	require.Equal(t, "Find things quickly and report back.", got.Brief)
	require.Equal(t, store.PromptsStatusReady, got.PromptsStatus)
}

func TestAgentsService_Update_NotFound(t *testing.T) {
	f := newAgentsFixture(t)

	_, err := f.svc.Update(context.Background(), store.Agent{ID: "missing", Name: "x"})
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestAgentsService_Update_EmptyName(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	created.Name = ""

	_, err = f.svc.Update(context.Background(), created)
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent name must not be empty")
}

func TestAgentsService_Delete_Success(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	requireDirExists(t, workspaceDir(t, f.ws, "scout"))

	require.NoError(t, f.svc.Delete(context.Background(), created.ID))

	_, err = f.svc.Get(context.Background(), created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	// The workspace directory goes with the row (design D11).
	requireDirAbsent(t, workspaceDir(t, f.ws, "scout"))
}

func TestAgentsService_Delete_NotFound(t *testing.T) {
	f := newAgentsFixture(t)

	err := f.svc.Delete(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestAgentsService_Delete_RemovesKnowledgeScope pins the delete cascade's
// file half (add-knowledge-corpus design D9, spec data-store "Agent deletion
// removes scoped rows"): the row delete cascades the agent's document rows,
// the agent's knowledge scope directory goes best-effort with it, and global
// and other-agent documents survive untouched.
func TestAgentsService_Delete_RemovesKnowledgeScope(t *testing.T) {
	f := newAgentsFixture(t)
	ctx := context.Background()
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	other := f.seedAgent(t, "rival", store.PromptsStatusReady)

	_, err := f.know.Save(ctx, knowledge.SaveInput{
		Filename: "shared.md", MIME: "text/plain", Data: []byte("shared"),
		Scope: knowledge.ScopeGlobal, Source: knowledge.SourceUser,
	})
	require.NoError(t, err)
	_, err = f.know.Save(ctx, knowledge.SaveInput{
		Filename: "mine.md", MIME: "text/plain", Data: []byte("mine"),
		Scope: knowledge.ScopeAgent, AgentID: agent.ID, AgentSlug: agent.Slug, Source: knowledge.SourceUser,
	})
	require.NoError(t, err)
	_, err = f.know.Save(ctx, knowledge.SaveInput{
		Filename: "theirs.md", MIME: "text/plain", Data: []byte("theirs"),
		Scope: knowledge.ScopeAgent, AgentID: other.ID, AgentSlug: other.Slug, Source: knowledge.SourceUser,
	})
	require.NoError(t, err)

	require.NoError(t, f.svc.Delete(ctx, agent.ID))

	// The deleted agent's scope directory is gone.
	_, err = os.Stat(f.know.AgentDir("scout"))
	require.True(t, os.IsNotExist(err), "the agent's knowledge scope directory must be removed")
	// Global and other-agent documents survive on disk.
	_, err = os.Stat(filepath.Join(f.know.Roots().Global, "shared.md"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(f.know.AgentDir("rival"), "theirs.md"))
	require.NoError(t, err)

	// Rows: the FK cascade removed the agent's documents; the others remain.
	docs, err := f.know.List(ctx)
	require.NoError(t, err)
	byName := make(map[string]bool, len(docs))
	for _, doc := range docs {
		byName[doc.Filename] = true
	}
	require.NotContains(t, byName, "mine.md", "the agent's document rows cascade")
	require.Contains(t, byName, "shared.md")
	require.Contains(t, byName, "theirs.md")
}

func TestAgentsService_Delete_KeepsSessionsRemovesWorkspace(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")

	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	session := store.Session{AgentID: created.ID, Title: "First chat"}
	require.NoError(t, f.st.Sessions().Create(context.Background(), &session))

	require.NoError(t, f.svc.Delete(context.Background(), created.ID))

	sessions, err := f.st.Sessions().ListByAgent(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, session.ID, sessions[0].ID)
	requireDirAbsent(t, workspaceDir(t, f.ws, "scout"))
}

func TestAgentsService_RegeneratePrompts_Success(t *testing.T) {
	f := newAgentsFixture(t)
	provider := f.addProvider(t, "p1")
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)

	require.NoError(t, f.svc.RegeneratePrompts(context.Background(), agent.ID, "make it funnier"))

	require.Equal(t, 1, f.gen.regenCount())
	require.Equal(t, "scout", f.gen.slugs[0])
	params := f.gen.lastParams()
	require.Equal(t, provider, params.Provider)
	require.Equal(t, "claude-sonnet-4-5", params.Model)
	require.Equal(t, "stored brief for scout", params.Brief)
	require.Equal(t, "make it funnier", params.Instruction)

	got, err := f.svc.Get(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusReady, got.PromptsStatus)
	require.Empty(t, got.PromptsError)
}

func TestAgentsService_RegeneratePrompts_ConflictWhileGenerating(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	agent := f.seedAgent(t, "scout", store.PromptsStatusGenerating)

	err := f.svc.RegeneratePrompts(context.Background(), agent.ID, "too late")
	require.ErrorIs(t, err, ErrGenerationInFlight)
	require.Equal(t, 0, f.gen.regenCalls)

	got, err := f.svc.Get(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusGenerating, got.PromptsStatus)
}

func TestAgentsService_RegeneratePrompts_Failure(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	f.gen.regenErr = errors.New("429 rate limit exceeded")

	err := f.svc.RegeneratePrompts(context.Background(), agent.ID, "")
	require.ErrorIs(t, err, ErrPromptGeneration)

	got, err := f.svc.Get(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusFailed, got.PromptsStatus)
	require.Equal(t, "provider rate limit or quota exceeded — retry later", got.PromptsError)
}

func TestAgentsService_RegeneratePrompts_MissingProviderLeavesStatus(t *testing.T) {
	f := newAgentsFixture(t)
	agent := f.seedAgent(t, "scout", store.PromptsStatusReady)
	agent.ProviderID = "ghost"
	require.NoError(t, f.st.Agents().Update(context.Background(), &agent))

	err := f.svc.RegeneratePrompts(context.Background(), agent.ID, "")
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Equal(t, 0, f.gen.regenCalls)

	// Nothing ran, so the status never moved to generating.
	got, err := f.svc.Get(context.Background(), agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusReady, got.PromptsStatus)
}

func TestAgentsService_RegeneratePrompts_NotFound(t *testing.T) {
	f := newAgentsFixture(t)

	err := f.svc.RegeneratePrompts(context.Background(), "missing", "")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestAgentsService_SweepInterruptedGenerations(t *testing.T) {
	f := newAgentsFixture(t)
	generating := f.seedAgent(t, "in-flight", store.PromptsStatusGenerating)
	ready := f.seedAgent(t, "done", store.PromptsStatusReady)
	failed := f.seedAgent(t, "broken", store.PromptsStatusFailed)
	// Give the pre-existing failure its own reason so the sweep must not
	// touch it.
	failed.PromptsError = "older failure"
	require.NoError(t, f.st.Agents().Update(context.Background(), &failed))

	require.NoError(t, f.svc.SweepInterruptedGenerations(context.Background()))

	swept, err := f.svc.Get(context.Background(), generating.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusFailed, swept.PromptsStatus)
	require.Equal(t, "prompt generation interrupted — retry", swept.PromptsError)

	kept, err := f.svc.Get(context.Background(), ready.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusReady, kept.PromptsStatus)

	prior, err := f.svc.Get(context.Background(), failed.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusFailed, prior.PromptsStatus)
	require.Equal(t, "older failure", prior.PromptsError)
}

func TestAgentsService_SweepInterruptedGenerations_NoGeneratingRows(t *testing.T) {
	f := newAgentsFixture(t)
	f.seedAgent(t, "done", store.PromptsStatusReady)

	require.NoError(t, f.svc.SweepInterruptedGenerations(context.Background()))

	list, err := f.svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, store.PromptsStatusReady, list[0].PromptsStatus)
}

func TestAgentsService_GetPromptDocuments_RoundTrip(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	ctx := context.Background()

	// A fresh workspace holds no documents.
	docs, err := f.svc.GetPromptDocuments(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, docs.Identity)
	assert.False(t, docs.IdentityExists)
	assert.Empty(t, docs.Soul)
	assert.False(t, docs.SoulExists)
	assert.Empty(t, docs.Bootstrap)
	assert.False(t, docs.BootstrapExists)

	require.NoError(t, f.svc.SavePromptDocument(ctx, created.ID, workspaces.DocIdentity, "# Scout\nThe identity."))
	require.NoError(t, f.svc.SavePromptDocument(ctx, created.ID, workspaces.DocSoul, "Curious and kind."))

	docs, err = f.svc.GetPromptDocuments(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "# Scout\nThe identity.", docs.Identity)
	assert.True(t, docs.IdentityExists)
	assert.Equal(t, "Curious and kind.", docs.Soul)
	assert.True(t, docs.SoulExists)
	assert.Empty(t, docs.Bootstrap)
	assert.False(t, docs.BootstrapExists)

	// Saving rewrites the file: the next read reflects the new content.
	require.NoError(t, f.svc.SavePromptDocument(ctx, created.ID, workspaces.DocSoul, "Brave and loyal."))
	docs, err = f.svc.GetPromptDocuments(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Brave and loyal.", docs.Soul)
}

func TestAgentsService_SavePromptDocument_BackupOnOverwrite(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, f.svc.SavePromptDocument(ctx, created.ID, workspaces.DocSoul, "first"))
	require.NoError(t, f.svc.SavePromptDocument(ctx, created.ID, workspaces.DocSoul, "second"))

	backup, err := os.ReadFile(filepath.Join(workspaceDir(t, f.ws, "scout"), workspaces.DocSoul+".bak"))
	require.NoError(t, err)
	require.Equal(t, "first", string(backup))
}

func TestAgentsService_SavePromptDocument_RejectsUnknownDoc(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	created, err := f.svc.Create(context.Background(), f.validCreateInput())
	require.NoError(t, err)

	err = f.svc.SavePromptDocument(context.Background(), created.ID, "README.md", "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown document")
}

func TestAgentsService_PromptDocuments_NotFound(t *testing.T) {
	f := newAgentsFixture(t)

	_, err := f.svc.GetPromptDocuments(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	err = f.svc.SavePromptDocument(context.Background(), "missing", workspaces.DocSoul, "x")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestWorkspaceDocStore_CommitFresh(t *testing.T) {
	ws := workspaces.NewManager(t.TempDir())
	docs := NewWorkspaceDocStore(ws)
	want := promptgen.Documents{Identity: "i1", Soul: "s1", Bootstrap: "b1"}

	require.NoError(t, docs.CommitDocuments("scout", want))

	got, exists, err := docs.ReadDocuments("scout")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, want, got)
}

func TestWorkspaceDocStore_ReadDocuments_EmptyWorkspace(t *testing.T) {
	ws := workspaces.NewManager(t.TempDir())
	docs := NewWorkspaceDocStore(ws)

	got, exists, err := docs.ReadDocuments("scout")
	require.NoError(t, err)
	require.False(t, exists)
	require.Equal(t, promptgen.Documents{}, got)
}

func TestWorkspaceDocStore_CommitBacksUpPreviousContent(t *testing.T) {
	ws := workspaces.NewManager(t.TempDir())
	docs := NewWorkspaceDocStore(ws)
	first := promptgen.Documents{Identity: "i1", Soul: "s1", Bootstrap: "b1"}
	require.NoError(t, docs.CommitDocuments("scout", first))

	require.NoError(t, docs.CommitDocuments("scout", promptgen.Documents{Identity: "i2", Soul: "s2", Bootstrap: "b2"}))

	dir := workspaceDir(t, ws, "scout")
	for name, previous := range map[string]string{
		workspaces.DocIdentity:  "i1",
		workspaces.DocSoul:      "s1",
		workspaces.DocBootstrap: "b1",
	} {
		backup, err := os.ReadFile(filepath.Join(dir, name+".bak"))
		require.NoError(t, err, "document %s", name)
		require.Equal(t, previous, string(backup), "backup of %s", name)
	}
}

func TestWorkspaceDocStore_CommitAbortsBeforeAnyWrite(t *testing.T) {
	ws := workspaces.NewManager(t.TempDir())
	docs := NewWorkspaceDocStore(ws)
	dir := workspaceDir(t, ws, "scout")
	_, err := ws.Create("scout")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, workspaces.DocSoul), []byte("s1"), 0600))
	// An unreadable IDENTITY entry fails phase one (the backup pass), which
	// must abort the whole commit before any content is written.
	require.NoError(t, os.Mkdir(filepath.Join(dir, workspaces.DocIdentity), 0700))

	err = docs.CommitDocuments("scout", promptgen.Documents{Identity: "i2", Soul: "s2", Bootstrap: "b2"})
	require.Error(t, err)

	soul, err := os.ReadFile(filepath.Join(dir, workspaces.DocSoul))
	require.NoError(t, err)
	require.Equal(t, "s1", string(soul))
	_, err = os.Stat(filepath.Join(dir, workspaces.DocSoul+".bak"))
	require.True(t, os.IsNotExist(err))
}
