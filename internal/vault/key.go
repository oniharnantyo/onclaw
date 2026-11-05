package vault

// KeyProvider supplies the vault's master key: exactly 32 bytes of key
// material for the AES-GCM seal (add-browser-login-handover design D5).
type KeyProvider interface {
	// Key returns the 32-byte vault master key, resolving it from the OS
	// keyring (first) or the fallback keyfile.
	Key() ([]byte, error)
}

// Key material constants (add-browser-login-handover design D5).
const (
	// KeySize is the AES-GCM master-key length in bytes.
	KeySize = 32
	// KeyringService is the OS keyring service name the vault key is
	// stored under.
	KeyringService = "buddies"
	// keyringUser is the keyring account name; one vault key per install.
	keyringUser = "vault"
	// KeyfileName is the fallback keyfile name inside the injected
	// directory (normally the resolved config layout root).
	KeyfileName = "vault.key"
)
