package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Model is the Eino agentic chat-model interface the side-calls generate
// through. It mirrors internal/agents.Model because the runner imports this
// package for the ingestion seam and the reverse import would cycle; the
// composition root satisfies ModelFactory with the same factory the runner
// resolves conversation models through.
type Model = model.BaseModel[*schema.AgenticMessage]

// ModelFactory builds a side-call model from a workspace provider credential
// (the agents.DefaultAgenticModelFactory signature).
type ModelFactory func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (Model, error)

// ModelResolver resolves one side-call model for a workspace per call —
// credentials stay tenant-scoped and re-resolve, never cached.
type ModelResolver func(ctx context.Context, workspaceID, agentID string) (Model, error)

// sideCallConfig is the cheap-model seam shared by the gate and the gister
// (D10): provider-backed resolution by default, fully overridable for tests,
// with the Langfuse callback attached to every side-call when wired.
type sideCallConfig struct {
	providerType string
	modelName    string
	resolver     ModelResolver
	trace        callbacks.Handler
	agents       store.AgentStore
	settings     store.ToolSettingsStore
}

// SideCallOption configures the cheap-model side-call seam.
type SideCallOption func(*sideCallConfig)

// WithSideCallModel names the workspace provider type and model the
// side-calls resolve — the wiring passes the hooks evaluator's configured
// cheap tier. Unset, resolution fails per call and the stage fails soft.
func WithSideCallModel(providerType, modelName string) SideCallOption {
	return func(c *sideCallConfig) {
		c.providerType = providerType
		c.modelName = modelName
	}
}

// WithAgentModelSource enables agent-backed side-call resolution: with no
// static WithSideCallModel configured, per-turn stages resolve the model the
// run's own agent executes (workspace-level callers fall back to the
// workspace's first configured agent). Nil (unwired, the trace-handler
// precedent) leaves resolution failing soft per call.
func WithAgentModelSource(agents store.AgentStore) SideCallOption {
	return func(c *sideCallConfig) {
		c.agents = agents
	}
}

// WithWorkspaceModelSource enables the workspace-level side-call model
// choice from the memory settings record (sidecall_provider_id +
// sidecall_model): it sits between the agent's own override and the agent
// default model in the resolution order. Nil (unwired, the trace-handler
// precedent) skips that tier.
func WithWorkspaceModelSource(settings store.ToolSettingsStore) SideCallOption {
	return func(c *sideCallConfig) {
		c.settings = settings
	}
}

// WithModelResolver replaces provider-backed resolution wholesale — the
// test seam (a failing resolver exercises the fail-soft path).
func WithModelResolver(r ModelResolver) SideCallOption {
	return func(c *sideCallConfig) {
		c.resolver = r
	}
}

// WithTraceCallback attaches the Langfuse callback handler to every
// side-call. The wiring applies it only when a trace handler exists (the
// cli's `if traceHandler != nil` precedent); inside the package it is
// treated as always-set.
func WithTraceCallback(h callbacks.Handler) SideCallOption {
	return func(c *sideCallConfig) {
		c.trace = h
	}
}

// newSideCallResolver returns the configured override or the provider-backed
// resolution used in production.
func newSideCallResolver(providerStore store.ProviderStore, encryptionKey []byte, factory ModelFactory, cfg sideCallConfig) ModelResolver {
	if cfg.resolver != nil {
		return cfg.resolver
	}
	return func(ctx context.Context, workspaceID, agentID string) (Model, error) {
		return resolveSideCallModel(ctx, providerStore, cfg.agents, cfg.settings, encryptionKey, factory, workspaceID, agentID, cfg.providerType, cfg.modelName)
	}
}

