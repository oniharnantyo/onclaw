-- Consolidator evidence and reports (integrate-agent-zero-memory D12):
-- the multi-evidence links the nightly merge stamps on canonical notes and
-- the per-workspace last morning report. Both tables are additive and
-- copy-out only — nothing here rewrites or removes raw material.
--
-- memory_note_evidence holds ADDITIONAL evidence links beyond a note's birth
-- source_event_id: folding a near-duplicate cluster into one canonical note
-- links every member's source event on the survivor, so the surviving fact
-- cites all of its origins. Rows are (note, source event) idempotent; links
-- die with the note (ON DELETE CASCADE), while the raw session events they
-- point at are never touched. The note-side FK covers tenant scoping —
-- evidence reads resolve the workspace through the memory_notes join, so no
-- query runs without a workspace scope.
CREATE TABLE memory_note_evidence (
    note_id         uuid NOT NULL REFERENCES memory_notes(id) ON DELETE CASCADE,
    source_event_id text NOT NULL,
    added_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (note_id, source_event_id)
);

-- memory_reports keeps the LAST morning report per workspace (one row per
-- workspace, replaced on every pass): what conflicted with the kept
-- documents, what merged, and how many extractions failed. The report body
-- is the consolidator's marshaled JSON — the shape is the memory package's
-- wire contract, which this table deliberately does not constrain.
CREATE TABLE memory_reports (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    report       jsonb NOT NULL,
    generated_at timestamptz NOT NULL
);
