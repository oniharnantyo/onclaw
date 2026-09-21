package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// materialSerializer mirrors the runner's ADK session-event serializer: raw
// session rows parse back into typed events with the same
// HumanReadableSerializer the adapter persists with.
var materialSerializer = &schema.HumanReadableSerializer{}

// maxMaterialChars bounds the prompt material; the raw log itself is never
// truncated — only its rendered projection is.
const maxMaterialChars = 12000

// gistWindow is the session's unprocessed slice: everything since the latest
// gist's end event (the incremental cursor), with the window's start time
// (the provenance event_time) and end event id (the evidence pointer).
type gistWindow struct {
	events []domain.SessionEvent
	start  time.Time
	endID  string
}

// Gister is the per-run windowed gister (task 3.5, D3): at each job it
// summarizes everything in the session since the last gist into one
// memory_events row, stamped by the participant rule, and links the gist to
// the entities it mentions (wave3 D6 — the same side-call, zero extra model
// cost). Gisting is incremental — the cursor is the latest gist, no window
// is reprocessed. Failures return; the worker owns the logging.
type Gister struct {
	events   store.MemoryEventStore
	sessions store.SessionEventStore
	entities store.MemoryEntityStore
	resolver ModelResolver
	trace    callbacks.Handler
	log      *slog.Logger
}

// NewGister constructs the gister from its granular dependencies: the gist
// timeline store, the raw session-event store, the associative entity store
// (wave3 D6/D7 event links), the workspace provider catalog, the instance
// encryption key, and the shared agentic model factory.
func NewGister(events store.MemoryEventStore, sessions store.SessionEventStore, entities store.MemoryEntityStore, providerStore store.ProviderStore, encryptionKey []byte, factory ModelFactory, log *slog.Logger, opts ...SideCallOption) *Gister {
	var cfg sideCallConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gister{
		events:   events,
		sessions: sessions,
		entities: entities,
		resolver: newSideCallResolver(providerStore, encryptionKey, factory, cfg),
		trace:    cfg.trace,
		log:      log,
	}
}

// Window computes the session's unprocessed window: every raw event after
// the latest gist's end event (D3 — no window is reprocessed). A session
// with no gist yet windows from the log's beginning. An empty window comes
// back as a zero-value window with a nil error — nothing to ingest is a
// normal state, not a failure.
func (g *Gister) Window(ctx context.Context, job IngestJob) (gistWindow, error) {
	cursor, err := g.events.LatestEventForSession(ctx, job.WorkspaceID, job.SessionID)
	if err != nil {
		return gistWindow{}, fmt.Errorf("memory gister: read cursor: %w", err)
	}

	params := store.LoadSessionEventsParams{
		WorkspaceID: job.WorkspaceID,
		SessionID:   job.SessionID,
	}
	if cursor != nil {
		params.AfterEventID = cursor.SourceEventID
	}
	events, err := g.sessions.LoadEvents(ctx, params)
	if err != nil {
		return gistWindow{}, fmt.Errorf("memory gister: load window: %w", err)
	}

	win := gistWindow{events: events}
	if len(events) > 0 {
		win.start = events[0].OccurredAt
		win.endID = events[len(events)-1].EventID
	}
	return win, nil
}

// Gist summarizes the window into one memory_events row. The visibility
// comes from the participant rule, the origin is dialogue (origin=manual is
// forbidden for pipeline writes, D5), and the provenance birth tuple is
// complete at insert: event_time = window start, learned_at = now,
// source_event_id = the window's end event id.
func (g *Gister) Gist(ctx context.Context, job IngestJob, win gistWindow) (domain.MemoryEvent, error) {
	m, err := g.resolver(ctx, job.WorkspaceID, job.AgentID)
	if err != nil {
		return domain.MemoryEvent{}, fmt.Errorf("memory gister: resolve model: %w", err)
	}
	ctx = sideCallContext(ctx, g.trace, "memory.gister")

	raw, err := generateText(ctx, m, gistSystemPrompt, gistUserPrompt(renderMaterial(win.events)))
	if err != nil {
		return domain.MemoryEvent{}, fmt.Errorf("memory gister: model call: %w", err)
	}
	description, outcome, proposedEntities, err := parseGist(raw)
	if err != nil {
		return domain.MemoryEvent{}, err
	}

	visibility, owner := participantVisibility(job)
	now := time.Now().UTC()
	start := win.start
	if start.IsZero() {
		start = now
	}
	event := domain.MemoryEvent{
		WorkspaceID:   job.WorkspaceID,
		AgentID:       job.AgentID,
		SessionID:     job.SessionID,
		TurnID:        job.TurnID,
		Visibility:    visibility,
		UserID:        owner,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     start,
		LearnedAt:     now,
		SourceEventID: win.endID,
		Description:   description,
		Outcome:       outcome,
		Participants:  gistParticipants(job),
	}
	if err := g.events.InsertEvent(ctx, &event); err != nil {
		return domain.MemoryEvent{}, fmt.Errorf("memory gister: insert gist: %w", err)
	}

	// Entity links ride the same call that committed the gist (wave3 D6,
	// zero extra side-calls). The edge inherits the gist's tier — the
	// narrowest endpoint (D7). Fail-soft: a rejected proposal or edge never
	// uncommits the gist.
	linkEntities(ctx, g.entities, g.log, job.WorkspaceID, event.SourceEventID, proposedEntities, domain.MemoryTargetEvent, event.ID, event.Visibility, now)
	return event, nil
}

