package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

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

// JWTConfig holds configuration for HS256 JWT token issuance and verification.
type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

// jwtIssuer implements TokenIssuer using HS256 signed JWTs.
type jwtIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTIssuer creates a new jwtIssuer.
// If secret is empty, an ephemeral 256-bit secret is generated and a warning is logged.
func NewJWTIssuer(cfg JWTConfig) *jwtIssuer {
	secret := []byte(cfg.Secret)
	if len(secret) == 0 {
		ephemeral := make([]byte, 32)
		if _, err := rand.Read(ephemeral); err != nil {
			panic(fmt.Sprintf("failed to generate ephemeral JWT secret: %v", err))
		}
		secret = ephemeral
		slog.Warn("no JWT secret configured; using ephemeral secret (sessions will be invalidated on restart)")
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}

	return &jwtIssuer{
		secret: secret,
		ttl:    ttl,
	}
}

// Issue creates and signs a new HS256 JWT for the given user.
func (j *jwtIssuer) Issue(ctx context.Context, user *domain.User) (string, error) {
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("%w: cannot issue token for invalid user", domain.ErrInvalid)
	}

	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
		},
		UserID: user.ID,
		Email:  user.Email,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// Verify parses and verifies the signature and validity of a JWT string.
func (j *jwtIssuer) Verify(ctx context.Context, tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, domain.ErrUnauthenticated
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnauthenticated, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, domain.ErrUnauthenticated
	}

	if claims.UserID == "" {
		claims.UserID = claims.Subject
	}

	return claims, nil
}
