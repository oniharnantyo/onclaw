package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/channels"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/teams"
)

// DefaultChannelStreamKeepAlive is the channel SSE idle keepalive cadence; the
// composition root may override it through the router options.
const DefaultChannelStreamKeepAlive = 15 * time.Second

// DefaultChannelMessageLimit caps one feed page, matching the session-events
// list bound.
const DefaultChannelMessageLimit = 500

// ChannelPoster is the feed chokepoint capability the message-post handler
// uses (design integrate-agent-channels D2): the single pipeline that persists,
// resolves mentions, fans out agent runs, and broadcasts. *channels.Chokepoint
// satisfies it.
type ChannelPoster interface {
	Post(ctx context.Context, workspaceID, channelID string, author channels.Author, body string) (domain.ChannelMessage, error)
}

// ChannelEventHub is the subscription capability the SSE endpoint uses.
// *channels.Hub satisfies it.
type ChannelEventHub interface {
	Subscribe(channelID string) (<-chan channels.Event, func())
}

// ChannelKickoffPoster is the kickoff capability of the chokepoint
// (channel-teams D1): the flagged post mints the work session and summons the
// facilitator. *channels.Chokepoint satisfies it.
type ChannelKickoffPoster interface {
	PostKickoff(ctx context.Context, workspaceID, channelID string, author channels.Author, body string) (domain.ChannelMessage, *domain.WorkSession, error)
}

// ChannelActiveSessionReader resolves the channel's active (open or paused)
// work session for the awaiting-state surfaces (channel-teams design D8).
// *channels.Chokepoint satisfies it.
type ChannelActiveSessionReader interface {
	ActiveWorkSession(ctx context.Context, workspaceID, channelID string) (*domain.WorkSession, error)
}

// WorkSessionLister reads stored work sessions for the session endpoints
// (channel-teams task 7.1). The work-session store satisfies it.
type WorkSessionLister interface {
	// GetWorkSession resolves one session by id under the workspace; unknown
	// ids are domain.ErrNotFound.
	GetWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error)
	// ListWorkSessions returns the channel's sessions newest first.
	ListWorkSessions(ctx context.Context, workspaceID, channelID string) ([]domain.WorkSession, error)
}

// ---------------------------------------------------------------------------
// Payload shapes
// ---------------------------------------------------------------------------

// channelCreateRequest is the create payload: name + slug (D15 split), plus
// the freeform header line and CHANNEL.md conventions section.
type channelCreateRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Purpose     string `json:"purpose"`
	Conventions string `json:"conventions"`
}

// channelPatchRequest is the partial update payload. Slug is not patchable
// (it is the channel's URL/#handle identity); PATCH covers name, purpose, and
// conventions.
type channelPatchRequest struct {
	Name        *string `json:"name"`
	Purpose     *string `json:"purpose"`
	Conventions *string `json:"conventions"`
}

// channelMemberAddRequest adds one roster row: exactly one of user_id /
// agent_id according to member_type.
type channelMemberAddRequest struct {
	MemberType     string `json:"member_type"`
	UserID         string `json:"user_id"`
	AgentID        string `json:"agent_id"`
	Specialization string `json:"specialization"`
}

// channelMemberPatchRequest rewrites the per-channel role note and/or the
// member's facilitator role (channel-teams D2: role member|facilitator, at
// most one facilitator per channel).
type channelMemberPatchRequest struct {
	Specialization *string `json:"specialization"`
	Role           *string `json:"role"`
}

// channelMessagePostRequest is a human-authored feed post. IsKickoff flags
// the message as a work-session kickoff (channel-teams D1): explicit and
// human-gated, never parsed from text.
type channelMessagePostRequest struct {
	Body      string `json:"body"`
	IsKickoff bool   `json:"is_kickoff"`
}

// ---------------------------------------------------------------------------
// Read views
// ---------------------------------------------------------------------------

// channelMemberView is the roster read view: the raw row plus the resolved
// display name and @handle — agents by slug, humans by the shared handle rule
// over their display name (lowercase, spaces → dashes). The mention menu and
// the chokepoint's @-parsing both key off the handle. Role is the member's
// channel role (member|facilitator, channel-teams D2).
type channelMemberView struct {
	ID             string                   `json:"id"`
	ChannelID      string                   `json:"channel_id"`
	MemberType     domain.ChannelMemberType `json:"member_type"`
	UserID         string                   `json:"user_id,omitempty"`
	AgentID        string                   `json:"agent_id,omitempty"`
	DisplayName    string                   `json:"display_name"`
	Handle         string                   `json:"handle"`
	Specialization string                   `json:"specialization"`
	Role           domain.ChannelMemberRole `json:"role"`
	AddedAt        time.Time                `json:"added_at"`
}

