package secrets

import (
	"fmt"
)

type keyManagerImpl struct {
	dek []byte
}

// NewKeyManager constructs a new KeyManager instance.
func NewKeyManager(dek []byte) KeyManager {
	return &keyManagerImpl{dek: dek}
}

func (k *keyManagerImpl) Encrypt(plaintext []byte) (string, error) {
	return Encrypt(plaintext, k.dek)
}

func (k *keyManagerImpl) Decrypt(blob string) ([]byte, error) {
	return Decrypt(blob, k.dek)
}

func (k *keyManagerImpl) GetDEK() []byte {
	return k.dek
}

func (k *keyManagerImpl) SwitchToKeyfile(keyfilePath string) (string, error) {
	kek, err := GetOrCreateKeyfileKEK(keyfilePath)
	if err != nil {
		return "", fmt.Errorf("get or create keyfile KEK: %w", err)
	}

	newWrapped, err := WrapDEK(k.dek, kek)
	if err != nil {
		return "", fmt.Errorf("wrap DEK: %w", err)
	}

	return newWrapped, nil
}
