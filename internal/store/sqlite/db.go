// Package sqlite is the SQLite adapter for the store ports, backed by
// modernc.org/sqlite with WAL mode and embedded forward-only migrations.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	// Pure-Go SQLite driver (design D2: no CGO). Blank import registers the
	// "sqlite" database/sql driver.
	_ "modernc.org/sqlite"

	"github.com/oniharnantyo/buddies/internal/store"
)

// busyTimeoutMS bounds how long a writer waits for the SQLite write lock
// before giving up (design D2: WAL + bounded contention between UI calls and
// background services).
const busyTimeoutMS = 5000

// Store is the sqlite-backed implementation of store.Store. It MUST have
// migrations applied (Open does this) before being handed to services.
type Store struct {
	db          *sql.DB
	agents      *agentStore
	sessions    *sessionsStore
	providers   *providerStore
	mcp         *mcpServerStore
	mcpTokens   *mcpOAuthTokenStore
	schedules   *scheduleStore
	hooks       *hookStore
	gateway     *gatewaySettingsStore
	events      *sessionEventStore
	checkpts    *checkpointStore
	skills      *skillRegistry
	tools       *toolStateStore
	agentHooks  *agentHookStore
	hookExecs   *hookExecutionStore
	toolConfigs *toolSettingStore
	attachments *attachmentStore
	documents   *documentStore
	presets     *documentPresetStore
	// templates is the on-disk template store for template-document presets
	// (add-document-editing design D6). It is filesystem-backed — it needs
	// the data directory, not the database — so it is rooted at the database's
	// parent (the resolved layout's root, the backfill's precedent) and takes
	// no part in WithTx.
	templates store.DocumentTemplateStore
	models    *providerModelStore
	memory    *memoryStore
}

// Open connects to (creating if necessary) the SQLite database at path,
// enables WAL mode and a busy timeout, and applies pending migrations. A
// migration failure is returned as an error and no store is produced — the
// composition root must treat that as fatal so services never run against a
// stale schema.
func Open(ctx context.Context, path string) (store.Store, error) {
	migs, err := embeddedMigrations()
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return openWith(ctx, path, migs)
}

// openWith connects, applies the given migrations, and only then returns a
// store. It is the startup gate: on any migration error nothing usable is
// handed back.
func openWith(ctx context.Context, path string, migs []migration) (store.Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("open store: resolve path: %w", err)
	}

	db, err := sql.Open("sqlite", dsn(abs))
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}

	// The legacy system-prompt backfill runs against the boundary schema
	// (after 000004 added the new columns and before 000005 drops the
	// column). The workspaces root is the sibling "workspaces" directory of
	// the database's parent, mirroring the resolved layout.
	root := filepath.Join(filepath.Dir(abs), "workspaces")
	backfill := func(ctx context.Context, db *sql.DB) error {
		return backfillLegacySystemPrompts(ctx, db, root)
	}
	if err := migrate(ctx, db, migs, backfill); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open store %s: migrate: %w", path, err)
	}
	// The template store roots at the data directory — the database's parent,
	// mirroring the resolved layout exactly like the backfill's workspaces
	// root above (add-document-editing design D6).
	return newStore(db, filepath.Dir(abs)), nil
}

// dsn builds the modernc.org/sqlite DSN: writes start as BEGIN IMMEDIATE
// (so concurrent writers queue on the busy timeout instead of failing a
// deferred-lock upgrade) and every pooled connection enables WAL mode, the
// busy timeout, and foreign keys via _pragma.
func dsn(path string) string {
	q := url.Values{}
	q.Add("_txlock", "immediate")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS))
	q.Add("_pragma", "foreign_keys(1)")
	return "file:" + path + "?" + q.Encode()
}

