## Why

onclaw talks to remote LLM providers (Claude / OpenAI / Ollama), which requires
per-provider configuration (endpoint, model) and an API key. Today the CLI has no
provider concept and `onclaw run` is a placeholder. Keys must be stored so they
(a) survive restart, (b) can be added or changed without restarting a running
session, and (c) are **never** exposed as plaintext — at rest, in `config show`,
in logs, or in the session transcript.

## What Changes

- Add a sqlite-backed store (`internal/store`) and a provider package
  (`internal/provider`) holding **provider profiles** and **API keys**.
- **Provider profiles** (name, provider_type, api_base, model) are stored as plaintext
  rows — they are not sensitive.
- **API keys** are stored **encrypted** (AES-256-GCM) in a `config_secrets` table —
  never plaintext at rest. Each value carries its own random salt + nonce.
- **Hybrid key source:** a random `master.key` (mode 0600) by default for
  unattended operation; an opt-in `onclaw unlock` passphrase (Argon2id) for
  stronger at-rest protection. Both wrap a single data-encryption key (DEK), so
  switching modes never re-encrypts secrets.
- **CLI:** `onclaw provider [list|use|add|remove|login]`, `onclaw unlock`, and a
  `--provider <name>` override on `run`/`chat`.
- **Hot-reload:** adding or changing a provider via the CLI is picked up by a
  running session on the next turn (fsnotify on `.db` + `.db-wal`, SIGHUP
  fallback) — no restart.
- **Non-disclosure:** `onclaw config show` prints `api_key: ***` for every
  profile; secrets are never placed on the printed `Config` struct; the session
  transcript redacts known-secret patterns at the logging boundary.
- **Precedence:** `ONCLAW_PROVIDER_<NAME>_API_KEY` env overrides the stored
  secret and is in-memory only (never persisted).

## Capabilities

### New Capabilities

- `providers`: Provider profile and API-key storage, encryption-at-rest, secret
  resolution precedence (env > DB), key management (keyfile + optional
  passphrase), non-disclosure, and hot-reload without restart.

### Modified Capabilities

_None — this is greenfield (no existing specs in `openspec/specs/`)._

## Impact

- **New packages:** `internal/store` (sqlite open + migrations + 0600 file perms)
  and `internal/provider` (profile CRUD, secret encrypt/decrypt, DEK/key
  management, `ChatModel` build hook for the agent).
- **New dependencies:** `modernc.org/sqlite` (pure-Go, keeps `CGO_ENABLED=0`)
  and `golang.org/x/crypto/argon2` (passphrase KDF). No CGO; static binary
  preserved.
- **CLI additions:** `provider`, `unlock` subcommands; `--provider` flag on
  `run`/`chat`; `config show` redaction.
- **Security:** introduces the DEK + keyfile/passphrase crypto boundary; DB file
  created at mode 0600; secrets never logged or printed.
- **Foundation for M1:** the agent core (separate change) consumes
  `provider.Build(name)`; this change delivers the storage + provider layer it
  depends on.