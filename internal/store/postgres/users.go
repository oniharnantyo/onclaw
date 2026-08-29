package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// userStore implements storeport.UserStore for PostgreSQL.
type userStore struct {
	db Executor
}

// NewUserStore creates a new UserStore with the given database executor.
func NewUserStore(db Executor) storeport.UserStore {
	return &userStore{db: db}
}

func (us *userStore) Create(ctx context.Context, u *domain.User) error {
	if u == nil {
		return domain.ErrInvalid
	}
	email := domain.NormalizeEmail(u.Email)
	if email == "" {
		return fmt.Errorf("%w: user email cannot be empty", domain.ErrInvalid)
	}
	if err := domain.ValidateEmail(email); err != nil {
		return err
	}
	if u.Name == "" {
		return fmt.Errorf("%w: user name cannot be empty", domain.ErrInvalid)
	}

	if u.ID == "" {
		u.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	u.Email = email

	query := `
		INSERT INTO users (id, email, name, password_hash, avatar_key, avatar_url, disabled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := us.db.Exec(ctx, query,
		u.ID,
		u.Email,
		u.Name,
		u.PasswordHash,
		u.AvatarKey,
		u.AvatarURL,
		u.DisabledAt,
		u.CreatedAt,
		u.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (us *userStore) ByEmail(ctx context.Context, email string) (*domain.User, error) {
	email = domain.NormalizeEmail(email)
	if email == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, email, name, password_hash, avatar_key, avatar_url, disabled_at, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	var u domain.User
	err := us.db.QueryRow(ctx, query, email).Scan(
		&u.ID,
		&u.Email,
		&u.Name,
		&u.PasswordHash,
		&u.AvatarKey,
		&u.AvatarURL,
		&u.DisabledAt,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &u, nil
}

func (us *userStore) ByID(ctx context.Context, id string) (*domain.User, error) {
	if id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, email, name, password_hash, avatar_key, avatar_url, disabled_at, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	var u domain.User
	err := us.db.QueryRow(ctx, query, id).Scan(
		&u.ID,
		&u.Email,
		&u.Name,
		&u.PasswordHash,
		&u.AvatarKey,
		&u.AvatarURL,
		&u.DisabledAt,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &u, nil
}

func (us *userStore) List(ctx context.Context) ([]domain.User, error) {
	query := `
		SELECT u.id, u.email, u.name, u.password_hash, u.avatar_key, u.avatar_url, u.disabled_at, u.created_at, u.updated_at,
		       COUNT(wm.workspace_id) AS membership_count
		FROM users u
		LEFT JOIN workspace_members wm ON u.id = wm.user_id
		GROUP BY u.id
		ORDER BY u.created_at ASC, u.id ASC
	`
	rows, err := us.db.Query(ctx, query)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(
			&u.ID,
			&u.Email,
			&u.Name,
			&u.PasswordHash,
			&u.AvatarKey,
			&u.AvatarURL,
			&u.DisabledAt,
			&u.CreatedAt,
			&u.UpdatedAt,
			&u.MembershipCount,
		); err != nil {
			return nil, convertError(err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return users, nil
}

func (us *userStore) SetDisabled(ctx context.Context, id string, at *time.Time) error {
	if id == "" {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	query := `
		UPDATE users
		SET disabled_at = $1, updated_at = $2
		WHERE id = $3
	`
	tag, err := us.db.Exec(ctx, query, at, now, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (us *userStore) SetPasswordHash(ctx context.Context, id string, hash string) error {
	if id == "" {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	query := `
		UPDATE users
		SET password_hash = $1, updated_at = $2
		WHERE id = $3
	`
	tag, err := us.db.Exec(ctx, query, hash, now, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (us *userStore) Update(ctx context.Context, u *domain.User) error {
	if u == nil || u.ID == "" {
		return domain.ErrInvalid
	}

	if u.Email != "" {
		u.Email = domain.NormalizeEmail(u.Email)
		if err := domain.ValidateEmail(u.Email); err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	query := `
		UPDATE users
		SET email = CASE WHEN $1 <> '' THEN $1 ELSE email END,
		    name = CASE WHEN $2 <> '' THEN $2 ELSE name END,
		    avatar_key = $3,
		    avatar_url = $4,
		    disabled_at = $5,
		    password_hash = CASE WHEN $6::boolean THEN $7 ELSE password_hash END,
		    updated_at = $8
		WHERE id = $9
		RETURNING email, name, password_hash, avatar_key, avatar_url, disabled_at, created_at, updated_at
	`
	hasPassword := u.PasswordHash != nil
	var pwd string
	if hasPassword {
		pwd = *u.PasswordHash
	}

	err := us.db.QueryRow(ctx, query,
		u.Email,
		u.Name,
		u.AvatarKey,
		u.AvatarURL,
		u.DisabledAt,
		hasPassword,
		pwd,
		now,
		u.ID,
	).Scan(
		&u.Email,
		&u.Name,
		&u.PasswordHash,
		&u.AvatarKey,
		&u.AvatarURL,
		&u.DisabledAt,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}
