# Design

## Context

The attachment pipeline is two-sided: the backend store classifies uploads into lanes (`internal/attachments/attachments.go` `laneOf`) and the frontend mirrors that classification client-side (`frontend/src/features/chat/attachments.ts` `laneRejection`) so bad files reject without a round trip. The turn path (`internal/agents/turninput.go`) maps each lane to model-input content, and `read_file` (`internal/agents/backend/fs_jailed_backend.go`) already reads arbitrary workspace files — binary files read as text, office documents as structured views. add-document-editing task 5.5 widened the backend with the office lane but never the frontend mirror or picker, so the composer rejects what the store accepts. The chat-attachments spec never covered the office lane either.

## Goals / Non-Goals

**Goals:**
- Any file can be attached in chat and reaches the agent (as content when a kind is carriable, as a workspace reference otherwise).
- One catch-all lane replaces the "unsupported file type" rejection.
- Frontend mirror and backend classifier agree again; the spec names the office lane and the catch-all explicitly.

**Non-Goals:**
- New native carriage (no audio parts, no new connector rows in `internal/carriage`).
- Mime sniffing of file contents; classification stays on the reported mime.
- Transcript chip icon changes (chips are already lane-agnostic).
- Changes to per-message count (4), existing lane caps, or the uploads mirror.

## Decisions

- **D1 — Catch-all file lane, cap 20 MB.** `laneOf` gains `LaneFile` ("file") as the `default` branch, cap `MaxFileBytes = 20 << 20` — the same cap as PDF/video/office, so one "big files" mental model. An empty mime takes this lane, which also removes the WebKit-reports-empty-mime failure mode. Alternative considered: a 100 MB general cap — rejected; attachments are chat context, and the uploads mirror copies the file into the workspace where bigger artifacts belong.
- **D2 — File-lane delivery is always a pointer note.** In `turninput.go`, office documents already never ride (`reasonOfficeBytesWithheld`); the file lane joins that path with its own reason constant (`reasonFileBytesWithheld` = "its bytes are not sent; inspect it with the filesystem tools") so the note stays truthful — unlike office, there is no structured view. Leaving the file lane on the current fall-through is not an option: `modelGapReason` would claim "the active model cannot read X input", which is false for arbitrary binaries — the bytes are withheld by design, not by a model gap. With the explicit case in place the switch's final fall-through is unreachable for store-accepted mimes; it stays as defense in depth.
- **D3 — Picker goes unfiltered.** `ATTACHMENT_ACCEPT` is removed so the `accept` attribute disappears (an `accept` of `"*/*"` is inconsistent across platforms; omitting it is the standard accept-all). Drag-and-drop and paste need no change. The chat composer is the only surface touched — the knowledge panel's scoped picker keeps its filter (ingestion has real format limits).
- **D4 — Frontend mirror restructure, same shape.** `laneRejection` gains the office lane (docx/xlsx/pptx mimes → 20 MB, label "office document") and a default branch applying the file lane's 20 MB; the unsupported-type string disappears. `MAX_FILE_BYTES` and `MAX_OFFICE_BYTES` join the exported caps. The backend stays the authority — the mirror only pre-rejects.
- **D5 — No degrade warnings for never-riding lanes.** The composer's model-gap and connector-gap warnings exist because a part the user expects to arrive might not; office and file-lane attachments never ride by design, and warning on every such attachment is noise (office already warns nothing today). The pointer note in the transcript is the honest signal.
- **D6 — No binding changes.** `chat.saveAttachment(name, mime, base64)` is mime-agnostic; no Wails binding regeneration.

## Risks / Trade-offs

- [Agents receive pointer notes for popular types (e.g. legacy `.doc`, `.rtf` as `application/rtf`)] → read_file reads them as text best-effort; users can convert to office/text formats. Accepted: honest degradation beats rejection.
- [Users attach very large binaries and expect the model to see them] → The 20 MB cap and the pointer note's explicit "bytes are not sent" keep expectations set; oversize rejects with the reason on the chip.
- [`text/*` mimes that are really binary (e.g. `text/csv` is fine, but a mislabeled binary) still ride inline at 1 MB] → unchanged behavior, bounded by the small cap.

## Migration Plan

No data migration: stored rows keep their mimes, and previously rejected types were never stored. Rollback is a plain revert.

## Open Questions

None.
