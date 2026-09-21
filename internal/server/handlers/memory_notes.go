package handlers

// Memory notes/events management surface (integrate-agent-zero-memory
// tasks 5.4, D4/D7/D11/D12/D16): the extracted-facts browser, the episodic
// timeline, the consolidator's morning report, and the workspace memory
// settings record behind the Memory pane.
//
// Guard pattern mirrors the existing memory routes: reads ride membership
// (the workspace context gate), writes — promotion, tombstone delete,
// consolidate-now, settings PUT and the connection test — sit behind
// workspace.write (Owner/Admin), the same gate as the workspace memory PUT.
//
// Every read is scope-filtered by the store's structural visibility
// predicate (D4/D8): viewer user = the HTTP caller, serving agent = empty
// (the UI is a human surface; there is no agent-impersonation parameter, so
// agent-visibility rows simply contribute nothing). The embedding provider
// IS a workspace provider: the settings record pins provider+model+dimension
// and the connection test resolves the endpoint and sealed credential from
// that provider at call time — no embedding-specific secret is stored.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Memory visibility posture literals (D4's per-workspace policy switch).
// The memory package owns the runtime posture type; these are the wire and
// storage spellings the settings record carries.
const (
	postureNarrow    = "narrow"
	postureOrgShared = "org-shared"
)

// MemoryConsolidator is the narrow consumer-side seam the consolidate-now
// endpoint drives (tasks 6.2: the manual button shares one code path with
// the nightly ticker). *memory.Consolidator satisfies it.
type MemoryConsolidator interface {
	RunNow(ctx context.Context, workspaceID string) (memory.MorningReport, error)
}

// memorySettingsToolKey is the ToolSettingsStore row the workspace memory
// settings record rides (D16: absence = enabled defaults, narrow posture,
// no embedding provider).
const memorySettingsToolKey = "memory"

// memoryEmbeddingProbeTimeout bounds one connection-test request (the MCP
// probe default); a hung endpoint must not hold the settings pane open.
const memoryEmbeddingProbeTimeout = 10 * time.Second

// memoryNoteHandlers serves the extracted-memory management endpoints.
type memoryNoteHandlers struct {
	providers store.ProviderStore
	notes     store.MemoryNoteStore
	events    store.MemoryEventStore
	// searcher serves the fused free-text read (wave3 task 3.3): the notes
	// API's q filter rides the same lexical+vector fusion as turn-time
	// prefetch and the search tool — uniform semantics on one path.
	searcher     *memory.Searcher
	reports      store.MemoryReportStore
	settings     store.ToolSettingsStore
	consolidator MemoryConsolidator
	encKey       []byte
	// registry resolves a workspace provider's canonical origin when its
	// record carries no base URL (the canonical types).
	registry *providers.Registry
	// probe verifies the configured embedding endpoint; the seam the tests
	// stub (the storage-config probe pattern). The default performs the
	// real openai-compatible POST and returns the discovered dimension.
	probe func(ctx context.Context, endpoint, apiKey, model string) (int, error)
}

// MemoryNoteHandlerOption configures the memory note handlers; every knob
// has a production default.
type MemoryNoteHandlerOption func(*memoryNoteHandlers)

// WithEmbeddingProbe replaces the connection-test probe (the test seam).
func WithEmbeddingProbe(fn func(ctx context.Context, endpoint, apiKey, model string) (int, error)) MemoryNoteHandlerOption {
	return func(h *memoryNoteHandlers) {
		if fn != nil {
			h.probe = fn
		}
	}
}