// channelView is the channel read view: the raw row plus the awaiting state —
// the channel's active (open or paused) work session, or null when none is
// live (channel-teams D8).
type channelView struct {
	domain.Channel
	ActiveSession *domain.WorkSession `json:"active_session"`
}

// userHandle derives a human member's @handle from the display name —
// lowercase, spaces → dashes. This is the SAME rule the runner and the
// channel chokepoint apply when attributing output and resolving @mentions
// (internal/agents channels.go humanChannelHandle) — keep them in lockstep.
func userHandle(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// channelHandlers serves the workspace channel surface (design
// integrate-agent-channels D11): room CRUD, the heterogeneous membership
// roster, the shared feed (cursor reads + chokepoint posts), and the live SSE
// event stream — extended with the channel-teams surface: team-template
// materialization, kickoff-flagged posts, and work-session reads. Reads are
// gated by channels.read, writes by channels.write (D12); unknown ids,
// unknown slugs, and other workspaces' channels are 404 indistinguishably.
type channelHandlers struct {
	channels     store.ChannelStore
	agents       store.AgentStore
	users        store.UserStore
	poster       ChannelPoster
	kickoff      ChannelKickoffPoster
	activeSess   ChannelActiveSessionReader
	sessions     WorkSessionLister
	materializer *teams.Materializer
	hub          ChannelEventHub
	keepAlive    time.Duration
}

// NewChannelHandlers creates a new channelHandlers instance. A keepalive of
// zero selects DefaultChannelStreamKeepAlive. The kickoff poster, active
// session reader, and session lister are the chokepoint/work-session-store
// faces the channel-teams endpoints ride; the materializer materializes
// built-in team templates.
func NewChannelHandlers(channelStore store.ChannelStore, agents store.AgentStore, users store.UserStore, poster ChannelPoster, kickoff ChannelKickoffPoster, activeSess ChannelActiveSessionReader, sessions WorkSessionLister, materializer *teams.Materializer, hub ChannelEventHub, keepAlive time.Duration) *channelHandlers {
	if keepAlive <= 0 {
		keepAlive = DefaultChannelStreamKeepAlive
	}
	return &channelHandlers{
		channels:     channelStore,
		agents:       agents,
		users:        users,
		poster:       poster,
		kickoff:      kickoff,
		activeSess:   activeSess,
		sessions:     sessions,
		materializer: materializer,
		hub:          hub,
		keepAlive:    keepAlive,
	}
}

// resolveChannel resolves the channel addressed by the route (id or slug —
// the slug is the channel's URL form, the agentHandlers convention) under the
// current workspace.
func (h *channelHandlers) resolveChannel(c *gin.Context) (*domain.Workspace, *domain.Channel, bool) {
	ws := MustCurrentWorkspace(c)
	identifier := c.Param("id")
	if identifier == "" {
		RespondError(c, domain.ErrNotFound)
		return nil, nil, false
	}
	ctx := c.Request.Context()
	channel, err := h.channels.ChannelBySlug(ctx, ws.ID, identifier)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			RespondError(c, err)
			return nil, nil, false
		}
		channel, err = h.channels.ChannelByID(ctx, ws.ID, identifier)
	}
	if err != nil {
		RespondError(c, err)
		return nil, nil, false
	}
	return ws, channel, true
}

// memberView resolves one roster row's display name and handle.
func (h *channelHandlers) memberView(ctx context.Context, m *domain.ChannelMember) channelMemberView {
	v := channelMemberView{
		ID:             m.ID,
		ChannelID:      m.ChannelID,
		MemberType:     m.MemberType,
		UserID:         m.UserID,
		AgentID:        m.AgentID,
		Specialization: m.Specialization,
		Role:           m.Role,
		AddedAt:        m.AddedAt,
	}
	switch m.MemberType {
	case domain.ChannelMemberTypeAgent:
		if a, err := h.agents.ByID(ctx, m.WorkspaceID, m.AgentID); err == nil {
			v.DisplayName = a.Name
			v.Handle = a.Slug
		}
	case domain.ChannelMemberTypeUser:
		if u, err := h.users.ByID(ctx, m.UserID); err == nil {
			v.DisplayName = u.Name
			v.Handle = userHandle(u.Name)
		}
	}
	return v
}

