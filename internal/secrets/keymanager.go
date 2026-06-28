package secrets

// KeyManager defines the operations for encrypting/decrypting secrets and re-wrapping the DEK.
type KeyManager interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(blob string) ([]byte, error)
	GetDEK() []byte
	SwitchToKeyfile(keyfilePath string) (string, error) // Returns newWrapped, error
}
