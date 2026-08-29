package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Claims represents the JWT claims payload for authenticated user sessions.
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
	Email  string `json:"email"`
}

// TokenIssuer defines the interface for issuing and verifying authentication session tokens.
type TokenIssuer interface {
	Issue(ctx context.Context, user *domain.User) (string, error)
	Verify(ctx context.Context, tokenString string) (*Claims, error)
}