// NewMemoryNoteHandlers creates a new memoryNoteHandlers instance with its
// granular dependencies injected: the notes/events/report/settings stores,
// the fused memory searcher (the q filter's read path), the consolidator
// seam, the instance encryption key, and the provider registry
// (canonical-origin lookup for the embedding connection test).
func NewMemoryNoteHandlers(
	notes store.MemoryNoteStore,
	events store.MemoryEventStore,
	searcher *memory.Searcher,
	reports store.MemoryReportStore,
	settings store.ToolSettingsStore,
	providers store.ProviderStore,
	consolidator MemoryConsolidator,
	encKey []byte,
	registry *providers.Registry,
	opts ...MemoryNoteHandlerOption,
) *memoryNoteHandlers {
	h := &memoryNoteHandlers{
		providers:    providers,
		notes:        notes,
		events:       events,
		searcher:     searcher,
		reports:      reports,
		settings:     settings,
		consolidator: consolidator,
		encKey:       encKey,
		registry:     registry,
		probe:        probeEmbeddingEndpoint,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ---------------------------------------------------------------------------
// Wire helpers
// ---------------------------------------------------------------------------

// memoryVisibilityCounters renders the per-tier count map with every tier
// present (an absent key would read as null to the UI, not zero).
func memoryVisibilityCounters(counts map[domain.MemoryVisibility]int) gin.H {
	view := gin.H{"shared": 0, "user": 0, "agent": 0}
	for tier, n := range counts {
		view[string(tier)] = n
	}
	return view
}

// nonNilNotes guarantees the JSON list renders as [] rather than null.
func nonNilNotes(notes []domain.MemoryNote) []domain.MemoryNote {
	if notes == nil {
		return []domain.MemoryNote{}
	}
	return notes
}

// nonNilEvents is the events-list counterpart of nonNilNotes.
func nonNilEvents(events []domain.MemoryEvent) []domain.MemoryEvent {
	if events == nil {
		return []domain.MemoryEvent{}
	}
	return events
}

// emptyMorningReportView is the never-consolidated view: the shape is the
// memory package's wire contract (tasks 6.3), silence names itself —
// generated_at is null (a zero time would read as a real 1 AD stamp),
// conflicts/merges empty, failures zero.
func emptyMorningReportView() gin.H {
	return gin.H{
		"generated_at":        nil,
		"conflicts":           []memory.ConflictFlag{},
		"merges":              []memory.MergeRecord{},
		"extraction_failures": 0,
		"embedding_failures":  0,
		"entity_merges":       0,
	}
}

// ---------------------------------------------------------------------------
// Notes
// ---------------------------------------------------------------------------

// ListNotes returns the caller-visible curated facts with the UI's filters:
// free-text query, visibility tier, topic, event-time window, and the
// history flag that widens the listing past current state to superseded
// rows (D6's what-changed view). Tombstoned notes are hidden by the store
// on every path — the include_tombstoned flag is accepted for API
// compatibility but no store read can honor it.
func (h *memoryNoteHandlers) ListNotes(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	filters := store.MemoryNoteFilters{
		Topic: strings.TrimSpace(c.Query("topic")),
	}
	if v := c.Query("visibility"); v != "" {
		visibility := domain.MemoryVisibility(strings.ToLower(v))
		if !domain.ValidMemoryVisibility(visibility) {
			RespondError(c, fmt.Errorf("%w: unknown visibility %q", domain.ErrInvalid, v))
			return
		}
		filters.Visibility = visibility
	}
	window, ok := parseMemoryTimeWindow(c)
	if !ok {
		return
	}
	filters.TimeWindow = window
	filters.History = c.Query("include_superseded") == "true"
	// include_tombstoned accepted but inert: tombstones are hidden from every
	// read path by store contract (D6 — no flag overrides that).

	// A free-text query routes through the fused search path (wave3 task
	// 3.3): the same lexical+vector fusion the prefetch and the search tool
	// use, over the same filtered visible set — uniform matching semantics
	// on every consumer of the shared path. The listing is unbounded (no
	// limit filter on this API), and only the notes half of the fused result
	// renders here: this endpoint browses the curated store.
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		result, err := h.searcher.Search(c.Request.Context(), memory.Caller{
			WorkspaceID: ws.ID,
			UserID:      user.ID,
		}, memory.Query{
			Text:             q,
			TimeWindow:       filters.TimeWindow,
			VisibilityFilter: filters.Visibility,
			Topic:            filters.Topic,
		})
		if err != nil {
			RespondError(c, err)
			return
		}
		h.respondNotePage(c, ws.ID, user.ID, result.Notes)
		return
	}

	notes, err := h.notes.ListNotesForUI(c.Request.Context(), ws.ID, user.ID, "", filters)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.respondNotePage(c, ws.ID, user.ID, notes)
}

// respondNotePage renders a notes listing with the workspace-wide per-tier
// counts the pane's visibility chips read.
func (h *memoryNoteHandlers) respondNotePage(c *gin.Context, workspaceID, viewerUserID string, notes []domain.MemoryNote) {
	counts, err := h.notes.CountByVisibility(c.Request.Context(), workspaceID)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{
		"notes":  nonNilNotes(notes),
		"counts": memoryVisibilityCounters(counts),
		// Echo the effective viewer scope so clients can render provenance
		// links (the source_event_id's session) without guessing identity.
		"viewer_user_id": viewerUserID,
	})
}

