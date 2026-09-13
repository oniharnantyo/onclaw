## 1. Observability package

- [x] 1.1 Create `internal/observability/langfuse.go`: handler construction from config (host, keys, sample rate, batching defaults) returning a runner-compatible trace handler + flusher, or nothing when unconfigured (design D1)
- [x] 1.2 Implement the centralized `MaskFunc`: redact credential-shaped values using the existing secret-row patterns (web-search keys, gateway tokens, MCP credentials, hook secrets) with unit tests
- [x] 1.3 Implement trace-context application from `ExecRequest` coordinates: `SetTrace` with session/user/tags/metadata, trace naming (input line vs schedule name), and deterministic per-run sampling (design D2/D5) — unit tests for tag/metadata shape

## 2. Runner integration

- [x] 2.1 Add `WithTraceHandler` functional option to the runner; attach the handler to the eino callback chain only when provided
- [x] 2.2 Apply trace context per turn inside the run path and capture the trace id at export start (design D3)
- [x] 2.3 Persist the trace id on the run record (reuse run metadata or add a migration per the smallest-footprint path); verify retried turns keep one trace id
- [x] 2.4 Regression tests: unconfigured runner produces no trace calls; configured runner exports one trace per turn with generations + spans mapped from a scripted model/tool exchange

## 3. API and composition

- [x] 3.1 Read `ONCLAW_LANGFUSE_*` env vars in `internal/config` and wire the handler through `internal/cli` → router → runner composition (absent when unset)
- [x] 3.2 Compose `langfuse_url` server-side (host + trace id) into the run detail/list payloads in `internal/server/handlers/agent_runs.go`; omit when no trace id
- [x] 3.3 Flusher lifecycle: flush pending exports on graceful server shutdown

## 4. Web UI

- [x] 4.1 Add the "Open in Langfuse" action to the run view gated on `langfuse_url` presence (runs table row menu and run transcript view); no action when absent
- [x] 4.2 api.ts/type updates for the run payload field; vitest coverage for the gated action

## 5. Verification

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green; new tests for masking, sampling determinism, trace-per-turn mapping, and trace-id persistence
- [x] 5.2 Extend `scripts/smoke.sh`: run payloads expose `langfuse_url: null` on unconfigured instances (contract that tracing stays invisible when disabled)
- [ ] 5.3 Manual pass with a self-hosted Langfuse (docker-compose): run a web turn, a scheduler fire, and (if the gateway exists) a Telegram turn; verify per-turn traces, session grouping, origin tags, masked secrets, sampling at 0.5, and the runs-view deep link
