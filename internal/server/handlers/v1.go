package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/openresponses"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// v1Handlers handles the /v1 (OpenResponses) surface. Authentication happens
// in the API-key middleware; the key carries the workspace tenant scope and
// the creator user for every operation.
type v1Handlers struct {
	runner        *agents.Runner
	agents        store.AgentStore
	sessionEvents store.SessionEventStore
	keepAlive     time.Duration
}

// defaultKeepAlive is the SSE idle cadence: how often the stream re-sends the
// response snapshot while the run sits silent (a slow model call or a long
// browser tool can produce no events for minutes). Without traffic, proxies
// along the path reap the idle connection and strand the browser's stream
// client mid-turn.
const defaultKeepAlive = 15 * time.Second

// NewV1Handlers creates a new v1Handlers instance with injected dependencies.
// keepAlive <= 0 falls back to the default cadence.
func NewV1Handlers(runner *agents.Runner, agents store.AgentStore, sessionEvents store.SessionEventStore, keepAlive time.Duration) *v1Handlers {
	if keepAlive <= 0 {
		keepAlive = defaultKeepAlive
	}
	return &v1Handlers{runner: runner, agents: agents, sessionEvents: sessionEvents, keepAlive: keepAlive}
}

// ListModels implements GET /v1/models: the workspace's agents listed as
// models with the agent slug as the id.
func (h *v1Handlers) ListModels(c *gin.Context) {
	key := MustCurrentAPIKey(c)

	list, err := h.agents.ListForWorkspace(c.Request.Context(), key.WorkspaceID)
	if err != nil {
		respondV1Error(c, err)
		return
	}

	data := make([]gin.H, 0, len(list))
	for _, ag := range list {
		data = append(data, gin.H{
			"id":       ag.Slug,
			"object":   "model",
			"owned_by": "onclaw",
		})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// CreateResponse implements POST /v1/responses.
func (h *v1Handlers) CreateResponse(c *gin.Context) {
	key := MustCurrentAPIKey(c)

	var req openresponses.ResponseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondV1Error(c, domain.ErrInvalid)
		return
	}
	if req.Model == "" {
		respondV1InvalidParam(c, "model", "model is required")
		return
	}

	input, err := openresponses.FlattenInput(req.Input)
	if err != nil {
		respondV1InvalidParam(c, "input", err.Error())
		return
	}

	// Model resolution: the agent slug must exist in the key's workspace.
	// Foreign slugs fail with the same not-found (model_not_found) error.
	agent, err := h.agents.BySlug(c.Request.Context(), key.WorkspaceID, req.Model)
	if err != nil {
		respondV1ModelNotFound(c)
		return
	}

	// Session binding: metadata.onclaw_session (primary), then
	// previous_response_id, then ephemeral.
	sessionID, err := h.resolveSession(c, key.WorkspaceID, req.Metadata, req.PreviousResponseID)
	if err != nil {
		respondV1Error(c, err)
		return
	}

	// Tool narrowing: intersection of the request's names with the agent
	// allowlist (a request may narrow, never widen); tool_choice "none"
	// strips all tools for the turn.
	var allowedTools []string
	if req.ToolChoiceNone() {
		allowedTools = []string{}
	} else if names := req.RequestedToolNames(); len(names) > 0 {
		allow := map[string]bool{}
		for _, t := range agent.Tools {
			allow[t] = true
		}
		allowedTools = []string{}
		for _, t := range agent.Tools {
			if allow[t] && contains(names, t) {
				allowedTools = append(allowedTools, t)
			}
		}
	}

	execReq := agents.ExecRequest{
		WorkspaceID:  key.WorkspaceID,
		AgentID:      agent.ID,
		SessionID:    sessionID,
		UserID:       key.CreatedBy,
		Input:        input,
		AllowedTools: allowedTools,
	}

	var stream *agents.EventStream
	if isEphemeral(req) {
		stream, err = h.runner.RunEphemeral(c.Request.Context(), execReq)
	} else {
		stream, err = h.runner.Run(c.Request.Context(), execReq)
	}
	if err != nil {
		respondV1Error(c, err)
		return
	}

	resp := openresponses.NewResponse(sessionID, agent.Slug, req.Metadata)
	translator := openresponses.NewTranslator(resp, nil)

	if req.Stream {
		h.serveStream(c, stream, translator)
		return
	}
	h.serveAggregated(c, stream, translator)
}

// isEphemeral reports whether the request has no session binding at all.
func isEphemeral(req openresponses.ResponseRequest) bool {
	sid := sessionMetadata(req.Metadata)
	return sid == "" && req.PreviousResponseID == ""
}

func sessionMetadata(md map[string]string) string {
	if md == nil {
		return ""
	}
	return md["onclaw_session"]
}

// resolveSession maps the request's binding onto a session ID. Metadata
// binding births: a session with no persisted events in the key's workspace
// is born on first use under the client-chosen ID — the first persisted
// append is the birth, and the persistent session adapter scopes it to the
// workspace (a foreign workspace's session using the same ID is untouched
// and unreadable; the local session is independent). Chained binding
// (previous_response_id) is strictly bind-only: malformed IDs are invalid,
// unresolvable IDs are not-found, and the path never births.
func (h *v1Handlers) resolveSession(c *gin.Context, workspaceID string, md map[string]string, previousResponseID string) (string, error) {
	if sid := sessionMetadata(md); sid != "" {
		// Create-on-first-use: whether the session already exists or not,
		// the turn runs on the persistent adapter under the named ID.
		return sid, nil
	}

	if previousResponseID != "" {
		sid, _, err := openresponses.DecodeResponseID(previousResponseID)
		if err != nil {
			return "", fmt.Errorf("%w: previous_response_id: %v", domain.ErrInvalid, err)
		}
		if !h.sessionExists(c, workspaceID, sid) {
			return "", fmt.Errorf("%w: session not found", domain.ErrNotFound)
		}
		return sid, nil
	}

	// Unbound: fresh ephemeral session. The throwaway ID is never persisted —
	// the run executes with the no-store session adapter.
	return "ephemeral_" + uuid.NewString(), nil
}

func (h *v1Handlers) sessionExists(c *gin.Context, workspaceID, sessionID string) bool {
	rows, err := h.sessionEvents.LoadEvents(c.Request.Context(), store.LoadSessionEventsParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Limit:       1,
	})
	if err != nil || len(rows) == 0 {
		return false
	}
	return true
}

