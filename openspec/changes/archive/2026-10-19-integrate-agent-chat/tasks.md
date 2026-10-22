# Tasks

## 1. Foundation: overrides, capability catalog, model/effort resolution

- [x] 1.1 Add migration `000008` (`sessions.model`, `sessions.effort` TEXT DEFAULT ''; `agents.effort` TEXT DEFAULT '') with store columns, scan/INSERT/UPDATE, and Go struct fields; run `gofmt -w . && go vet ./... && go test ./...` and update any migration-count test pins to the new count
- [x] 1.2 Extend `internal/agents/catalog.go` with per-model reasoning-effort sets and an `Efforts(providerType, model)` accessor plus the openrouter-family id heuristic (unknown ids return an empty set); verify with table-driven tests covering every canonical model, a heuristic hit (`gpt-5*`/`claude-*`/`gemini-2.5*`), and an unknown id
- [x] 1.3 Widen `SettingsService.ProviderModels` to return `[]ModelCapability{ID, ContextWindow, Efforts, ImageInput, PDFInput}` (canonical → catalog, live `/models` fetch still id-only + heuristic efforts); run `task generate:bindings`, update `client.ts`/`transport.ts` and the `AgentConfigModal`/`CreateAgentModal` consumers, verify `npm run build` passes in `frontend/`
- [x] 1.4 Add an override parameter to `registry.Build` (model, effort) and map effort to each connector's native knob (`acl/openai.WithReasoningEffort`, claude `WithThinkingConfig` budgets 4k/16k/32k, gemini `WithThinkingConfig` budgets 512/2048/8192, unset = parameter omitted); verify with table-driven tests asserting the option each provider receives per effort, plus rejection tests for unsupported effort and listing-rejected model
- [x] 1.5 Resolve effective config in `manager.Send`: session override → agent default for model and effort, rejection errors surfaced as `turn_error` events; add a store/session method + bound `ChatService.SetSessionOverrides(sessionID, model, effort)`; run `task generate:bindings`, add the `client.ts` wrapper, verify with tests: override wins, unset inherits agent default, rejected effort produces a clear turn error
- [x] 1.6 Land the group's gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `frontend: npm run build && npm test && vp check src` all exit 0 (this group introduces no `vp fmt` violations)

## 2. Composer pickers, agent picker, config effort default

- [x] 2.1 Add session-override state to the chat store (hydrate from session row, call `SetSessionOverrides`, seed new sessions from sticky `useLocalPreference` defaults falling back to the agent's config); verify with store tests: hydration, persist-on-pick, seed-on-create
- [x] 2.2 Build the model and effort popovers on the composer's bottom control rail (model options from the capability DTO; effort options only for the selected model, control hidden when the set is empty; effective model/effort shown when unset); verify with component tests covering hidden-when-empty effort and both-theme styling, `vp check src` clean
- [x] 2.3 Wire the composer agent picker to the existing `openChat`/`chatTarget` navigation (switching context leaves the viewed session binding untouched); verify with a component test: pick another agent → chat context switches, original session unchanged
- [x] 2.4 Add the default-effort select to `AgentConfigModal`'s Model tab (options limited to the selected model's set, cleared when switching to a model without support, saved through the agent update DTO — add `agents.effort` to create/update inputs); run `task generate:bindings`, verify with component tests: options follow model, reset on model change, save round-trips
- [x] 2.5 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 3. Turn cancellation

- [x] 3.1 Replace the `busy` map with a running-turn record holding a per-turn `context.WithCancel` and an `adk.WithCancel()` func (2s escalation goroutine to ctx cancel); add bound `ChatService.Cancel(sessionID)`; run `task generate:bindings`, verify with tests: cancel stops an in-flight turn, cancel with no active turn is safe, second cancel is a no-op
- [x] 3.2 Switch `record`/`failTurn` to `recordCtx`, add the `turn_cancelled` event kind (`knownKind` whitelist + payload), emit it on cancel with partial output retained, and report `RunFinished(..., "cancelled")` to hooks; verify with tests: terminal cancelled event persists after ctx cancellation and hooks observe the cancelled status
- [x] 3.3 Frontend: swap the composer send control to stop while busy, finalize streaming entries on `turn_cancelled`, and auto-dispatch the next queued message after cancellation; verify with store/component tests: stop mid-stream keeps partial text, control returns to send, queue drains, `vp check src` clean
- [x] 3.4 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 4. Durable approvals

