package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// fakeKeyring is an in-memory keyringSeam with injectable failure modes.
type fakeKeyring struct {
	store  map[string]string
	getErr error
	setErr error
	sets   int
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{store: make(map[string]string)}
}

func (f *fakeKeyring) key() string {
	return f.store[KeyringService+"/"+keyringUser]
}

func (f *fakeKeyring) Get(_, _ string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	secret, ok := f.store[KeyringService+"/"+keyringUser]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return secret, nil
}

func (f *fakeKeyring) Set(_, _, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.sets++
	f.store[KeyringService+"/"+keyringUser] = secret
	return nil
}

// TestSeal_RoundTrip covers the AES-GCM contract: every plaintext
// round-trips through Seal/Open, every seal uses a fresh nonce, and a
// wrong key or tampered blob is an error.
func TestSeal_RoundTrip(t *testing.T) {
	key := make([]byte, KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	t.Run("round trip", func(t *testing.T) {
		for _, plaintext := range [][]byte{
			nil,
			[]byte(""),
			[]byte("hunter2"),
			[]byte("pässwörd with spaces and ünïcode ✓"),
			make([]byte, 4096),
		} {
			sealed, err := Seal(key, plaintext)
			require.NoError(t, err)
			require.NotEqual(t, plaintext, sealed, "the blob must not be the plaintext")

			opened, err := Open(key, sealed)
			require.NoError(t, err)
			if len(plaintext) == 0 {
				require.Empty(t, opened, "an empty secret opens back empty")
				continue
			}
			require.Equal(t, plaintext, opened)
		}
	})

	t.Run("fresh nonce per seal", func(t *testing.T) {
		first, err := Seal(key, []byte("same secret"))
		require.NoError(t, err)
		second, err := Seal(key, []byte("same secret"))
		require.NoError(t, err)
		require.NotEqual(t, first, second, "sealing twice must not reuse the nonce")
	})

	t.Run("wrong key is an error", func(t *testing.T) {
		sealed, err := Seal(key, []byte("secret"))
		require.NoError(t, err)
		other := make([]byte, KeySize)
		_, err = rand.Read(other)
		require.NoError(t, err)
		_, err = Open(other, sealed)
		require.Error(t, err)
	})

	t.Run("tampered blob is an error", func(t *testing.T) {
		sealed, err := Seal(key, []byte("secret"))
		require.NoError(t, err)
		sealed[len(sealed)-1] ^= 0x01
		_, err = Open(key, sealed)
		require.Error(t, err)
	})

	t.Run("truncated blob is an error", func(t *testing.T) {
		_, err := Open(key, []byte{0x01, 0x02})
		require.Error(t, err)
	})

	t.Run("wrong key size is an error", func(t *testing.T) {
		_, err := Seal(make([]byte, 16), []byte("secret"))
		require.Error(t, err)
	})
}

// TestKeyProvider_KeyringHit covers the primary path: a stored keyring
// secret is the master key, decoded and reused.
func TestKeyProvider_KeyringHit(t *testing.T) {
	key := bytes32(t)
	ring := newFakeKeyring()
	ring.store[KeyringService+"/"+keyringUser] = hex.EncodeToString(key)

	p := &keyProvider{ring: ring, dir: t.TempDir()}
	got, err := p.Key()
	require.NoError(t, err)
	require.Equal(t, key, got)
	require.Zero(t, ring.sets, "an existing key must never be replaced")
}

// TestKeyProvider_KeyringMissingMintsAndStores covers the cold path: a
// missing keyring secret is generated, stored, and returned.
func TestKeyProvider_KeyringMissingMintsAndStores(t *testing.T) {
	ring := newFakeKeyring()

	p := &keyProvider{ring: ring, dir: t.TempDir()}
	got, err := p.Key()
	require.NoError(t, err)
	require.Len(t, got, KeySize)
	require.Equal(t, hex.EncodeToString(got), ring.key(), "the minted key must be stored in the keyring")

	again, err := p.Key()
	require.NoError(t, err)
	require.Equal(t, got, again, "the stored key must be reused")
	require.Equal(t, 1, ring.sets, "only one mint")
}

// TestKeyProvider_KeyringEmptySecretMints covers an empty keyring entry:
// it reads as absent and is replaced.
func TestKeyProvider_KeyringEmptySecretMints(t *testing.T) {
	ring := newFakeKeyring()
	ring.store[KeyringService+"/"+keyringUser] = ""

	p := &keyProvider{ring: ring, dir: t.TempDir()}
	got, err := p.Key()
	require.NoError(t, err)
	require.Len(t, got, KeySize)
	require.Equal(t, hex.EncodeToString(got), ring.key())
}

// TestKeyProvider_KeyringUnavailableFallsBackToKeyfile covers the
// fallback path: with the keyring unavailable, the keyfile is created
// with mode 0600, 32 random bytes, and reused on the next call.
func TestKeyProvider_KeyringUnavailableFallsBackToKeyfile(t *testing.T) {
	ring := newFakeKeyring()
	ring.getErr = errors.New("no keyring service")
	ring.setErr = errors.New("no keyring service")

	dir := t.TempDir()
	p := &keyProvider{ring: ring, dir: dir}

	got, err := p.Key()
	require.NoError(t, err)
	require.Len(t, got, KeySize)
	require.Zero(t, ring.sets, "a failed mint must not store in the keyring")

	path := filepath.Join(dir, KeyfileName)
	info, err := os.Stat(path)
	require.NoError(t, err, "the fallback keyfile must exist")
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the keyfile must be 0600")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	decoded, err := hex.DecodeString(string(bytes.TrimSpace(raw)))
	require.NoError(t, err)
	require.Equal(t, got, decoded, "the keyfile must hold exactly the resolved key")

	// Reuse: the second call reads the same file, never re-minting.
	ring.getErr = nil // even if the keyring recovers, a stored key wins; force unavailable again
	ring.getErr = errors.New("no keyring service")
	again, err := p.Key()
	require.NoError(t, err)
	require.Equal(t, got, again, "the keyfile key must be reused")
}

// TestKeyProvider_ExistingKeyfileRespected covers pre-seeded fallbacks: a
// valid keyfile is the key (no keyring interaction needed).
func TestKeyProvider_ExistingKeyfileRespected(t *testing.T) {
	key := bytes32(t)
	dir := t.TempDir()
	path := filepath.Join(dir, KeyfileName)
	require.NoError(t, os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600))

	ring := newFakeKeyring()
	ring.getErr = errors.New("no keyring service")
	p := &keyProvider{ring: ring, dir: dir}

	got, err := p.Key()
	require.NoError(t, err)
	require.Equal(t, key, got)
	require.Zero(t, ring.sets)
}

