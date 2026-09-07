package services

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"golang.org/x/crypto/argon2"
)

// Argon2Params holds the parameters used for Argon2id password hashing.
type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params defines standard RFC 9106 recommended Argon2id parameters.
var DefaultArgon2Params = Argon2Params{
	Memory:      64 * 1024, // 64 MB
	Iterations:  1,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// dummyHash is a precomputed valid argon2id PHC string used to prevent timing attacks.
// Format: $argon2id$v=19$m=65536,t=1,p=4$<16 bytes salt>$<32 bytes hash>
const dummyHash = "$argon2id$v=19$m=65536,t=1,p=4$c29tZXNhbHRzb21lc2FsdA$YmFzZTY0aGFzaGJhc2U2NGhhc2hiYXNlNjRoYXNoYmE"

// PasswordHasher handles password hashing and verification using Argon2id.
type PasswordHasher interface {
	Params() Argon2Params
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}

type passwordHasher struct {
	params Argon2Params
}

// NewPasswordHasher creates a new PasswordHasher with standard RFC 9106 recommended parameters.
func NewPasswordHasher() PasswordHasher {
	return &passwordHasher{
		params: DefaultArgon2Params,
	}
}

// NewPasswordHasherWithParams creates a new PasswordHasher with specified parameters.
func NewPasswordHasherWithParams(params Argon2Params) PasswordHasher {
	return &passwordHasher{
		params: params,
	}
}

// Params returns the Argon2Params configured on the hasher.
func (h *passwordHasher) Params() Argon2Params {
	return h.params
}

// Hash hashes a plain-text password using Argon2id with the configured parameters and returns a PHC string.
func (h *passwordHasher) Hash(password string) (string, error) {
	params := h.params
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLength,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.Memory,
		params.Iterations,
		params.Parallelism,
		b64Salt,
		b64Hash,
	)

	return encoded, nil
}

// Verify verifies whether a plain-text password matches an encoded Argon2id PHC string.
func (h *passwordHasher) Verify(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, fmt.Errorf("%w: invalid argon2id PHC format", domain.ErrInvalid)
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("%w: invalid argon2 version: %v", domain.ErrInvalid, err)
	}
	if version != argon2.Version {
		return false, fmt.Errorf("%w: incompatible argon2 version %d", domain.ErrInvalid, version)
	}

	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("%w: invalid argon2 params: %v", domain.ErrInvalid, err)
	}

	salt, err := decodeBase64(parts[4])
	if err != nil {
		return false, fmt.Errorf("%w: invalid base64 salt: %v", domain.ErrInvalid, err)
	}

	hash, err := decodeBase64(parts[5])
	if err != nil {
		return false, fmt.Errorf("%w: invalid base64 hash: %v", domain.ErrInvalid, err)
	}

	expectedHash := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memory,
		parallelism,
		uint32(len(hash)),
	)

	if subtle.ConstantTimeCompare(hash, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}

func decodeBase64(s string) ([]byte, error) {
	// Support both unpadded (standard PHC) and padded base64
	if decoded, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
