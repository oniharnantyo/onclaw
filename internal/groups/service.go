// Package groups implements the agent-group service (add-agent-groups
// design D2-D8): the shared authored transcript, deterministic routing with
// a lead default, the revision-guarded task lifecycle with dispatch budgets,
// per-member real sessions fed by context packets, copy-back attribution,
// versioned transcript summaries, restart recovery, and agent-deletion
// cleanup. It sits on the store's AgentGroupStore and drives the agent
// runtime through the narrow Manager seam (agents.Manager satisfies it);
// live UI push goes through the Events callback interface so this package
// never imports internal/services or any frontend package.
package groups

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Manager is the slice of the agent runtime the groups service drives:
// member-session creation, group turn dispatch, lane-free summarization,
// live session subscriptions, and turn cancellation. agents.Manager
// satisfies it; it is declared narrowly so tests fake only what the service
// touches (add-agent-groups tasks 3.1).
type Manager interface {
	// CreateSession registers a new, untitled chat session for the agent —
	// the same path ordinary sessions take (design D2: member threads are
	// real sessions).
	CreateSession(ctx context.Context, agentID string) (sessions.Session, error)
	// SubmitGroup dispatches one group member turn from a ready-made packet;
	// it queues on the agent's lane and never fails with
	// agents.ErrTurnInProgress.
	SubmitGroup(ctx context.Context, req agents.GroupTurnRequest) (*agents.Turn, error)
	// SummarizeText runs one direct summarizer call on the agent's model
	// without acquiring its turn lane (design D3).
	SummarizeText(ctx context.Context, agentID, instruction, text string) (string, error)
	// Subscribe registers a live subscription for the session's events (the
	// copy-back pump's source, design D7).
	Subscribe(ctx context.Context, sessionID string) (agents.Subscription, error)
	// Cancel stops the session's in-flight turn and drops its queued group
	// dispatches (task interruption, design D8).
	Cancel(ctx context.Context, sessionID string) error
}

// GroupEventKind discriminates per-group live updates — the
// group:<id>:event payload kinds (design D9). The frontend refetches tasks
// for the group on a task event; an appended message is carried inline so an
// open transcript appends without a refetch.
type GroupEventKind string

const (
	// GroupEventMessageAppended announces one appended transcript row;
	// MessageID and Seq name it and Message carries the row.
	GroupEventMessageAppended GroupEventKind = "message-appended"
	// GroupEventTaskUpdated announces a task's state/owner/revision change;
	// TaskID names it.
	GroupEventTaskUpdated GroupEventKind = "task-updated"
)

// GroupEvent is one per-group live update pushed through Events.GroupEvent
// (design D9). Exactly the fields matching Kind are set.
type GroupEvent struct {
	// Kind is the update's kind — one of the GroupEventKind values.
	Kind GroupEventKind `json:"kind"`
	// GroupID names the group the update belongs to.
	GroupID string `json:"groupId"`
	// MessageID and Seq name the appended transcript row
	// (GroupEventMessageAppended only).
	MessageID string `json:"messageId,omitempty"`
	Seq       int64  `json:"seq,omitempty"`
	// Message carries the appended transcript row
	// (GroupEventMessageAppended only), so an open transcript appends it
	// without a refetch.
	Message *store.AgentGroupMessage `json:"message,omitempty"`
	// TaskID names the task whose state changed (GroupEventTaskUpdated
	// only).
	TaskID string `json:"taskId,omitempty"`
}

// Events is the UI-push seam (design D9): the services layer implements it
// over the Wails emitter (groups:changed and group:<id>:event).
// Implementations must not call back into the Service synchronously —
// events fire while the triggering group's write lock is held, and re-entry
// would deadlock.
type Events interface {
	// GroupChanged announces a roster- or list-level change (group created,
	// edited, deleted, membership or lead change): the frontend refetches
	// the group list.
	GroupChanged()
	// GroupEvent announces one per-group transcript or task update.
	GroupEvent(groupID string, ev GroupEvent)
}

