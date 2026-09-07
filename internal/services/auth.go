// Package services implements application services: authentication and model catalog.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// LoginRequest holds parameters for initiating authentication.
type LoginRequest struct {
	Provider    string            `json:"provider"`
	Credentials map[string]string `json:"credentials"`
}

// LoginResult contains the issued token and authenticated user.
type LoginResult struct {
	Token string       `json:"token"`
	User  *domain.User `json:"user"`
}

// MeResult contains the user identity and their workspace memberships with roles.
type MeResult struct {
	User        *domain.User        `json:"user"`
	Memberships []domain.MemberView `json:"memberships"`
}

// AuthService defines the core authentication and identity domain service interface.
type AuthService interface {
	Registry() *registry
	Issuer() TokenIssuer
	Login(ctx context.Context, req LoginRequest) (*LoginResult, error)
	Me(ctx context.Context, userID string) (*MeResult, error)
}

// authService is the core authentication and identity domain service.
type authService struct {
	users    store.UserStore
	members  store.MemberStore
	issuer   TokenIssuer
	registry *registry
}

// NewAuthService creates a new AuthService.
func NewAuthService(users store.UserStore, members store.MemberStore, issuer TokenIssuer, reg *registry) AuthService {
	if reg == nil {
		reg = NewRegistry()
		if users != nil {
			reg.Register(NewPasswordProvider(users))
		}
	} else {
		// If password provider is in custom registry and needs store
		if p, err := reg.Get(ProviderPassword); err == nil {
			if pp, ok := p.(*passwordProvider); ok && pp.users == nil && users != nil {
				pp.users = users
			}
		} else if users != nil {
			reg.Register(NewPasswordProvider(users))
		}
	}

	return &authService{
		users:    users,
		members:  members,
		issuer:   issuer,
		registry: reg,
	}
}

// Registry returns the provider registry used by the service.
func (s *authService) Registry() *registry {
	return s.registry
}

// Issuer returns the token issuer used by the service.
func (s *authService) Issuer() TokenIssuer {
	return s.issuer
}

// Login authenticates credentials via the specified provider and issues a session token.
func (s *authService) Login(ctx context.Context, req LoginRequest) (*LoginResult, error) {
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		providerName = ProviderPassword
	}

	provider, err := s.registry.Get(providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: unknown auth provider %q", domain.ErrInvalid, providerName)
	}

	identity, err := provider.Authenticate(ctx, req.Credentials)
	if err != nil {
		return nil, err
	}
	if identity == nil || strings.TrimSpace(identity.Email) == "" {
		return nil, domain.ErrUnauthenticated
	}

	user, err := s.ensureUser(ctx, provider, identity)
	if err != nil {
		return nil, err
	}

	token, err := s.issuer.Issue(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to issue token: %w", err)
	}

	return &LoginResult{
		Token: token,
		User:  user,
	}, nil
}

// ensureUser enforces user provisioning and backfill policies.
func (s *authService) ensureUser(ctx context.Context, provider Provider, identity *Identity) (*domain.User, error) {
	email := domain.NormalizeEmail(identity.Email)

	existingUser, err := s.users.ByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	// Non-provisioning provider (e.g. password) requires an existing account
	if !provider.ProvisionsUsers() {
		if errors.Is(err, domain.ErrNotFound) || existingUser == nil {
			return nil, domain.ErrUnauthenticated
		}
		if existingUser.IsDisabled() {
			return nil, domain.ErrUnauthenticated
		}

		// Avatar backfill if user has no avatar set
		if identity.AvatarURL != "" && (existingUser.AvatarURL == nil || *existingUser.AvatarURL == "") && (existingUser.AvatarKey == nil || *existingUser.AvatarKey == "") {
			existingUser.AvatarURL = &identity.AvatarURL
			if updateErr := s.users.Update(ctx, existingUser); updateErr != nil {
				return nil, updateErr
			}
		}

		return existingUser, nil
	}

	// Provisioning provider (e.g. SSO / mock provisioning)
	if errors.Is(err, domain.ErrNotFound) || existingUser == nil {
		name := strings.TrimSpace(identity.Name)
		if name == "" {
			parts := strings.Split(email, "@")
			name = parts[0]
		}

		newUser := &domain.User{
			Email: email,
			Name:  name,
		}
		if identity.AvatarURL != "" {
			newUser.AvatarURL = &identity.AvatarURL
		}

		if createErr := s.users.Create(ctx, newUser); createErr != nil {
			return nil, createErr
		}
		return newUser, nil
	}

	// Existing user logging in via provisioning provider
	if existingUser.IsDisabled() {
		return nil, domain.ErrUnauthenticated
	}

	// Avatar backfill if user has no avatar set
	if identity.AvatarURL != "" && (existingUser.AvatarURL == nil || *existingUser.AvatarURL == "") && (existingUser.AvatarKey == nil || *existingUser.AvatarKey == "") {
		existingUser.AvatarURL = &identity.AvatarURL
		if updateErr := s.users.Update(ctx, existingUser); updateErr != nil {
			return nil, updateErr
		}
	}

	return existingUser, nil
}

// Me retrieves the current user profile and their workspace memberships with roles.
func (s *authService) Me(ctx context.Context, userID string) (*MeResult, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, domain.ErrUnauthenticated
	}

	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthenticated
		}
		return nil, err
	}
	if user.IsDisabled() {
		return nil, domain.ErrUnauthenticated
	}

	memberships, err := s.members.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &MeResult{
		User:        user,
		Memberships: memberships,
	}, nil
}