// serveAggregated drains the stream to the terminal event and returns the
// folded Response JSON. The connection is held for the turn's duration — the
// spec's own semantic for stream: false.
func (h *v1Handlers) serveAggregated(c *gin.Context, stream *agents.EventStream, translator *openresponses.Translator) {
	defer func() { _ = stream.Close() }()

	stopped := false
	for {
		ev, err := stream.Recv()
		if err != nil {
			break
		}
		if translator.Handle(ev) {
			stopped = true
			break
		}
	}
	_ = stopped

	c.JSON(http.StatusOK, translator.Response())
}

// serveStream translates the live stream onto the OpenResponses SSE contract.
func (h *v1Handlers) serveStream(c *gin.Context, stream *agents.EventStream, translator *openresponses.Translator) {
	defer func() { _ = stream.Close() }()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	// Ask reverse proxies not to buffer frames (nginx honors this).
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeFrame := func(ev map[string]any) {
		_, _ = c.Writer.Write([]byte("data: " + string(openresponses.JSON(ev)) + "\n\n"))
		c.Writer.Flush()
	}
	translator.SetSink(writeFrame)

	// Pump Recv into a channel so the writer loop can also select the
	// keepalive ticker and the client-disconnect signal — every socket write
	// stays on this single goroutine.
	events := make(chan *agents.TranscriptEvent)
	pumpDone := make(chan struct{})
	defer close(pumpDone)
	go func() {
		defer close(events)
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			select {
			case events <- ev:
			case <-pumpDone:
				return
			}
		}
	}()

	keepalive := time.NewTicker(h.keepAlive)
	defer keepalive.Stop()

	stopped := false
	for !stopped {
		select {
		case ev, ok := <-events:
			if !ok {
				// The run goroutine finished without a terminal event reaching
				// the tap. End the stream so the client's event loop exits and
				// its turn resolves instead of hanging on an open socket.
				stopped = true
			} else if translator.Handle(ev) {
				stopped = true
			}
		case <-keepalive.C:
			translator.KeepAlive()
		case <-c.Request.Context().Done():
			// The browser went away (navigation, refresh, abort) — stop early
			// instead of draining the rest of the run into a dead socket.
			stopped = true
		}
	}

	_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
	c.Writer.Flush()
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func respondV1InvalidParam(c *gin.Context, param, message string) {
	RespondV1Error(c, http.StatusBadRequest, V1ErrorTypeInvalidRequest, param, "", message)
}

func respondV1ModelNotFound(c *gin.Context) {
	RespondV1Error(c, http.StatusNotFound, V1ErrorTypeNotFound, "model", "model_not_found", "model not found")
}

// respondV1Error maps a domain error onto the OpenResponses envelope.
func respondV1Error(c *gin.Context, err error) {
	if err == nil {
		return
	}
	RespondV1DomainError(c, err)
}
