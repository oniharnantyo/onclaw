package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

// executor is the query surface shared by *sql.DB (outside a transaction)
// and *sql.Tx (inside the WithTx transaction). Per-aggregate stores are
// written against it so their operations join an ambient transaction when
// one is open.
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx runs fn atomically: the Store handed to fn routes every write
// through one transaction. If fn returns an error the transaction rolls
// back, leaving no partial rows; otherwise it commits.
func (s *Store) WithTx(ctx context.Context, fn func(store.Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE via the DSN txlock
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txStore := &Store{
		db:          s.db,
		agents:      &agentStore{tx},
		sessions:    &sessionsStore{tx},
		providers:   &providerStore{tx},
		mcp:         &mcpServerStore{tx},
		mcpTokens:   &mcpOAuthTokenStore{e: tx, locks: s.mcpTokens.locks},
		schedules:   &scheduleStore{tx},
		hooks:       &hookStore{tx},
		gateway:     &gatewaySettingsStore{tx},
		events:      &sessionEventStore{tx},
		checkpts:    &checkpointStore{tx},
		skills:      &skillRegistry{tx},
		tools:       &toolStateStore{tx},
		agentHooks:  &agentHookStore{tx},
		hookExecs:   &hookExecutionStore{tx},
		toolConfigs: &toolSettingStore{tx},
		attachments: &attachmentStore{tx},
		presets:     &documentPresetStore{tx},
		models:      &providerModelStore{tx},
	}

	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("transaction: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