// resolveSideCallModel mirrors the hooks evaluator's model resolution: the
// workspace's configured provider of the named type, its credential
// decrypted with the workspace id as AAD, and the shared agentic model
// factory. With no static model configured, resolution walks the override
// chain most-specific-first: the agent's own memory side-call model, the
// workspace memory settings' side_call_model, then the model the named
// agent executes (or, for workspace-level callers, the first configured
// agent's) — the one model the workspace has already proven it can run.
// No silent default provider exists — an unresolvable workspace fails the
// side-call and the stage fails soft.
func resolveSideCallModel(ctx context.Context, providerStore store.ProviderStore, agents store.AgentStore, settings store.ToolSettingsStore, encryptionKey []byte, factory ModelFactory, workspaceID, agentID, providerType, modelName string) (Model, error) {
	if providerType == "" && agents != nil {
		agent, err := resolveSideCallAgent(ctx, agents, workspaceID, agentID)
		if err != nil {
			return nil, err
		}
		// 1. The agent defined its own memory model.
		if agent.MemorySidecallProviderID != "" && agent.MemorySidecallModel != "" {
			provider, err := providerStore.ByID(ctx, workspaceID, agent.MemorySidecallProviderID)
			if err != nil {
				return nil, fmt.Errorf("memory side-call: agent override provider: %w", err)
			}
			return buildSideCallModel(ctx, providerStore, encryptionKey, factory, provider, agent.MemorySidecallModel, workspaceID)
		}
		// 2. The workspace memory settings pinned a side-call model.
		if settings != nil {
			if providerID, model, ok := workspaceSideCallModel(ctx, settings, workspaceID); ok {
				provider, err := providerStore.ByID(ctx, workspaceID, providerID)
				if err != nil {
					return nil, fmt.Errorf("memory side-call: workspace override provider: %w", err)
				}
				return buildSideCallModel(ctx, providerStore, encryptionKey, factory, provider, model, workspaceID)
			}
		}
		// 3. Agent default: the model the agent itself runs.
		provider, err := providerStore.ByID(ctx, workspaceID, agent.ProviderID)
		if err != nil {
			return nil, fmt.Errorf("memory side-call: agent provider: %w", err)
		}
		return buildSideCallModel(ctx, providerStore, encryptionKey, factory, provider, agent.Model, workspaceID)
	}
	list, err := providerStore.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("memory side-call: list providers: %w", err)
	}
	var provider *domain.ProviderConfig
	for i := range list {
		if list[i].Type == providerType {
			provider = &list[i]
			break
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("memory side-call: workspace has no %q provider", providerType)
	}
	return buildSideCallModel(ctx, providerStore, encryptionKey, factory, provider, modelName, workspaceID)
}

// workspaceSideCallModel reads the memory settings record's side-call
// choice (flat keys on the structured record, mirroring the handler's
// storage); ok=false when unset or half-set — the handler never stores half,
// but a hand-edited row degrades to the next tier instead of failing.
func workspaceSideCallModel(ctx context.Context, settings store.ToolSettingsStore, workspaceID string) (providerID, model string, ok bool) {
	row, err := settings.Get(ctx, workspaceID, "memory")
	if err != nil || row == nil || row.Config == nil {
		return "", "", false
	}
	providerID, _ = row.Config["sidecall_provider_id"].(string)
	model, _ = row.Config["sidecall_model"].(string)
	if providerID == "" || model == "" {
		return "", "", false
	}
	return providerID, model, true
}

// resolveSideCallAgent picks the agent whose model backs workspace side-calls:
// the requested agent when named, else the workspace's first configured agent.
func resolveSideCallAgent(ctx context.Context, agents store.AgentStore, workspaceID, agentID string) (*domain.Agent, error) {
	if agentID != "" {
		agent, err := agents.ByID(ctx, workspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("memory side-call: agent: %w", err)
		}
		return agent, nil
	}
	list, err := agents.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("memory side-call: list agents: %w", err)
	}
	for i := range list {
		if list[i].ProviderID != "" && list[i].Model != "" {
			return &list[i], nil
		}
	}
	return nil, errors.New("memory side-call: workspace has no configured agent to source the side-call model from")
}

func buildSideCallModel(ctx context.Context, _ store.ProviderStore, encryptionKey []byte, factory ModelFactory, provider *domain.ProviderConfig, modelName, workspaceID string) (Model, error) {
	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(encryptionKey, []byte(workspaceID), provider.KeyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("memory side-call: decrypt provider credentials")
		}
		apiKey = string(plaintext)
	}
	m, err := factory(ctx, provider.Type, providers.Credential{
		Type:    provider.Type,
		BaseURL: provider.BaseURL,
		APIKey:  apiKey,
	}, modelName)
	if err != nil {
		return nil, fmt.Errorf("memory side-call: build model: %w", err)
	}
	return m, nil
}