// GetNote returns one note with its full provenance (the domain shape
// carries origin/event_time/learned_at/source_event_id and the promotion
// audit) plus the note's multi-evidence links (D12): every raw session
// event the fact was extracted from.
func (h *memoryNoteHandlers) GetNote(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	note, err := h.notes.GetNote(c.Request.Context(), ws.ID, user.ID, "", c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	if note == nil {
		AbortNotFound(c, "memory note not found")
		return
	}

	evidence, err := h.notes.ListNoteEvidence(c.Request.Context(), ws.ID, note.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if evidence == nil {
		evidence = []domain.MemoryNoteEvidence{}
	}
	RespondOK(c, gin.H{"note": note, "evidence": evidence})
}

// promoteNoteRequest is the optional promotion body: a free-text note
// riding the audited widening (D4 — only humans widen).
type promoteNoteRequest struct {
	Note string `json:"note"`
}

// PromoteNote widens a note to shared under the caller's identity — the
// store records promoted_by/promoted_at (D4's audited promotion).
func (h *memoryNoteHandlers) PromoteNote(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req promoteNoteRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondError(c, domain.ErrInvalid)
			return
		}
	}

	if err := h.notes.PromoteNote(c.Request.Context(), ws.ID, c.Param("id"), user.ID); err != nil {
		RespondError(c, err)
		return
	}

	// Refetch so the response carries the store-stamped promotion record.
	note, err := h.notes.GetNote(c.Request.Context(), ws.ID, user.ID, "", c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"note": note})
}

// DeleteNote tombstones the note (D6): hidden from every read path, the
// audit trail stays. Absent or already-tombstoned notes 404.
func (h *memoryNoteHandlers) DeleteNote(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	if err := h.notes.TombstoneNote(c.Request.Context(), ws.ID, c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// parseMemoryTimeWindow reads the from/until pair (RFC 3339, open ends) —
// a malformed bound is a 400 and ok=false.
func parseMemoryTimeWindow(c *gin.Context) (*store.MemoryTimeWindow, bool) {
	from := c.Query("from")
	until := c.Query("until")
	if from == "" && until == "" {
		return nil, true
	}
	window := &store.MemoryTimeWindow{}
	if from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			RespondError(c, fmt.Errorf("%w: from must be an RFC 3339 timestamp", domain.ErrInvalid))
			return nil, false
		}
		window.From = t
	}
	if until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			RespondError(c, fmt.Errorf("%w: until must be an RFC 3339 timestamp", domain.ErrInvalid))
			return nil, false
		}
		window.To = t
	}
	return window, true
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// ListEvents returns the caller-visible episodic timeline (D3 gists) with
// the session/visibility/time filters the browsing UI offers.
func (h *memoryNoteHandlers) ListEvents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	filters := store.MemoryEventFilters{
		SessionID: c.Query("session_id"),
	}
	if v := c.Query("visibility"); v != "" {
		visibility := domain.MemoryVisibility(strings.ToLower(v))
		if !domain.ValidMemoryVisibility(visibility) {
			RespondError(c, fmt.Errorf("%w: unknown visibility %q", domain.ErrInvalid, v))
			return
		}
		filters.Visibility = visibility
	}
	window, ok := parseMemoryTimeWindow(c)
	if !ok {
		return
	}
	filters.TimeWindow = window

	events, err := h.events.ListEventsForUI(c.Request.Context(), ws.ID, user.ID, "", filters)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"events": nonNilEvents(events)})
}

// ---------------------------------------------------------------------------
// Consolidation + morning report
// ---------------------------------------------------------------------------

// ConsolidateNow runs one consolidation pass for the workspace (tasks 6.2:
// the same RunNow the nightly ticker drives) and returns the fresh morning
// report.
func (h *memoryNoteHandlers) ConsolidateNow(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	report, err := h.consolidator.RunNow(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"report": report})
}

