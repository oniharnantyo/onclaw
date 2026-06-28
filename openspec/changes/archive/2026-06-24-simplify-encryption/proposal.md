## Why

The current encryption-at-rest implementation uses a per-value HKDF-SHA256 key derivation step to generate a unique encryption key for every secret from the master Data Encryption Key (DEK). On our target low-resource single-board computers (~2 GB RAM, 8 GB storage), running HKDF on every read/write is redundant (as AES-256-GCM is secure with unique nonces), increases database storage footprint by 16 bytes per secret for the salt, adds code complexity, and introduces an external dependency (`golang.org/x/crypto/hkdf`).

## What Changes

- **Direct DEK Encryption**: Secrets will be encrypted and decrypted directly using the 32-byte DEK with AES-256-GCM, removing the per-value HKDF-SHA256 key derivation.
- **Simplified Storage Format**: The database secret format will change from `base64(salt ‖ nonce ‖ ciphertext ‖ tag)` to `base64(nonce ‖ ciphertext ‖ tag)`.
- **Remove Dependency**: Remove the external dependency `golang.org/x/crypto/hkdf` from `internal/secrets/crypto.go`.
- **Unit Tests**: Update tests in `internal/secrets/crypto_test.go` to reflect the new format and remove salt-related tests.
- **BREAKING**: Existing database-stored secrets encrypted with the old salt-based format will fail to decrypt. Users must re-login to providers (e.g. `onclaw provider login <name>`).

## Capabilities

### New Capabilities
_None_

### Modified Capabilities
- `providers`: Update the encryption-at-rest requirement to encrypt secrets directly using the DEK and a random nonce, without a per-value HKDF key derivation.

## Impact

- **Affected Code**: `internal/secrets/crypto.go` and `internal/secrets/crypto_test.go`.
- **Dependencies**: Remove `golang.org/x/crypto/hkdf`.
- **Operations**: Reduces CPU and memory overhead for cryptographic operations, and shrinks database size for stored secrets.
