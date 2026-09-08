package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// AgentHistoryReader retrieves session transcript history and resolves
// pending shell approvals.
type AgentHistoryReader interface {
	History(ctx context.Context, req agents.HistoryRequest) (*agents.HistoryResult, error)
	PendingApproval(ctx context.Context, workspaceID, sessionID string) (*agents.ApprovalPayload, error)
	Resume(ctx context.Context, req agents.ExecRequest, approval *agents.ApprovalPayload, approved bool) (*agents.EventStream, error)
}

// AgentRunCanceler cancels the live run for a session. It reports false when
// no run is live for the session.
type AgentRunCanceler interface {
	CancelRun(workspaceID, agentID, sessionID string) bool
}

// AgentRunTapper attaches live subscriber streams to in-flight runs so a
// streaming session-events client can follow a run after replaying committed
// history (design D2, live-run-reattach-and-catchup). The production runner
// (*agents.Runner) implements it alongside AgentHistoryReader; the capability
// is discovered by assertion on the injected runner, so the composition root
// needs no extra wiring and fakes that only read history keep working — for
// them the stream degrades to replay-plus-[DONE], the no-active-run path.
// SubscribeRun's ok return subsumes an IsRunActive pre-check (and atomically,
// at that), so only the two methods the handler uses are on the seam.
type AgentRunTapper interface {
	// SubscribeRun attaches a fresh subscriber stream to the run executing
	// for key. The stream stays live until the run finishes, at which point
	// it is closed so Recv drains to io.EOF. ok is false when no run is live.
	SubscribeRun(key agents.RunKey) (subID uint64, stream *agents.EventStream, ok bool)
	// UnsubscribeRun detaches a subscriber added by SubscribeRun without
	// affecting the run or other subscribers. Unknown keys and sub-IDs are
	// no-ops.
	UnsubscribeRun(key agents.RunKey, subID uint64)
}

// agentHandlers handles workspace agent CRUD, prompt regeneration, and session endpoints.
type agentHandlers struct {
	agents        store.AgentStore
	providers     store.ProviderStore
	sessionEvents store.SessionEventStore
	encryptionKey []byte
	registry      *providers.Registry
	modelCatalog  *services.ModelCatalog
	agentService  *promptgen.Service
	workspaceDir  string
	runner        AgentHistoryReader
	runCanceler   AgentRunCanceler
}

// NewAgentHandlers creates a new agentHandlers instance with injected dependencies.
func NewAgentHandlers(agentStore store.AgentStore, providerStore store.ProviderStore, sessionEvents store.SessionEventStore, encryptionKey []byte, reg *providers.Registry, mc *services.ModelCatalog, as *promptgen.Service, workspaceDir string, runner AgentHistoryReader, runCanceler AgentRunCanceler) *agentHandlers {
	return &agentHandlers{
		agents:        agentStore,
		providers:     providerStore,
		sessionEvents: sessionEvents,
		encryptionKey: encryptionKey,
		registry:      reg,
		modelCatalog:  mc,
		agentService:  as,
		workspaceDir:  workspaceDir,
		runner:        runner,
		runCanceler:   runCanceler,
	}
}

// resolveAgent resolves an agent in the workspace by slug first, falling back to ID.
func (h *agentHandlers) resolveAgent(ctx context.Context, workspaceID, identifier string) (*domain.Agent, error) {
	if workspaceID == "" || identifier == "" {
		return nil, domain.ErrNotFound
	}
	agent, err := h.agents.BySlug(ctx, workspaceID, identifier)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return h.agents.ByID(ctx, workspaceID, identifier)
}

// composePromptDocuments fills an agent's identity/soul/bootstrap projection
// from the agent's workspace files under dir. Best-effort: a missing or
// unreadable file composes as empty — the status column carries the truth
// about readiness. The directory derives from the current root and slugs; the
// row records no path.
func composePromptDocuments(dir string, agent *domain.Agent) {
	agent.Identity, agent.Soul, agent.Bootstrap, _ = promptdocs.ReadPromptDocuments(dir)
}

