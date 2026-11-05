package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/groups"
	"github.com/oniharnantyo/buddies/internal/store"
)

// GroupsService is the bound service for agent groups (add-agent-groups
// tasks 4.2): group CRUD and membership, the shared transcript reads, the
// task lifecycle controls, and message routing over the groups service
// (design D4/D8). Live updates flow through groupsEvents, the groups.Events
// implementation the groups service calls back (design D9): roster changes
// emit EventGroupsChanged and one group's transcript/task updates emit
// GroupEventName(groupID) — the same emitter mechanism the chat service's
// session pump forwards through.
type GroupsService struct {
	svc *groups.Service
}

// NewGroupsService returns the bound groups service over svc.
func NewGroupsService(svc *groups.Service) *GroupsService {
	return &GroupsService{svc: svc}
}

// CreateGroup creates a group with the given name, optional title and
// instructions, and roster; the first member becomes the lead (design D5).
// store.ErrNotFound when a roster agent does not exist.
func (s *GroupsService) CreateGroup(ctx context.Context, name, title, instructions string, memberAgentIDs []string) (store.AgentGroup, error) {
	g, err := s.svc.CreateGroup(ctx, name, title, instructions, memberAgentIDs)
	if err != nil {
		return store.AgentGroup{}, fmt.Errorf("create group: %w", err)
	}
	return *g, nil
}

// UpdateGroup rewrites the group's editable fields (name, title,
// instructions), keeping lead, roster, and transcript untouched.
func (s *GroupsService) UpdateGroup(ctx context.Context, groupID, name, title, instructions string) (store.AgentGroup, error) {
	g, err := s.svc.UpdateGroup(ctx, groupID, name, title, instructions)
	if err != nil {
		return store.AgentGroup{}, fmt.Errorf("update group: %w", err)
	}
	return *g, nil
}

