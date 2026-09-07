package services_test

import (
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/services"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hasher := services.NewPasswordHasher()
	password := "SecretPass123!@#"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Errorf("encoded hash has unexpected format: %s", encoded)
	}

	// Verify with correct password
	matched, err := hasher.Verify(password, encoded)
	if err != nil {
		t.Fatalf("unexpected error verifying password: %v", err)
	}
	if !matched {
		t.Errorf("expected password to match encoded hash")
	}

	// Verify with wrong password
	matched, err = hasher.Verify("WrongPassword", encoded)
	if err != nil {
		t.Fatalf("unexpected error verifying wrong password: %v", err)
	}
	if matched {
		t.Errorf("expected wrong password not to match")
	}
}

func TestVerifyPassword_MalformedHashes(t *testing.T) {
	hasher := services.NewPasswordHasher()
	testCases := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"not enough parts", "$argon2id$v=19$m=65536,t=1,p=4$salt"},
		{"wrong prefix", "$bcrypt$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA"},
		{"invalid version", "$argon2id$v=99$m=65536,t=1,p=4$c2FsdA$aGFzaA"},
		{"malformed params", "$argon2id$v=19$invalid$c2FsdA$aGFzaA"},
		{"invalid base64 salt", "$argon2id$v=19$m=65536,t=1,p=4$!@#invalid$aGFzaA"},
		{"invalid base64 hash", "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$!@#invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matched, err := hasher.Verify("password", tc.hash)
			if err == nil {
				t.Errorf("expected error for malformed hash, got matched=%v", matched)
			}
		})
	}
}

func TestHashPasswordWithCustomParams(t *testing.T) {
	params := services.Argon2Params{
		Memory:      16 * 1024,
		Iterations:  2,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}

	hasher := services.NewPasswordHasherWithParams(params)
	encoded, err := hasher.Hash("custom-pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(encoded, "m=16384,t=2,p=2") {
		t.Errorf("encoded hash does not contain custom params: %s", encoded)
	}

	matched, err := hasher.Verify("custom-pass", encoded)
	if err != nil || !matched {
		t.Errorf("expected custom params hash to verify successfully, err=%v, matched=%v", err, matched)
	}
}