// agentResponse is the response-only payload: it embeds the domain agent and
// adds computed context fields derived from the same resolution execution
// applies (design D4). Request binding goes through CreateAgentRequest and
// PatchAgentRequest, which lack these fields — they can never be written.
type agentResponse struct {
	domain.Agent
	EffectiveContextWindow     int `json:"effective_context_window"`
	SummarizationTriggerTokens int `json:"summarization_trigger_tokens"`
}

// newAgentResponse derives the agent's context budget: the effective window
// (stored value → catalog → default) and the summarization trigger execution
// arms at the runner's default margin.
func newAgentResponse(a *domain.Agent) agentResponse {
	effective := domain.ResolveContextWindow(a.ContextWindow, nil)
	return agentResponse{
		Agent:                      *a,
		EffectiveContextWindow:     effective,
		SummarizationTriggerTokens: int(float64(effective) * agents.DefaultSummarizationMargin),
	}
}

// ListAgents lists all agents in the current workspace.
func (h *agentHandlers) ListAgents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agentList, err := h.agents.ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	// Compose-on-read is skipped for lists (design D4, summary roster);
	// prompt documents are fetched via the detail endpoint (GetAgent).

	wrapped := make([]agentResponse, 0, len(agentList))
	for i := range agentList {
		wrapped = append(wrapped, newAgentResponse(&agentList[i]))
	}

	RespondOK(c, gin.H{"agents": wrapped})
}

// GetAgent retrieves a single agent by slug or ID.
func (h *agentHandlers) GetAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug), agent)
	RespondOK(c, gin.H{"agent": newAgentResponse(agent)})
}

// CreateAgentRequest holds payload parameters for creating a new agent.
type CreateAgentRequest struct {
	Name          string                `json:"name"`
	Slug          string                `json:"slug"`
	Role          string                `json:"role"`
	Description   string                `json:"description"`
	Brief         string                `json:"brief"`
	ProviderID    string                `json:"provider_id"`
	Model         string                `json:"model"`
	Temperature   *float64              `json:"temperature,omitempty"`
	MaxTokens     *int                  `json:"max_tokens,omitempty"`
	Effort        *string               `json:"effort,omitempty"`
	Autonomy      *domain.AgentAutonomy `json:"autonomy,omitempty"`
	ContextWindow *int                  `json:"context_window,omitempty"`
	Tools         []string              `json:"tools,omitempty"`
	EnabledMCPS   []string              `json:"enabled_mcps,omitempty"`
	Avatar        json.RawMessage       `json:"avatar,omitempty"`
}

// CreateAgent creates a new agent in the current workspace and generates its prompts synchronously.
func (h *agentHandlers) CreateAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	agent, err := buildAgentFromCreateRequest(c.Request.Context(), ws.ID, user.ID, &req, h.providers, h.registry, h.modelCatalog)
	if err != nil {
		RespondError(c, err)
		return
	}

	// The slug must be free before any filesystem or generation work: seeding
	// and generating into a taken directory would clobber a live agent's prompt
	// documents, and the post-insert cleanup would delete its directory.
	if _, err := h.agents.BySlug(c.Request.Context(), ws.ID, agent.Slug); err == nil {
		RespondError(c, fmt.Errorf("%w: agent slug already exists in this workspace", domain.ErrConflict))
		return
	} else if !errors.Is(err, domain.ErrNotFound) {
		RespondError(c, err)
		return
	}

	// Assign the agent's on-disk workspace directory and seed it with the base
	// prompt before any DB write, so a failure rejects the request cleanly.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug)
	if err := promptdocs.SeedWorkspace(agentDir); err != nil {
		RespondError(c, fmt.Errorf("failed to create agent workspace directory: %w", err))
		return
	}

	// Generate prompts BEFORE persisting: a failed generation aborts the create
	// with no agent row and no directory — the client keeps its form state and
	// the workspace never sees an error agent.
	// The generation service owns the generation budget; the context is
	// detached from the request so the request dying cannot strand state.
	if err := h.agentService.GenerateForCreate(context.Background(), agentDir, ws.ID, agent); err != nil {
		os.RemoveAll(agentDir)
		RespondError(c, fmt.Errorf("%w: %v", domain.ErrInvalid, promptgen.SanitizeError(err)))
		return
	}
	agent.PromptsStatus = domain.PromptsStatusReady

	if err := h.agents.Create(c.Request.Context(), agent); err != nil {
		// No directory cleanup here: after the pre-check above, a conflict
		// means a raced create won the slug, and the directory now belongs to
		// that agent. An orphaned directory is inert and gets cleared by the
		// next SeedWorkspace for this slug.
		RespondError(c, err)
		return
	}

	// Refetch so the response carries store-assigned fields (id, timestamps).
	if refreshed, err := h.agents.ByID(c.Request.Context(), ws.ID, agent.ID); err == nil {
		agent = refreshed
	}
	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug), agent)

	RespondCreated(c, gin.H{"agent": newAgentResponse(agent)})
}