// SendRequest is one user message into a group (add-agent-groups design D4):
// the text with optional attachment metadata, an optional explicit recipient
// from the composer's mention picker, and an optional reply target.
type SendRequest struct {
	// GroupID names the group the message goes to.
	GroupID string
	// Text is the message body; mentions (@Name, @everyone) are parsed from
	// it host-side.
	Text string
	// Attachments carries the uploaded files riding the message; empty for
	// a plain text send.
	Attachments []sessions.Attachment
	// RecipientAgentID names the member the mention picker chose; the same
	// mechanism as a typed mention (no coordinator line). Empty when the
	// message is untargeted. A non-roster id falls through to the normal
	// tiers.
	RecipientAgentID string
	// ReplyToMessageID pins the transcript row this message replies to;
	// empty when not a reply.
	ReplyToMessageID string
}

// groupPayload is the transcript rows' JSON payload schema, owned by this
// package (design D1: the payload column is opaque JSON the service owns).
// Text carries the row's display text; Attachments is set on user message
// rows only.
type groupPayload struct {
	Text        string                `json:"text,omitempty"`
	Attachments []sessions.Attachment `json:"attachments,omitempty"`
}

// makePayload marshals the row payload; an empty payload marshals to "" so
// textless rows store no JSON. The string (not []byte) keeps the bindings
// honest: Wails base64-marshals byte slices, which the frontend cannot
// JSON-decode.
func makePayload(text string, attachments []sessions.Attachment) (string, error) {
	p := groupPayload{Text: text, Attachments: attachments}
	if p.Text == "" && len(p.Attachments) == 0 {
		return "", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal group message payload: %w", err)
	}
	return string(b), nil
}

// payloadText decodes a row's display text.
func payloadText(m store.AgentGroupMessage) string {
	if m.Payload == "" {
		return ""
	}
	var p groupPayload
	if err := json.Unmarshal([]byte(m.Payload), &p); err != nil {
		return ""
	}
	return p.Text
}

// Service implements the agent-group aggregate (add-agent-groups tasks
// section 3). All group mutations serialize per group through a per-group
// write lock, so member-session copy-back and routing mutations never
// interleave SQLite writes for one group (design D7).
type Service struct {
	deps Deps

	// mu guards the in-memory maps below.
	mu sync.Mutex
	// pumps holds one live copy-back pump per (group, member) pair, keyed by
	// (groupID, agentID) (design D7).
	pumps map[groupKey]*groupPump
	// inflight holds each dispatched member turn's task and the revision the
	// dispatch started from, keyed by (groupID, agentID) — the in-channel
	// tools resolve the calling task through it (design D4).
	inflight map[groupKey]*inflightTurn
	// checkpoints fingerprints each member session's compaction checkpoint
	// at packet-build and cursor-commit time (design D3: a changed
	// fingerprint rebuilds the full packet).
	checkpoints map[groupKey]checkpointFingerprint
	// summarizing marks groups with a summary build in flight so concurrent
	// member turns do not double-summarize (design D3).
	summarizing map[string]bool

	// writeMu guards writeLocks.
	writeMu sync.Mutex
	// writeLocks holds one serialization lock per group, created on first
	// use. Never take two group locks at once.
	writeLocks map[string]*sync.Mutex
}

// groupKey is the composite identity of per-member service state: (group id,
// agent id).
type groupKey struct {
	GroupID string
	AgentID string
}

// inflightTurn is one dispatched member turn's service-side record: the task
// it works and the task revision the dispatch started from — the revision
// guard's baseline (design D8) — plus the packet's coverage, which the
// summary path keys off after the turn.
type inflightTurn struct {
	TaskID string
	// DispatchRev is the task's Revision as this dispatch claimed it; a
	// turn-end handler that reads a different revision is stale and writes
	// nothing.
	DispatchRev int64
	// ThroughSeq is the transcript sequence the dispatch's packet covered.
	ThroughSeq int64
	// UnreadCount is the packet's unread-row count — the summary
	// threshold's input (design D3).
	UnreadCount int
}

// Deps carries the service's constructor-injected dependencies; all are
// required and assumed non-nil (nil is a wiring bug).
type Deps struct {
	// Store is the app store; the service uses Groups() plus Agents() for
	// member names and responsibility lines.
	Store store.Store
	// Sessions reads member sessions' compaction checkpoints — the
	// full-packet signal (design D3). Session creation goes through the
	// manager, not this. Wire sessions.NewService(store).
	Sessions sessions.Service
	// Manager is the agent runtime seam (agents.Manager satisfies it).
	Manager Manager
	// Ref is the late-binding group turn source holder: the constructor
	// binds the service into it so group dispatches reach this service
	// (add-agent-groups task 4.3).
	Ref *agents.GroupTurnRef
	// Events pushes roster and per-group updates to the UI (design D9).
	Events Events
}