// GetReport returns the last saved morning report. No report yet is a
// normal state: the empty shape rides along (silence names itself —
// extraction_failures: 0, empty conflicts/merges).
func (h *memoryNoteHandlers) GetReport(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	rep, err := h.reports.Get(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if rep == nil {
		RespondOK(c, gin.H{"report": emptyMorningReportView()})
		return
	}

	// The report body is the consolidator's marshaled MorningReport — the
	// memory package owns the shape, so decode through it. A shape from an
	// older build degrades to the empty report stamped with its save time.
	var report memory.MorningReport
	if err := json.Unmarshal(rep.Report, &report); err != nil {
		RespondOK(c, gin.H{"report": emptyMorningReportView()})
		return
	}
	if report.Conflicts == nil {
		report.Conflicts = []memory.ConflictFlag{}
	}
	if report.Merges == nil {
		report.Merges = []memory.MergeRecord{}
	}
	RespondOK(c, gin.H{"report": report})
}

// ---------------------------------------------------------------------------
// Workspace memory settings (D16)
// ---------------------------------------------------------------------------

// memorySettingsView renders the settings record: posture, the ingestion
// toggle, and the embedding provider with the API key reduced to a
// set/not-set flag (the key is write-only — it never crosses the wire).
func (h *memoryNoteHandlers) memorySettingsView(row *domain.WorkspaceToolSetting) gin.H {
	posture := postureNarrow
	ingestionEnabled := true
	config := map[string]any{}
	if row != nil {
		if row.Config != nil {
			config = row.Config
		}
	}
	if p, ok := config["visibility_posture"].(string); ok && p == postureOrgShared {
		posture = postureOrgShared
	}
	// Ingestion rides the structured record, not the row's Enabled bit —
	// Enabled belongs to the memory TOOL's availability (the Tools pane
	// toggle); absence of the key is the enabled default.
	if v, ok := config["ingestion_enabled"].(bool); ok {
		ingestionEnabled = v
	}
	// Raw-embedding toggle (wave3: the per-workspace switch for raw-turn
	// vectors under storage pressure); absence is the ON default.
	rawEmbeddingEnabled := true
	if v, ok := config["raw_embedding_enabled"].(bool); ok {
		rawEmbeddingEnabled = v
	}

	var embedding gin.H
	providerID, _ := config["embedding_provider_id"].(string)
	model, _ := config["embedding_model"].(string)
	// The dimension is stored as a JSON number: float64 after a decode,
	// fixed-width int from in-process writers — coerce both.
	dimension := 0
	switch d := config["embedding_dimension"].(type) {
	case float64:
		dimension = int(d)
	case int:
		dimension = d
	case int64:
		dimension = int(d)
	}
	// Endpoint and credentials are no longer part of the embedding record —
	// the embedding provider IS a workspace provider and the probe resolves
	// base URL + key from it at call time.
	if providerID != "" || model != "" {
		embedding = gin.H{
			"provider_id": providerID,
			"model":       model,
			"dimension":   dimension,
		}
	}

	var sideCallModel gin.H
	scProvider, _ := config["sidecall_provider_id"].(string)
	scModel, _ := config["sidecall_model"].(string)
	if scProvider != "" && scModel != "" {
		sideCallModel = gin.H{"provider_id": scProvider, "model": scModel}
	}

	// The intent gate's classification budget always resolves (D1/D2): absent
	// or out-of-contract stored values degrade to the default, the same
	// resolution the runner's gate calls see.
	gateBudgetMS := memory.DefaultGateBudgetMS
	if budget, ok := memory.GateBudgetFromConfig(config); ok {
		gateBudgetMS = int(budget / time.Millisecond)
	}

	return gin.H{
		"visibility_posture":    posture,
		"ingestion_enabled":     ingestionEnabled,
		"raw_embedding_enabled": rawEmbeddingEnabled,
		"side_call_model":       sideCallModel,
		"embedding":             embedding,
		"gate_budget_ms":        gateBudgetMS,
	}
}

// GetSettings returns the workspace memory settings view; absence is the
// defaults (narrow posture, ingestion on, no embedding provider).
func (h *memoryNoteHandlers) GetSettings(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	row, err := h.storedMemorySettings(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"settings": h.memorySettingsView(row)})
}

