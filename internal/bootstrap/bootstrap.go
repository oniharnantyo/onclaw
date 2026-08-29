// Package bootstrap handles fresh-instance initialization, master tenant setup, and superadmin seeding.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// bootstrapper handles fresh-instance initialization, master tenant setup, and superadmin seeding with injected dependencies.
type bootstrapper struct {
	store  store.Store
	hasher auth.PasswordHasher
}

// New creates a new bootstrapper instance with the provided store and default password hasher.
func New(st store.Store) *bootstrapper {
	return NewWithHasher(st, auth.NewPasswordHasher())
}

// NewWithHasher creates a new bootstrapper instance with the provided store and custom password hasher.
func NewWithHasher(st store.Store, hasher auth.PasswordHasher) *bootstrapper {
	if hasher == nil {
		hasher = auth.NewPasswordHasher()
	}
	return &bootstrapper{
		store:  st,
		hasher: hasher,
	}
}

// SuperadminSeedConfig holds configuration for seeding an initial superadmin account.
type SuperadminSeedConfig struct {
	Email        string
	Password     string
	PasswordFile string
	Name         string
}

// EnsureMaster idempotently ensures that the master workspace and its built-in roles
// (Superadmin and Member) exist.
func (b *bootstrapper) EnsureMaster(ctx context.Context) (*domain.Workspace, error) {
	var master *domain.Workspace

	err := b.store.WithTx(ctx, func(txStore store.Store) error {
		ws, err := txStore.Workspaces().BySlug(ctx, domain.MasterWorkspaceSlug)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("failed to query master workspace: %w", err)
		}

		if errors.Is(err, domain.ErrNotFound) {
			ws = &domain.Workspace{
				Slug:     domain.MasterWorkspaceSlug,
				Name:     "Master",
				Timezone: "UTC",
				IsMaster: true,
			}
			if err := txStore.Workspaces().Create(ctx, ws); err != nil {
				return fmt.Errorf("failed to create master workspace: %w", err)
			}
		}

		// Ensure Superadmin built-in role
		superadminRole, err := txStore.Roles().FindByName(ctx, ws.ID, domain.RoleSuperadmin)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("failed to query superadmin role: %w", err)
		}
		if errors.Is(err, domain.ErrNotFound) {
			superadminRole = &domain.Role{
				WorkspaceID: ws.ID,
				Name:        domain.RoleSuperadmin,
				IsOwner:     true,
				Permissions: domain.SuperadminPermissions,
				BuiltIn:     true,
			}
			if err := txStore.Roles().Create(ctx, superadminRole); err != nil {
				return fmt.Errorf("failed to create superadmin role: %w", err)
			}
		}

		// Ensure Member built-in role
		memberRole, err := txStore.Roles().FindByName(ctx, ws.ID, domain.RoleMember)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("failed to query member role: %w", err)
		}
		if errors.Is(err, domain.ErrNotFound) {
			memberRole = &domain.Role{
				WorkspaceID: ws.ID,
				Name:        domain.RoleMember,
				IsOwner:     false,
				Permissions: domain.MemberPermissions,
				BuiltIn:     true,
			}
			if err := txStore.Roles().Create(ctx, memberRole); err != nil {
				return fmt.Errorf("failed to create member role: %w", err)
			}
		}

		master = ws
		return nil
	})

	if err != nil {
		return nil, err
	}
	return master, nil
}

// SeedSuperadmin idempotently seeds the initial superadmin user and master tenant membership.
// If the master workspace already has any member holding the Superadmin role, seeding is skipped.
// Credentials are never logged.
func (b *bootstrapper) SeedSuperadmin(ctx context.Context, cfg SuperadminSeedConfig) (*domain.User, error) {
	password := cfg.Password
	if password == "" && cfg.PasswordFile != "" {
		data, err := os.ReadFile(cfg.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read superadmin password file: %w", err)
		}
		password = strings.TrimSpace(string(data))
	}

	email := domain.NormalizeEmail(cfg.Email)
	if email == "" || password == "" {
		slog.Info("no superadmin credentials configured; skipping superadmin seeding")
		return nil, nil
	}

	if err := domain.ValidateEmail(email); err != nil {
		return nil, fmt.Errorf("invalid superadmin email: %w", err)
	}

	var seededUser *domain.User

	err := b.store.WithTx(ctx, func(txStore store.Store) error {
		master, err := New(txStore).EnsureMaster(ctx)
		if err != nil {
			return err
		}

		superadminRole, err := txStore.Roles().FindByName(ctx, master.ID, domain.RoleSuperadmin)
		if err != nil {
			return fmt.Errorf("superadmin role not found in master workspace: %w", err)
		}

		count, err := txStore.Roles().CountMembers(ctx, superadminRole.ID)
		if err != nil {
			return fmt.Errorf("failed to count superadmin members: %w", err)
		}

		if count > 0 {
			slog.Info("superadmin already seeded in master tenant; skipping", "email", email)
			return nil
		}

		hash, err := b.hasher.Hash(password)
		if err != nil {
			return fmt.Errorf("failed to hash superadmin password: %w", err)
		}

		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			name = "Superadmin"
		}

		user, err := txStore.Users().ByEmail(ctx, email)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("failed to check existing user: %w", err)
		}

		if errors.Is(err, domain.ErrNotFound) {
			user = &domain.User{
				Email:        email,
				Name:         name,
				PasswordHash: &hash,
			}
			if err := txStore.Users().Create(ctx, user); err != nil {
				return fmt.Errorf("failed to create superadmin user: %w", err)
			}
		} else {
			if err := txStore.Users().SetPasswordHash(ctx, user.ID, hash); err != nil {
				return fmt.Errorf("failed to set superadmin password hash: %w", err)
			}
		}

		member := &domain.Member{
			WorkspaceID: master.ID,
			UserID:      user.ID,
			RoleID:      superadminRole.ID,
		}
		if err := txStore.Members().Add(ctx, member); err != nil {
			return fmt.Errorf("failed to add superadmin membership: %w", err)
		}

		slog.Info("superadmin seeded successfully in master tenant", "email", email)
		seededUser = user
		return nil
	})

	if err != nil {
		return nil, err
	}
	return seededUser, nil
}