// New constructs the groups service and binds it as the runtime's group
// turn source through deps.Ref.
func New(d Deps) *Service {
	s := &Service{
		deps:        d,
		pumps:       make(map[groupKey]*groupPump),
		inflight:    make(map[groupKey]*inflightTurn),
		checkpoints: make(map[groupKey]checkpointFingerprint),
		summarizing: make(map[string]bool),
		writeLocks:  make(map[string]*sync.Mutex),
	}
	d.Ref.Set(s)
	return s
}

// groupLock returns the group's serialization lock, creating it on first
// use. Never take two group locks at once.
func (s *Service) groupLock(groupID string) *sync.Mutex {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	l, ok := s.writeLocks[groupID]
	if !ok {
		l = &sync.Mutex{}
		s.writeLocks[groupID] = l
	}
	return l
}

// fireGroupEvent pushes one per-group update; a panicking listener is
// contained — a dead listener must not break a committed mutation.
func (s *Service) fireGroupEvent(ev GroupEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("groups: group event listener panicked", "group", ev.GroupID, "panic", r)
		}
	}()
	s.deps.Events.GroupEvent(ev.GroupID, ev)
}

// fireGroupChanged pushes a roster/list-level change; same panic guard as
// fireGroupEvent.
func (s *Service) fireGroupChanged() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("groups: groups-changed listener panicked", "panic", r)
		}
	}()
	s.deps.Events.GroupChanged()
}

// CreateGroup creates a group with the given name, optional title and
// instructions, and roster. The first member becomes the lead (design D5);
// duplicate agent ids in the roster collapse to one membership. Returns
// store.ErrNotFound when a roster agent does not exist.
func (s *Service) CreateGroup(ctx context.Context, name, title, instructions string, memberAgentIDs []string) (*store.AgentGroup, error) {
	if name == "" {
		return nil, fmt.Errorf("create group: name is required")
	}
	g := &store.AgentGroup{Name: name, Title: title, Instructions: instructions}
	if err := s.deps.Store.Groups().CreateGroup(ctx, g); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	seen := make(map[string]bool, len(memberAgentIDs))
	for _, agentID := range memberAgentIDs {
		if seen[agentID] {
			continue
		}
		seen[agentID] = true
		if err := s.AddMember(ctx, g.ID, agentID); err != nil {
			return nil, err
		}
	}
	s.fireGroupChanged()
	out, err := s.deps.Store.Groups().GroupByID(ctx, g.ID)
	if err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	return out, nil
}

// UpdateGroup rewrites the group's editable fields (name, title,
// instructions), keeping lead, roster, and transcript untouched.
func (s *Service) UpdateGroup(ctx context.Context, groupID, name, title, instructions string) (*store.AgentGroup, error) {
	l := s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()

	g, err := s.deps.Store.Groups().GroupByID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	g.Name = name
	g.Title = title
	g.Instructions = instructions
	g.Revision++
	if err := s.deps.Store.Groups().UpdateGroup(ctx, g); err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	out, err := s.deps.Store.Groups().GroupByID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	s.fireGroupChanged()
	return out, nil
}

// DeleteGroup removes the group and — through the store's cascades — its
// roster, transcript, cursors, tasks, and summary. Member sessions (real
// sessions) survive: they are ordinary sessions, not group-owned.
func (s *Service) DeleteGroup(ctx context.Context, groupID string) error {
	l := s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()

	if _, err := s.deps.Store.Groups().GroupByID(ctx, groupID); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	s.closeGroupPumps(groupID)
	if err := s.deps.Store.Groups().DeleteGroup(ctx, groupID); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	s.fireGroupChanged()
	return nil
}

// Group returns one group; store.ErrNotFound when absent.
func (s *Service) Group(ctx context.Context, groupID string) (*store.AgentGroup, error) {
	g, err := s.deps.Store.Groups().GroupByID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	return g, nil
}

// ListGroups returns every group ordered by creation time. The result is
// never nil.
func (s *Service) ListGroups(ctx context.Context) ([]store.AgentGroup, error) {
	groups, err := s.deps.Store.Groups().ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, nil
}

