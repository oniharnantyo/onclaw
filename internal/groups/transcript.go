package groups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Copy-back attribution (add-agent-groups design D7): each member session's
// live event stream is pumped by one goroutine per (group, member); FINAL
// assistant text rows are copied into agent_group_messages authored by the
// member, keyed by the turn id so a regenerated turn REPLACES its earlier
// copy via the store's (group_id, message_id) uniqueness. Tool, reasoning,
// delta, and every other event kind are never copied. Mutations for one
// group serialize through the group's write lock.

// transcriptReadPage is the transcript window size the packet builder, the
// reply-target scan, and the summary range walk read in.
const transcriptReadPage = 100

// groupPump is one member session's live copy-back pump.
type groupPump struct {
	key       groupKey
	sessionID string
	sub       agents.Subscription
}

// ensurePump subscribes the member session's event stream and starts its
// pump goroutine, replacing an existing pump whose session changed. Safe to
// call repeatedly: an up-to-date pump is kept.
func (s *Service) ensurePump(groupID, agentID, sessionID string) {
	key := groupKey{GroupID: groupID, AgentID: agentID}
	s.mu.Lock()
	if p := s.pumps[key]; p != nil && p.sessionID == sessionID {
		s.mu.Unlock()
		return
	}
	if p := s.pumps[key]; p != nil {
		p.sub.Close()
	}
	sub, err := s.deps.Manager.Subscribe(context.Background(), sessionID)
	if err != nil {
		s.mu.Unlock()
		// A member whose session cannot be subscribed still receives work;
		// only the live copy-back is lost until the next dispatch re-seeds
		// the pump.
		slog.Warn("groups: subscribe member session", "group", groupID, "agent", agentID, "error", err)
		return
	}
	p := &groupPump{key: key, sessionID: sessionID, sub: sub}
	s.pumps[key] = p
	s.mu.Unlock()

	go s.runPump(p)
}

// runPump forwards the subscription's events to the copy-back writer until
// the subscription closes (replacement, member removal, or group deletion).
func (s *Service) runPump(p *groupPump) {
	for ev := range p.sub.Events() {
		s.copyBack(p, ev)
	}
}

// closePump ends one member's pump; idempotent.
func (s *Service) closePump(groupID, agentID string) {
	key := groupKey{GroupID: groupID, AgentID: agentID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.pumps[key]; ok {
		p.sub.Close()
		delete(s.pumps, key)
	}
}

// closeGroupPumps ends every member pump of the group.
func (s *Service) closeGroupPumps(groupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, p := range s.pumps {
		if key.GroupID == groupID {
			p.sub.Close()
			delete(s.pumps, key)
		}
	}
}

// copyBack writes one member-session event into the shared transcript
// (design D7): only final assistant text rows copy, keyed by the turn id —
// a regenerated turn's reply events carry the original turn id in VariantOf,
// so the stable message id upserts and replaces the superseded copy. The
// caller runs on the pump goroutine; transcript writes take the group's
// write lock (per-group serialization, design D7). The store writes the
// row's allocated Seq back into the passed struct.
func (s *Service) copyBack(p *groupPump, ev sessions.Event) {
	if ev.Kind != sessions.KindAssistantMessage {
		return // tool, reasoning, delta, and lifecycle rows never copy
	}
	text := strings.TrimSpace(ev.Text)
	turnKey := ev.TurnID
	if ev.VariantOf != "" {
		// A regenerated turn's replies replace the original's copy: the
		// message id stays the original turn's.
		turnKey = ev.VariantOf
	}
	if text == "" || turnKey == "" {
		return
	}

	// The in-flight task links the copied row to the work the turn serves;
	// read before the lock, best-effort by design.
	s.mu.Lock()
	rec := s.inflight[p.key]
	s.mu.Unlock()
	var taskID *string
	if rec != nil {
		id := rec.TaskID
		taskID = &id
	}

	l := s.groupLock(p.key.GroupID)
	l.Lock()
	defer l.Unlock()

	payload, err := makePayload(ev.Text, nil)
	if err != nil {
		slog.Error("groups: marshal copy-back payload", "group", p.key.GroupID, "error", err)
		return
	}
	msg := &store.AgentGroupMessage{
		GroupID:       p.key.GroupID,
		MessageID:     turnKey,
		Kind:          store.GroupMessageKindMessage,
		AuthorKind:    store.GroupAuthorKindAgent,
		AuthorAgentID: &p.key.AgentID,
		TaskID:        taskID,
		Payload:       payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(context.Background(), msg); err != nil {
		if !errors.Is(err, store.ErrDuplicate) {
			slog.Error("groups: copy-back append", "group", p.key.GroupID, "turn", turnKey, "error", err)
			return
		}
		// The turn already has a copied row (retry or regenerate): replace
		// it in place, keeping its Seq (design D7).
		if err := s.deps.Store.Groups().UpdateMessage(context.Background(), msg); err != nil {
			slog.Error("groups: copy-back replace", "group", p.key.GroupID, "turn", turnKey, "error", err)
			return
		}
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: p.key.GroupID, MessageID: turnKey, Seq: msg.Seq, Message: msg})
}

// findMessageLocked scans the transcript for one message id, paging from the
// newest window downward; the caller must hold the group's write lock.
// Replies address recent rows; a miss after the full walk reports
// found=false.
func (s *Service) findMessageLocked(ctx context.Context, groupID, messageID string) (*store.AgentGroupMessage, bool, error) {
	beforeSeq := int64(0)
	for {
		page, err := s.deps.Store.Groups().ListMessages(ctx, groupID, beforeSeq, transcriptReadPage)
		if err != nil {
			return nil, false, fmt.Errorf("find group message: %w", err)
		}
		if len(page) == 0 {
			return nil, false, nil
		}
		for i := range page {
			if page[i].MessageID == messageID {
				out := page[i]
				return &out, true, nil
			}
		}
		beforeSeq = page[0].Seq
	}
}

// Transcript returns one window of the group's transcript: the newest limit
// rows strictly below beforeSeq — or the newest limit rows overall when
// beforeSeq is 0 — in transcript order. A non-positive limit takes the
// store's page size.
func (s *Service) Transcript(ctx context.Context, groupID string, beforeSeq int64, limit int) ([]store.AgentGroupMessage, error) {
	if _, err := s.deps.Store.Groups().GroupByID(ctx, groupID); err != nil {
		return nil, fmt.Errorf("group transcript: %w", err)
	}
	rows, err := s.deps.Store.Groups().ListMessages(ctx, groupID, beforeSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("group transcript: %w", err)
	}
	return rows, nil
}

// Tasks returns the group's tasks ordered by task id. The result is never
// nil.
func (s *Service) Tasks(ctx context.Context, groupID string) ([]store.AgentGroupTask, error) {
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("group tasks: %w", err)
	}
	return tasks, nil
}

// renderTranscriptRows renders transcript rows for model consumption (the
// history tool and the summary range): one "[seq] [author] text" line per
// row.
func renderTranscriptRows(rows []store.AgentGroupMessage) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		author := "[user]"
		switch r.AuthorKind {
		case store.GroupAuthorKindAgent:
			author = "[agent " + deref(r.AuthorAgentID) + "]"
		case store.GroupAuthorKindCoordinator:
			author = "[coordinator]"
		}
		lines = append(lines, fmt.Sprintf("[%d] %s %s", r.Seq, author, payloadText(r)))
	}
	return strings.Join(lines, "\n")
}
