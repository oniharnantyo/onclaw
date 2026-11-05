package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// Seal encrypts plaintext under the KeySize-byte key with AES-GCM: a
// fresh random nonce is prefixed to the ciphertext on every call, so
// sealing the same secret twice never produces the same blob.
func Seal(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, fmt.Errorf("seal secret: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("seal secret: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts a Seal blob (nonce-prefixed AES-GCM ciphertext). A wrong
// key or a tampered blob is an authentication error, never a panic.
func Open(key, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, fmt.Errorf("open secret: %w", err)
	}
	if len(blob) < gcm.NonceSize() {
		return nil, fmt.Errorf("open secret: blob shorter than the nonce")
	}
	plain, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("open secret: %w", err)
	}
	return plain, nil
}

// newGCM builds the AES-GCM AEAD for the key, enforcing KeySize.
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// keyProvider resolves the vault master key (design D5): the OS keyring
// first (service "buddies"), the keyfile at dir/vault.key — mode 0600, 32
// random bytes created on first use — where the keyring is unavailable.
type keyProvider struct {
	ring keyringSeam
	// dir is the injected keyfile directory (the config layout root);
	// never a hard-coded path.
	dir string
}

// keyringSeam is the OS keyring surface, mirrored from
// github.com/zalando/go-keyring so tests can fake both resolution paths.
type keyringSeam interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
}

// osKeyring adapts the real OS keyring.
type osKeyring struct{}

func (osKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

func (osKeyring) Set(service, user, secret string) error { return keyring.Set(service, user, secret) }

// NewKeyProvider returns the production KeyProvider: OS keyring first
// (service "buddies"), keyfile fallback at dir/vault.key. dir is
// injected — the composition root passes config.Resolve().Root.
func NewKeyProvider(dir string) KeyProvider {
	return &keyProvider{ring: osKeyring{}, dir: dir}
}

// Key resolves the master key: a stored keyring secret wins (a corrupt
// one is an error, never silently replaced); a missing or empty one is
// generated and stored; any other keyring failure (unsupported platform,
// daemon down, refused write) hands ownership to the keyfile fallback.
func (p *keyProvider) Key() ([]byte, error) {
	secret, err := p.ring.Get(KeyringService, keyringUser)
	switch {
	case err == nil && secret != "":
		key, decodeErr := decodeKey([]byte(secret))
		if decodeErr != nil {
			return nil, decodeErr
		}
		return key, nil
	case err == nil || errors.Is(err, keyring.ErrNotFound):
		return p.mintKeyringKey()
	default:
		return p.keyfileKey()
	}
}

// mintKeyringKey generates a fresh key and stores it in the keyring; a
// refused write falls back to the keyfile.
func (p *keyProvider) mintKeyringKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate vault key: %w", err)
	}
	if err := p.ring.Set(KeyringService, keyringUser, hex.EncodeToString(key)); err != nil {
		return p.keyfileKey()
	}
	return key, nil
}

// keyfileKey reads the fallback keyfile, creating it on first use. The
// error path never echoes key material.
func (p *keyProvider) keyfileKey() ([]byte, error) {
	path := p.keyfilePath()
	raw, err := os.ReadFile(path)
	if err == nil {
		key, decodeErr := decodeKey(raw)
		if decodeErr != nil {
			return nil, fmt.Errorf("%s is corrupt: %w", path, decodeErr)
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return p.createKeyfile(path)
}

// keyfilePath joins the injected directory with the keyfile name.
func (p *keyProvider) keyfilePath() string {
	return filepath.Join(p.dir, KeyfileName)
}

// createKeyfile mints the fallback key: 32 random bytes hex-encoded into
// dir/vault.key with mode 0600 (the directory 0700). The O_EXCL create
// keeps a concurrent resolver from clobbering an existing key; losing the
// race re-reads the winner's file.
func (p *keyProvider) createKeyfile(path string) ([]byte, error) {
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", p.dir, err)
	}
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate vault key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return p.keyfileKey()
		}
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := file.Write([]byte(hex.EncodeToString(key) + "\n")); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", path, err)
	}
	return key, nil
}

// decodeKey hex-decodes stored key material and enforces KeySize. The
// error never echoes the material.
func decodeKey(encoded []byte) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("vault key material is not valid hex")
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("vault key material must decode to %d bytes, got %d", KeySize, len(key))
	}
	return key, nil
}
