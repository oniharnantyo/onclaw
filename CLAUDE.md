# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

**onclaw** is an on-device AI coding agent CLI built for **low-resource single-board computers (~2 GB RAM, 8 GB storage)** — Raspberry Pi / Orange Pi class. Every design choice optimizes for that: a single statically-linked binary (`CGO_ENABLED=0`), a pure-Go SQLite (no CGO/libc dependency so it cross-compiles to ARM), and conservative defaults (`concurrency: 1`, `max_context_tokens: 8192`). Keep memory footprint in mind when adding features.

> **Status:** CLI shell + provider/secrets storage layer are implemented. The agent itself is **not** — `onclaw run` is a placeholder, `internal/agent/` is a stub, and only a stub LLM adapter is registered.

## Commands

```bash
make build            # static, stripped binary -> bin/onclaw
make run ARGS='version'           # build then run
make test             # go test ./...
make vet              # go vet ./...
make lint             # golangci-lint, falls back to go vet
make fmt              # gofmt -s -w .

make build-all        # cross-compile linux amd64 / arm64 / armv7

# single package / single test:
go test ./internal/secrets/...
go test -run TestName ./internal/cli/...
```

## Architecture

`main.go` is trivial — it calls `internal/cli.New().Run(...)`. All command wiring lives in `internal/cli/` (urfave/cli v3).

**Assembly root:** `internal/cli/context.go` `getProviderManager()` is the spine of the app. It opens the SQLite DB (`sqlite.ResolveDbPath` → `sqlite.Open` → `sqlite.Migrate`), then either initializes a fresh DEK in **keyfile mode** (first run) or decrypts the wrapped DEK via keyfile or Argon2id passphrase, and finally assembles the `llm.Service`. Read this function first when orienting.

**Config** (`internal/config/`, Viper-backed): layered `defaults < config file < ONCLAW_* env < CLI flags`. `onclaw config show` prints the merged result. The root command's `Before` hook applies config + logging so global flags work everywhere.

**LLM** (`internal/llm/`): `Service` is a facade over four injected collaborators — `store.ProfileStore`, `store.SecretStore`, `secrets.KeyManager`, and `adapter.Registry`. It caches profiles + decrypted API keys in memory behind an `atomic` reload-pending flag. `Service.Build(name)` resolves the secret (env `ONCLAW_PROVIDER_<NAME>_API_KEY` > DB) and dispatches to the registered adapter, which returns a `cloudwego/eino` `model.ChatModel`. **Only a stub adapter is registered today** (`internal/llm/adapter/stub.go`).

**Secrets** (`internal/secrets/`): AES-256-GCM with a DEK/KEK split. Default is **keyfile mode** — DEK wrapped under a `master.key` (0600) for unattended operation. `onclaw unlock` re-wraps the DEK under an Argon2id-derived passphrase KEK (`SwitchToPassphrase`). Never log/return decrypted secrets in the clear; `internal/logging/` redacts known credential fields.

**Store** (`internal/store/`): interfaces (`store.go`) + DTOs (`types.go`) kept separate from the `internal/store/sqlite/` implementation (`db.go` lifecycle/migrations; one file per entity: `profile.go`, `secret.go`, `kv.go`). Follow this contract/types/impl separation for new entities.

**Hot-reload:** provider profile edits made by `onclaw provider …` write a PID file and `SIGHUP` the running process; a `fsnotify` watcher is the in-process fallback. Both set `Service.reloadPending`, so the next turn re-reads from SQLite.

## Conventions

- **OpenSpec** (`openspec/`) drives planned changes — proposals under `openspec/changes/`, specs under `openspec/specs/`. Check there before designing non-trivial features.
- **IMPORTANT**: Go style + the store-package layout rules live in `.claude/rules/coding-style.md` (tabs/gofmt, separate contract/types/impl files, `errors.Is`-friendly `%w` wrapping, `context.Context` first param). You should strictly follow the rules.