// DeleteGroup removes the group and — through the store's cascades — its
// roster, transcript, cursors, tasks, and summary. Member sessions survive:
// they are ordinary sessions, not group-owned.
func (s *GroupsService) DeleteGroup(ctx context.Context, groupID string) error {
	if err := s.svc.DeleteGroup(ctx, groupID); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

// Group returns one group; store.ErrNotFound when absent.
func (s *GroupsService) Group(ctx context.Context, groupID string) (store.AgentGroup, error) {
	g, err := s.svc.Group(ctx, groupID)
	if err != nil {
		return store.AgentGroup{}, fmt.Errorf("get group: %w", err)
	}
	return *g, nil
}

// ListGroups returns every group ordered by creation time. The result is
// never nil.
func (s *GroupsService) ListGroups(ctx context.Context) ([]store.AgentGroup, error) {
	groups, err := s.svc.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, nil
}

// Members returns the group's roster in add order. The result is never nil.
func (s *GroupsService) Members(ctx context.Context, groupID string) ([]store.AgentGroupMember, error) {
	members, err := s.svc.Members(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return members, nil
}

// AddMember adds one agent to the roster: a real member session is created
// (design D2), the first member of a leadless group becomes the lead
// (design D5), and a change line lands in the transcript. Adding the same
// agent twice fails with store.ErrDuplicate.
func (s *GroupsService) AddMember(ctx context.Context, groupID, agentID string) error {
	if err := s.svc.AddMember(ctx, groupID, agentID); err != nil {
		return fmt.Errorf("add group member: %w", err)
	}
	return nil
}

// RemoveMember drops one roster row. The member stops receiving work: its
// open tasks pause with a visible reason, and a removed lead falls to the
// first remaining member (design D5). The member's session and past
// contributions survive.
func (s *GroupsService) RemoveMember(ctx context.Context, groupID, agentID string) error {
	if err := s.svc.RemoveMember(ctx, groupID, agentID); err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	return nil
}

// SetLead assigns the lead to a roster member (design D5: exactly one lead);
// tasks that already have owners keep them.
func (s *GroupsService) SetLead(ctx context.Context, groupID, agentID string) error {
	if err := s.svc.SetLead(ctx, groupID, agentID); err != nil {
		return fmt.Errorf("set lead: %w", err)
	}
	return nil
}

// TranscriptPage returns one window of the group's transcript: the newest
// limit rows strictly below beforeSeq — or the newest limit rows overall
// when beforeSeq is 0 — in transcript order (the chat view's windowing
// contract, design D6).
func (s *GroupsService) TranscriptPage(ctx context.Context, groupID string, beforeSeq int64, limit int) ([]store.AgentGroupMessage, error) {
	rows, err := s.svc.Transcript(ctx, groupID, beforeSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("group transcript page: %w", err)
	}
	return rows, nil
}

// Tasks returns the group's tasks ordered by task id. The result is never
// nil.
func (s *GroupsService) Tasks(ctx context.Context, groupID string) ([]store.AgentGroupTask, error) {
	tasks, err := s.svc.Tasks(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("group tasks: %w", err)
	}
	return tasks, nil
}

// Send appends the user message to the transcript and routes it (design D4):
// a mention, the explicit recipient, a task or member reply, a sole-open-task
// follow-up, a single-member group, or the lead.
func (s *GroupsService) Send(ctx context.Context, req groups.SendRequest) error {
	if err := s.svc.Send(ctx, req); err != nil {
		return fmt.Errorf("send to group: %w", err)
	}
	return nil
}

// StopTask pauses the task and its descendants with a visible reason
// (design D8), interrupting the owners' active member turns; an empty reason
// reads as "stopped by user".
func (s *GroupsService) StopTask(ctx context.Context, taskID, reason string) error {
	if err := s.svc.StopTask(ctx, taskID, reason); err != nil {
		return fmt.Errorf("stop task: %w", err)
	}
	return nil
}

// ContinueTask re-dispatches a paused task with the continue message as a
// follow-up (design D8); only a paused task continues.
func (s *GroupsService) ContinueTask(ctx context.Context, taskID, message string) error {
	if err := s.svc.ContinueTask(ctx, taskID, message); err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	return nil
}

// ReassignTask moves an open task to another roster member and re-dispatches
// it to the new owner (design D8); a running task's previous owner turn is
// interrupted first.
func (s *GroupsService) ReassignTask(ctx context.Context, taskID, newOwnerAgentID string) error {
	if err := s.svc.ReassignTask(ctx, taskID, newOwnerAgentID); err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	return nil
}

// CancelTask ends every open task of the named task's root chain (design D8)
// and interrupts the active turns of the cancelled tasks' owners.
func (s *GroupsService) CancelTask(ctx context.Context, taskID string) error {
	if err := s.svc.CancelTask(ctx, taskID); err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	return nil
}

// groupsEvents is the groups.Events implementation the groups service pushes
// live updates through (add-agent-groups design D9). It mirrors the chat
// service's subscription forwarder: a mutation fires, the event is emitted
// onto the host's event bus under its composed name. The groups service
// calls it while holding the triggering group's write lock, so it never
// calls back into the service (groups.Events contract).
type groupsEvents struct {
	emitter Emitter
}

// NewGroupsEvents returns the groups.Events seam forwarding the groups
// service's live updates onto emitter — the wiring piece between the groups
// service and the host's event bus.
func NewGroupsEvents(emitter Emitter) groups.Events {
	return groupsEvents{emitter: emitter}
}

// GroupChanged announces a roster- or list-level change (group created,
// edited, deleted, membership or lead change) under EventGroupsChanged.
func (e groupsEvents) GroupChanged() {
	e.emitter.Emit(EventGroupsChanged, GroupsChangedEvent{})
}

// GroupEvent announces one per-group transcript or task update under
// GroupEventName(groupID), converting the groups package's event onto the
// binding-surface GroupEvent payload.
func (e groupsEvents) GroupEvent(groupID string, ev groups.GroupEvent) {
	e.emitter.Emit(GroupEventName(groupID), GroupEvent{
		Kind:      string(ev.Kind),
		MessageID: ev.MessageID,
		Seq:       ev.Seq,
		Message:   ev.Message,
		TaskID:    ev.TaskID,
	})
}