// PatchAgentRequest holds editable fields for updating an existing agent.
type PatchAgentRequest struct {
	Name          *string               `json:"name,omitempty"`
	Role          *string               `json:"role,omitempty"`
	Description   *string               `json:"description,omitempty"`
	Brief         *string               `json:"brief,omitempty"`
	Identity      *string               `json:"identity,omitempty"`
	Soul          *string               `json:"soul,omitempty"`
	ProviderID    *string               `json:"provider_id,omitempty"`
	Model         *string               `json:"model,omitempty"`
	Temperature   *float64              `json:"temperature,omitempty"`
	MaxTokens     *int                  `json:"max_tokens,omitempty"`
	Effort        *string               `json:"effort,omitempty"`
	Autonomy      *domain.AgentAutonomy `json:"autonomy,omitempty"`
	ContextWindow *int                  `json:"context_window,omitempty"`
	Tools         *[]string             `json:"tools,omitempty"`
	EnabledMCPS   *[]string             `json:"enabled_mcps,omitempty"`
	Avatar        *json.RawMessage      `json:"avatar,omitempty"`
}

// PatchAgent updates an existing agent's fields.
func (h *agentHandlers) PatchAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	param := c.Param("agent")

	var req PatchAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Role == nil && req.Description == nil &&
		req.Brief == nil && req.Identity == nil && req.Soul == nil && req.ProviderID == nil &&
		req.Model == nil && req.Temperature == nil && req.MaxTokens == nil && req.Effort == nil &&
		req.Autonomy == nil && req.ContextWindow == nil && req.Tools == nil &&
		req.EnabledMCPS == nil && req.Avatar == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	existing, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: name cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Name = trimmed
	}

	if req.Role != nil {
		trimmed := strings.TrimSpace(*req.Role)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: role cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Role = trimmed
	}

	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}

	if req.Brief != nil {
		trimmed := strings.TrimSpace(*req.Brief)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: brief cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Brief = trimmed
	}

	targetProviderID := existing.ProviderID
	if req.ProviderID != nil {
		targetProviderID = strings.TrimSpace(*req.ProviderID)
		if targetProviderID == "" {
			RespondError(c, fmt.Errorf("%w: provider_id cannot be empty", domain.ErrInvalid))
			return
		}
	}

	targetModel := existing.Model
	if req.Model != nil {
		targetModel = strings.TrimSpace(*req.Model)
		if targetModel == "" {
			RespondError(c, fmt.Errorf("%w: model cannot be empty", domain.ErrInvalid))
			return
		}
	}

	provider, err := h.providers.ByID(c.Request.Context(), ws.ID, targetProviderID)
	if err != nil {
		RespondError(c, fmt.Errorf("%w: provider not found in workspace", domain.ErrInvalid))
		return
	}

	var targetMaxTokens *int = existing.MaxTokens
	if req.MaxTokens != nil {
		targetMaxTokens = req.MaxTokens
	}

	providerImpl, err := h.registry.Get(provider.Type)
	if err != nil {
		RespondError(c, err)
		return
	}

	if providerImpl.RequiresMaxTokens() {
		if targetMaxTokens == nil || *targetMaxTokens <= 0 {
			RespondError(c, fmt.Errorf("%w: max_tokens is required for provider type %q", domain.ErrInvalid, provider.Type))
			return
		}
	}
	if targetMaxTokens != nil && *targetMaxTokens <= 0 {
		RespondError(c, fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid))
		return
	}

	var targetEffort *string = existing.Effort
	if req.Effort != nil {
		trimmed := strings.TrimSpace(*req.Effort)
		if trimmed == "" {
			targetEffort = nil
		} else {
			targetEffort = &trimmed
		}
	}
	if targetEffort != nil {
		if err := validateEffort(c.Request.Context(), h.modelCatalog, provider.Type, targetModel, *targetEffort); err != nil {
			RespondError(c, err)
			return
		}
	}

	existing.ProviderID = targetProviderID
	existing.Model = targetModel
	existing.MaxTokens = targetMaxTokens
	existing.Effort = targetEffort

	if req.Temperature != nil {
		if err := domain.ValidateAgentTemperature(*req.Temperature); err != nil {
			RespondError(c, err)
			return
		}
		existing.Temperature = *req.Temperature
	}

	if req.Autonomy != nil {
		if err := domain.ValidateAgentAutonomy(*req.Autonomy); err != nil {
			RespondError(c, err)
			return
		}
		existing.Autonomy = *req.Autonomy
	}

	if req.Avatar != nil {
		if err := domain.ValidateAgentAvatar(*req.Avatar); err != nil {
			RespondError(c, err)
			return
		}
		existing.Avatar = *req.Avatar
	}

	if req.ContextWindow != nil {
		if err := domain.ValidateAgentContextWindow(req.ContextWindow); err != nil {
			RespondError(c, err)
			return
		}
		existing.ContextWindow = req.ContextWindow
	} else if (req.ProviderID != nil || req.Model != nil) && h.modelCatalog != nil {
		existing.ContextWindow = h.modelCatalog.ResolveContextLimit(c.Request.Context(), provider.Type, targetModel)
	}

	if req.Tools != nil {
		existing.Tools = *req.Tools
	}

	if req.EnabledMCPS != nil {
		existing.EnabledMCPS = *req.EnabledMCPS
	}

	existing.UpdatedBy = &user.ID

	// Prompt documents are files: write whichever the payload carries before
	// the row update. Status stays untouched — manual edits before any
	// generation are allowed and a later regeneration overwrites the files.
	// The directory derives from the current root and slugs.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug)
	if req.Identity != nil {
		if err := promptdocs.WritePromptDocument(agentDir, "IDENTITY.md", *req.Identity); err != nil {
			RespondError(c, fmt.Errorf("failed to write identity prompt document: %w", err))
			return
		}
	}
	if req.Soul != nil {
		if err := promptdocs.WritePromptDocument(agentDir, "SOUL.md", *req.Soul); err != nil {
			RespondError(c, fmt.Errorf("failed to write soul prompt document: %w", err))
			return
		}
	}

	if err := h.agents.Update(c.Request.Context(), existing); err != nil {
		RespondError(c, err)
		return
	}

	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug), existing)
	RespondOK(c, gin.H{"agent": newAgentResponse(existing)})
}