// respondChannelValidation writes the fielded 422 envelope (the tool-settings
// convention) for channel payloads.
func respondChannelValidation(c *gin.Context, details ...ErrorDetail) {
	message := "validation failed"
	if len(details) > 0 {
		message = details[0].Message
	}
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, message, details...)
}

func bindChannelRequest(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return false
	}
	return true
}

// -------------------------------------------------------------------------
// Channel CRUD
// -------------------------------------------------------------------------

// ListChannels returns the workspace's channels in creation order, each with
// its awaiting state (the active work session, or null).
func (h *channelHandlers) ListChannels(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	ctx := c.Request.Context()

	rows, err := h.channels.ListChannels(ctx, ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	items := make([]channelView, 0, len(rows))
	for i := range rows {
		items = append(items, h.channelViewState(ctx, ws.ID, &rows[i]))
	}
	RespondOK(c, gin.H{"channels": items})
}

// channelViewState projects one channel onto the read view with its active
// work session (design D8). A read failure degrades to a null active_session
// — the awaiting state is a surface, not a gate.
func (h *channelHandlers) channelViewState(ctx context.Context, workspaceID string, ch *domain.Channel) channelView {
	session, err := h.activeSess.ActiveWorkSession(ctx, workspaceID, ch.ID)
	if err != nil {
		session = nil
	}
	return channelView{Channel: *ch, ActiveSession: session}
}

// CreateChannel registers a room; slug conflicts (case-insensitive, workspace
// scoped) are 409 through the domain sentinel chain.
func (h *channelHandlers) CreateChannel(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req channelCreateRequest
	if !bindChannelRequest(c, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	slug := strings.TrimSpace(req.Slug)
	var details []ErrorDetail
	if name == "" {
		details = append(details, ErrorDetail{Field: "name", Message: "name cannot be empty"})
	}
	if err := domain.ValidateChannelSlug(slug); err != nil {
		details = append(details, ErrorDetail{Field: "slug", Message: err.Error()})
	}
	if len(details) > 0 {
		respondChannelValidation(c, details...)
		return
	}

	channel := &domain.Channel{
		WorkspaceID: ws.ID,
		Name:        name,
		Slug:        slug,
		Purpose:     req.Purpose,
		Conventions: req.Conventions,
		CreatedBy:   &user.ID,
	}
	if err := h.channels.CreateChannel(c.Request.Context(), channel); err != nil {
		RespondError(c, err)
		return
	}
	RespondJSON(c, http.StatusCreated, gin.H{"channel": channel})
}

// GetChannel returns one channel by id or slug, with its awaiting state.
func (h *channelHandlers) GetChannel(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	RespondOK(c, gin.H{"channel": h.channelViewState(c.Request.Context(), ws.ID, channel)})
}

// PatchChannel overlays name/purpose/conventions onto the stored row; the
// slug is not patchable.
func (h *channelHandlers) PatchChannel(c *gin.Context) {
	_, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}

	var req channelPatchRequest
	if !bindChannelRequest(c, &req) {
		return
	}

	var details []ErrorDetail
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		details = append(details, ErrorDetail{Field: "name", Message: "name cannot be empty"})
	}
	if len(details) > 0 {
		respondChannelValidation(c, details...)
		return
	}

	changed := false
	if req.Name != nil {
		if name := strings.TrimSpace(*req.Name); name != channel.Name {
			channel.Name = name
			changed = true
		}
	}
	if req.Purpose != nil && *req.Purpose != channel.Purpose {
		channel.Purpose = *req.Purpose
		changed = true
	}
	if req.Conventions != nil && *req.Conventions != channel.Conventions {
		channel.Conventions = *req.Conventions
		changed = true
	}
	if !changed {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if err := h.channels.UpdateChannel(c.Request.Context(), channel); err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"channel": channel})
}

