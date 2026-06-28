## 1. Core Cryptography Implementation

- [x] 1.1 Remove per-value HKDF-SHA256 key derivation in `internal/secrets/crypto.go`
- [x] 1.2 Update `Encrypt` to encrypt directly with the DEK using AES-256-GCM and a random 12-byte nonce
- [x] 1.3 Update `Decrypt` to decrypt directly with the DEK using AES-256-GCM, extracting only the 12-byte nonce and ciphertext/tag from base64
- [x] 1.4 Remove the unused `golang.org/x/crypto/hkdf` dependency from the imports of `internal/secrets/crypto.go`

## 2. Testing & Verification

- [x] 2.1 Update `internal/secrets/crypto_test.go` to remove salt-related test assertions and update blob length validations
- [x] 2.2 Run unit tests for `internal/secrets` to verify correct encrypt/decrypt roundtrip and tamper detection

## 3. Build & Integration

- [x] 3.1 Verify the entire codebase compiles cleanly by running `make build`
- [x] 3.2 Run the full test suite (`make test`) to ensure everything is correct