// DeleteAgent deletes an agent and its workspace directory.
func (h *agentHandlers) DeleteAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	existing, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	if err := h.agents.Delete(c.Request.Context(), ws.ID, existing.ID); err != nil {
		RespondError(c, err)
		return
	}

	// The agent's workspace directory dies with the row. The path derives from
	// the current root and slugs; no recorded path exists.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug)
	if err := os.RemoveAll(agentDir); err != nil {
		log.Printf("[handlers.agents] failed to remove agent workspace directory %s: %v", agentDir, err)
	}

	RespondNoContent(c)
}

// regenerateAgentRequest is the optional body for RegenerateAgent. An empty
// body or blank instruction regenerates without requested changes.
type regenerateAgentRequest struct {
	Instruction string `json:"instruction"`
}

// RegenerateAgent re-runs prompt generation synchronously from the stored
// brief, optionally steered by a client-supplied change instruction.
func (h *agentHandlers) RegenerateAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	// The body is optional: an empty body regenerates without an instruction.
	var req regenerateAgentRequest
	_ = c.ShouldBindJSON(&req)

	existing, resolveErr := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if resolveErr != nil {
		RespondError(c, resolveErr)
		return
	}

	if existing.PromptsStatus == domain.PromptsStatusGenerating {
		RespondError(c, fmt.Errorf("%w: prompt generation is already in flight for this agent", domain.ErrConflict))
		return
	}

	if err := h.agents.SetPromptState(c.Request.Context(), ws.ID, existing.ID, domain.PromptsStatusGenerating, nil); err != nil {
		RespondError(c, err)
		return
	}

	// Detached context: the generation service owns the budget and records the
	// final prompt state regardless of this request's lifetime.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug)
	if err := h.agentService.Generate(context.Background(), agentDir, ws.ID, existing.ID, req.Instruction); err != nil {
		log.Printf("[handlers.agents] prompt regeneration failed for agent %s/%s: %v", ws.ID, existing.ID, err)
	}

	// Refetch so the response reflects the final prompt state.
	if refreshed, err := h.agents.ByID(c.Request.Context(), ws.ID, existing.ID); err == nil {
		existing = refreshed
	}
	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug), existing)

	RespondOK(c, gin.H{"agent": newAgentResponse(existing)})
}

