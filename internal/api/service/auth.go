package service

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

// VerifyPassword verifies the user's password against the hashed passphrase stored in KVStore.
func (s *Service) VerifyPassword(ctx context.Context, pw string) (bool, error) {
	hash, err := s.kv.Get(ctx, "web_password_hash")
	if err != nil {
		return false, classify(err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
	if err != nil {
		return false, nil
	}

	return true, nil
}