// Members returns the group's roster in add order. The result is never nil.
func (s *Service) Members(ctx context.Context, groupID string) ([]store.AgentGroupMember, error) {
	members, err := s.deps.Store.Groups().ListMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return members, nil
}

// AddMember adds one agent to the roster: a real member session is created
// through the manager (design D2), the acceptance cursor is seeded, the
// first member becomes the lead (design D5), and a change line lands in the
// transcript. Adding the same agent twice fails with store.ErrDuplicate.
func (s *Service) AddMember(ctx context.Context, groupID, agentID string) error {
	l := s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()

	g, err := s.deps.Store.Groups().GroupByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("add group member: %w", err)
	}
	agent, err := s.deps.Store.Agents().ByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("add group member: %w", err)
	}

	// The member's real session (design D2): the same creation path
	// ordinary sessions take, then the cursor row seeds the acceptance
	// state.
	sess, err := s.deps.Manager.CreateSession(ctx, agentID)
	if err != nil {
		return fmt.Errorf("add group member: create member session: %w", err)
	}
	if err := s.deps.Store.Groups().SetCursor(ctx, store.AgentGroupContext{
		GroupID:   groupID,
		AgentID:   agentID,
		SessionID: sess.ID,
	}); err != nil {
		return fmt.Errorf("add group member: seed cursor: %w", err)
	}
	if err := s.deps.Store.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: groupID, AgentID: agentID}); err != nil {
		return fmt.Errorf("add group member: %w", err)
	}

	// Lead rules (design D5): the first member of a leadless group becomes
	// the lead.
	if g.LeadAgentID == nil {
		g.LeadAgentID = &agentID
		if err := s.deps.Store.Groups().UpdateGroup(ctx, g); err != nil {
			return fmt.Errorf("add group member: assign lead: %w", err)
		}
	}

	if _, err := s.appendChangeLine(ctx, groupID, "added "+agent.Name); err != nil {
		return err
	}
	s.ensurePump(groupID, agentID, sess.ID)
	s.fireGroupChanged()
	slog.Debug("groups: member added", "group", groupID, "agent", agentID)
	return nil
}

// RemoveMember drops one roster row. The member's session and past
// contributions survive (spec: removing a member); the member stops
// receiving work: its live copy-back pump closes, an in-flight turn is
// interrupted, and its open tasks pause with a visible reason. When the
// removed member was the lead, the lead falls to the first remaining member
// (design D5) and a member-less group keeps no lead.
func (s *Service) RemoveMember(ctx context.Context, groupID, agentID string) error {
	if _, err := s.deps.Store.Groups().GroupByID(ctx, groupID); err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	return s.removeMemberFromGroup(ctx, groupID, agentID, "member removed from group")
}

// SetLead assigns the lead to a roster member (design D5: exactly one lead);
// the member must be in the roster. Tasks that already have owners keep
// them.
func (s *Service) SetLead(ctx context.Context, groupID, agentID string) error {
	l := s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()

	if _, err := s.deps.Store.Groups().GroupByID(ctx, groupID); err != nil {
		return fmt.Errorf("set lead: %w", err)
	}
	members, err := s.deps.Store.Groups().ListMembers(ctx, groupID)
	if err != nil {
		return fmt.Errorf("set lead: %w", err)
	}
	inRoster := false
	for _, m := range members {
		if m.AgentID == agentID {
			inRoster = true
			break
		}
	}
	if !inRoster {
		return fmt.Errorf("set lead: agent %s is not a member of group %s: %w", agentID, groupID, store.ErrNotFound)
	}
	g, err := s.deps.Store.Groups().GroupByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("set lead: %w", err)
	}
	g.LeadAgentID = &agentID
	g.Revision++
	if err := s.deps.Store.Groups().UpdateGroup(ctx, g); err != nil {
		return fmt.Errorf("set lead: %w", err)
	}
	s.fireGroupChanged()
	return nil
}