- [x] 4.1 Swap phase 9 from `turnAgent.Run` to `adk.NewRunner` with `CheckPointStore: store.Checkpoints()` (key prefix `adk_<turnID>`, `WithCheckPointID`), keeping phases 1–8 and drain mapping byte-identical; verify with a post-swap smoke test: a normal turn produces the same event-kind sequence as before, `npm test` and `go test ./...` pass
- [x] 4.2 Add the interrupt branch in `drainTurn`: on `Action.Interrupted` persist `approval_required` (interrupt id, command, checkpoint id), end the run without a terminal event, keep the session busy, and clear/reject on cancel path; add `approval_resolved` kind and the pending derivation (latest required without matching resolved); verify with tests for pending-survives-reload, double-resolve conflict, and cancel-clears
- [x] 4.3 Add the dangerous-command gate in `JailedShell.Execute`: safety-pattern list → resume-context check → decision ledger (checkpoint KV `approval/<sha256>`, consumed on read) → `tool.Interrupt(ctx, ShellApprovalInfo{Command})`; verify with tests: first match interrupts without executing, approved decision is consumed once (second identical command interrupts again), decision round-trips through the checkpoint store
- [x] 4.4 Implement bound `ChatService.ResolveApproval(sessionID, interruptID, approved)` + `resumeTurn`: validate against pending (stale ⇒ conflict error), record the ledger entry, resume live or cold (rebuild phases 4–9, `Runner.Resume` from checkpoint), denial fed back as resume data; run `task generate:bindings`, verify with tests covering approve-runs-command, deny-reports-refusal-to-model, stale-id conflict, and a cold resume after simulated process loss
- [x] 4.5 Frontend approval card: pending card (command text, Approve/Deny), resolved states on replay, actions disabled once resolved; verify component tests for pending/resolved/replay rendering in both themes and the bound error on stale decisions, `vp check src` clean
- [x] 4.6 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 5. Attachments