// DeleteChannel removes the room; its roster and feed cascade in the store.
func (h *channelHandlers) DeleteChannel(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	if err := h.channels.DeleteChannel(c.Request.Context(), ws.ID, channel.ID); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// -------------------------------------------------------------------------
// Membership
// -------------------------------------------------------------------------

// validateChannelMemberAdd turns the heterogeneous payload into a domain row,
// reporting fielded validation details for the 422 path.
func validateChannelMemberAdd(wsID, channelID string, req *channelMemberAddRequest) (*domain.ChannelMember, []ErrorDetail) {
	member := &domain.ChannelMember{
		WorkspaceID:    wsID,
		ChannelID:      channelID,
		MemberType:     domain.ChannelMemberType(req.MemberType),
		UserID:         req.UserID,
		AgentID:        req.AgentID,
		Specialization: req.Specialization,
	}
	var details []ErrorDetail
	switch member.MemberType {
	case domain.ChannelMemberTypeUser:
		if member.UserID == "" {
			details = append(details, ErrorDetail{Field: "user_id", Message: "user_id is required for user members"})
		}
	case domain.ChannelMemberTypeAgent:
		if member.AgentID == "" {
			details = append(details, ErrorDetail{Field: "agent_id", Message: "agent_id is required for agent members"})
		}
	default:
		details = append(details, ErrorDetail{Field: "member_type", Message: fmt.Sprintf("member_type %q must be %q or %q", req.MemberType, domain.ChannelMemberTypeUser, domain.ChannelMemberTypeAgent)})
	}
	return member, details
}

// AddChannelMember attaches one user or agent to the roster. Duplicate rows
// are 409 (domain.ErrDuplicateChannelMember); references outside the
// workspace are 404.
func (h *channelHandlers) AddChannelMember(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}

	var req channelMemberAddRequest
	if !bindChannelRequest(c, &req) {
		return
	}

	member, details := validateChannelMemberAdd(ws.ID, channel.ID, &req)
	if len(details) > 0 {
		respondChannelValidation(c, details...)
		return
	}

	if err := h.channels.AddChannelMember(c.Request.Context(), member); err != nil {
		RespondError(c, err)
		return
	}
	RespondJSON(c, http.StatusCreated, gin.H{"member": h.memberView(c.Request.Context(), member)})
}

// ListChannelMembers returns the roster in add order with resolved display
// names and handles.
func (h *channelHandlers) ListChannelMembers(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}

	rows, err := h.channels.ListChannelMembers(c.Request.Context(), ws.ID, channel.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	ctx := c.Request.Context()
	items := make([]channelMemberView, 0, len(rows))
	for i := range rows {
		items = append(items, h.memberView(ctx, &rows[i]))
	}
	RespondOK(c, gin.H{"members": items})
}

// PatchChannelMember rewrites the per-channel specialization note and/or the
// member's facilitator role (channel-teams D2). Assigning facilitator when
// the channel already has one is a 409 (domain.ErrChannelFacilitatorExists).
func (h *channelHandlers) PatchChannelMember(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	memberID := c.Param("mid")

	var req channelMemberPatchRequest
	if !bindChannelRequest(c, &req) {
		return
	}
	if req.Specialization == nil && req.Role == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	ctx := c.Request.Context()
	if req.Role != nil {
		role := domain.ChannelMemberRole(strings.TrimSpace(*req.Role))
		switch role {
		case domain.ChannelMemberRoleMember, domain.ChannelMemberRoleFacilitator:
		default:
			respondChannelValidation(c, ErrorDetail{Field: "role", Message: fmt.Sprintf("role %q must be %q or %q", *req.Role, domain.ChannelMemberRoleMember, domain.ChannelMemberRoleFacilitator)})
			return
		}
		if err := h.channels.UpdateChannelMemberRole(ctx, ws.ID, channel.ID, memberID, role); err != nil {
			respondChannelMemberError(c, err)
			return
		}
	}
	if req.Specialization != nil {
		if err := h.channels.UpdateChannelMemberSpecialization(ctx, ws.ID, channel.ID, memberID, *req.Specialization); err != nil {
			respondChannelMemberError(c, err)
			return
		}
	}

	// The row came back untouched except for the patched fields; resolve it
	// from the roster for the response view.
	rows, err := h.channels.ListChannelMembers(ctx, ws.ID, channel.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	for i := range rows {
		if rows[i].ID == memberID {
			RespondOK(c, gin.H{"member": h.memberView(ctx, &rows[i])})
			return
		}
	}
	RespondError(c, domain.ErrNotFound)
}

// respondChannelMemberError maps roster-mutation failures: the
// single-facilitator conflict is a 409 with a clean message (channel-teams
// D2); everything else rides the sentinel chain.
func respondChannelMemberError(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrChannelFacilitatorExists) {
		AbortWithError(c, http.StatusConflict, CodeConflict, "channel already has a facilitator")
		return
	}
	RespondError(c, err)
}

