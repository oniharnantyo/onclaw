package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/migrations"
)

func init() {
	storeport.Register("postgres", Open)
}

// Executor abstracts common query execution methods between pgxpool.Pool and pgx.Tx.
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// store is a PostgreSQL implementation of storeport.Store using pgx/v5.
type store struct {
	pool *pgxpool.Pool
	db   Executor
}

// Open creates a Store instance using the given DSN configuration.
func Open(ctx context.Context, cfg storeport.DSNConfig) (storeport.Store, error) {
	return New(ctx, cfg.DSN)
}

// New creates a new Store connected to the given PostgreSQL DSN.
func New(ctx context.Context, dsn string) (storeport.Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("%w: missing database DSN", domain.ErrInvalid)
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid database DSN: %v", domain.ErrInvalid, err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	return &store{
		pool: pool,
		db:   pool,
	}, nil
}

// Users returns the UserStore sub-port.
func (s *store) Users() storeport.UserStore {
	return NewUserStore(s.db)
}

// Workspaces returns the WorkspaceStore sub-port.
func (s *store) Workspaces() storeport.WorkspaceStore {
	return NewWorkspaceStore(s.db)
}

// Roles returns the RoleStore sub-port.
func (s *store) Roles() storeport.RoleStore {
	return NewRoleStore(s.db)
}

// Members returns the MemberStore sub-port.
func (s *store) Members() storeport.MemberStore {
	return NewMemberStore(s.db)
}

// Providers returns the ProviderStore sub-port.
func (s *store) Providers() storeport.ProviderStore {
	return NewProviderStore(s.db)
}

// Agents returns the AgentStore sub-port.
func (s *store) Agents() storeport.AgentStore {
	return NewAgentStore(s.db)
}

// Memories returns the MemoryStore sub-port.
func (s *store) Memories() storeport.MemoryStore {
	return NewMemoryStore(s.db)
}

// SessionEvents returns the SessionEventStore sub-port.
func (s *store) SessionEvents() storeport.SessionEventStore {
	return NewSessionEventStore(s.db)
}

// SessionCheckpoints returns the SessionCheckpointStore sub-port.
func (s *store) SessionCheckpoints() storeport.SessionCheckpointStore {
	return NewSessionCheckpointStore(s.db)
}

// APIKeys returns the WorkspaceAPIKeyStore sub-port.
func (s *store) APIKeys() storeport.WorkspaceAPIKeyStore {
	return NewAPIKeyStore(s.db)
}

// WorkspaceSkills returns the WorkspaceSkillStore sub-port.
func (s *store) WorkspaceSkills() storeport.WorkspaceSkillStore {
	return NewWorkspaceSkillStore(s.db)
}

// ToolSettings returns the ToolSettingsStore sub-port.
func (s *store) ToolSettings() storeport.ToolSettingsStore {
	return NewToolSettingStore(s.db)
}

// WorkspaceMCPServers returns the WorkspaceMCPServers sub-port.
func (s *store) WorkspaceMCPServers() storeport.WorkspaceMCPServers {
	return NewWorkspaceMCPServerStore(s.db)
}

// AgentMCPServers returns the AgentMCPServers sub-port.
func (s *store) AgentMCPServers() storeport.AgentMCPServers {
	return NewAgentMCPServerStore(s.db)
}

// Hooks returns the HookStore sub-port.
func (s *store) Hooks() storeport.HookStore {
	return NewHookStore(s.db)
}

// Channels returns the ChannelStore sub-port.
func (s *store) Channels() storeport.ChannelStore {
	return NewChannelStore(s.db)
}

// WorkSessions returns the WorkSessionStore sub-port.
func (s *store) WorkSessions() storeport.WorkSessionStore {
	return NewWorkSessionStore(s.db)
}

// Attachments returns the AttachmentStore sub-port.
func (s *store) Attachments() storeport.AttachmentStore {
	return NewAttachmentStore(s.db)
}

// WorkspaceStorage returns the WorkspaceStorageStore sub-port.
func (s *store) WorkspaceStorage() storeport.WorkspaceStorageStore {
	return NewWorkspaceStorageStore(s.db)
}

