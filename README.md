# onclaw

**Open, on-device AI coding agent for low-resource devices**

A Claude Code–style agent designed for Raspberry Pi / Orange Pi class hardware (~2 GB RAM, 8 GB storage). Ships as a single statically-linked binary with no runtime dependencies.

## What is this?

**onclaw** is an on-device AI coding agent CLI built for low-resource single-board computers. Every design choice optimizes for tiny footprint: pure Go (no CGO), statically-linked binary, pure-Go SQLite (no libc dependency for ARM cross-compilation), and conservative defaults.

**Current status:** CLI shell + provider/secrets storage layer implemented. Agent logic is not yet — `onclaw run` is a placeholder.

## Quick Start

```bash
# Clone and build
git clone <repo-url>
cd onclaw
make build              # -> bin/onclaw

# Run interactive onboarding to configure your first provider
./bin/onclaw init

# Or configure manually
./bin/onclaw provider add openai --kind openai --model gpt-4o
./bin/onclaw provider login openai
./bin/onclaw provider use openai
```

That's it. Configuration is stored in `~/.onclaw/onclaw.db` (SQLite) with encrypted API keys.

## Features

- 🪶 **Tiny static binary** — `CGO_ENABLED=0`, stripped, no libc needed
- 🧩 **Cross-compile matrix** — Linux `amd64`, `arm64`, `armv7` from one command
- ⚙️ **Layered config** — defaults `<` config file `<` env `<` CLI flags
- 📋 **Structured logging** — Go stdlib `log/slog`, text or JSON
- 🔐 **Encrypted secrets** — AES-256-GCM with keyfile or passphrase KEK
- 🔄 **Hot-reload** — Running sessions reload provider profiles live

## Build & Install

```bash
# Build for host platform
make build              # -> bin/onclaw
make install            # -> /usr/local/bin

# Cross-compile for low-resource devices
make build-all          # -> bin/onclaw-linux-{amd64,arm64,armv7}

# Development
make test               # go test ./...
make lint               # golangci-lint (or go vet)
make fmt                # gofmt -s -w .
```

## Configuration

Config resolves in priority order (higher overrides lower):

1. **Defaults** — conservative, low-resource tuned
2. **File** — `.env` searched in `.`, `~/.onclaw`, `/etc/onclaw` (first found wins)
3. **Env** — `ONCLAW_*` environment variables
4. **Flags** — `--config`, `--log-level`, etc.

Inspect effective config:

```bash
onclaw config show      # merged config from all layers
onclaw config path      # which .env file was loaded
```

## Project Structure

```
onclaw/
├── bin/                    # Build output
├── docs/                   # Documentation (security model, migration guide)
├── internal/
│   ├── agent/             # Agent core (stub — not implemented)
│   ├── cli/               # Command tree (urfave/cli v3)
│   ├── config/            # Layered config (Viper-backed)
│   ├── llm/               # LLM service facade + adapter registry
│   ├── logging/           # Structured logging with secret redaction
│   ├── observability/     # Observability hooks
│   ├── secrets/           # AES-256-GCM encryption, DEK/KEK management
│   ├── store/             # SQLite storage (profiles, secrets, KV)
│   ├── version/           # Build-time metadata
│   └── workspace/         # Workspace management
├── openspec/
│   ├── changes/           # Change proposals
│   └── specs/             # Feature specifications
├── main.go                # Entrypoint (delegates to internal/cli)
├── Makefile               # build / test / cross-compile / release
├── CLAUDE.md              # Claude Code workspace guidance
├── AGENTS.md              # Agent execution context
└── go.mod                 # Go module definition
```

## Documentation

| Resource | Description |
|----------|-------------|
| [CLAUDE.md](CLAUDE.md) | Project guidance for Claude Code (architecture, conventions) |
| [AGENTS.md](AGENTS.md) | Agent execution context and capabilities |
| [docs/security-model.md](docs/security-model.md) | Encryption architecture and threat model |
| [docs/MIGRATION_GUIDE.md](docs/MIGRATION_GUIDE.md) | Config migration from YAML to .env |

## Contributing

**Roadmap — what's next:**
- [x] Provider profiles & encrypted secrets storage
- [ ] Agent core: prompt handling and ReAct execution loop
- [ ] Tool execution loop
- [ ] Interactive chat mode
- [ ] Memory budgeting tuned for 2 GB devices

Development workflow:
```bash
make fmt                  # Format with gofmt
make lint                 # Lint with golangci-lint
make test                 # Run tests
make vet                  # go vet ./...
```

See [CLAUDE.md](CLAUDE.md) for architecture and coding conventions.

## License

TBD — choose a license before publishing.
