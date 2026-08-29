package auth

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ProviderPassword is the provider name for standard password authentication.
const ProviderPassword = "password"

// passwordProvider implements the Provider interface for email/password credentials.
type passwordProvider struct {
	users  store.UserStore
	hasher PasswordHasher
}

// NewPasswordProvider creates a new passwordProvider with the default password hasher.
func NewPasswordProvider(users store.UserStore) *passwordProvider {
	return NewPasswordProviderWithHasher(users, NewPasswordHasher())
}

// NewPasswordProviderWithHasher creates a new passwordProvider with a custom PasswordHasher.
func NewPasswordProviderWithHasher(users store.UserStore, hasher PasswordHasher) *passwordProvider {
	if hasher == nil {
		hasher = NewPasswordHasher()
	}
	return &passwordProvider{
		users:  users,
		hasher: hasher,
	}
}

// Name returns the provider identifier.
func (p *passwordProvider) Name() string {
	return ProviderPassword
}

// ProvisionsUsers reports whether this provider automatically JIT-provisions new users.
// Password authentication is non-provisioning (requires an existing account).
func (p *passwordProvider) ProvisionsUsers() bool {
	return false
}

// SetUserStore updates the underlying UserStore.
func (p *passwordProvider) SetUserStore(users store.UserStore) {
	p.users = users
}

// Authenticate validates email and password credentials.
// It implements uniform 401 semantics: all failure modes (unknown email, wrong password,
// passwordless account, disabled account) return domain.ErrUnauthenticated.
func (p *passwordProvider) Authenticate(ctx context.Context, credentials map[string]string) (*Identity, error) {
	if credentials == nil {
		_, _ = p.hasher.Verify("dummy", dummyHash)
		return nil, domain.ErrUnauthenticated
	}

	email := domain.NormalizeEmail(credentials["email"])
	password := credentials["password"]

	if email == "" || password == "" {
		_, _ = p.hasher.Verify("dummy", dummyHash)
		return nil, domain.ErrUnauthenticated
	}

	if p.users == nil {
		_, _ = p.hasher.Verify("dummy", dummyHash)
		return nil, domain.ErrUnauthenticated
	}

	user, err := p.users.ByEmail(ctx, email)
	if err != nil {
		// Run dummy verification to avoid timing attacks on non-existent accounts
		_, _ = p.hasher.Verify(password, dummyHash)
		return nil, domain.ErrUnauthenticated
	}

	if user.IsDisabled() {
		_, _ = p.hasher.Verify(password, dummyHash)
		return nil, domain.ErrUnauthenticated
	}

	if user.PasswordHash == nil || *user.PasswordHash == "" {
		_, _ = p.hasher.Verify(password, dummyHash)
		return nil, domain.ErrUnauthenticated
	}

	matched, err := p.hasher.Verify(password, *user.PasswordHash)
	if err != nil || !matched {
		return nil, domain.ErrUnauthenticated
	}

	return &Identity{
		Email: user.Email,
		Name:  user.Name,
	}, nil
}

// Begin is not supported for password authentication.
func (p *passwordProvider) Begin(ctx context.Context, params map[string]string) (string, string, error) {
	return "", "", fmt.Errorf("%w: password provider does not support begin", domain.ErrInvalid)
}

// Complete is not supported for password authentication.
func (p *passwordProvider) Complete(ctx context.Context, state string, params map[string]string) (*Identity, error) {
	return nil, fmt.Errorf("%w: password provider does not support complete", domain.ErrInvalid)
}