// storedMemorySettings fetches the memory settings row, collapsing absence
// to nil (the storage-config storedConfig idiom).
func (h *memoryNoteHandlers) storedMemorySettings(ctx context.Context, workspaceID string) (*domain.WorkspaceToolSetting, error) {
	row, err := h.settings.Get(ctx, workspaceID, memorySettingsToolKey)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

// memorySettingsPut is the settings PUT body: every field optional, an
// absent embedding object leaves the stored provider untouched, and an
// empty api_key means "keep the stored credential" (secrets are write-only).
type memorySettingsPut struct {
	VisibilityPosture *string `json:"visibility_posture"`
	IngestionEnabled  *bool   `json:"ingestion_enabled"`
	// RawEmbeddingEnabled toggles the raw-turn vector channel (wave3);
	// absent keeps the stored choice, and absence-in-storage is ON.
	RawEmbeddingEnabled *bool `json:"raw_embedding_enabled"`
	// SideCallModel rides RawMessage because Go decodes JSON null and an
	// absent key into the same nil pointer, and the three cases differ here:
	// absent = keep the stored choice; null = clear to agent default; an
	// object pins a specific provider+model for the pipeline's cheap calls.
	SideCallModel json.RawMessage     `json:"side_call_model"`
	Embedding     *memoryEmbeddingPut `json:"embedding"`
	// Absent keeps the stored gate budget (the settings record's
	// absence-is-defaults); when present it must sit inside the save-time
	// bounds the memory package owns.
	GateBudgetMs *int `json:"gate_budget_ms"`
}

// memorySideCallModelPut is the workspace-level side-call model choice.
type memorySideCallModelPut struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

// decodeSideCallModelPut distinguishes the side_call_model cases: nil
// (absent) keeps the stored choice, JSON null clears to agent default, and
// an object carries the pinned provider+model.
func decodeSideCallModelPut(raw json.RawMessage) (*memorySideCallModelPut, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed == "null" {
		return nil, nil
	}
	var put memorySideCallModelPut
	if err := json.Unmarshal(raw, &put); err != nil {
		return nil, fmt.Errorf("%w: side_call_model: %v", domain.ErrInvalid, err)
	}
	return &put, nil
}

// memoryEmbeddingPut is the embedding provider's partial update shape. The
// embedding provider IS a workspace provider: the base URL and credentials
// live on the provider record and are resolved at probe time — the settings
// record pins only which provider, which model, and the known dimension.
type memoryEmbeddingPut struct {
	ProviderID *string `json:"provider_id"`
	Model      *string `json:"model"`
	Dimension  *int    `json:"dimension"`
}

// PutSettings validates and persists the workspace memory settings record.
// The API key is sealed on write with the workspace id as AAD (the
// workspace provider-credential mechanism) and only a non-empty supply
// replaces it.
func (h *memoryNoteHandlers) PutSettings(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req memorySettingsPut
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, fmt.Errorf("%w: request body: %v", domain.ErrInvalid, err))
		return
	}

	if req.VisibilityPosture != nil {
		switch posture := strings.ToLower(strings.TrimSpace(*req.VisibilityPosture)); posture {
		case postureNarrow, postureOrgShared:
		default:
			RespondError(c, fmt.Errorf("%w: visibility_posture must be %q or %q", domain.ErrInvalid, postureNarrow, postureOrgShared))
			return
		}
	}
	if req.Embedding != nil && req.Embedding.Dimension != nil && *req.Embedding.Dimension <= 0 {
		RespondError(c, fmt.Errorf("%w: embedding dimension must be a positive integer", domain.ErrInvalid))
		return
	}
	if req.GateBudgetMs != nil && (*req.GateBudgetMs < memory.MinGateBudgetMS || *req.GateBudgetMs > memory.MaxGateBudgetMS) {
		RespondError(c, fmt.Errorf("%w: gate_budget_ms must be between %d and %d milliseconds", domain.ErrUnprocessable, memory.MinGateBudgetMS, memory.MaxGateBudgetMS))
		return
	}

	stored, err := h.storedMemorySettings(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	// Enabled stays whatever the row already carries (the memory TOOL's
	// availability, owned by the Tools pane); a fresh row keeps the tool
	// enabled. Ingestion is a structured-record field below — the two
	// toggles never overload each other.
	enabled := true
	config := map[string]any{}
	if stored != nil {
		enabled = stored.Enabled
		if stored.Config != nil {
			config = stored.Config
		}
	}
	if req.IngestionEnabled != nil {
		config["ingestion_enabled"] = *req.IngestionEnabled
	}
	if req.RawEmbeddingEnabled != nil {
		config["raw_embedding_enabled"] = *req.RawEmbeddingEnabled
	}
	if req.VisibilityPosture != nil {
		config["visibility_posture"] = strings.ToLower(strings.TrimSpace(*req.VisibilityPosture))
	}
	if req.GateBudgetMs != nil {
		config["gate_budget_ms"] = *req.GateBudgetMs
	}
	if sideCallModelPut, err := decodeSideCallModelPut(req.SideCallModel); err != nil {
		RespondError(c, err)
		return
	} else if sideCallModelPut != nil {
		// An object pins a specific provider+model (both required, provider
		// must exist in this workspace); JSON null already cleared above.
		providerID := strings.TrimSpace(sideCallModelPut.ProviderID)
		model := strings.TrimSpace(sideCallModelPut.Model)
		if providerID == "" || model == "" {
			RespondError(c, fmt.Errorf("%w: side_call_model needs both provider_id and model, or null for agent default", domain.ErrInvalid))
			return
		}
		if _, err := h.providers.ByID(c.Request.Context(), ws.ID, providerID); err != nil {
			RespondError(c, fmt.Errorf("%w: side_call_model provider not found in workspace", domain.ErrInvalid))
			return
		}
		config["sidecall_provider_id"] = providerID
		config["sidecall_model"] = model
	} else if len(req.SideCallModel) > 0 {
		// Explicit JSON null: clear back to agent default.
		delete(config, "sidecall_provider_id")
		delete(config, "sidecall_model")
	}
	if req.Embedding != nil {
		// The embedding provider is a workspace provider — validate it like
		// side_call_model does; an empty value clears the pin. Legacy
		// embedding_endpoint/embedding_api_key keys are dropped on every
		// save: the provider record owns the endpoint and credential now.
		delete(config, "embedding_endpoint")
		delete(config, "embedding_api_key")
		if req.Embedding.ProviderID != nil {
			providerID := strings.TrimSpace(*req.Embedding.ProviderID)
			if providerID != "" {
				if _, err := h.providers.ByID(c.Request.Context(), ws.ID, providerID); err != nil {
					RespondError(c, fmt.Errorf("%w: embedding provider not found in workspace", domain.ErrInvalid))
					return
				}
			}
			config["embedding_provider_id"] = providerID
		}
		if req.Embedding.Model != nil {
			config["embedding_model"] = strings.TrimSpace(*req.Embedding.Model)
		}
		if req.Embedding.Dimension != nil {
			config["embedding_dimension"] = *req.Embedding.Dimension
		}
	}

	setting := &domain.WorkspaceToolSetting{
		WorkspaceID: ws.ID,
		ToolKey:     memorySettingsToolKey,
		Enabled:     enabled,
		Config:      config,
	}
	if err := h.settings.Upsert(c.Request.Context(), setting); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"settings": h.memorySettingsView(setting)})
}

