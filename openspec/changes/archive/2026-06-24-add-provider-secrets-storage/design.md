## Context

onclaw is a remote-brain agent: the LLM runs off-device, and the on-device
process (single static Go binary, `CGO_ENABLED=0`) holds everything else. To call
a provider it needs per-provider config + an API key. Relevant current state:

- `internal/config` resolves `defaults < file < env < flags` via Viper; the
  `Config` struct has **no secret/provider fields yet**.
- `onclaw config show` marshals the whole `Config` struct to JSON — so any secret
  placed on that struct is printed. Footgun by construction.
- `onclaw run` is a placeholder; there is no provider concept.

Hard constraints: 2 GB RAM target (Raspberry Pi / Orange Pi class), headless
(no OS keychain / secret-service daemon usually present), `CGO_ENABLED=0` static
binary, and a prior product decision that **adding a provider must not require
restarting a running session** (hot-reload).

## Goals / Non-Goals

**Goals:**
- Persist provider profiles and API keys so keys are **not plaintext at rest**.
- Add/change a provider without restarting a running session.
- Resolve secret precedence `env > DB > error`; never print or log secrets.
- Keep a CGO-free static binary and fit comfortably in 2 GB.
- Make the crypto boundary honest about what it does and does not protect against.

**Non-Goals:**
- OS keychain integration (deferred — unreliable on headless Pi).
- The agent core, tools, memory, and plugin system (separate changes). This
  change ships the storage + provider layer plus a `provider.Build(name)` hook
  the agent will consume.
- Encrypting non-secret profile fields (endpoint/model are not sensitive).
- Vector/embedding storage (unrelated).

## Decisions

### D1 — Two tables: `llm_providers` (plain) + `config_secrets` (encrypted)
Only the API key is sensitive. Encrypting the rest adds cost for no security
benefit and complicates queries/listing. Profiles are queryable plaintext;
`config_secrets` holds encrypted blobs keyed by profile name. Keyless providers (e.g.
local Ollama) simply have no `config_secrets` row.

### D2 — AES-256-GCM, per-row salt + nonce, base64 blob
Authenticated encryption (tamper-evident), pure-Go stdlib (`crypto/aes` +
`crypto/cipher`) → no CGO. A random 16-byte salt and 12-byte GCM nonce per value
prevents nonce/key reuse. Stored layout: `base64(salt ‖ nonce ‖ ciphertext ‖ tag)`.

### D3 — DEK + wrap architecture (keyfile default, passphrase optional)
A random 32-byte **data-encryption key (DEK)** encrypts all secrets. The DEK
itself is stored **wrapped** by a key-encryption key (KEK) derived from one of:
- **Keyfile mode (default, unattended):** random `master.key` (mode 0600) is the
  KEK source. Works headless with no interaction; hot-reload + restart both clean.
- **Passphrase mode (opt-in via `onclaw unlock`):** KEK = Argon2id(passphrase,
  salt). Stronger at-rest — an attacker with the DB but not the passphrase cannot
  decrypt — at the cost of re-unlocking on (re)start.

Switching modes only re-wraps the DEK under a new KEK; **secrets are never
re-encrypted**. v1 supports one active wrap at a time; the DEK design keeps
multi-wrap / rotation a low-cost future addition.

_Alternatives considered:_ passphrase-only (strongest but blocks unattended
operation), keyfile-only (obfuscation grade only), OS keychain (Pi-unavailable),
machine-bound derivation (weakest — obfuscation).

### D4 — Argon2id tuned down for the Pi
Memory cost ~16–32 MB (not the typical 64 MB+), time = 3, parallelism = 2 —
deliberately kind to a 2 GB device. Fallback: PBKDF2-SHA256 (pure stdlib, no
`x/crypto` dependency, lower RAM, weaker) if Argon2id proves too heavy.

### D5 — Secret resolution precedence: env > DB > error
`ONCLAW_PROVIDER_<NAME>_API_KEY` (env) wins and is **in-memory only** — never
persisted, never encrypted-stored. If absent, decrypt the DB `config_secrets` row. If
neither, the agent errors with “run `onclaw provider login <name>`.”

### D6 — Plaintext never persists, prints, or transits logs
Decryption produces a short-lived buffer used solely to build the `ChatModel`.
Secrets are **not** fields on the printed `Config` struct; `config show` renders
`api_key: ***`; the JSONL session transcript redacts known-secret patterns at the
logging boundary.