// ListSessionEvents returns the translated transcript events for an agent session.
// With stream=true it serves server-sent events instead of the JSON snapshot:
// Phase 1 replays the committed history, Phase 2 taps the live run (when one
// is executing) and streams new events to completion, Phase 3 ends with the
// [DONE] sentinel (design D2, live-run-reattach-and-catchup).
func (h *agentHandlers) ListSessionEvents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	sessionID := c.Param("session")
	after := c.Query("after")
	limit := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n <= 0 {
			RespondError(c, fmt.Errorf("%w: limit must be a positive integer", domain.ErrInvalid))
			return
		}
		if n > 500 {
			n = 500
		}
		limit = n
	}

	streaming, _ := strconv.ParseBool(c.Query("stream"))
	key := agents.RunKey{WorkspaceID: ws.ID, AgentID: agent.ID, SessionID: sessionID}

	// Phase 2 attachment happens before the history query: the tap registers
	// first so events emitted while the snapshot loads buffer on the stream
	// instead of being missed; Phase 1's seen set dedups the overlap.
	var (
		tap        *agents.EventStream
		tapSubID   uint64
		subscribed bool
	)
	if streaming {
		if tapper, ok := h.runner.(AgentRunTapper); ok {
			if subID, s, live := tapper.SubscribeRun(key); live {
				tap, tapSubID, subscribed = s, subID, true
				// Disconnect hygiene: release the tap on every exit path so
				// abandoned connections never leak subscribers.
				defer tapper.UnsubscribeRun(key, tapSubID)
			}
		}
	}

	res, err := h.runner.History(c.Request.Context(), agents.HistoryRequest{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		SessionID:   sessionID,
		After:       after,
		Limit:       limit,
	})
	if err != nil {
		RespondError(c, err)
		return
	}

	if !streaming {
		RespondOK(c, res)
		return
	}

	h.serveSessionEventStream(c, res, tap, subscribed)
}