// memorySettingsTestRequest is the connection-test body (D16): the provider
// and model the form currently holds; the endpoint and credential resolve
// from that workspace provider, so the pane can validate without ever
// touching a secret.
type memorySettingsTestRequest struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	Dimension  int    `json:"dimension"`
}

// TestMemorySettings verifies the configured embedding provider by embedding
// one probe string through it: {ok, dimension} on success — the discovered
// length, which must match the configured dimension when one is set — and a
// structured error naming the failure otherwise. No credential is echoed.
func (h *memoryNoteHandlers) TestMemorySettings(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req memorySettingsTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, fmt.Errorf("%w: request body: %v", domain.ErrInvalid, err))
		return
	}

	providerID := strings.TrimSpace(req.ProviderID)
	model := strings.TrimSpace(req.Model)
	var missing []string
	if providerID == "" {
		missing = append(missing, "provider_id")
	}
	if model == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		RespondError(c, fmt.Errorf("%w: missing required embedding fields: %s", domain.ErrInvalid, strings.Join(missing, ", ")))
		return
	}

	provider, err := h.providers.ByID(c.Request.Context(), ws.ID, providerID)
	if err != nil {
		RespondError(c, fmt.Errorf("%w: embedding provider not found in workspace", domain.ErrInvalid))
		return
	}
	// The provider record owns the credentials: decrypt its sealed key the
	// same way the side-call resolver does. A keyless provider probes
	// keyless — the endpoint's answer names the auth failure either way.
	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(h.encKey, []byte(ws.ID), provider.KeyCiphertext)
		if err != nil {
			RespondError(c, fmt.Errorf("failed to unseal embedding provider credentials"))
			return
		}
		apiKey = string(plaintext)
	}
	endpoint := provider.BaseURL
	if endpoint == "" {
		impl, err := h.registry.Get(provider.Type)
		if err != nil {
			RespondError(c, fmt.Errorf("%w: %v", domain.ErrInvalid, err))
			return
		}
		endpoint = impl.CanonicalOrigin()
	}

	dimension, err := h.probe(c.Request.Context(), endpoint, apiKey, model)
	if err != nil {
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, err.Error())
		return
	}
	if req.Dimension > 0 && req.Dimension != dimension {
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest,
			fmt.Sprintf("embedding dimension mismatch: endpoint returned %d, configured %d", dimension, req.Dimension))
		return
	}
	RespondOK(c, gin.H{"ok": true, "dimension": dimension})
}

