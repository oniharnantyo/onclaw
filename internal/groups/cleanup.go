package groups

import (
	"context"
	"fmt"
	"log/slog"
)

// Agent-deletion cleanup (add-agent-groups spec "Agent deletion cleanup"):
// deleting an agent removes its memberships from every group, falls the lead
// back where it was the lead, and pauses its open group tasks with a visible
// reason. The groups themselves, their transcripts, and the other members'
// work survive. The next wave's AgentsService.Delete calls this.

// cleanupStopReason is the visible stop reason stamped on a deleted agent's
// open tasks.
const cleanupStopReason = "agent deleted"

// CleanupAgent removes the agent's memberships from every group, falling the
// lead back where it was the lead, pausing the agent's open tasks with a
// visible reason, and interrupting its in-flight turns. Idempotent: an agent
// in no group is a no-op.
func (s *Service) CleanupAgent(ctx context.Context, agentID string) error {
	groups, err := s.deps.Store.Groups().ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("cleanup agent: %w", err)
	}
	for i := range groups {
		members, err := s.deps.Store.Groups().ListMembers(ctx, groups[i].ID)
		if err != nil {
			return fmt.Errorf("cleanup agent: %w", err)
		}
		inRoster := false
		for _, m := range members {
			if m.AgentID == agentID {
				inRoster = true
				break
			}
		}
		if !inRoster {
			continue
		}
		if err := s.removeMemberFromGroup(ctx, groups[i].ID, agentID, cleanupStopReason); err != nil {
			return err
		}
		slog.Debug("groups: agent cleaned up", "group", groups[i].ID, "agent", agentID)
	}
	return nil
}

// removeMemberFromGroup is the shared member-removal body of RemoveMember
// and CleanupAgent. The group's write lock must NOT be held; the method
// takes it.
func (s *Service) removeMemberFromGroup(ctx context.Context, groupID, agentID, pauseReason string) error {
	l := s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()

	name := agentID
	if agent, err := s.deps.Store.Agents().ByID(ctx, agentID); err == nil {
		name = agent.Name
	}
	if err := s.deps.Store.Groups().RemoveMember(ctx, groupID, agentID); err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}

	// The member no longer receives work: stop the pump, interrupt the
	// turn, pause its open tasks.
	s.closePump(groupID, agentID)
	s.interruptMember(ctx, groupID, agentID)
	if err := s.pauseMemberTasks(ctx, groupID, agentID, pauseReason); err != nil {
		return err
	}

	// Lead fallback (design D5): the first remaining member takes over; a
	// member-less group has no lead.
	cur, err := s.deps.Store.Groups().GroupByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("remove group member: lead fallback: %w", err)
	}
	if cur.LeadAgentID != nil && *cur.LeadAgentID == agentID {
		members, err := s.deps.Store.Groups().ListMembers(ctx, groupID)
		if err != nil {
			return fmt.Errorf("remove group member: lead fallback: %w", err)
		}
		if len(members) > 0 {
			cur.LeadAgentID = &members[0].AgentID
		} else {
			cur.LeadAgentID = nil
		}
		if err := s.deps.Store.Groups().UpdateGroup(ctx, cur); err != nil {
			return fmt.Errorf("remove group member: lead fallback: %w", err)
		}
	}

	if _, err := s.appendChangeLine(ctx, groupID, "removed "+name); err != nil {
		return err
	}
	s.fireGroupChanged()
	return nil
}