// gistParticipants names the participants the job's shape proves: the
// producing agent always, and the human when one is present. The
// participant rule itself reads the job's human count, not this list.
func gistParticipants(job IngestJob) []domain.MemoryParticipant {
	participants := make([]domain.MemoryParticipant, 0, 2)
	if job.HumanParticipants >= 1 && job.UserID != "" {
		participants = append(participants, domain.MemoryParticipant{Kind: "user", ID: job.UserID})
	}
	participants = append(participants, domain.MemoryParticipant{Kind: "agent", ID: job.AgentID})
	return participants
}

// parseGist extracts the summary pair and the optional entity mentions from
// the model response; fences and surrounding prose are tolerated, an empty
// description is a failure. Malformed entity proposals skip individually
// downstream (D6) — parsing carries them through as proposals.
func parseGist(raw string) (description, outcome string, entities []entityProposal, err error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", "", nil, errors.New("memory gister: no JSON object in response")
	}
	var gist struct {
		Description string           `json:"description"`
		Outcome     string           `json:"outcome"`
		Entities    []entityProposal `json:"entities"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &gist); err != nil {
		return "", "", nil, fmt.Errorf("memory gister: decode gist: %w", err)
	}
	if strings.TrimSpace(gist.Description) == "" {
		return "", "", nil, errors.New("memory gister: empty gist description")
	}
	return strings.TrimSpace(gist.Description), strings.TrimSpace(gist.Outcome), gist.Entities, nil
}

const gistSystemPrompt = `Summarize the given slice of an AI agent workspace session into one episodic gist.

Emit ONLY a JSON object — no prose:
{"description":"what happened, one or two sentences","outcome":"the result or current state, one sentence","entities":[{"label":"Acme","normalized_label":"acme"}]}

Cover every distinct thing that happened in the slice; keep each fact concrete and self-contained. The entities array names the people, projects, systems, and vendors the slice mentions: "label" exactly as the material spells it, "normalized_label" its lowercase trimmed singular form. Omit the array when none appear; skip anything you are unsure is an entity.`

// gistUserPrompt renders the gister call's user turn: the raw window.
func gistUserPrompt(material string) string {
	var sb strings.Builder
	sb.WriteString("## Session slice\n")
	sb.WriteString(material)
	return sb.String()
}

// renderMaterial flattens raw session events into prompt material: one
// speaker-labeled line per message event, in log order. Unparseable or
// text-less rows (control and extension events) contribute nothing — the
// raw log is read-only evidence either way.
func renderMaterial(events []domain.SessionEvent) string {
	var sb strings.Builder
	for _, row := range events {
		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := materialSerializer.Unmarshal(row.Payload, &se); err != nil || se.Message == nil {
			continue
		}
		text := agenticText(se.Message)
		if strings.TrimSpace(text) == "" {
			continue
		}
		speaker := "assistant"
		if strings.EqualFold(strings.TrimSpace(string(se.Message.Role)), string(schema.AgenticRoleTypeUser)) {
			speaker = "user"
		}
		sb.WriteString(speaker)
		sb.WriteString(": ")
		sb.WriteString(text)
		sb.WriteByte('\n')
		if sb.Len() >= maxMaterialChars {
			break
		}
	}
	return truncateRunes(sb.String(), maxMaterialChars)
}

// agenticText extracts the renderable text from one agentic message:
// generated and user-input blocks only — reasoning and tool plumbing are
// never part of the material.
func agenticText(msg *schema.AgenticMessage) string {
	var sb []byte
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.AssistantGenText != nil {
			sb = append(sb, block.AssistantGenText.Text...)
		}
		if block.UserInputText != nil {
			sb = append(sb, block.UserInputText.Text...)
		}
	}
	return string(sb)
}

// truncateRunes caps s at max runes without splitting a multi-byte rune.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
