# onclaw

> Open, on-device AI coding agent for low-resource devices.

**onclaw** is an open alternative to Claude Code–style agents designed to run on
small single-board computers (**~2 GB RAM, 8 GB storage**, e.g. Raspberry Pi /
Orange Pi class hardware). It ships as a single statically-linked binary with no
runtime dependencies.

> **Status:** this repository currently contains the **CLI boilerplate** only —
> command tree, layered config, structured logging, and a cross-compile release
> pipeline. Agent/LLM logic is not implemented yet (`onclaw run` is a placeholder).

## Features

- 🪶 **Tiny static binary** — `CGO_ENABLED=0`, stripped (`-s -w`), no libc needed.
- 🧩 **Cross-compile matrix** — Linux `amd64`, `arm64`, and `armv7` from one command.
- ⚙️ **Layered config** — defaults `<` config file `<` `ONCLAW_*` env `<` CLI flags.
- 📋 **Structured logging** — Go stdlib `log/slog`, text or JSON.
- 🧱 **Clean layout** — `cmd/`, `internal/`, ready to drop the agent into.

## Requirements

- Go 1.26+ (toolchain already pinned in `go.mod`)
- GNU Make
- *(optional)* [GoReleaser](https://goreleaser.com/) for release archives
- *(optional)* `golangci-lint` for `make lint` (falls back to `go vet`)

## Build & install

```bash
# Build for the host
make build                 # -> bin/onclaw

# Install to /usr/local/bin
make install

# Or via go install (no version metadata)
go install .
```

### Cross-compile for a low-resource device

```bash
make build-all
# -> bin/onclaw-linux-amd64
# -> bin/onclaw-linux-arm64
# -> bin/onclaw-linux-armv7
```

Copy the matching binary to the device — no extra runtime is required.

## Configuration

Config is resolved in priority order; higher layers override lower ones:

1. **Defaults** (conservative, low-resource tuned)
2. **File** — `config.yaml`, searched in `.` (cwd), `~/.config/onclaw`, `/etc/onclaw`
3. **Env** — `ONCLAW_*` (e.g. `ONCLAW_LOG_LEVEL=debug`)
4. **Flags** — `--config`, `--log-level`, `--log-format`

Example `~/.config/onclaw/config.yaml`:

```yaml
log_level: info
log_format: text
concurrency: 1
max_context_tokens: 8192
model: "" # leave unset; the agent will pick a default
```

Inspect what onclaw actually resolved:

```bash
onclaw config show   # effective config (all layers merged)
onclaw config path   # which file (if any) was loaded + searched paths
```

## Usage

```bash
onclaw                      # show help
onclaw --version            # print version
onclaw version              # print version
onclaw config show          # effective configuration
onclaw run "hello"          # (placeholder) run a prompt
onclaw --log-level=debug run "hello"   # flag overrides config + env
```

## Development

```bash
make test     # go test ./...
make vet      # go vet ./...
make lint     # golangci-lint (or go vet)
make tidy     # go mod tidy + verify
make fmt      # gofmt -s -w .
make release  # GoReleaser snapshot (local, no publish) -> dist/
```

## Project layout

```
main.go            Entrypoint (trivial; delegates to internal/cli)
internal/
  cli/             urfave/cli v3 command tree + Before-hook setup
  config/          Viper-backed layered config
  logging/         log/slog setup
  version/         Build-time metadata (-ldflags -X)
Makefile           build / run / test / cross-compile / release
.goreleaser.yaml   Release matrix (linux + darwin; amd64/arm64/arm)
```

## Roadmap

- [ ] Agent core: prompt handling, model provider abstraction
- [ ] Tool execution loop
- [ ] Interactive chat mode
- [ ] Memory budgeting tuned for 2 GB devices

## License

TBD — pick a license before publishing.