// serveSessionEventStream writes the two-phase SSE response: the committed
// history snapshot, then (when a live tap is attached) new events until the
// run finishes, then the [DONE] sentinel. Every socket write stays on the
// request goroutine — Recv is pumped on a helper goroutine so the writer loop
// can also select the client-disconnect signal (the v1.go SSE pattern).
func (h *agentHandlers) serveSessionEventStream(c *gin.Context, res *agents.HistoryResult, tap *agents.EventStream, subscribed bool) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	// Ask reverse proxies not to buffer frames (nginx honors this).
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeEvent := func(ev *agents.TranscriptEvent) {
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		_, _ = c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
		c.Writer.Flush()
	}
	writeDone := func() {
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
		c.Writer.Flush()
	}

	// A live tap means the turn is still executing: say so before the history
	// replay so the reconnected page shows its running state immediately —
	// the first real run event can be seconds away (a long tool call or model
	// stream commits nothing until it finishes).
	if subscribed {
		writeEvent(&agents.TranscriptEvent{Kind: agents.TranscriptEventRunActive, OccurredAt: time.Now().UTC()})
	}

	// Phase 1: committed history replay, recording event IDs so events that
	// reached both the store and the live tap are not written twice.
	seen := make(map[string]bool, len(res.Events))
	for i := range res.Events {
		ev := &res.Events[i]
		if ev.ID != "" {
			seen[ev.ID] = true
		}
		writeEvent(ev)
	}

	if !subscribed {
		// No run in flight: the snapshot is the whole transcript.
		writeDone()
		return
	}

	// Phase 2: pump the live tap into a channel so the writer loop can also
	// select the client-disconnect signal — every socket write stays on this
	// single goroutine.
	events := make(chan *agents.TranscriptEvent)
	pumpDone := make(chan struct{})
	defer close(pumpDone)
	go func() {
		defer close(events)
		for {
			ev, err := tap.Recv()
			if err != nil {
				// The run finished and closed the tap (or the tap was torn
				// down): the buffered drain is complete.
				return
			}
			select {
			case events <- ev:
			case <-pumpDone:
				return
			}
		}
	}()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				writeDone()
				return
			}
			if ev.ID != "" {
				if seen[ev.ID] {
					continue
				}
				seen[ev.ID] = true
			}
			writeEvent(ev)
			if isTerminalSessionEvent(ev.Kind) {
				writeDone()
				return
			}
		case <-c.Request.Context().Done():
			// The browser went away (navigation, refresh, abort) — stop
			// pumping into the dead socket.
			writeDone()
			return
		}
	}
}

// isTerminalSessionEvent reports whether the kind ends a live turn. The tap
// closing (Recv EOF) is the primary completion signal — this only ends the
// SSE early once the terminal marker is already visible.
func isTerminalSessionEvent(kind agents.TranscriptEventKind) bool {
	switch kind {
	case agents.TranscriptEventTurnCompleted, agents.TranscriptEventError, agents.TranscriptEventCancelled:
		return true
	}
	return false
}

