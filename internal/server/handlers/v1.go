package handlers

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/attachments"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/openresponses"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// v1Handlers handles the /v1 (OpenResponses) surface. Authentication happens
// in the API-key middleware; the key carries the workspace tenant scope and
// the creator user for every operation.
type v1Handlers struct {
	runner        *agents.Runner
	agents        store.AgentStore
	sessionEvents store.SessionEventStore
	attachments   store.AttachmentStore
	wsStorage     *resolver.WorkspaceStorage
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
func NewV1Handlers(runner *agents.Runner, agents store.AgentStore, sessionEvents store.SessionEventStore, attachments store.AttachmentStore, wsStorage *resolver.WorkspaceStorage, keepAlive time.Duration) *v1Handlers {
	if keepAlive <= 0 {
		keepAlive = defaultKeepAlive
	}
	return &v1Handlers{runner: runner, agents: agents, sessionEvents: sessionEvents, attachments: attachments, wsStorage: wsStorage, keepAlive: keepAlive}
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

	input, inputAtts, err := openresponses.FlattenInputParts(req.Input)
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

	command := compactCommand(req.Metadata)

	// Compact turns are text-only (openresponses spec): an attachment on a
	// compaction fails invalid_param before attachment resolution and before
	// session binding, leaving the session untouched.
	if command == agents.CommandCompact && len(inputAtts) > 0 {
		respondV1InvalidParam(c, "input", "compaction accepts text only")
		return
	}

	// Session binding: metadata.onclaw_session (primary), then
	// previous_response_id, then ephemeral. Compact-command turns bind
	// strictly (D2): they never birth and never run ephemeral.
	sessionID, err := h.resolveSession(c, key.WorkspaceID, req.Metadata, req.PreviousResponseID, command)
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

	// Attachment resolution (attachments design D9): capability URLs resolve
	// against the key's workspace; inline data URLs demote to stored
	// attachments on arrival (D2). Any failure fails the request before a run
	// is created.
	var refs []agents.AttachmentRef
	if len(inputAtts) > 0 {
		refs, err = h.resolveAttachments(c, key.WorkspaceID, key.CreatedBy, inputAtts)
		if err != nil {
			switch {
			case errors.Is(err, domain.ErrPayloadTooLarge):
				// Oversize rides the standard payload-too-large envelope
				// (400 invalid_request_error naming the cap).
				respondV1Error(c, err)
			case errors.Is(err, domain.ErrInvalid):
				// Rejected or unresolvable reference: invalid_param, no run.
				respondV1InvalidParam(c, "input", err.Error())
			default:
				// Storage/store infrastructure failures surface as the
				// standard 5xx envelope.
				respondV1Error(c, err)
			}
			return
		}
	}

	execReq := agents.ExecRequest{
		WorkspaceID:  key.WorkspaceID,
		AgentID:      agent.ID,
		SessionID:    sessionID,
		UserID:       key.CreatedBy,
		Input:        input,
		Command:      command,
		AllowedTools: allowedTools,
		Attachments:  refs,
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

// resolveAttachments turns parsed input attachments into runner refs
// (attachments design D9): capability URLs resolve through the attachment
// store — the lookup is global, so the row's workspace is verified against
// the key's (foreign and unknown fail identically) — and inline data URLs
// demote to stored attachments (D2). Validation failures wrap
// domain.ErrInvalid; infrastructure failures pass through bare.
func (h *v1Handlers) resolveAttachments(c *gin.Context, workspaceID, userID string, atts []openresponses.InputAttachment) ([]agents.AttachmentRef, error) {
	refs := make([]agents.AttachmentRef, 0, len(atts))
	for _, att := range atts {
		if att.Inline {
			ref, err := h.demoteInlineAttachment(c, workspaceID, userID, att)
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
			continue
		}

		key, ok := att.CapabilityKey()
		if !ok {
			return nil, fmt.Errorf("%w: attachment reference %q is not an onclaw capability URL", domain.ErrInvalid, att.URL)
		}
		row, err := h.attachments.ByStorageKey(c.Request.Context(), key)
		if err != nil {
			return nil, fmt.Errorf("%w: attachment reference %q does not resolve in this workspace", domain.ErrInvalid, att.URL)
		}
		if row.WorkspaceID != workspaceID {
			// Foreign capability keys are indistinguishable from unknown ones
			// to the caller (tenancy) — the same invalid_param either way.
			return nil, fmt.Errorf("%w: attachment reference %q does not resolve in this workspace", domain.ErrInvalid, att.URL)
		}
		refs = append(refs, agents.AttachmentRef{ID: row.ID, Name: row.Name, MimeType: row.MimeType, Lane: row.Lane, Size: row.Size})
	}
	return refs, nil
}

// demoteInlineAttachment stores an inline data-URL attachment on arrival
// (attachments design D2): the decoded bytes are classified, written to the
// workspace's backend under a fresh capability key, and recorded as an
// attachments row — exactly the upload path's shape, so downstream sees the
// same reference either way.
func (h *v1Handlers) demoteInlineAttachment(c *gin.Context, workspaceID, userID string, att openresponses.InputAttachment) (agents.AttachmentRef, error) {
	data, err := decodeDataURL(att.URL)
	if err != nil {
		return agents.AttachmentRef{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}

	// Read the same classification head the upload handler sniffs.
	head := data
	if len(head) > sniffHeadBytes {
		head = head[:sniffHeadBytes]
	}

	name := att.Filename
	if name == "" {
		// Mime-derived default filename: the sniff is authoritative, the
		// declared data-URL media type is not.
		ext := ""
		if exts, extErr := mime.ExtensionsByType(http.DetectContentType(head)); extErr == nil && len(exts) > 0 {
			ext = exts[0]
		}
		name = "attachment" + ext
	}

	sniffed, lane, err := attachments.Classify(name, int64(len(data)), head)
	if err != nil {
		// Classify's rejections carry the caps-matrix reasons (invalid) and
		// oversize (*ErrTooLarge → domain.ErrPayloadTooLarge).
		return agents.AttachmentRef{}, err
	}

	st, err := h.wsStorage.ForWorkspace(c.Request.Context(), workspaceID)
	if err != nil {
		return agents.AttachmentRef{}, err
	}
	backend, err := h.wsStorage.DriverName(c.Request.Context(), workspaceID)
	if err != nil {
		return agents.AttachmentRef{}, err
	}

	key, err := storage.NewKey()
	if err != nil {
		return agents.AttachmentRef{}, err
	}

	if err := st.Put(c.Request.Context(), key, bytes.NewReader(data), int64(len(data)), sniffed); err != nil {
		return agents.AttachmentRef{}, err
	}

	row := &domain.Attachment{
		WorkspaceID: workspaceID,
		StorageKey:  key,
		Backend:     backend,
		Name:        name,
		MimeType:    sniffed,
		Size:        int64(len(data)),
		Lane:        lane,
		CreatedBy:   userID,
	}
	if err := h.attachments.Create(c.Request.Context(), row); err != nil {
		return agents.AttachmentRef{}, err
	}

	return agents.AttachmentRef{ID: row.ID, Name: row.Name, MimeType: row.MimeType, Lane: row.Lane, Size: row.Size}, nil
}

// decodeDataURL decodes a base64 data URL into its bytes. Only base64
// payloads are accepted — the demotion path needs the raw bytes to sniff.
func decodeDataURL(raw string) ([]byte, error) {
	if len(raw) < 5 || !strings.EqualFold(raw[:5], "data:") {
		return nil, fmt.Errorf("inline attachment URL is not a data URL")
	}
	rest := raw[5:]
	header, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, fmt.Errorf("inline attachment data URL is malformed")
	}
	if !strings.Contains(strings.ToLower(header), "base64") {
		return nil, fmt.Errorf("inline attachment data URL must be base64-encoded")
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload), ""))
	if err != nil {
		return nil, fmt.Errorf("inline attachment data URL: invalid base64 payload")
	}
	return data, nil
}

// isEphemeral reports whether the request has no session binding at all.
func isEphemeral(req openresponses.ResponseRequest) bool {
	sid := sessionMetadata(req.Metadata)
	return sid == "" && req.PreviousResponseID == ""
}

// compactCommand maps metadata.onclaw_command onto the ExecRequest command
// value (chat-compact-command D2): "compact" marks the turn as a context
// compaction whose input is the summarizer focus text; anything else
// (including empty) is an ordinary model turn.
func compactCommand(md map[string]string) string {
	if md != nil && md["onclaw_command"] == agents.CommandCompact {
		return agents.CommandCompact
	}
	return ""
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
// unresolvable IDs are not-found, and the path never births. Compact-command
// turns are bind-only everywhere (chat-compact-command D2): a compaction
// targets an existing history, so metadata binding loses its birth power,
// the unbound path fails instead of going ephemeral, and only the chained
// path behaves as for ordinary turns.
func (h *v1Handlers) resolveSession(c *gin.Context, workspaceID string, md map[string]string, previousResponseID, command string) (string, error) {
	if sid := sessionMetadata(md); sid != "" {
		// Compact never births (D2): an onclaw_session with no persisted
		// events in the key's workspace has nothing to compact and fails
		// not-found, mirroring previous_response_id.
		if command == agents.CommandCompact && !h.sessionExists(c, workspaceID, sid) {
			return "", fmt.Errorf("%w: session not found", domain.ErrNotFound)
		}
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

	// A compact request with no binding at all has no session to compact
	// (D2): fail not-found instead of running ephemeral.
	if command == agents.CommandCompact {
		return "", fmt.Errorf("%w: session not found", domain.ErrNotFound)
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
