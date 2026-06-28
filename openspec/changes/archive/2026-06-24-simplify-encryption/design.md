## Context

The current secrets storage system encrypts API keys using AES-256-GCM. However, instead of using the Data Encryption Key (DEK) directly, it derives a per-value key using HKDF-SHA256 with a unique 16-byte salt for each secret. This is a redundant cryptographic step that adds file space overhead in the database (16 bytes per secret + base64 encoding expansion) and introduces an external dependency `golang.org/x/crypto/hkdf`.

## Goals / Non-Goals

**Goals:**
- Simplify the encryption and decryption pipeline in `internal/secrets/crypto.go`.
- Remove the external dependency on `golang.org/x/crypto/hkdf`.
- Reduce database storage footprint for encrypted API keys.
- Keep the DEK wrapping architecture (keyfile vs passphrase KEK) unchanged.

**Non-Goals:**
- We do not seek to maintain backward compatibility with previously stored secrets. This is a breaking change.
- We do not change the core `KeyManager` interface or how the DEK itself is wrapped/unwrapped.

## Decisions

### Decision: Direct AES-256-GCM Encryption with DEK
- **Rationale**: AES-256-GCM is secure when encrypting multiple payloads with the same key, provided a unique random 12-byte (96-bit) nonce is used for each payload. The number of API keys stored in `onclaw` is extremely small (typically less than 10). The collision probability for a 96-bit nonce is negligible. Thus, per-value key derivation via HKDF is cryptographically unnecessary.
- **Alternative considered**: Keep HKDF key derivation but use a faster KDF (e.g. HKDF-SHA-512/256 or a custom HMAC-based derivation). This would still require keeping the external HKDF dependency and storing a salt.

### Decision: Breaking Change with No Fallback Decryption
- **Rationale**: Supporting legacy format fallback would require retaining the `golang.org/x/crypto/hkdf` dependency and maintaining two code paths in `Decrypt`. Since `onclaw` is in pre-release/early development (adapters are stubs), a clean breaking change is the most maintainable path.
- **Alternative considered**: Detect old format by base64 string length and try HKDF decryption as a fallback. Rejected to ensure complete removal of HKDF dependency.

## Risks / Trade-offs

- **[Risk] Existing secrets become undecryptable** → **[Mitigation]** The CLI commands (e.g. `getProviderManager`) will fail to load or decrypt the DEK-encrypted keys, prompting the user to login again. This is acceptable for early-stage development.
