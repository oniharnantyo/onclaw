package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// oauthAppStore implements storeport.OAuthApps for PostgreSQL. Rows are
// instance-scoped (provider is the primary key) — no workspace partition
// anywhere.
type oauthAppStore struct {
	db Executor
}

// NewOAuthAppStore creates a new OAuthApps store with the given database
// executor.
func NewOAuthAppStore(db Executor) storeport.OAuthApps {
	return &oauthAppStore{db: db}
}

// Upsert create-or-replaces the provider's app (INSERT ... ON CONFLICT):
// identity (provider) and created_at are fixed at birth, the credential
// fields are replaced, and updated_at advances. RETURNING re-reads the row so
// the caller's struct stays faithful to what is stored.
func (oas *oauthAppStore) Upsert(ctx context.Context, app *domain.InstanceOAuthApp) error {
	if app == nil {
		return domain.ErrInvalid
	}
	if err := app.Validate(); err != nil {
		return err
	}

	query := `
		INSERT INTO instance_oauth_apps (
			provider, client_id, client_secret_ciphertext, client_secret_hint,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $5
		)
		ON CONFLICT (provider) DO UPDATE SET
			client_id = EXCLUDED.client_id,
			client_secret_ciphertext = EXCLUDED.client_secret_ciphertext,
			client_secret_hint = EXCLUDED.client_secret_hint,
			updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`
	now := time.Now().UTC()
	err := oas.db.QueryRow(ctx, query,
		app.Provider,
		app.ClientID,
		app.ClientSecretCiphertext,
		app.ClientSecretHint,
		now,
	).Scan(&app.CreatedAt, &app.UpdatedAt)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (oas *oauthAppStore) Get(ctx context.Context, provider string) (*domain.InstanceOAuthApp, error) {
	if provider == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT provider, client_id, client_secret_ciphertext, client_secret_hint,
		       created_at, updated_at
		FROM instance_oauth_apps
		WHERE provider = $1
	`
	return scanOAuthApp(oas.db.QueryRow(ctx, query, provider))
}

func (oas *oauthAppStore) List(ctx context.Context) ([]domain.InstanceOAuthApp, error) {
	query := `
		SELECT provider, client_id, client_secret_ciphertext, client_secret_hint,
		       created_at, updated_at
		FROM instance_oauth_apps
		ORDER BY provider ASC
	`
	rows, err := oas.db.Query(ctx, query)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	apps := make([]domain.InstanceOAuthApp, 0)
	for rows.Next() {
		app, err := scanOAuthApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, *app)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return apps, nil
}

func scanOAuthApp(row pgx.Row) (*domain.InstanceOAuthApp, error) {
	var a domain.InstanceOAuthApp
	err := row.Scan(
		&a.Provider,
		&a.ClientID,
		&a.ClientSecretCiphertext,
		&a.ClientSecretHint,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &a, nil
}