// Schedulers returns the SchedulerStore sub-port.
func (s *store) Schedulers() storeport.SchedulerStore {
	return NewSchedulerStore(s.db)
}

// Gateways returns the GatewayStore sub-port.
func (s *store) Gateways() storeport.GatewayStore {
	return NewGatewayStore(s.db)
}

// GatewayBindings returns the GatewayBindings sub-port.
func (s *store) GatewayBindings() storeport.GatewayBindings {
	return NewGatewayBindingStore(s.db)
}

// GatewayLinks returns the GatewayLinks sub-port.
func (s *store) GatewayLinks() storeport.GatewayLinks {
	return NewGatewayLinkStore(s.db)
}

// GatewayOutbox returns the GatewayOutbox sub-port.
func (s *store) GatewayOutbox() storeport.GatewayOutbox {
	return NewGatewayOutboxStore(s.db)
}

// WithTx executes the provided function within a database transaction.
// If the store is already in a transaction, a SAVEPOINT is used for nested isolation.
func (s *store) WithTx(ctx context.Context, fn func(storeport.Store) error) error {
	if tx, ok := s.db.(pgx.Tx); ok {
		subTx, err := tx.Begin(ctx)
		if err != nil {
			return convertError(err)
		}
		defer func() {
			_ = subTx.Rollback(ctx)
		}()

		txStore := &store{
			pool: s.pool,
			db:   subTx,
		}

		if err := fn(txStore); err != nil {
			return err
		}

		if err := subTx.Commit(ctx); err != nil {
			return convertError(err)
		}
		return nil
	}

	if s.pool == nil {
		return fmt.Errorf("%w: cannot start transaction without connection pool", domain.ErrInvalid)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txStore := &store{
		pool: s.pool,
		db:   tx,
	}

	if err := fn(txStore); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return convertError(err)
	}

	return nil
}

// Close closes the underlying connection pool.
func (s *store) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// migrator handles database schema migrations for PostgreSQL.
type migrator struct {
	dsn string
}

// NewMigrator creates a new migrator instance for the given PostgreSQL DSN.
func NewMigrator(dsn string) *migrator {
	return &migrator{dsn: dsn}
}

func (m *migrator) driver() (*migrate.Migrate, error) {
	srcDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to load migration source: %w", err)
	}

	pgxURL := m.dsn
	if strings.HasPrefix(pgxURL, "postgres://") {
		pgxURL = "pgx5://" + strings.TrimPrefix(pgxURL, "postgres://")
	} else if strings.HasPrefix(pgxURL, "postgresql://") {
		pgxURL = "pgx5://" + strings.TrimPrefix(pgxURL, "postgresql://")
	} else if !strings.HasPrefix(pgxURL, "pgx5://") && !strings.HasPrefix(pgxURL, "pgx://") {
		pgxURL = "pgx5://" + pgxURL
	}

	mig, err := migrate.NewWithSourceInstance("iofs", srcDriver, pgxURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migration driver: %w", err)
	}
	return mig, nil
}

// Up applies all pending database migrations.
func (m *migrator) Up() error {
	mig, err := m.driver()
	if err != nil {
		return err
	}
	defer mig.Close()

	if err := mig.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration up failed: %w", err)
	}
	return nil
}

// Down rolls back migrations. If steps <= 0, all migrations are rolled back.
func (m *migrator) Down(steps int) error {
	mig, err := m.driver()
	if err != nil {
		return err
	}
	defer mig.Close()

	if steps <= 0 {
		if err := mig.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migration down failed: %w", err)
		}
	} else {
		if err := mig.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migration down failed: %w", err)
		}
	}
	return nil
}

// MigrateToVersion migrates the database to a specific version.
func (m *migrator) MigrateToVersion(version uint) error {
	mig, err := m.driver()
	if err != nil {
		return err
	}
	defer mig.Close()

	if err := mig.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration to version %d failed: %w", version, err)
	}
	return nil
}

// Status returns the current migration version and whether the schema is dirty.
func (m *migrator) Status() (version uint, dirty bool, err error) {
	mig, err := m.driver()
	if err != nil {
		return 0, false, err
	}
	defer mig.Close()

	v, d, err := mig.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to retrieve migration status: %w", err)
	}
	return v, d, nil
}
