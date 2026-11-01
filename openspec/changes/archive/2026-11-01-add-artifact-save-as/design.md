# Design

## D1: Backend seam mirrors the view read

New `ChatService.SaveGeneratedDocumentAs(ctx, agentID, name string) (string, error)` in `internal/services/chat_documents.go`: identical name validation and `artifacts/`-first / root-fallback resolution as `GeneratedDocument` (extract the shared validation into a helper both call). Then open the host save dialog (`application.Get().SaveFileDialog` seeded with `name` — wails v3 beta.29 ships a server-mode no-op that yields an empty result) and on a confirmed path `os.ReadFile` the jailed source + `os.WriteFile` the destination. Return the chosen path; empty/cancelled returns a sentinel (`ErrExportCancelled`), server-mode/no-dialog returns `ErrExportUnavailable` — distinct typed errors so the UI can stay silent on cancel and toast on unavailable.

## D2: Dialog dependency stays out of the service hot path

The dialog is reached through a narrow interface injected into ChatService (`SaveDialog` with one method), defaulted to a no-op-returns-unavailable implementation; the desktop host wires the wails-backed one. This keeps `services` unit-testable without an application loop and matches the existing `Emitter`/`URLOpener` injection precedent in `services.Assemble` callers.

## D3: Capability flag

`AppInfo.Metadata` gains `NativeDialogs bool` — true from the desktop host's wiring, false in server assembly. `CreateDocumentTool` reads it through the existing metadata binding and hides the Save As control when false. (Simplest honest signal; avoids the UI having to probe by failing.)

## D4: Frontend

`CreateDocumentTool.tsx`: a Save As button beside the existing view affordance for all formats; on call, success toasts the destination's base name, `ErrExportCancelled` stays silent, `ErrExportUnavailable` toasts unavailable. Loading state while the dialog is open; the card must not double-invoke. Both themes per the styling constraint.

## D5: Non-goals

- Export from turn-artifact chips, knowledge files, or workspace previews (follow-up change if wanted).
- Move semantics, batch export, or destination remembering.
- Any change to generation, presets, or the view-read contract itself.
