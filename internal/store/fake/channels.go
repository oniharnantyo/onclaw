package fake

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// channelData is the channel area's state, grouped so the fakeStore struct
// carries a single field for the whole area (snapshot-cloned with everything
// else).
type channelData struct {
	channels     map[string]*domain.Channel        // key: ID
	channelSlugs map[string]string                 // key: workspaceID + ":" + lower(slug) -> ID
	members      map[string]*domain.ChannelMember  // key: ID
	memberRefs   map[string]string                 // key: channelID + ":" + memberType + ":" + refID -> member ID
	messages     map[string]*domain.ChannelMessage // key: ID
	feeds        map[string][]string               // key: channelID -> message IDs in ascending seq order
	seqs         map[string]int64                  // key: channelID -> last assigned seq
	workSessions map[string]*domain.WorkSession    // key: ID
}

func newChannelData() *channelData {
	return &channelData{
		channels:     make(map[string]*domain.Channel),
		channelSlugs: make(map[string]string),
		members:      make(map[string]*domain.ChannelMember),
		memberRefs:   make(map[string]string),
		messages:     make(map[string]*domain.ChannelMessage),
		feeds:        make(map[string][]string),
		seqs:         make(map[string]int64),
		workSessions: make(map[string]*domain.WorkSession),
	}
}

func (d *channelData) clone() *channelData {
	cp := newChannelData()
	for id, c := range d.channels {
		cp.channels[id] = cloneChannel(c)
	}
	for key, id := range d.channelSlugs {
		cp.channelSlugs[key] = id
	}
	for id, m := range d.members {
		cp.members[id] = cloneChannelMember(m)
	}
	for key, id := range d.memberRefs {
		cp.memberRefs[key] = id
	}
	for id, m := range d.messages {
		cp.messages[id] = cloneChannelMessage(m)
	}
	for chID, ids := range d.feeds {
		copied := make([]string, len(ids))
		copy(copied, ids)
		cp.feeds[chID] = copied
	}
	for chID, seq := range d.seqs {
		cp.seqs[chID] = seq
	}
	for id, s := range d.workSessions {
		cp.workSessions[id] = cloneWorkSession(s)
	}
	return cp
}

// Channels returns the ChannelStore sub-port.
func (s *fakeStore) Channels() store.ChannelStore {
	return &channelStore{s: s}
}

func cloneChannel(c *domain.Channel) *domain.Channel {
	if c == nil {
		return nil
	}
	cp := *c
	if c.CreatedBy != nil {
		cb := *c.CreatedBy
		cp.CreatedBy = &cb
	}
	return &cp
}

func cloneChannelMember(m *domain.ChannelMember) *domain.ChannelMember {
	if m == nil {
		return nil
	}
	cp := *m
	return &cp
}

func cloneChannelMessage(m *domain.ChannelMessage) *domain.ChannelMessage {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Mentions = cloneMentions(m.Mentions)
	if m.SessionID != nil {
		sid := *m.SessionID
		cp.SessionID = &sid
	}
	if m.TurnID != nil {
		tid := *m.TurnID
		cp.TurnID = &tid
	}
	if m.RunSummary != nil {
		rs := *m.RunSummary
		if m.RunSummary.Tools != nil {
			rs.Tools = make(map[string]int, len(m.RunSummary.Tools))
			for tool, n := range m.RunSummary.Tools {
				rs.Tools[tool] = n
			}
		}
		cp.RunSummary = &rs
	}
	if m.RootMessageID != nil {
		rid := *m.RootMessageID
		cp.RootMessageID = &rid
	}
	if m.WorkSessionID != nil {
		wid := *m.WorkSessionID
		cp.WorkSessionID = &wid
	}
	return &cp
}

func cloneWorkSession(s *domain.WorkSession) *domain.WorkSession {
	if s == nil {
		return nil
	}
	cp := *s
	if s.ClosedAt != nil {
		t := *s.ClosedAt
		cp.ClosedAt = &t
	}
	return &cp
}