// sideCallContext presents the side-call's RunInfo to the callback chain so
// the attached Langfuse handler renders the generation under the memory
// pipeline's identity; InitCallbacks is the sanctioned standalone-component
// attach (the model's own EnsureRunInfo finds the manager and keeps it).
func sideCallContext(ctx context.Context, trace callbacks.Handler, name string) context.Context {
	if trace == nil {
		return ctx
	}
	return callbacks.InitCallbacks(ctx, &callbacks.RunInfo{
		Name:      name,
		Type:      "onclaw.memory.side_call",
		Component: components.ComponentOfAgenticModel,
	}, trace)
}

// generateText performs one side-call: system+user in, the response's
// generated text out. Side-calls run with no tools — free text is the only
// channel; reasoning and tool plumbing are never read.
func generateText(ctx context.Context, m Model, system, user string) (string, error) {
	out, err := m.Generate(ctx, []*schema.AgenticMessage{
		schema.SystemAgenticMessage(system),
		schema.UserAgenticMessage(user),
	})
	if err != nil {
		return "", err
	}
	if out == nil {
		return "", errors.New("memory side-call: empty model response")
	}
	var sb []byte
	for _, block := range out.ContentBlocks {
		if block != nil && block.AssistantGenText != nil {
			sb = append(sb, block.AssistantGenText.Text...)
		}
	}
	return string(sb), nil
}

// Gate tunables, defaulted conservatively (design open questions: dedupe
// similarity tuning and prompt sizing are wave-0-adjustable).
const (
	dedupeSimilarity = 0.8
	topSimilarNotes  = 8
	highImportance   = 8
	maxImportance    = 10
	maxDocDigest     = 2000
	// docConflictFlag is the ConflictFlag value stamped on notes the gate
	// flags against the kept documents (D7). The pipeline never edits the
	// documents — the flag is the review surface.
	docConflictFlag = "doc-conflict"
)

// Gate is the curation gate (tasks 3.3/3.4): one cheap-model call per turn
// that proposes ADD/UPDATE/SUPERSEDE/NOOP ops, applied through the notes
// store with dedupe-before-write and the session's visibility ceiling
// clamping every proposal. Each op may carry entity mentions (wave3 D6),
// resolved and linked to the committed notes at zero extra model cost. Any
// failure fails soft: the error returns, the raw session events stay
// untouched.
type Gate struct {
	notes    store.MemoryNoteStore
	entities store.MemoryEntityStore
	docs     store.MemoryStore
	resolver ModelResolver
	trace    callbacks.Handler
	log      *slog.Logger
	// posture resolves the workspace's visibility posture (D4's policy
	// switch); nil behaves as Narrow. Wired through the worker's
	// WithPostureFunc option — the gate reads it per curation call.
	posture PostureFunc
}

// NewGate constructs the gate from its granular dependencies: the curated
// notes store, the associative entity store (wave3 D6/D7 note links), the
// kept documents (conflict context only — never written), the workspace
// provider catalog, the instance encryption key, and the shared agentic
// model factory.
func NewGate(notes store.MemoryNoteStore, entities store.MemoryEntityStore, docs store.MemoryStore, providerStore store.ProviderStore, encryptionKey []byte, factory ModelFactory, log *slog.Logger, opts ...SideCallOption) *Gate {
	var cfg sideCallConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gate{
		notes:    notes,
		entities: entities,
		docs:     docs,
		resolver: newSideCallResolver(providerStore, encryptionKey, factory, cfg),
		trace:    cfg.trace,
		log:      log,
	}
}

// gateOp is one curation-gate operation — the strict JSON the side-call
// emits per candidate fact. The optional entities array (wave3 D6) names the
// people, projects, systems, and vendors the fact mentions; each becomes an
// entity→note edge on commit.
type gateOp struct {
	Op              string           `json:"op"`
	Content         string           `json:"content"`
	Visibility      string           `json:"visibility"`
	Importance      int              `json:"importance"`
	Pin             bool             `json:"pin"`
	ExplicitRequest bool             `json:"explicit_request"`
	Supersedes      *string          `json:"supersedes"`
	Topic           *string          `json:"topic"`
	ConflictWithDoc bool             `json:"conflict_with_doc"`
	Entities        []entityProposal `json:"entities"`
}

// GateResult is the committed-op summary the chip counts. Notes carries the
// committed note rows so the worker's row-embedding stage can index content
// without re-reading the store.
type GateResult struct {
	NoteIDs []string
	Notes   []domain.MemoryNote
	Counts  MemoryIngestedCounts
}