// newStore wires the per-aggregate stores onto one *sql.DB. The token
// store's per-server locks (design D3) are created once here so every
// instance derived from this database shares them. dataDir is the on-disk
// data directory the filesystem-backed template store roots at.
func newStore(db *sql.DB, dataDir string) *Store {
	s := &Store{db: db}
	locks := newServerLocks()
	s.agents = &agentStore{db}
	s.sessions = &sessionsStore{db}
	s.providers = &providerStore{db}
	s.mcp = &mcpServerStore{db}
	s.mcpTokens = &mcpOAuthTokenStore{e: db, locks: locks}
	s.schedules = &scheduleStore{db}
	s.hooks = &hookStore{db}
	s.gateway = &gatewaySettingsStore{db}
	s.events = &sessionEventStore{db}
	s.checkpts = &checkpointStore{db}
	s.skills = &skillRegistry{db}
	s.tools = &toolStateStore{db}
	s.agentHooks = &agentHookStore{db}
	s.hookExecs = &hookExecutionStore{db}
	s.toolConfigs = &toolSettingStore{db}
	s.attachments = &attachmentStore{db}
	s.documents = &documentStore{db}
	s.presets = &documentPresetStore{db}
	s.templates = store.NewDocumentTemplateStore(dataDir)
	s.models = &providerModelStore{db}
	s.memory = &memoryStore{db}
	return s
}

func (s *Store) Agents() store.AgentStore                       { return s.agents }
func (s *Store) Sessions() store.SessionsStore                  { return s.sessions }
func (s *Store) Providers() store.ProviderStore                 { return s.providers }
func (s *Store) MCPServers() store.MCPServerStore               { return s.mcp }
func (s *Store) MCPOAuthTokens() store.MCPOAuthTokenStore       { return s.mcpTokens }
func (s *Store) Schedules() store.ScheduleStore                 { return s.schedules }
func (s *Store) Hooks() store.HookStore                         { return s.hooks }
func (s *Store) GatewaySettings() store.GatewaySettingsStore    { return s.gateway }
func (s *Store) SessionEvents() store.SessionEventStore         { return s.events }
func (s *Store) Checkpoints() store.CheckpointStore             { return s.checkpts }
func (s *Store) Skills() store.SkillRegistry                    { return s.skills }
func (s *Store) ToolStates() store.ToolStateStore               { return s.tools }
func (s *Store) AgentHooks() store.AgentHookStore               { return s.agentHooks }
func (s *Store) HookExecutions() store.HookExecutionStore       { return s.hookExecs }
func (s *Store) ToolSettings() store.ToolSettingStore           { return s.toolConfigs }
func (s *Store) Attachments() store.AttachmentStore             { return s.attachments }
func (s *Store) Documents() store.DocumentStore                 { return s.documents }
func (s *Store) DocumentPresets() store.DocumentPresetStore     { return s.presets }
func (s *Store) DocumentTemplates() store.DocumentTemplateStore { return s.templates }
func (s *Store) ProviderModels() store.ProviderModelStore       { return s.models }
func (s *Store) Memory() store.MemoryStore                      { return s.memory }

func (s *Store) Create(ctx context.Context, model *store.ProviderModel) error {
	return s.models.Create(ctx, model)
}

func (s *Store) ByID(ctx context.Context, id int64) (*store.ProviderModel, error) {
	return s.models.ByID(ctx, id)
}

func (s *Store) ByProvider(ctx context.Context, providerID string) ([]*store.ProviderModel, error) {
	return s.models.ByProvider(ctx, providerID)
}

func (s *Store) Update(ctx context.Context, model *store.ProviderModel) error {
	return s.models.Update(ctx, model)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	return s.models.Delete(ctx, id)
}

func (s *Store) DeleteByProvider(ctx context.Context, providerID string) error {
	return s.models.DeleteByProvider(ctx, providerID)
}

func (s *Store) UpsertMissing(ctx context.Context, models []*store.ProviderModel) (int, error) {
	return s.models.UpsertMissing(ctx, models)
}

// Close releases the underlying database connections.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	return nil
}