// buildAgentFromCreateRequest validates and constructs a domain.Agent from CreateAgentRequest.
func buildAgentFromCreateRequest(ctx context.Context, wsID, userID string, req *CreateAgentRequest, providers store.ProviderStore, reg *providers.Registry, mc *services.ModelCatalog) (*domain.Agent, error) {
	if req == nil {
		return nil, domain.ErrInvalid
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: agent name is required", domain.ErrInvalid)
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		return nil, fmt.Errorf("%w: agent slug is required", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentSlug(slug); err != nil {
		return nil, err
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		return nil, fmt.Errorf("%w: agent role is required", domain.ErrInvalid)
	}

	brief := strings.TrimSpace(req.Brief)
	if brief == "" {
		return nil, fmt.Errorf("%w: agent brief is required", domain.ErrInvalid)
	}

	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("%w: provider_id is required", domain.ErrInvalid)
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", domain.ErrInvalid)
	}

	// Validate provider belongs to workspace
	provider, err := providers.ByID(ctx, wsID, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: provider not found in workspace", domain.ErrInvalid)
	}

	providerImpl, err := reg.Get(provider.Type)
	if err != nil {
		return nil, err
	}

	// Anthropic / capability max_tokens requirement
	if providerImpl.RequiresMaxTokens() {
		if req.MaxTokens == nil || *req.MaxTokens <= 0 {
			return nil, fmt.Errorf("%w: max_tokens is required for provider type %q", domain.ErrInvalid, provider.Type)
		}
	}
	if req.MaxTokens != nil && *req.MaxTokens <= 0 {
		return nil, fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid)
	}

	// Effort resolution validation
	if req.Effort != nil && strings.TrimSpace(*req.Effort) != "" {
		if err := validateEffort(ctx, mc, provider.Type, model, *req.Effort); err != nil {
			return nil, err
		}
	}

	temp := 1.0
	if req.Temperature != nil {
		if err := domain.ValidateAgentTemperature(*req.Temperature); err != nil {
			return nil, err
		}
		temp = *req.Temperature
	}

	autonomy := domain.AutonomyApproval
	if req.Autonomy != nil && *req.Autonomy != "" {
		if err := domain.ValidateAgentAutonomy(*req.Autonomy); err != nil {
			return nil, err
		}
		autonomy = *req.Autonomy
	}

	avatar := json.RawMessage("{}")
	if len(req.Avatar) > 0 {
		if err := domain.ValidateAgentAvatar(req.Avatar); err != nil {
			return nil, err
		}
		avatar = req.Avatar
	}

	contextWindow := req.ContextWindow
	if contextWindow != nil {
		if err := domain.ValidateAgentContextWindow(contextWindow); err != nil {
			return nil, err
		}
	} else if mc != nil {
		contextWindow = mc.ResolveContextLimit(ctx, provider.Type, model)
	}

	agentTools := req.Tools
	if agentTools == nil {
		agentTools = []string{}
	}
	enabledMCPS := req.EnabledMCPS
	if enabledMCPS == nil {
		enabledMCPS = []string{}
	}

	var effort *string
	if req.Effort != nil && strings.TrimSpace(*req.Effort) != "" {
		eff := strings.TrimSpace(*req.Effort)
		effort = &eff
	}

	var creator *string
	if userID != "" {
		creator = &userID
	}

	return &domain.Agent{
		WorkspaceID:   wsID,
		Slug:          slug,
		Name:          name,
		Role:          role,
		Description:   strings.TrimSpace(req.Description),
		Brief:         brief,
		ProviderID:    providerID,
		Model:         model,
		Temperature:   temp,
		MaxTokens:     req.MaxTokens,
		Effort:        effort,
		Autonomy:      autonomy,
		ContextWindow: contextWindow,
		Tools:         agentTools,
		EnabledMCPS:   enabledMCPS,
		Avatar:        avatar,
		PromptsStatus: domain.PromptsStatusGenerating,
		CreatedBy:     creator,
		UpdatedBy:     creator,
	}, nil
}

func validateEffort(ctx context.Context, mc *services.ModelCatalog, providerType, modelID, effort string) error {
	trimmed := strings.TrimSpace(effort)
	if trimmed == "" {
		return nil
	}

	validEfforts := mc.ResolveEfforts(ctx, providerType, modelID)

	if len(validEfforts) == 0 {
		return fmt.Errorf("%w: model %q does not support reasoning effort", domain.ErrInvalid, modelID)
	}

	for _, v := range validEfforts {
		if strings.EqualFold(v, trimmed) {
			return nil
		}
	}

	return fmt.Errorf("%w: invalid effort %q for model %q (valid: %s)", domain.ErrInvalid, trimmed, modelID, strings.Join(validEfforts, ", "))
}

// ResolveApprovalRequest carries the human decision for a pending shell approval.
type ResolveApprovalRequest struct {
	Approved *bool `json:"approved"`
}

// ResolveApproval resolves a pending shell-approval interrupt and resumes the
// paused turn. The response returns once the resume has been initiated; the
// continued turn's events appear in the session history.
func (h *agentHandlers) ResolveApproval(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}

	var req ResolveApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Approved == nil {
		RespondError(c, fmt.Errorf("%w: approved (bool) is required", domain.ErrInvalid))
		return
	}

	sessionID := c.Param("session")
	interruptID := c.Param("interruptID")

	pending, err := h.runner.PendingApproval(c.Request.Context(), ws.ID, sessionID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if pending == nil || pending.InterruptID != interruptID {
		RespondError(c, fmt.Errorf("%w: no pending approval matches interrupt %q", domain.ErrConflict, interruptID))
		return
	}

	_, err = h.runner.Resume(c.Request.Context(), agents.ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     agent.ID,
		SessionID:   sessionID,
		UserID:      user.ID,
	}, pending, *req.Approved)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{
		"resumed":      true,
		"interrupt_id": interruptID,
		"approved":     *req.Approved,
	})
}