// RemoveChannelMember detaches one roster row.
func (h *channelHandlers) RemoveChannelMember(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	if err := h.channels.RemoveChannelMember(c.Request.Context(), ws.ID, channel.ID, c.Param("mid")); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// -------------------------------------------------------------------------
// Messages (feed)
// -------------------------------------------------------------------------

// ListMessages serves the feed in ascending seq order, strictly after the
// `after` cursor (0 selects from the beginning). `limit` is optional; absent
// means uncapped, present must be positive and caps at
// DefaultChannelMessageLimit (the session-events list bounds).
func (h *channelHandlers) ListMessages(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}

	after := int64(0)
	if s := c.Query("after"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			RespondError(c, fmt.Errorf("%w: after must be a non-negative integer", domain.ErrInvalid))
			return
		}
		after = n
	}
	limit := 0
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			RespondError(c, fmt.Errorf("%w: limit must be a positive integer", domain.ErrInvalid))
			return
		}
		if n > DefaultChannelMessageLimit {
			n = DefaultChannelMessageLimit
		}
		limit = n
	}

	rows, err := h.channels.ListChannelMessages(c.Request.Context(), store.ListChannelMessagesParams{
		WorkspaceID: ws.ID,
		ChannelID:   channel.ID,
		AfterSeq:    after,
		Limit:       limit,
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	if rows == nil {
		rows = []domain.ChannelMessage{}
	}
	RespondOK(c, gin.H{"messages": rows})
}

// PostMessage persists a human post through the chokepoint (D2) with the
// authenticated user as the author; mention resolution, the silence policy,
// and fan-out all ride that single pipeline. An `is_kickoff` post enters the
// channel-teams branch instead (D1): the chokepoint mints the work session
// rooted at the message and summons the facilitator, and the response carries
// both the message and the minted session. An already-active session is a
// 409; a channel without a facilitator is a fielded 422.
func (h *channelHandlers) PostMessage(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	user := MustCurrentUser(c)

	var req channelMessagePostRequest
	if !bindChannelRequest(c, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		respondChannelValidation(c, ErrorDetail{Field: "body", Message: "body cannot be empty"})
		return
	}

	author := channels.Author{
		Type:   string(domain.ChannelMemberTypeUser),
		UserID: user.ID,
	}

	if req.IsKickoff {
		msg, session, err := h.kickoff.PostKickoff(c.Request.Context(), ws.ID, channel.ID, author, req.Body)
		if err != nil {
			respondKickoffError(c, err)
			return
		}
		RespondJSON(c, http.StatusCreated, gin.H{"message": msg, "session": session})
		return
	}

	msg, err := h.poster.Post(c.Request.Context(), ws.ID, channel.ID, author, req.Body)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondJSON(c, http.StatusCreated, gin.H{"message": msg})
}

// respondKickoffError maps the chokepoint's kickoff failures: an open or
// paused session already rooted in the channel is a 409 (design D1 — one
// session per channel at a time); validation failures (empty goal text, no
// facilitator member on the roster) are fielded 422s; everything else rides
// the sentinel chain.
func respondKickoffError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrWorkSessionActive):
		AbortWithError(c, http.StatusConflict, CodeConflict, "channel already has an active work session")
	case errors.Is(err, domain.ErrInvalid):
		respondChannelValidation(c, ErrorDetail{Field: "body", Message: err.Error()})
	default:
		RespondError(c, err)
	}
}

// -------------------------------------------------------------------------
// SSE event stream
// -------------------------------------------------------------------------

// StreamEvents serves the channel's live feed events as SSE (`data: <json>\n\n`
// frames with `: keepalive` comments) — message_posted, summon_considering,
// summon_decided, run_started, run_finished (D11). Events carry their feed seq
// on the wire; seq-keyed dedup against the REST load is a client concern. The
// subscription is released on every exit path so abandoned connections never
// leak subscribers.
func (h *channelHandlers) StreamEvents(c *gin.Context) {
	_, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}

	// Subscribe before the headers go out: events emitted while the response
	// writes buffer on the subscription instead of being missed.
	events, unsubscribe := h.hub.Subscribe(channel.ID)
	defer unsubscribe()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	// Ask reverse proxies not to buffer frames (nginx honors this).
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	// Flush immediately so the client's Do() returns and its event loop can
	// start; nothing else is written until the first event or keepalive.
	c.Writer.Flush()

	keepalive := time.NewTicker(h.keepAlive)
	defer keepalive.Stop()

	for {
		select {
		case ev, open := <-events:
			if !open {
				// The hub tore the subscription down: the stream is over.
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
			c.Writer.Flush()
		case <-keepalive.C:
			_, _ = c.Writer.Write([]byte(": keepalive\n\n"))
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			// The browser went away — stop pumping into the dead socket.
			return
		}
	}
}