// Curate runs the one curation call for the turn and applies the committed
// ops. Any error — model down, unparseable JSON, store failure — fails soft:
// the caller logs and counts it, and the raw session events stay untouched.
// A turn with no usable material is a quiet NOOP, not a failure.
func (g *Gate) Curate(ctx context.Context, job IngestJob, material []domain.SessionEvent, windowStart time.Time, endEventID string) (GateResult, error) {
	turn := renderMaterial(material)
	if strings.TrimSpace(turn) == "" {
		g.log.Debug("memory: curation gate skipped; turn carried no material",
			"workspace_id", job.WorkspaceID, "agent_id", job.AgentID,
			"session_id", job.SessionID, "turn_id", job.TurnID)
		return GateResult{NoteIDs: []string{}}, nil
	}
	start := time.Now()

	m, err := g.resolver(ctx, job.WorkspaceID, job.AgentID)
	if err != nil {
		return GateResult{}, fmt.Errorf("memory gate: resolve model: %w", err)
	}
	ctx = sideCallContext(ctx, g.trace, "memory.curation_gate")

	similar, err := g.notes.SearchNotes(ctx, job.WorkspaceID, job.UserID, job.AgentID, turn, store.MemoryNoteFilters{Limit: topSimilarNotes})
	if err != nil {
		return GateResult{}, fmt.Errorf("memory gate: list similar notes: %w", err)
	}

	raw, err := generateText(ctx, m, gateSystemPrompt, gateUserPrompt(job, turn, similar, g.docDigest(ctx, job)))
	if err != nil {
		return GateResult{}, fmt.Errorf("memory gate: model call: %w", err)
	}
	ops, err := parseGateOps(raw)
	if err != nil {
		return GateResult{}, err
	}

	ceiling := sessionCeiling(job)
	posture := g.postureFor(ctx, job.WorkspaceID)
	now := time.Now().UTC()
	result := GateResult{NoteIDs: []string{}}
	rejected := 0
	for _, op := range ops {
		note, err := g.applyOp(ctx, job, op, ceiling, posture, windowStart, endEventID, now)
		if err != nil {
			// One malformed or rejected op never rejects the batch (D4).
			g.log.Warn("memory gate: op rejected", "op", op.Op, "error", err)
			rejected++
			continue
		}
		if note == nil {
			continue
		}
		result.NoteIDs = append(result.NoteIDs, note.ID)
		result.Notes = append(result.Notes, *note)
		bumpCount(&result.Counts, note.Visibility)

		// Entity links ride the same call that committed the note (wave3
		// D6, zero extra side-calls). The edge inherits the note's tier —
		// the narrowest endpoint (D7). Fail-soft: a rejected proposal or
		// edge never uncommits the note.
		linkEntities(ctx, g.entities, g.log, job.WorkspaceID, endEventID, op.Entities, domain.MemoryTargetNote, note.ID, note.Visibility, now)
	}
	logCurationDecision(g.log, job, ops, len(result.NoteIDs), rejected, time.Since(start))
	return result, nil
}

// logCurationDecision logs one completed curation decision at info — the
// write-side counterpart of the intent gate's per-turn record: what the
// side-call proposed for the turn's facts (ops by kind), how many committed
// or were rejected, and how long the decision took. Curation always runs
// through the LLM side-call — the decision backend cannot serve it (it
// generates ops, which encoders cannot) — so there is no source field here.
func logCurationDecision(log *slog.Logger, job IngestJob, ops []gateOp, committed, rejected int, elapsed time.Duration) {
	counts := map[string]int{}
	for _, op := range ops {
		kind := strings.ToUpper(strings.TrimSpace(op.Op))
		if kind == "" {
			kind = "NOOP"
		}
		counts[kind]++
	}
	log.Info("memory: curation gate decided turn facts",
		"workspace_id", job.WorkspaceID, "agent_id", job.AgentID,
		"session_id", job.SessionID, "turn_id", job.TurnID,
		"ops_proposed", len(ops),
		"ops_add", counts["ADD"], "ops_update", counts["UPDATE"],
		"ops_supersede", counts["SUPERSEDE"], "ops_noop", counts["NOOP"],
		"committed", committed, "rejected", rejected,
		"elapsed_ms", elapsed.Milliseconds())
}