func cloneMentions(src []domain.Mention) []domain.Mention {
	if src == nil {
		return nil
	}
	cp := make([]domain.Mention, len(src))
	copy(cp, src)
	return cp
}

// channelMemberRefKey builds the roster-identity key enforcing one row per
// (channel, member type, reference) — the partial unique indexes' shape.
func channelMemberRefKey(channelID string, memberType domain.ChannelMemberType, refID string) string {
	return channelID + ":" + string(memberType) + ":" + refID
}

// -------------------------------------------------------------------------
// Channels implementation
// -------------------------------------------------------------------------

type channelStore struct {
	s *fakeStore
}

func (cs *channelStore) CreateChannel(ctx context.Context, channel *domain.Channel) error {
	if channel == nil {
		return domain.ErrInvalid
	}
	if err := channel.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if _, exists := cs.s.workspaces[channel.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	slugKey := channel.WorkspaceID + ":" + strings.ToLower(channel.Slug)
	if _, exists := cs.s.channels.channelSlugs[slugKey]; exists {
		return fmt.Errorf("%w: channel slug %q already taken in workspace", domain.ErrChannelSlugConflict, channel.Slug)
	}

	if channel.ID != "" {
		if _, exists := cs.s.channels.channels[channel.ID]; exists {
			return fmt.Errorf("%w: channel with id %q already exists", domain.ErrConflict, channel.ID)
		}
	} else {
		channel.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if channel.CreatedAt.IsZero() {
		channel.CreatedAt = now
	}
	if channel.UpdatedAt.IsZero() {
		channel.UpdatedAt = now
	}

	cs.s.channels.channels[channel.ID] = cloneChannel(channel)
	cs.s.channels.channelSlugs[slugKey] = channel.ID
	return nil
}

func (cs *channelStore) ChannelByID(ctx context.Context, workspaceID, id string) (*domain.Channel, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	channel, exists := cs.s.channels.channels[id]
	if !exists || channel.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneChannel(channel), nil
}

func (cs *channelStore) ChannelBySlug(ctx context.Context, workspaceID, slug string) (*domain.Channel, error) {
	if workspaceID == "" || slug == "" {
		return nil, domain.ErrNotFound
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	id, exists := cs.s.channels.channelSlugs[workspaceID+":"+strings.ToLower(slug)]
	if !exists {
		return nil, domain.ErrNotFound
	}
	channel := cs.s.channels.channels[id]
	if channel == nil || channel.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneChannel(channel), nil
}

func (cs *channelStore) ListChannels(ctx context.Context, workspaceID string) ([]domain.Channel, error) {
	if workspaceID == "" {
		return []domain.Channel{}, nil
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	channels := make([]domain.Channel, 0)
	for _, channel := range cs.s.channels.channels {
		if channel.WorkspaceID == workspaceID {
			channels = append(channels, *cloneChannel(channel))
		}
	}
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].CreatedAt.Equal(channels[j].CreatedAt) {
			return channels[i].ID < channels[j].ID
		}
		return channels[i].CreatedAt.Before(channels[j].CreatedAt)
	})
	return channels, nil
}

func (cs *channelStore) UpdateChannel(ctx context.Context, channel *domain.Channel) error {
	if channel == nil || channel.ID == "" || channel.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := channel.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	existing, exists := cs.s.channels.channels[channel.ID]
	if !exists || existing.WorkspaceID != channel.WorkspaceID {
		return domain.ErrNotFound
	}

	if !strings.EqualFold(channel.Slug, existing.Slug) {
		slugKey := channel.WorkspaceID + ":" + strings.ToLower(channel.Slug)
		if otherID, exists := cs.s.channels.channelSlugs[slugKey]; exists && otherID != channel.ID {
			return fmt.Errorf("%w: channel slug %q already taken in workspace", domain.ErrChannelSlugConflict, channel.Slug)
		}
		delete(cs.s.channels.channelSlugs, channel.WorkspaceID+":"+strings.ToLower(existing.Slug))
		cs.s.channels.channelSlugs[slugKey] = channel.ID
	}

	existing.Name = channel.Name
	existing.Slug = channel.Slug
	existing.Purpose = channel.Purpose
	existing.Conventions = channel.Conventions
	existing.UpdatedAt = time.Now().UTC()

	*channel = *cloneChannel(existing)
	return nil
}

func (cs *channelStore) DeleteChannel(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	channel, exists := cs.s.channels.channels[id]
	if !exists || channel.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	// Cascade: the channel's roster and feed die with it (the sessions table
	// cascades on channel_id; messages referencing a session die with the
	// channel too).
	for _, memberID := range cs.channelMemberIDsLocked(id) {
		member := cs.s.channels.members[memberID]
		delete(cs.s.channels.memberRefs, channelMemberRefKey(id, member.MemberType, member.RefID()))
		delete(cs.s.channels.members, memberID)
	}
	for _, messageID := range cs.s.channels.feeds[id] {
		delete(cs.s.channels.messages, messageID)
	}
	delete(cs.s.channels.feeds, id)
	delete(cs.s.channels.seqs, id)
	for _, session := range cs.s.channels.workSessions {
		if session != nil && session.ChannelID == id {
			delete(cs.s.channels.workSessions, session.ID)
		}
	}

	// Reference-document channel joins die with the channel
	// (reference_document_channels ON DELETE CASCADE): the fake mirrors the
	// cascade by stripping the id from every stored document's channel list
	// — the join state IS that list.
	for _, doc := range cs.s.referenceDocuments {
		if !slices.Contains(doc.ChannelIDs, id) {
			continue
		}
		remaining := make([]string, 0, len(doc.ChannelIDs))
		for _, channelID := range doc.ChannelIDs {
			if channelID != id {
				remaining = append(remaining, channelID)
			}
		}
		doc.ChannelIDs = remaining
	}

	delete(cs.s.channels.channels, id)
	delete(cs.s.channels.channelSlugs, workspaceID+":"+strings.ToLower(channel.Slug))
	return nil
}

// channelMemberIDsLocked returns the roster ids of one channel in add order.
// Caller must hold cs.s.mu for writing.
func (cs *channelStore) channelMemberIDsLocked(channelID string) []string {
	ids := make([]string, 0, len(cs.s.channels.members))
	for id, member := range cs.s.channels.members {
		if member.ChannelID == channelID {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := cs.s.channels.members[ids[i]], cs.s.channels.members[ids[j]]
		if a.AddedAt.Equal(b.AddedAt) {
			return a.ID < b.ID
		}
		return a.AddedAt.Before(b.AddedAt)
	})
	return ids
}

// -------------------------------------------------------------------------
// Membership implementation
// -------------------------------------------------------------------------

func (cs *channelStore) AddChannelMember(ctx context.Context, member *domain.ChannelMember) error {
	if member == nil {
		return domain.ErrInvalid
	}
	if err := member.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if _, exists := cs.s.workspaces[member.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	channel, exists := cs.s.channels.channels[member.ChannelID]
	if !exists || channel.WorkspaceID != member.WorkspaceID {
		return fmt.Errorf("%w: channel not found in workspace", domain.ErrNotFound)
	}

	// The reference must exist inside the workspace: users must hold a
	// workspace membership, agents must belong to the workspace.
	switch member.MemberType {
	case domain.ChannelMemberTypeUser:
		if _, exists := cs.s.members[member.WorkspaceID+":"+member.UserID]; !exists {
			return fmt.Errorf("%w: user is not a member of the workspace", domain.ErrNotFound)
		}
	case domain.ChannelMemberTypeAgent:
		a, exists := cs.s.agents[member.AgentID]
		if !exists || a.WorkspaceID != member.WorkspaceID {
			return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
		}
	}

	refKey := channelMemberRefKey(member.ChannelID, member.MemberType, member.RefID())
	if _, exists := cs.s.channels.memberRefs[refKey]; exists {
		return fmt.Errorf("%w: %s %q already a member of channel", domain.ErrDuplicateChannelMember, member.MemberType, member.RefID())
	}

	// Facilitator uniqueness (channel-teams D2): at most one per channel —
	// the store-level check the partial unique index backstops in postgres.
	if member.Role == domain.ChannelMemberRoleFacilitator {
		if _, hasFacilitator := cs.facilitatorLocked(member.ChannelID); hasFacilitator {
			return fmt.Errorf("%w: channel %q already has a facilitator", domain.ErrChannelFacilitatorExists, member.ChannelID)
		}
	}

	if member.ID != "" {
		if _, exists := cs.s.channels.members[member.ID]; exists {
			return fmt.Errorf("%w: channel member with id %q already exists", domain.ErrConflict, member.ID)
		}
	} else {
		member.ID = uuid.NewString()
	}

	if member.AddedAt.IsZero() {
		member.AddedAt = time.Now().UTC()
	}

	cs.s.channels.members[member.ID] = cloneChannelMember(member)
	cs.s.channels.memberRefs[refKey] = member.ID
	return nil
}

func (cs *channelStore) RemoveChannelMember(ctx context.Context, workspaceID, channelID, memberID string) error {
	if workspaceID == "" || channelID == "" || memberID == "" {
		return domain.ErrNotFound
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	member, exists := cs.s.channels.members[memberID]
	if !exists || member.WorkspaceID != workspaceID || member.ChannelID != channelID {
		return domain.ErrNotFound
	}

	delete(cs.s.channels.memberRefs, channelMemberRefKey(channelID, member.MemberType, member.RefID()))
	delete(cs.s.channels.members, memberID)
	return nil
}

func (cs *channelStore) UpdateChannelMemberSpecialization(ctx context.Context, workspaceID, channelID, memberID, specialization string) error {
	if workspaceID == "" || channelID == "" || memberID == "" {
		return domain.ErrNotFound
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	member, exists := cs.s.channels.members[memberID]
	if !exists || member.WorkspaceID != workspaceID || member.ChannelID != channelID {
		return domain.ErrNotFound
	}

	member.Specialization = specialization
	return nil
}

// facilitatorLocked returns the channel's facilitator roster row, if any.
// Caller must hold cs.s.mu for writing.
func (cs *channelStore) facilitatorLocked(channelID string) (*domain.ChannelMember, bool) {
	for _, member := range cs.s.channels.members {
		if member.ChannelID == channelID && member.Role == domain.ChannelMemberRoleFacilitator {
			return member, true
		}
	}
	return nil, false
}

func (cs *channelStore) UpdateChannelMemberRole(ctx context.Context, workspaceID, channelID, memberID string, role domain.ChannelMemberRole) error {
	switch role {
	case domain.ChannelMemberRoleMember, domain.ChannelMemberRoleFacilitator:
	default:
		return fmt.Errorf("%w: role %q must be %q or %q", domain.ErrInvalid, role, domain.ChannelMemberRoleMember, domain.ChannelMemberRoleFacilitator)
	}
	if workspaceID == "" || channelID == "" || memberID == "" {
		return domain.ErrNotFound
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	member, exists := cs.s.channels.members[memberID]
	if !exists || member.WorkspaceID != workspaceID || member.ChannelID != channelID {
		return domain.ErrNotFound
	}

	if role == domain.ChannelMemberRoleFacilitator && member.Role != domain.ChannelMemberRoleFacilitator {
		if current, hasFacilitator := cs.facilitatorLocked(channelID); hasFacilitator && current.ID != member.ID {
			return fmt.Errorf("%w: channel %q already has a facilitator", domain.ErrChannelFacilitatorExists, channelID)
		}
	}

	member.Role = role
	return nil
}

func (cs *channelStore) SetChannelMessageWorkSession(ctx context.Context, workspaceID, channelID, messageID string, workSessionID *string) error {
	if workspaceID == "" || channelID == "" || messageID == "" {
		return domain.ErrNotFound
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	message, exists := cs.s.channels.messages[messageID]
	if !exists || message.WorkspaceID != workspaceID || message.ChannelID != channelID {
		return domain.ErrNotFound
	}

	// FK parity: the session must exist in this workspace when linking.
	if workSessionID != nil {
		session, exists := cs.s.channels.workSessions[*workSessionID]
		if !exists || session.WorkspaceID != workspaceID {
			return fmt.Errorf("%w: work session not found in workspace", domain.ErrNotFound)
		}
	}

	if workSessionID == nil {
		message.WorkSessionID = nil
	} else {
		wid := *workSessionID
		message.WorkSessionID = &wid
	}
	return nil
}

func (cs *channelStore) ListChannelMembers(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMember, error) {
	if workspaceID == "" || channelID == "" {
		return []domain.ChannelMember{}, nil
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	members := make([]domain.ChannelMember, 0)
	for _, member := range cs.s.channels.members {
		if member.WorkspaceID == workspaceID && member.ChannelID == channelID {
			members = append(members, *cloneChannelMember(member))
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].AddedAt.Equal(members[j].AddedAt) {
			return members[i].ID < members[j].ID
		}
		return members[i].AddedAt.Before(members[j].AddedAt)
	})
	return members, nil
}

func (cs *channelStore) ChannelMemberByRef(ctx context.Context, workspaceID, channelID string, memberType domain.ChannelMemberType, refID string) (*domain.ChannelMember, error) {
	switch memberType {
	case domain.ChannelMemberTypeUser, domain.ChannelMemberTypeAgent:
	default:
		return nil, fmt.Errorf("%w: member_type %q must be %q or %q", domain.ErrInvalid, memberType, domain.ChannelMemberTypeUser, domain.ChannelMemberTypeAgent)
	}
	if workspaceID == "" || channelID == "" || refID == "" {
		return nil, domain.ErrNotFound
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	memberID, exists := cs.s.channels.memberRefs[channelMemberRefKey(channelID, memberType, refID)]
	if !exists {
		return nil, domain.ErrNotFound
	}
	member := cs.s.channels.members[memberID]
	if member == nil || member.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneChannelMember(member), nil
}

// -------------------------------------------------------------------------
// Messages implementation
// -------------------------------------------------------------------------

func (cs *channelStore) InsertChannelMessage(ctx context.Context, message *domain.ChannelMessage) error {
	if message == nil {
		return domain.ErrInvalid
	}
	if err := message.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if _, exists := cs.s.workspaces[message.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	channel, exists := cs.s.channels.channels[message.ChannelID]
	if !exists || channel.WorkspaceID != message.WorkspaceID {
		return fmt.Errorf("%w: channel not found in workspace", domain.ErrNotFound)
	}

	// The author must exist inside the workspace (users hold a workspace
	// membership; agents belong to the workspace).
	switch message.AuthorType {
	case domain.ChannelMemberTypeUser:
		if _, exists := cs.s.members[message.WorkspaceID+":"+message.AuthorUserID]; !exists {
			return fmt.Errorf("%w: author is not a member of the workspace", domain.ErrNotFound)
		}
	case domain.ChannelMemberTypeAgent:
		a, exists := cs.s.agents[message.AuthorAgentID]
		if !exists || a.WorkspaceID != message.WorkspaceID {
			return fmt.Errorf("%w: author agent not found in workspace", domain.ErrNotFound)
		}
	}

	// The chain root, when present, must be an existing message of the same
	// channel (FK parity: unknown roots are domain.ErrNotFound).
	if message.RootMessageID != nil && *message.RootMessageID != "" {
		root, exists := cs.s.channels.messages[*message.RootMessageID]
		if !exists || root.ChannelID != message.ChannelID || root.WorkspaceID != message.WorkspaceID {
			return fmt.Errorf("%w: root message not found in channel", domain.ErrNotFound)
		}
	}

	// The work session link, when present, must be an existing session of the
	// same workspace (FK parity with channel_messages.work_session_id).
	if message.WorkSessionID != nil && *message.WorkSessionID != "" {
		session, exists := cs.s.channels.workSessions[*message.WorkSessionID]
		if !exists || session.WorkspaceID != message.WorkspaceID {
			return fmt.Errorf("%w: work session not found in workspace", domain.ErrNotFound)
		}
	}

	if message.ID != "" {
		if _, exists := cs.s.channels.messages[message.ID]; exists {
			return fmt.Errorf("%w: channel message with id %q already exists", domain.ErrConflict, message.ID)
		}
	} else {
		message.ID = uuid.NewString()
	}

	// seq is DB-assigned in postgres; the fake mirrors it with a per-channel
	// monotonic counter starting at 1.
	cs.s.channels.seqs[message.ChannelID]++
	message.Seq = cs.s.channels.seqs[message.ChannelID]

	if message.Mentions == nil {
		message.Mentions = []domain.Mention{}
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}

	cs.s.channels.messages[message.ID] = cloneChannelMessage(message)
	cs.s.channels.feeds[message.ChannelID] = append(cs.s.channels.feeds[message.ChannelID], message.ID)
	return nil
}

func (cs *channelStore) ListChannelMessages(ctx context.Context, params store.ListChannelMessagesParams) ([]domain.ChannelMessage, error) {
	if params.WorkspaceID == "" || params.ChannelID == "" {
		return []domain.ChannelMessage{}, nil
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	messages := make([]domain.ChannelMessage, 0)
	for _, id := range cs.s.channels.feeds[params.ChannelID] {
		message := cs.s.channels.messages[id]
		if message == nil || message.WorkspaceID != params.WorkspaceID {
			continue
		}
		if message.Seq <= params.AfterSeq {
			continue
		}
		messages = append(messages, *cloneChannelMessage(message))
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].Seq < messages[j].Seq })
	if params.Limit > 0 && len(messages) > params.Limit {
		messages = messages[:params.Limit]
	}
	return messages, nil
}

func (cs *channelStore) ChannelMessagesByRoot(ctx context.Context, workspaceID, channelID, rootMessageID string) ([]domain.ChannelMessage, error) {
	if workspaceID == "" || channelID == "" || rootMessageID == "" {
		return []domain.ChannelMessage{}, nil
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	messages := make([]domain.ChannelMessage, 0)
	for _, id := range cs.s.channels.feeds[channelID] {
		message := cs.s.channels.messages[id]
		if message == nil || message.WorkspaceID != workspaceID {
			continue
		}
		if message.RootMessageID == nil || *message.RootMessageID != rootMessageID {
			continue
		}
		messages = append(messages, *cloneChannelMessage(message))
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].Seq < messages[j].Seq })
	return messages, nil
}

func (cs *channelStore) UpdateChannelMessageRunSummary(ctx context.Context, workspaceID, channelID, sessionID, turnID string, summary domain.RunSummary) error {
	if workspaceID == "" || channelID == "" || sessionID == "" {
		return domain.ErrInvalid
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if _, exists := cs.s.channels.channels[channelID]; !exists {
		return domain.ErrNotFound
	}

	for _, id := range cs.s.channels.feeds[channelID] {
		message := cs.s.channels.messages[id]
		if message == nil || message.WorkspaceID != workspaceID {
			continue
		}
		if message.SessionID == nil || *message.SessionID != sessionID {
			continue
		}
		// An empty turnID matches NULL turn_id rows (posts that raced
		// turn-id discovery); otherwise the turn must match exactly.
		if turnID == "" {
			if message.TurnID != nil {
				continue
			}
		} else if message.TurnID == nil || *message.TurnID != turnID {
			continue
		}

		// Reset onto the stored row: a fresh summary, deep-copied.
		stored := summary
		if stored.Tools != nil {
			tools := make(map[string]int, len(stored.Tools))
			for tool, n := range stored.Tools {
				tools[tool] = n
			}
			stored.Tools = tools
		}
		message.RunSummary = &stored
	}
	return nil
}