### D7 — Hot-reload without restart
fsnotify watcher on the `.db` **and** `.db-wal` files (WAL writes land in the WAL
before checkpoint; watching both avoids missed events), with a **SIGHUP fallback**
that `onclaw provider add|login|use` sends when a long-running onclaw is detected
(pidfile/socket). Reloads apply on the **next agent turn** — an in-flight request
finishes with the old provider.

## Risks / Trade-offs

- **[Keyfile mode is obfuscation-grade vs. a disk-access attacker]** → Document
  the threat model explicitly; keyfile defeats casual exposure (DB dumps, grep,
  backups) but not someone who can read the filesystem. Offer passphrase mode for
  real protection.
- **[Passphrase mode conflicts with unattended restart]** → It is opt-in; the
  daemon re-prompts on restart. Users accept this knowingly.
- **[Argon2 memory cost on 2 GB]** → Tune params down (D4) and benchmark on a Pi;
  fall back to PBKDF2 if needed.
- **[Forgotten passphrase]** → With no keyfile wrap, secrets are irrecoverable.
  Provide `onclaw unlock --reset` (destroys secrets, re-initializes DEK + keyfile)
  and document the trade-off.
- **[DB corruption loses keys]** → Profiles are plain-recoverable; encrypted
  secrets are not. Recommend backups; future `config export/import` is deferred.
- **[WAL watcher misses an event]** → SIGHUP fallback + mtime-poll safety net.

## Migration Plan

Greenfield — no existing secrets to migrate. First run:

1. Create the sqlite DB at `$XDG_DATA_HOME/onclaw/onclaw.db` with mode **0600**,
   WAL mode, and run migrations (`llm_providers`, `config_secrets`, `preferences`).
2. Generate a random DEK; generate `master.key` (0600); store the DEK wrapped
   under the keyfile KEK. (Default = keyfile/unattended mode.)
3. No providers configured yet → `onclaw run` errors with the `provider login`
   hint until the user adds one.

Rollback: deleting the DB + `master.key` returns to the pre-change state (data
loss of any configured providers, which is acceptable pre-release).

## Open Questions

- Default first-run mode = keyfile (unattended)? **Recommended: yes**; user can
  upgrade to passphrase via `onclaw unlock`.
- In passphrase mode, also keep a keyfile wrap as recovery? **Default: no** (it
  would weaken passphrase protection); offer only as an explicit opt-in.
- Exact Argon2id parameters — confirm on real Pi hardware during implementation.
- Column/table naming follows goclaw: `llm_providers` (`provider_type`,
  `api_base`) and `config_secrets`. ✓ decided.

## Abstractions (replaceability) — refinement

The initial implementation fused storage, crypto, provider construction, and the
reload watcher into one `provider.Manager` struct. This refinement splits it into
small interfaces composed by a thin facade, so each layer is independently
replaceable (test fakes, future backends, pluggable provider kinds). Inspired by
goclaw's `store` (interface + swappable backend) and `providers` (adapter
registry) — but scaled to a personal agent: no tenants, round-robin, failover, or
build-tags.

### D8 — Storage is interface-backed
Three small interfaces in `internal/store`: `ProfileStore`, `SecretStore`
(crypto-agnostic — opaque blobs in/out), and `KVStore` (preferences). The sqlite
implementation is the only backend today; an in-memory fake backs unit tests, and
a future backend would implement the same interfaces with zero caller changes.
`SecretStore` stores opaque encrypted blobs — **the facade owns crypto**, not the
store (keeps the store free of a crypto dependency, maximally swappable).

### D9 — Provider kinds are pluggable via an adapter registry
A new `internal/providers` package defines an `Adapter` interface
(`Build(ctx, profile, apiKey) → model.ChatModel`) and an `AdapterRegistry`
mapping profile `kind → AdapterFactory`. `Build(name)` becomes pure orchestration:
load profile → resolve secret → look up adapter by `kind` → build. Adding a
provider kind = `adapters.Register(...)`, never a switch edit. Builtins:
`anthropic`, `openai`, `openai-compatible`, `ollama` (keyless). Eino's
`model.ChatModel` remains the runtime provider contract (onclaw does not define
its own chat interface).

### D10 — KeyManager interface centralizes crypto
`internal/secrets` exposes a `KeyManager` interface (Encrypt/Decrypt, DEK,
SwitchToPassphrase/SwitchToKeyfile). This consolidates the DEK/wrap logic
currently duplicated across `cli/context.go` and `cli/unlock_cmd.go`, and makes
the verify-finding (keyfile not removed on keyfile→passphrase switch) a one-line
fix inside `SwitchToPassphrase`.