// docDigest renders the kept documents for conflict context. It is
// best-effort context: a failed or absent document simply contributes
// nothing — reading the docs must never gate the write path (D7).
func (g *Gate) docDigest(ctx context.Context, job IngestJob) string {
	digests := make([]string, 0, 2)
	if wsMem, err := g.docs.WorkspaceMemory(ctx, job.WorkspaceID); err == nil && wsMem != nil && strings.TrimSpace(wsMem.Content) != "" {
		digests = append(digests, truncateRunes(wsMem.Content, maxDocDigest))
	}
	if job.UserID != "" {
		if uMem, err := g.docs.UserMemory(ctx, job.WorkspaceID, job.UserID); err == nil && uMem != nil && strings.TrimSpace(uMem.Content) != "" {
			digests = append(digests, truncateRunes(uMem.Content, maxDocDigest))
		}
	}
	return strings.Join(digests, "\n\n")
}

// postureFor resolves the workspace's posture for this curation call; a nil
// resolver or any outcome other than OrgShared behaves as Narrow (the
// fail-safe default — the pipeline never widens on absent settings).
func (g *Gate) postureFor(ctx context.Context, workspaceID string) Posture {
	if g.posture == nil {
		return PostureNarrow
	}
	if p := g.posture(ctx, workspaceID); p == PostureOrgShared {
		return PostureOrgShared
	}
	return PostureNarrow
}

// applyOp validates and commits one gate op, returning the committed note
// (nil for NOOP). Unknown ops, empty content, target-less corrections, and
// store rejections error; the caller skips them individually.
func (g *Gate) applyOp(ctx context.Context, job IngestJob, op gateOp, ceiling domain.MemoryVisibility, posture Posture, windowStart time.Time, endEventID string, now time.Time) (*domain.MemoryNote, error) {
	kind := strings.ToUpper(strings.TrimSpace(op.Op))
	switch kind {
	case "NOOP", "":
		return nil, nil
	case "ADD", "UPDATE", "SUPERSEDE":
	default:
		return nil, fmt.Errorf("unknown op %q", op.Op)
	}

	content := strings.TrimSpace(op.Content)
	if content == "" {
		return nil, errors.New("op content is empty")
	}
	target := ""
	if op.Supersedes != nil {
		target = strings.TrimSpace(*op.Supersedes)
	}
	if (kind == "UPDATE" || kind == "SUPERSEDE") && target == "" {
		return nil, errors.New("update/supersede without a target note id")
	}

	visibility, clamped := clampVisibility(op.Visibility, ceiling, posture)
	if clamped {
		g.log.Warn("memory gate: visibility clamped to session ceiling",
			"proposed", op.Visibility, "ceiling", ceiling)
	}

	importance := op.Importance
	if importance < 0 {
		importance = 0
	}
	if importance > maxImportance {
		importance = maxImportance
	}
	pinned := op.Pin
	if op.ExplicitRequest {
		// Explicit "remember this" is high-importance and pin-eligible (3.4).
		importance = max(importance, highImportance)
		pinned = true
	}

	note := &domain.MemoryNote{
		WorkspaceID: job.WorkspaceID,
		Visibility:  visibility,
		// Pipeline writes are dialogue-provenanced; origin=manual is
		// forbidden for them (D5).
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     windowStart,
		LearnedAt:     now,
		SourceEventID: endEventID,
		Content:       content,
		Importance:    importance,
		Pinned:        pinned,
		Topic:         trimTopic(op.Topic),
	}
	if op.ConflictWithDoc {
		flag := docConflictFlag
		note.ConflictFlag = &flag
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		note.UserID = &job.UserID
	case domain.MemoryVisibilityAgent:
		note.AgentID = &job.AgentID
	}

	switch kind {
	case "ADD":
		// Dedupe-before-write: an ADD that substantially duplicates an
		// existing note carries no new information — it collapses to NOOP.
		// Corrections must arrive as UPDATE/SUPERSEDE naming their target.
		similar, err := g.notes.CountSimilar(ctx, job.WorkspaceID, job.UserID, job.AgentID, content, dedupeSimilarity)
		if err != nil {
			return nil, err
		}
		if similar > 0 {
			return nil, nil
		}
		if err := g.notes.InsertNote(ctx, note, ceiling); err != nil {
			return nil, err
		}
		return note, nil
	default: // UPDATE / SUPERSEDE
		if err := g.notes.SupersedeNote(ctx, job.WorkspaceID, target, note, ceiling); err != nil {
			return nil, err
		}
		return note, nil
	}
}