// Send appends the user message to the transcript and routes it (design D4):
// a mention, the explicit recipient, a task or member reply, a sole-open-task
// follow-up, a single-member group, or the lead. Task-targeted routes write a
// coordinator assigned line; mention-routed and lead-default messages add
// none. Each routed recipient gets a root task (or continues the follow-up
// task) and a member dispatch, budget-guarded.
func (s *Service) Send(ctx context.Context, req SendRequest) error {
	l := s.groupLock(req.GroupID)
	l.Lock()
	defer l.Unlock()

	g, err := s.deps.Store.Groups().GroupByID(ctx, req.GroupID)
	if err != nil {
		return fmt.Errorf("send to group: %w", err)
	}
	members, err := s.deps.Store.Groups().ListMembers(ctx, req.GroupID)
	if err != nil {
		return fmt.Errorf("send to group: %w", err)
	}
	roster, err := s.loadRoster(ctx, g, members)
	if err != nil {
		return fmt.Errorf("send to group: %w", err)
	}

	// The user message lands first, whatever the routing decides.
	payload, err := makePayload(req.Text, req.Attachments)
	if err != nil {
		return fmt.Errorf("send to group: %w", err)
	}
	msg := &store.AgentGroupMessage{
		GroupID:    req.GroupID,
		Kind:       store.GroupMessageKindMessage,
		AuthorKind: store.GroupAuthorKindUser,
		Payload:    payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(ctx, msg); err != nil {
		return fmt.Errorf("send to group: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: req.GroupID, MessageID: msg.MessageID, Seq: msg.Seq, Message: msg})

	if len(roster.Members) == 0 {
		return nil // a member-less group accepts the message; nothing routes
	}

	// Reply context: the replied-to row's task link and member author.
	reply, err := s.resolveReplyTarget(ctx, req.GroupID, req.ReplyToMessageID)
	if err != nil {
		return fmt.Errorf("send to group: %w", err)
	}
	openTasks, err := s.openTasks(ctx, req.GroupID)
	if err != nil {
		return fmt.Errorf("send to group: %w", err)
	}

	decision := routeMessage(routingInput{
		Text:              req.Text,
		Roster:            roster,
		ExplicitRecipient: req.RecipientAgentID,
		Reply:             reply,
		OpenTasks:         openTasks,
	})

	for _, recipient := range decision.Recipients {
		if err := s.routeToMember(ctx, req.GroupID, roster, recipient, decision, req, msg.MessageID); err != nil {
			return err
		}
	}
	return nil
}

// routeToMember creates-or-continues the recipient's task for the routed
// message and dispatches it (budget-guarded). The assigned coordinator line
// is written only for targeted routes (design D4).
func (s *Service) routeToMember(ctx context.Context, groupID string, roster routingRoster, recipientAgentID string, d routingDecision, req SendRequest, userMessageID string) error {
	var task *store.AgentGroupTask
	if d.FollowUpTaskID != "" {
		existing, err := s.deps.Store.Groups().TaskByID(ctx, d.FollowUpTaskID)
		if err != nil {
			return fmt.Errorf("send to group: follow-up task: %w", err)
		}
		task = existing
	} else {
		taskID, err := newID()
		if err != nil {
			return fmt.Errorf("send to group: create task: %w", err)
		}
		// Root tasks self-reference: TaskID is assigned up front so
		// RootTaskID can carry it in the single insert (the store keeps the
		// chain fields immutable afterwards).
		task = &store.AgentGroupTask{
			TaskID:           taskID,
			GroupID:          groupID,
			RootTaskID:       taskID,
			OwnerAgentID:     &recipientAgentID,
			RequestMessageID: userMessageID,
			Instruction:      req.Text,
			State:            store.GroupTaskStateQueued,
			Revision:         1,
		}
		if err := s.deps.Store.Groups().CreateTask(ctx, task); err != nil {
			return fmt.Errorf("send to group: create task: %w", err)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: groupID, TaskID: task.TaskID})
	}

	if d.AssignedLine {
		if _, err := s.appendAssignedLine(ctx, groupID, task.TaskID, "Assigned to "+roster.NameOf(recipientAgentID)); err != nil {
			return err
		}
	}

	// A follow-up continues the existing task; a fresh root's first
	// dispatch carries the raw message text.
	if _, err := s.dispatchTask(ctx, task, req.Text, req.Attachments, d.FollowUpTaskID == ""); err != nil {
		return err
	}
	return nil
}

// routingRoster is the loaded roster the routing and Send paths share: the
// member refs in add order plus the lead, with name lookup.
type routingRoster struct {
	Members []memberRef
	// LeadAgentID is the group's lead; empty when the group has no members.
	LeadAgentID string
}

// NameOf returns the member's display name; unknown ids read as the id.
func (r routingRoster) NameOf(agentID string) string {
	for _, m := range r.Members {
		if m.AgentID == agentID {
			return m.Name
		}
	}
	return agentID
}

// leadOf returns the group's lead agent id; empty when the group has none.
func leadOf(g *store.AgentGroup) string {
	if g.LeadAgentID == nil {
		return ""
	}
	return *g.LeadAgentID
}

// loadRoster resolves the roster rows into routing member refs (agent name
// and id, add order) with the group's lead.
func (s *Service) loadRoster(ctx context.Context, g *store.AgentGroup, members []store.AgentGroupMember) (routingRoster, error) {
	roster := routingRoster{Members: make([]memberRef, 0, len(members)), LeadAgentID: leadOf(g)}
	for _, m := range members {
		agent, err := s.deps.Store.Agents().ByID(ctx, m.AgentID)
		if err != nil {
			return routingRoster{}, fmt.Errorf("load roster: %w", err)
		}
		roster.Members = append(roster.Members, memberRef{AgentID: agent.ID, Name: agent.Name})
	}
	return roster, nil
}

// appendChangeLine writes a coordinator change row into the transcript.
func (s *Service) appendChangeLine(ctx context.Context, groupID, text string) (string, error) {
	payload, err := makePayload(text, nil)
	if err != nil {
		return "", fmt.Errorf("append change line: %w", err)
	}
	msg := &store.AgentGroupMessage{
		GroupID:    groupID,
		Kind:       store.GroupMessageKindChange,
		AuthorKind: store.GroupAuthorKindCoordinator,
		Payload:    payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(ctx, msg); err != nil {
		return "", fmt.Errorf("append change line: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: groupID, MessageID: msg.MessageID, Seq: msg.Seq, Message: msg})
	return msg.MessageID, nil
}

// appendAssignedLine writes the coordinator "Assigned to <name>" row linked
// to the task (design D4: targeted routes are visible).
func (s *Service) appendAssignedLine(ctx context.Context, groupID, taskID, text string) (string, error) {
	payload, err := makePayload(text, nil)
	if err != nil {
		return "", fmt.Errorf("append assigned line: %w", err)
	}
	msg := &store.AgentGroupMessage{
		GroupID:    groupID,
		Kind:       store.GroupMessageKindAssigned,
		AuthorKind: store.GroupAuthorKindCoordinator,
		TaskID:     &taskID,
		Payload:    payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(ctx, msg); err != nil {
		return "", fmt.Errorf("append assigned line: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: groupID, MessageID: msg.MessageID, Seq: msg.Seq, Message: msg})
	return msg.MessageID, nil
}

// resolveReplyTarget loads the reply routing context for one replied-to
// transcript row: the open task the message belongs to (its own TaskID link
// or a task created from it), else the message's member author. The caller
// must hold the group's write lock (Send does).
func (s *Service) resolveReplyTarget(ctx context.Context, groupID, replyToMessageID string) (*replyTarget, error) {
	if replyToMessageID == "" {
		return nil, nil
	}
	target := &replyTarget{}
	msg, found, err := s.findMessageLocked(ctx, groupID, replyToMessageID)
	if err != nil {
		return nil, err
	}
	if found {
		target.AuthorAgentID = deref(msg.AuthorAgentID)
		if msg.TaskID != nil {
			target.TaskID = *msg.TaskID
		}
	}
	// Tasks created from this message link through RequestMessageID (a
	// fan-out message seeds several roots; the reply continues the first
	// open one in store order).
	if target.TaskID == "" {
		tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
		if err != nil {
			return nil, fmt.Errorf("resolve reply target: %w", err)
		}
		for i := range tasks {
			if tasks[i].RequestMessageID == replyToMessageID && taskOpen(&tasks[i]) {
				target.TaskID = tasks[i].TaskID
				break
			}
		}
	}
	// The task reference resolves only while the task is open.
	if target.TaskID != "" {
		t, err := s.deps.Store.Groups().TaskByID(ctx, target.TaskID)
		if err != nil {
			return nil, fmt.Errorf("resolve reply target: %w", err)
		}
		if taskOpen(t) {
			target.Task = &taskRef{TaskID: t.TaskID, OwnerAgentID: deref(t.OwnerAgentID)}
		} else {
			target.TaskID = ""
		}
	}
	return target, nil
}

// deref reads a nullable string as "".
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
