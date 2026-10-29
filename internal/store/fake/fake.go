// Package fake implements an in-memory store.Store for unit tests. Entities
// are held as values behind one mutex; every read returns a copy so callers
// can never alias the fake's internal state. WithTx provides no isolation:
// it simply runs fn against the same store — atomicity semantics are
// exercised against the sqlite adapter, not here.
package fake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Store is the in-memory implementation of store.Store.
type Store struct {
	mu sync.Mutex

	agents          map[string]store.Agent
	sessions        map[string]store.Session
	providers       map[string]store.Provider
	mcpServers      map[string]store.MCPServer
	mcpOAuthTokens  map[string]store.MCPOAuthToken // key: serverID
	schedules       map[string]store.Schedule
	hooks           map[string]store.Hook
	gatewaySettings *store.GatewaySettings
	sessionEvents   map[string][]store.SessionEvent // key: sessionID, kept in Seq order
	checkpoints     map[string][]byte
	skills          map[string]store.Skill          // key: name
	toolStates      map[string]store.ToolState      // key: name
	agentHooks      map[agentHookKey]int            // key: (agentID, hookID), value: position
	hookExecutions  []store.HookExecution           // append-only audit trail
	toolSettings    map[string]string               // key: tool, value: opaque JSON
	attachments     map[string]store.Attachment     // key: capability key
	documents       map[string]store.Document       // key: document id
	documentPresets map[string]store.DocumentPreset // key: preset id
	providerModels  map[int64]store.ProviderModel
	nextModelID     int64
	memoryTraces    map[string]store.MemoryTrace // key: trace id, append-only
	memoryNotes     map[memoryNoteKey]store.MemoryNoteState
	memoryStaging   map[memoryStagingKey]store.MemoryStaging
	memorySweeps    map[string]store.MemorySweep // key: agent id
	// templateDir is the lazily created directory the filesystem-backed
	// template store roots at (see documenttemplates.go); empty until a
	// template is first stored or looked up through the aggregate.
	templateDir string
}

// New returns an empty in-memory store.
func New() *Store {
	return &Store{
		agents:          make(map[string]store.Agent),
		sessions:        make(map[string]store.Session),
		providers:       make(map[string]store.Provider),
		mcpServers:      make(map[string]store.MCPServer),
		mcpOAuthTokens:  make(map[string]store.MCPOAuthToken),
		schedules:       make(map[string]store.Schedule),
		hooks:           make(map[string]store.Hook),
		sessionEvents:   make(map[string][]store.SessionEvent),
		checkpoints:     make(map[string][]byte),
		skills:          make(map[string]store.Skill),
		toolStates:      make(map[string]store.ToolState),
		agentHooks:      make(map[agentHookKey]int),
		toolSettings:    make(map[string]string),
		attachments:     make(map[string]store.Attachment),
		documents:       make(map[string]store.Document),
		documentPresets: make(map[string]store.DocumentPreset),
		providerModels:  make(map[int64]store.ProviderModel),
		nextModelID:     1,
		memoryTraces:    make(map[string]store.MemoryTrace),
		memoryNotes:     make(map[memoryNoteKey]store.MemoryNoteState),
		memoryStaging:   make(map[memoryStagingKey]store.MemoryStaging),
		memorySweeps:    make(map[string]store.MemorySweep),
	}
}

func (s *Store) Agents() store.AgentStore                    { return &agentStore{s} }
func (s *Store) Sessions() store.SessionsStore               { return &sessionsStore{s} }
func (s *Store) Providers() store.ProviderStore              { return &providerStore{s} }
func (s *Store) MCPServers() store.MCPServerStore            { return &mcpServerStore{s} }
func (s *Store) MCPOAuthTokens() store.MCPOAuthTokenStore    { return &mcpOAuthTokenStore{s} }
func (s *Store) Schedules() store.ScheduleStore              { return &scheduleStore{s} }
func (s *Store) Hooks() store.HookStore                      { return &hookStore{s} }
func (s *Store) GatewaySettings() store.GatewaySettingsStore { return &gatewaySettingsStore{s} }
func (s *Store) SessionEvents() store.SessionEventStore      { return &sessionEventStore{s} }
func (s *Store) Checkpoints() store.CheckpointStore          { return &checkpointStore{s} }
func (s *Store) Skills() store.SkillRegistry                 { return &skillRegistry{s} }
func (s *Store) ToolStates() store.ToolStateStore            { return &toolStateStore{s} }
func (s *Store) AgentHooks() store.AgentHookStore            { return &agentHookStore{s} }
func (s *Store) HookExecutions() store.HookExecutionStore    { return &hookExecutionStore{s} }
func (s *Store) ToolSettings() store.ToolSettingStore        { return &toolSettingStore{s} }
func (s *Store) Attachments() store.AttachmentStore          { return &attachmentStore{s} }
func (s *Store) Documents() store.DocumentStore              { return &documentStore{s} }
func (s *Store) DocumentPresets() store.DocumentPresetStore  { return &documentPresetStore{s} }
func (s *Store) ProviderModels() store.ProviderModelStore    { return &providerModelStore{s} }
func (s *Store) Memory() store.MemoryStore                   { return &memoryStore{s} }

func (s *Store) Create(ctx context.Context, model *store.ProviderModel) error {
	return (&providerModelStore{s}).Create(ctx, model)
}

func (s *Store) ByID(ctx context.Context, id int64) (*store.ProviderModel, error) {
	return (&providerModelStore{s}).ByID(ctx, id)
}

func (s *Store) ByProvider(ctx context.Context, providerID string) ([]*store.ProviderModel, error) {
	return (&providerModelStore{s}).ByProvider(ctx, providerID)
}

func (s *Store) Update(ctx context.Context, model *store.ProviderModel) error {
	return (&providerModelStore{s}).Update(ctx, model)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	return (&providerModelStore{s}).Delete(ctx, id)
}

func (s *Store) DeleteByProvider(ctx context.Context, providerID string) error {
	return (&providerModelStore{s}).DeleteByProvider(ctx, providerID)
}

func (s *Store) UpsertMissing(ctx context.Context, models []*store.ProviderModel) (int, error) {
	return (&providerModelStore{s}).UpsertMissing(ctx, models)
}

func (s *Store) deleteProviderModelsLocked(providerID string) {
	for id, m := range s.providerModels {
		if m.ProviderID == providerID {
			delete(s.providerModels, id)
		}
	}
}

// WithTx runs fn against the same in-memory store; there is no transaction
// isolation to simulate.
func (s *Store) WithTx(_ context.Context, fn func(store.Store) error) error {
	return fn(s)
}

// Close is a no-op for the in-memory store.
func (s *Store) Close() error { return nil }

// nextID returns a random hex ID, mirroring the store-assigned identity of
// the real adapters.
func nextID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("fake: generate id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// stamp fills zero timestamps app-side, mirroring the app-managed timestamp
// invariant.
func stamp(createdAt, updatedAt *time.Time) {
	now := time.Now().UTC()
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt.IsZero() {
		*updatedAt = now
	}
}