// clampVisibility normalizes the proposed tier, clamps proposals above the
// session ceiling to the ceiling, and owns the ambiguous-proposal default
// (D4: asymmetric cost — promotion is cheap, a leak is not). Narrow keeps
// the narrowest tier; the org-shared posture raises only the DEFAULT
// (unknown or absent proposal) to shared — still inside the ceiling, so a
// DM births at most user-visibility facts — and never widens an explicit
// narrower proposal.
func clampVisibility(proposed string, ceiling domain.MemoryVisibility, posture Posture) (domain.MemoryVisibility, bool) {
	visibility := domain.MemoryVisibility(strings.ToLower(strings.TrimSpace(proposed)))
	if !domain.ValidMemoryVisibility(visibility) {
		if posture == PostureOrgShared {
			return ceiling, false
		}
		return domain.MemoryVisibilityAgent, false
	}
	if err := domain.ValidateMemoryVisibilityWithin(ceiling, visibility); err != nil {
		return ceiling, true
	}
	return visibility, false
}

// trimTopic normalizes the optional topic label; empty becomes nil.
func trimTopic(topic *string) *string {
	if topic == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*topic)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// parseGateOps extracts the ops array from the model response: code fences
// and surrounding prose are tolerated, anything unparseable is an error the
// caller fails soft on.
func parseGateOps(raw string) ([]gateOp, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil, errors.New("memory gate: no ops array in response")
	}
	var ops []gateOp
	if err := json.Unmarshal([]byte(raw[start:end+1]), &ops); err != nil {
		return nil, fmt.Errorf("memory gate: decode ops: %w", err)
	}
	return ops, nil
}

const gateSystemPrompt = `You are the memory curation gate for an AI agent workspace. From the turn material, decide which durable facts are worth storing and how.

Emit ONLY a JSON array — no prose — with one object per candidate fact:
[{"op":"ADD","content":"the fact, one self-contained sentence","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false,"entities":[{"label":"Sari","normalized_label":"sari"}]}]

Rules:
- op is ADD (new fact), UPDATE or SUPERSEDE (corrects or materially extends a listed existing note — always name it in "supersedes"), or NOOP ({"op":"NOOP","content":"","visibility":"user",...}).
- NOOP for greetings, small talk, transient state, and anything trivial; emit [] when nothing is worth storing.
- visibility is who the fact is true for: "user" for facts about the individual, "agent" for facts about this agent's own configuration or behavior, "shared" only for facts the whole workspace needs. When ambiguous, choose the narrowest tier. Proposals wider than the stated ceiling are clamped.
- explicit_request is true when the user asked to remember something ("remember this", "note that ..."); such facts get importance >= 8.
- conflict_with_doc is true when the fact contradicts the workspace documents below. Never propose editing the documents.
- entities lists the named people, projects, systems, and vendors the fact mentions: "label" exactly as the material spells it, "normalized_label" its lowercase trimmed singular form. Omit the array when the fact names none; skip anything you are unsure is an entity.
- content must be self-contained: a reader with no transcript must understand it.`

// gateUserPrompt renders the curation call's user turn: the ceiling, the
// turn material, the similar existing notes (dedupe and correction targets),
// and the kept documents digest for conflict flags.
func gateUserPrompt(job IngestJob, turn string, similar []domain.MemoryNote, docs string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Session visibility ceiling: %s. Facts wider than this are clamped.\n\n", sessionCeiling(job))
	if docs != "" {
		sb.WriteString("## Workspace documents (conflict context — never edit)\n")
		sb.WriteString(docs)
		sb.WriteString("\n\n")
	}
	sb.WriteString("## Turn material\n")
	sb.WriteString(turn)
	sb.WriteString("\n")
	if len(similar) > 0 {
		sb.WriteString("## Existing notes (UPDATE/SUPERSEDE targets and dedupe)\n")
		for _, note := range similar {
			fmt.Fprintf(&sb, "- [%s] (%s) %s\n", note.ID, note.Visibility, truncateRunes(note.Content, 300))
		}
	}
	return sb.String()
}
