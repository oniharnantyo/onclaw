# Implementation Tasks

## 1. Storage layer (`internal/store`)

- [x] 1.1 Add `modernc.org/sqlite` dependency; confirm `CGO_ENABLED=0` build still produces a static binary
- [x] 1.2 Implement DB open: resolve path from config (`db_path` or `$XDG_DATA_HOME/onclaw/onclaw.db`), open with mode 0600, enable WAL
- [x] 1.3 Refuse to operate if the DB file cannot be secured to 0600 (fail closed)
- [x] 1.4 Add migration runner creating `provider_profiles`, `secrets`, `preferences` tables (idempotent)
- [x] 1.5 Unit-test migrations (fresh + re-run is a no-op) and the 0600 permission assertion

## 2. Crypto & key management (`internal/provider` or `internal/secrets`)

- [x] 2.1 Implement AES-256-GCM seal/open producing `salt ‖ nonce ‖ ciphertext ‖ tag` (base64), per-value random salt + nonce
- [x] 2.2 Implement DEK generation (`crypto/rand` 32 bytes) and DEK wrap/unwrap under a KEK
- [x] 2.3 Keyfile mode: generate/read `master.key` (0600) as KEK source; wrap DEK on first run
- [x] 2.4 Passphrase mode: Argon2id KDF (memory ~16–32 MB, time 3, parallelism 2) as KEK source; `onclaw unlock` flow
- [x] 2.5 Implement mode switching that re-wraps the DEK only (no secret re-encryption)
- [x] 2.6 Unit-test seal/open round-trip, tamper detection (GCM auth failure), and keyfile→passphrase re-wrap preserving decryptability

## 3. Provider package (`internal/provider`)

- [x] 3.1 Profile CRUD: add/list/get/remove on `provider_profiles` (unique-name enforcement)
- [x] 3.2 Secret store: encrypt+upsert and decrypt for a named profile; keyless providers have no `secrets` row
- [x] 3.3 Secret resolution with precedence `ONCLAW_PROVIDER_<NAME>_API_KEY` > DB > guided error
- [x] 3.4 `Build(name)` hook: construct an Eino `model.ChatModel` from a profile + resolved secret (stub provider ok for this change; real wiring in the agent-core change)
- [x] 3.5 Unit-test CRUD, encryption-at-rest (raw column is not plaintext), and resolution precedence

## 4. CLI surface (`internal/cli`)

- [x] 4.1 `onclaw provider add` (non-interactive) and `onclaw provider login` (interactive, hidden key input)
- [x] 4.2 `onclaw provider list` (redacts keys), `use <name>` (sets default pref), `remove <name>`
- [x] 4.3 `onclaw unlock` (set/enable passphrase) and `onclaw unlock --reset` (destroy secrets, re-init keyfile)
- [x] 4.4 `--provider <name>` flag on `run` (override default for one invocation)
- [x] 4.5 `config show` redaction: keep secrets off the printed `Config` struct; render `api_key: ***`
- [x] 4.6 Unit/integration-test: `provider login` then `config show` shows no plaintext key

## 5. Hot-reload

- [x] 5.1 fsnotify watcher on `.db` and `.db-wal`; on change, reload provider profiles + secrets
- [x] 5.2 SIGHUP handler as fallback reload trigger; pidfile/socket detection so `provider add|login|use` can signal a running process
- [x] 5.3 Apply reloads on the next agent turn (in-flight requests finish with the prior provider)
- [x] 5.4 Integration-test: write a profile via CLI mid-session → next turn sees it without restart

## 6. Hardening, tests, docs

- [x] 6.1 Secret redaction at the JSONL transcript logging boundary (known-secret patterns)
- [x] 6.2 Confirm no plaintext key appears in logs under debug logging
- [x] 6.3 `make test`, `make vet`, `make fmt` green; coverage ≥ 80% for new packages
- [x] 6.4 Update `README.md` provider/secrets section and the superpowers design doc threat model to match implementation
- [x] 6.5 `openspec validate add-provider-secrets-storage` passes

## 7. Abstraction refactor + schema refinement

- [x] 7.1 Extract `ProfileStore`, `SecretStore` (opaque blobs), `KVStore` interfaces in `internal/store`; keep sqlite as the implementation
- [x] 7.2 Promote `internal/secrets` to a `KeyManager` interface (Encrypt/Decrypt/DEK/SwitchToPassphrase/SwitchToKeyfile); consolidate logic duplicated in `cli/context.go` + `cli/unlock_cmd.go`
- [x] 7.3 Add `internal/providers`: `Adapter` interface + `AdapterRegistry` + builtin adapters (anthropic, openai, openai-compatible, ollama); stub ChatModels ok initially
- [x] 7.4 Convert `provider.Manager` into a thin `provider.Service` facade composing ProfileStore + SecretStore + KeyManager + AdapterRegistry; `Build(name)` dispatches by `kind` via the registry
- [x] 7.5 Move the fsnotify/SIGHUP watcher into `provider/watcher.go`
- [x] 7.6 Schema: rename `provider_profiles` → `llm_providers`; align columns (`kind`→`provider_type`, `base_url`→`api_base`); rename `secrets` → `config_secrets`; add `settings` JSONB + `enabled`; migrate `config_secrets` to generic KV keyed by `key` (`provider:<name>`); update migrations + tests
- [x] 7.7 Rewire CLI (`provider_cmd.go`, `unlock_cmd.go`, `context.go`, `run.go`) to the facade; keep all existing tests green and lift `internal/cli` coverage ≥ 80%
- [x] 7.8 Security fix (verify-finding): on keyfile→passphrase switch, remove/zero `master.key` so passphrase mode delivers strong at-rest protection
- [x] 7.9 Update `design.md` schema and the superpowers design doc to match D12–D14; re-run `openspec validate`

## 8. File organization refactor (interface / types / impl split — D15)

- [ ] 8.1 `internal/store`: split `store.go` → `types.go` (Profile), `store.go` (ProfileStore/SecretStore/KVStore interfaces), `db.go` (Open/Migrate/ResolveDbPath), `sqlite.go` (sqlite impls)
- [ ] 8.2 `internal/secrets`: split `secrets.go` → `keymanager.go` (KeyManager interface), `crypto.go` (Encrypt/Decrypt/GenerateDEK/WrapDEK/UnwrapDEK/DeriveKEKFromPassphrase), `keyfile.go` (ResolveKeyfilePath/GetOrCreateKeyfileKEK), `keymanager_impl.go` (keyManagerImpl)
- [ ] 8.3 `internal/providers`: split `providers.go` → `types.go` (Profile DTO), `adapter.go` (Adapter + AdapterFactory), `registry.go` (Registry interface + registryImpl + NewRegistry), `stub.go` (StubChatModel/stubAdapter/NewStubAdapter)
- [ ] 8.4 `internal/provider`: keep `service.go` (Service facade) + `watcher.go`; ensure no interface shares a file with its implementation
- [ ] 8.5 keep `go test ./...` green, coverage ≥ 80%, `CGO_ENABLED=0` build green, `openspec validate` passes