- [x] 5.1 Add migration `000009` (attachments table) and create `~/.buddies/attachments/` in the home-layout bootstrap; verify with migration + layout tests, update migration-count pins, `go test ./...` passes
- [x] 5.2 Implement bound `SaveAttachment(name, mime, data)` (lane classification + caps: 4 files/message, image 5 MB, PDF 20 MB, text 1 MB → rejected with reason) and `AttachmentContent(key)` (mime + base64, capability-keyed, no raw path exposure); run `task generate:bindings`, verify with tests: accept/reject per cap and type, unknown key refused, content round-trips
- [x] 5.3 Extend the turn path: user-message payload carries attachment metadata (persisted for replay) and multi-part model input (image/PDF bytes when the model's capability flags allow, reference pointer otherwise); re-send stored content on regenerate, never re-upload; verify with tests: replay reconstructs metadata, text-only model receives pointer note instead of bytes, capable model receives content
- [x] 5.4 Frontend chip tray: attach button, chat-view drag-drop, paste-image, per-chip progress/cancel/retry, send blocked while uploading, capability warning when the model lacks image/PDF input; verify component tests for each state and the send-block, `vp check src` clean
- [x] 5.5 Transcript rendering: image thumbnails + document chips (name/type/size) fetched as object URLs from `AttachmentContent`, attachment-only sends render; verify component tests incl. reload-from-metadata, `vp check src` clean
- [x] 5.6 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 6. Transcript chrome

- [x] 6.1 Add bound `TranscriptPage(sessionID, beforeSeq, limit)` (newest page first, backward paging) while keeping `Transcript` for full replay; run `task generate:bindings`, switch `selectSession` to newest-page + subscribe, implement "load earlier" prepending through `applyEvent` with the height-delta scroll anchor and mid-turn page-boundary merge; verify with backend paging tests (order, boundary) and store tests for prepend-dedupe and anchor behavior
- [x] 6.2 Day dividers between dated messages (Today/Yesterday/date, full timestamp on the divider, no leading divider); verify with component tests for both boundaries, both themes, `vp check src` clean
- [x] 6.3 Chat header above the transcript (avatar, name, running/idle status tied to turn lifecycle incl. approval-paused, mono chip with effective model + effort when set); verify component tests for status transitions and override chip, `vp check src` clean
- [x] 6.4 Right sidebar shows the session's effective model and uses its context limit; verify with component test: override row reflects session choice, `vp check src` clean
- [x] 6.5 Session delete: bound `ChatService.DeleteSession(sessionID)` (removes session + events), confirm dialog, running indicator on in-flight entries; run `task generate:bindings`, verify with tests: delete removed-and-gone, cancel-delete no-op, running indicator flags an active turn
- [x] 6.6 Conversation minimap (tick per rendered message, active highlight on scroll, click-to-scroll, hidden below two messages and on narrow widths); verify with component tests, `vp check src` clean
- [x] 6.7 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 7. Regenerate and branch variants

- [x] 7.1 Backend `ChatService.Regenerate(sessionID, turnID)`: load the original turn's user text, run a new turn with `variant_of` payload markers and suppressed duplicate user-message persist; run `task generate:bindings`, verify with tests: only one user message in the log, reply events carry the marker, failure emits the turn error without markers orphaning the previous reply
- [x] 7.2 Frontend slot grouping in `applyEvent`: variants array keyed by `variant_of`, active index (newest when live), `n / total` picker component; verify with tests: live second variant appears with picker, replay reconstructs both variants, switching variants performs no requests, `vp check src` clean
- [x] 7.3 Failed regeneration restores the previous variant and drops the empty one; verify with a component test matching the spec scenario
- [x] 7.4 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 8. Right panel

- [x] 8.1 Bound jailed reader `WorkspaceFile(agentID, relPath)` + `ListWorkspace(agentID, relPath)` (path clean + prefix check inside the agent's workspace, traversal/symlink refused); run `task generate:bindings`, verify with tests: in-workspace read works, `../` and absolute foreign paths refused
- [x] 8.2 Panel shell: docked column at wide widths, overlay sheet at narrow, width persisted, per-tab close, per-tab content registry, resets closed on session switch, opens mid-turn without interrupting; verify component tests incl. both themes, `vp check src` clean
- [x] 8.3 Panel sources: workspace file preview, prompt documents (via existing prompt-doc API), subagent trajectory files from `TranscriptPath`; wire tool-card file/path links and markdown chips to open the right tab; verify component tests: link opens preview, non-workspace path shows the refused outcome, `vp check src` clean
- [x] 8.4 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 9. Run card

- [x] 9.1 Tasks lane: floating card over the transcript derived from `task` entries of the active session (description, running spinner, terminal result), visible only while content exists, reclaims state after replay; verify component tests for live + replay + hidden-when-empty, both themes, `vp check src` clean
- [x] 9.2 Plan lane: fold the latest `todo_write` tool activity's persisted items into a checklist (pending/active/done/failed), live updates mid-turn, lane hidden when absent; verify component tests per spec scenarios, `vp check src` clean
- [x] 9.3 Gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build`, `npm run build && npm test && vp check src` exit 0

## 10. Integration verification

- [x] 10.1 Full-repo gates in one pass: `gofmt -w .` (no diff), `go vet ./...`, `go test ./...`, `task build` (confirm bindings regenerated, not stale), `frontend: npm run build && npm test && vp check src` — all exit 0
- [ ] 10.2 Live end-to-end smoke against a real session (desktop `task run` or server `bin/buddies-server` recipe): pick model/effort in composer → send → verify provider request carries effort → stop mid-stream → queued message dispatches → attach image → regenerate (picker 2/2, reload keeps variants) → trigger a dangerous command and approve it (and once via restart mid-pending) → windowed load-earlier preserves position → record screenshots in the change directory as evidence
  - STATUS 2026-10-18: runtime behaviors covered by automated end-to-end tests (runner_swap_test.go, resume_test.go, attachments_turn_test.go, windowing/store frontend tests); GUI screenshot pass still to record before archive. Scenario mapping in verification.md (10.3).
- [x] 10.3 Cross-check every spec scenario in `specs/` against the smoke run or automated tests and record the mapping as a checklist in the change directory; any unverifiable scenario must be re-worded or implemented before completion