### D11 — Thin `provider.Service` facade
`provider.Service` composes `ProfileStore + SecretStore + KeyManager +
AdapterRegistry + reload flag`, exposing `Build(name)`, `ResolveSecret`, and
`Reload/ReloadIfNeeded`. This is the only type the CLI/agent import. The
fsnotify/SIGHUP watcher moves to `provider/watcher.go`.

## Persistence model (goclaw-informed refinement)

Surveying goclaw (`internal/store`, `internal/providers`): goclaw stores a
provider as one `llm_providers` row with the **encrypted API key inline**, a
**`settings` JSONB** for provider-specific extras, and an **`enabled`** flag; it
separates app secrets (`config_secrets` KV) and gateway auth keys (`api_keys`,
hashed). onclaw borrows the JSONB/enable/generic-secret ideas but **keeps the key
in a separate table** (redaction strength) and **skips** gateway keys + tenancy
(not applicable to a personal agent).

**Naming (goclaw-aligned):** provider table `llm_providers`; columns
`provider_type`/`api_base` (not `kind`/`base_url`); encrypted KV table
`config_secrets` (goclaw's name). Divergences kept on purpose: no inline
`api_key` column, no `tenant_id`/`display_name`/uuid id (personal agent).

### D12 — Keep secrets in a separate table (do NOT copy goclaw's inline key)
onclaw's value is redaction ("profiles are always safe to print", keyless
providers, `config show` safety). A separate encrypted table structurally
guarantees that; inlining the key in the profile row (goclaw) would trade that
away for a multi-tenant SaaS model onclaw doesn't have.

### D13 — Add `settings` JSONB and `enabled` to profiles
`llm_providers.settings` (JSON, default `{}`) carries provider-specific
config (custom headers, ollama keep-alive, reasoning effort) without a schema
migration per quirk. `enabled` (default 1) allows parking a provider without
deleting it. `provider_type` is the adapter-registry key (matching goclaw).

### D14 — Generalize the secret store to an encrypted key-value namespace
`config_secrets` becomes a generic encrypted KV keyed by `key` (e.g. `provider:claude`)
rather than a provider-only `profile_name` column. Providers use
`provider:<name>`; the M3 plugin system will reuse the same store with
`plugin:<name>:<field>` and MCP OAuth tokens with `mcp:<server>` keys — one store,
one interface, many uses. Mirrors goclaw's `config_secrets` generality.

## File organization convention (D15)

Each package separates **contract, DTOs, and implementation** into minimal files
— no file mixes an interface with its implementation, and request/response/domain
types live apart from both. This is goclaw's idiom (interfaces in `store/`, impls
in `sqlitestore/`) taken one step further: one concern per file.

Per-package rules:
- **`types.go`** — domain models / DTOs (row structs, request/response) only.
- **one file per interface** (e.g. `store.go`, `adapter.go`, `keymanager.go`) —
  the interface + factory signatures only, no implementation.
- **impl files** — concrete implementations only (`sqlite.go`, `stub.go`,
  `registry.go`, `keymanager_impl.go`, …).
- **`db.go`** — DB lifecycle (`Open` / `Migrate` / `ResolveDbPath`).

### Revised package layout (file-level)

```
internal/
  store/
    types.go            Profile (DTO / row model)
    store.go            ProfileStore, SecretStore, KVStore interfaces
    db.go               Open, Migrate, ResolveDbPath
    sqlite.go           sqliteProfileStore / sqliteSecretStore / sqliteKVStore impls
  secrets/
    keymanager.go       KeyManager interface
    crypto.go           AES-256-GCM Encrypt/Decrypt, GenerateDEK, WrapDEK/UnwrapDEK, DeriveKEKFromPassphrase
    keyfile.go          ResolveKeyfilePath, GetOrCreateKeyfileKEK
    keymanager_impl.go  keyManagerImpl (Encrypt/Decrypt/SwitchToPassphrase/SwitchToKeyfile)
  providers/
    types.go            Profile (DTO consumed by adapters)
    adapter.go          Adapter interface + AdapterFactory
    registry.go         Registry interface + registryImpl + NewRegistry
    stub.go             StubChatModel + stubAdapter + NewStubAdapter
  provider/
    service.go          Service facade (composes store + secrets + providers)
    watcher.go          fsnotify + SIGHUP reload watcher
```
