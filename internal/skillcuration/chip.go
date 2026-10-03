package skillcuration

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/ingest"
)

// SessionEventKindSkillCandidate is the application-owned session-event kind
// (the ADK extension namespace, the x.prompt_blocked / x.memory_ingested
// convention) that persists the skill-candidate signal on a qualifying
// run's transcript. The ADK runner never produces it; the runner-side wiring
// appends it from the payload this package computes, so hydrated transcripts
// render it identically to the live stream and surfaces can badge pending
// work (spec: "Qualifying run emits a candidate signal").
const SessionEventKindSkillCandidate = adk.SessionEventKind("x.skill_candidate")

// SkillCandidatePayload is the durable chip payload for one qualifying run:
// the run's identity coordinates and its cluster linkage. SkillName is
// deliberately empty at qualification time — the candidate row (and its
// validated draft name) comes later, from the proposer's side-call; the
// qualification signal only badges the run as candidate-bearing evidence.
// The JSON tags are the web contract, identical for the live emission and
// the hydrated read.
type SkillCandidatePayload struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	// RunID is the qualifying run's turn id (the ingest job's TurnID).
	RunID   string `json:"run_id"`
	Cluster string `json:"cluster_id"`
	// SkillName is the proposed skill's slug once the proposer has drafted
	// one; empty while the payload only marks qualification.
	SkillName string `json:"skill_name"`
	// CandidateID is the stored candidate row's id once the proposer has
	// persisted its draft; empty while the payload only marks
	// qualification. The proposer's emission (task 5) fills it so the
	// transcript chip links straight at the review row; the qualification
	// emission (qualifier.go) leaves it empty.
	CandidateID string `json:"candidate_id,omitempty"`
}

// The chip payload rides the session-event Extension any field; registering
// the concrete type is what lets the serializer round-trip it as the struct
// instead of a generic map (the promptBlockedEvent / MemoryIngestedPayload
// precedent).
func init() {
	schema.Register[SkillCandidatePayload]()
}

// CandidateChipSink receives one qualifying run's chip payload after the run
// indexed and qualified; the runner-side wiring (task 7) appends it to the
// session's event stream under SessionEventKindSkillCandidate and broadcasts
// the live event, cloning the memory chip's AppendMemoryChip shape. The
// clone carries the same origin rule the memory chip does: scheduler- and
// heartbeat-origin runs never emit chips (unattended runs have no transcript
// audience; spec agent-memory-pipeline, Ingested-chip event) — the skip
// lives in the sink implementation, not here. An absent sink is the unwired
// chip capability (the memory ChipSink precedent): qualification still
// indexes and triggers proposal work; only the transcript signal is dropped.
type CandidateChipSink func(ctx context.Context, job ingest.Job, payload SkillCandidatePayload)