// TestKeyProvider_CorruptKeyfileIsError covers the corrupt fallbacks:
// non-hex and wrong-length keyfiles are errors, never silent re-mints
// (re-minting would orphan every sealed secret).
func TestKeyProvider_CorruptKeyfileIsError(t *testing.T) {
	t.Run("not hex", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, KeyfileName), []byte("not-hex-at-all"), 0o600))
		ring := newFakeKeyring()
		ring.getErr = errors.New("no keyring service")
		p := &keyProvider{ring: ring, dir: dir}
		_, err := p.Key()
		require.Error(t, err)
		require.NotContains(t, err.Error(), "not-hex-at-all", "no key material may appear in errors")
	})

	t.Run("wrong length", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, KeyfileName),
			[]byte(hex.EncodeToString(make([]byte, 16))), 0o600))
		ring := newFakeKeyring()
		ring.getErr = errors.New("no keyring service")
		p := &keyProvider{ring: ring, dir: dir}
		_, err := p.Key()
		require.Error(t, err)
	})

	t.Run("corrupt keyring secret is an error", func(t *testing.T) {
		ring := newFakeKeyring()
		ring.store[KeyringService+"/"+keyringUser] = "zzz-not-hex"
		p := &keyProvider{ring: ring, dir: t.TempDir()}
		_, err := p.Key()
		require.Error(t, err)
	})
}

// TestKeyProvider_KeyfileDirCreated covers a missing directory: it is
// created (0700) with the keyfile inside.
func TestKeyProvider_KeyfileDirCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "vault")
	ring := newFakeKeyring()
	ring.getErr = errors.New("no keyring service")
	ring.setErr = errors.New("no keyring service")

	p := &keyProvider{ring: ring, dir: dir}
	got, err := p.Key()
	require.NoError(t, err)
	require.Len(t, got, KeySize)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

// bytes32 returns KeySize random bytes for fixtures.
func bytes32(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}