// probeEmbeddingEndpoint posts one openai-compatible embeddings request and
// returns the discovered vector length. Failures carry the endpoint's own
// reason — never the request's credentials.
func probeEmbeddingEndpoint(ctx context.Context, endpoint, apiKey, model string) (int, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return 0, fmt.Errorf("embedding endpoint %q is not a valid http(s) URL", endpoint)
	}

	base := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(base, "/embeddings") {
		// The operator may already point at the full embeddings route.
		base = strings.TrimSuffix(base, "/embeddings")
	}

	body := fmt.Sprintf(`{"model":%q,"input":["onclaw memory probe"]}`, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embeddings", strings.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("embedding probe request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: memoryEmbeddingProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("embedding endpoint unreachable: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("embedding endpoint read failed: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("embedding endpoint returned HTTP %d: %s", resp.StatusCode, truncateProbeReason(string(raw)))
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("embedding endpoint response is not openai-compatible JSON")
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return 0, fmt.Errorf("embedding endpoint returned no embedding vector")
	}
	return len(out.Data[0].Embedding), nil
}

// truncateProbeReason bounds an endpoint error body to a client-safe
// one-liner (the storage probe's verbatim-reason convention, size-capped).
func truncateProbeReason(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// MemoryPostureFromToolSetting resolves the workspace's memory visibility
// posture (D4's policy switch) from a stored settings row: absent row,
// absent key, or an unknown spelling means narrow — the fail-safe default.
// Returns the memory package's runtime posture the worker option consumes.
func MemoryPostureFromToolSetting(row *domain.WorkspaceToolSetting) memory.Posture {
	if row == nil {
		return memory.PostureNarrow
	}
	if p, ok := row.Config["visibility_posture"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case postureOrgShared:
			return memory.PostureOrgShared
		case postureNarrow:
			return memory.PostureNarrow
		}
	}
	return memory.PostureNarrow
}

// MemoryPostureForWorkspace resolves the posture from the settings store,
// failing safe to narrow on any read error (the pipeline never widens
// because settings are unavailable).
func MemoryPostureForWorkspace(ctx context.Context, settings store.ToolSettingsStore, workspaceID string) memory.Posture {
	row, err := settings.Get(ctx, workspaceID, memorySettingsToolKey)
	if err != nil {
		return memory.PostureNarrow
	}
	return MemoryPostureFromToolSetting(row)
}

// RawEmbeddingEnabledForWorkspace resolves the workspace's raw-embedding
// toggle (wave3) from the settings store. Absent row, absent key, or a read
// error all mean ON — absence is the default, and storage trouble must
// never silently change what gets indexed, only what was explicitly asked.
func RawEmbeddingEnabledForWorkspace(ctx context.Context, settings store.ToolSettingsStore, workspaceID string) bool {
	row, err := settings.Get(ctx, workspaceID, memorySettingsToolKey)
	if err != nil || row == nil || row.Config == nil {
		return true
	}
	if v, ok := row.Config["raw_embedding_enabled"].(bool); ok {
		return v
	}
	return true
}

// derefOrEmpty reads an optional string pointer as a plain string.
func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
