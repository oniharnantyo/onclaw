# Tasks — add-langfuse-observability

## 1. Dependency & API confirmation

- [x] 1.1 `go get github.com/cloudwego/eino-ext/callbacks/langfuse@v0.1.1`; confirm it coexists with eino v0.9.9 (`go mod tidy` + `make build`).
- [x] 1.2 `go doc github.com/cloudwego/eino-ext/callbacks/langfuse` — confirm the `NewLangfuseHandler` signature and `Config` fields (notably `MaskFunc func(string) string`); record exact names before coding.
- [x] 1.3 Verify the module stays pure-Go: `CGO_ENABLED=0 make build` and `make build-all` (amd64/arm64/armv7) still succeed.

## 2. Config keys (`internal/config/`)

- [x] 2.1 Add `LangfuseConfig` (`host`, `public_key`, `secret_key`, `session_id`, `release`, `mask`) and a `Langfuse` field on `Config` in `config.go`.
- [x] 2.2 Set defaults in `defaults.go` (`Mask: true`; host/keys empty ⇒ disabled).
- [x] 2.3 Add `v.SetDefault` lines for each `langfuse.*` key in `Load()` (mirrors the existing keys).
- [x] 2.4 Tests: `ONCLAW_LANGFUSE_HOST`/`_SECRET_KEY` env + a config file populate `cfg.Langfuse`; default `Mask == true`.

## 3. `internal/observability` leaf package

- [x] 3.1 `observability.go`: `Config` + `Setup(ctx, cfg, maskFunc func(string) string) (flush func(), err error)`; leaf package (only eino/callbacks + langfuse + stdlib).
- [x] 3.2 `langfuse.go`: validate (all-empty ⇒ disabled/nil; partial ⇒ error naming missing fields); choose mask (`cfg.Mask && maskFunc != nil`); build `langfuse.Config`; `NewLangfuseHandler`; `callbacks.AppendGlobalHandlers(handler)`; return flusher.
- [x] 3.3 Tests: empty ⇒ `(nil, nil)`; partial ⇒ error; full (host = `httptest.Server`, no real egress) ⇒ non-nil flush, nil err, `flush()` no panic; mask-selection unit.

## 4. Wire `onclaw run` (`internal/cli/run.go`)

- [x] 4.1 Before `agent.RunAgent`, call `observability.Setup(...)` mapping `st.cfg.Langfuse` and injecting `tools.Redact`; `defer flush()` when non-nil.
- [x] 4.2 Confirm defer ordering (LIFO) places the flush before `db.Close` etc.

## 5. Secret hygiene

- [x] 5.1 In `internal/cli/config_cmd.go`, redact `langfuse.secret_key` in `onclaw config show` (same treatment as provider API keys).
- [x] 5.2 In `internal/logging/`, extend the redacted-field set to cover `langfuse.secret_key` if redaction keys on field names.

## 6. Verification & hardening

- [x] 6.1 `make fmt`, `make vet`, `make lint`, `make test` green; ≥80% coverage on new packages.
- [x] 6.2 `make build` (static, `CGO_ENABLED=0`) and `make build-all` succeed (no CGO/libc transitive dep).
- [x] 6.3 Manual E2E against a Langfuse project: a traced `run` produces model + tool spans; unsetting the env vars ⇒ no-op, no error; `onclaw config show` redacts `langfuse.secret_key`; a prompt containing an `sk-...` token appears masked (`[REDACTED]`) in the trace unless `langfuse.mask: false`.
