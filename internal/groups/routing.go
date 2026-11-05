package groups

import (
	"strings"
)

// Deterministic routing (add-agent-groups design D4, user-locked): a group
// message is routed with no model call, in tier order — leading mentions,
// the composer's explicit recipient, a reply's task or member, the sole open
// task, a single-member group, and the lead default. Everything here is a
// pure function over a loaded snapshot so the tier order is table-testable.

// memberRef is one roster member as routing sees it: the agent id and its
// display name (mention matching).
type memberRef struct {
	AgentID string
	Name    string
}

// taskRef is one open task as routing sees it: its id and owner.
type taskRef struct {
	TaskID       string
	OwnerAgentID string
}

// replyTarget is the reply tier's context for one replied-to transcript row:
// the member author (empty for user and coordinator rows) and the open task
// the row belongs to (TaskID with the task ref, empty/nil when none).
type replyTarget struct {
	AuthorAgentID string
	// TaskID names the replied-to row's task link before the open-task
	// check resolves it; routing reads Task only.
	TaskID string
	Task   *taskRef
}

// routingInput is the snapshot one routing decision is made over.
type routingInput struct {
	// Text is the raw message text; leading mentions parse from it.
	Text string
	// Roster is the group's membership in add order, with the lead id. A
	// member not in the roster is never selected (design D4).
	Roster routingRoster
	// ExplicitRecipient is the composer mention picker's agent id; empty
	// when the message is untargeted.
	ExplicitRecipient string
	// Reply is the replied-to row's context; nil when not a reply.
	Reply *replyTarget
	// OpenTasks lists every open task of the group (any state in
	// store.GroupTaskOpenStates), for the sole-open-task tier.
	OpenTasks []taskRef
}

// routingDecision is the routed outcome: the recipient agent ids (mention
// order for mentions, roster order for @everyone), the task a follow-up
// continues (empty for a fresh root), and whether the coordinator assigned
// line is written. The assigned line appears ONLY for the task-targeted
// tiers (reply-to-task, sole-open-task, and tool delegation) — mention- and
// lead-routed messages add none (design D4).
type routingDecision struct {
	Recipients     []string
	FollowUpTaskID string
	AssignedLine   bool
}

// everyoneMention is the @everyone keyword: the message addresses every
// member.
const everyoneMention = "everyone"

// routeMessage applies the D4 tier order to one routing snapshot. The first
// matching tier wins; an unmatched tier falls through to the next.
func routeMessage(in routingInput) routingDecision {
	// Tier 1-2: leading mentions and the explicit recipient — the same
	// mechanism (design D4). No coordinator line.
	if mentioned := parseMentions(in.Text, in.Roster.Members); len(mentioned) > 0 {
		return routingDecision{Recipients: mentioned}
	}
	if in.ExplicitRecipient != "" && rosterContains(in.Roster.Members, in.ExplicitRecipient) {
		return routingDecision{Recipients: []string{in.ExplicitRecipient}}
	}
	// Tier 3: a reply to a message that belongs to an open task continues
	// that task with its owner — a targeted route with an assigned line.
	// The owner must still be able to take work (in the roster): a task
	// whose owner left falls through, its work reachable again via
	// Reassign.
	if in.Reply != nil && in.Reply.Task != nil && rosterContains(in.Roster.Members, in.Reply.Task.OwnerAgentID) {
		return routingDecision{
			Recipients:     []string{in.Reply.Task.OwnerAgentID},
			FollowUpTaskID: in.Reply.Task.TaskID,
			AssignedLine:   true,
		}
	}
	// Tier 4: a reply with no task to a member's message goes back to that
	// member — no assigned line.
	if in.Reply != nil && in.Reply.AuthorAgentID != "" && rosterContains(in.Roster.Members, in.Reply.AuthorAgentID) {
		return routingDecision{Recipients: []string{in.Reply.AuthorAgentID}}
	}
	// Tier 5: a follow-up on the group's sole open task goes to its owner —
	// a targeted route with an assigned line. The same workability gate as
	// tier 3 applies.
	if len(in.OpenTasks) == 1 && rosterContains(in.Roster.Members, in.OpenTasks[0].OwnerAgentID) {
		return routingDecision{
			Recipients:     []string{in.OpenTasks[0].OwnerAgentID},
			FollowUpTaskID: in.OpenTasks[0].TaskID,
			AssignedLine:   true,
		}
	}
	// Tier 6: a single-member group routes to that member — no line.
	if len(in.Roster.Members) == 1 {
		return routingDecision{Recipients: []string{in.Roster.Members[0].AgentID}}
	}
	// Tier 7: everything untargeted goes to the lead, which answers
	// directly and may delegate through its in-channel tools — no line.
	if in.Roster.LeadAgentID != "" && rosterContains(in.Roster.Members, in.Roster.LeadAgentID) {
		return routingDecision{Recipients: []string{in.Roster.LeadAgentID}}
	}
	// A member-less group (or, defensively, a leadless roster) routes
	// nowhere; the message still lands in the transcript.
	return routingDecision{}
}

// rosterContains reports whether the agent is in the roster — the only
// work-taking gate in this change (design D4: a member that cannot take work
// is never selected).
func rosterContains(members []memberRef, agentID string) bool {
	for _, m := range members {
		if m.AgentID == agentID {
			return true
		}
	}
	return false
}

// mentionBoundary reports whether the rune after a matched name ends the
// mention: whitespace, punctuation, or the end of the text. Letters and
// digits continue the name (so "@Scout2" does not match "Scout").
func mentionBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', ',', '.', ';', ':', '!', '?', '"', '\'':
		return true
	}
	return false
}

// parseMentions parses the text's LEADING mentions (design D4): each token
// from the start that is @Name for a roster member (case-insensitive,
// longest name winning when two names match the same span) or @everyone
// routes to that member. The scan runs over the leading @tokens and ends at
// the first non-mention token — mentions mid-sentence do not route. A token
// naming no roster member contributes nothing (design D4: a member that
// cannot take work is never selected); the scan continues past it.
// Duplicates collapse, preserving first mention order; @everyone yields the
// whole roster in add order and ends the scan.
func parseMentions(text string, members []memberRef) []string {
	names := make([]memberRef, len(members))
	copy(names, members)
	// Longest name first, so "@Jo Ann" picks "Jo Ann" over "Jo".
	// Insertion sort keeps the roster order stable among equal lengths.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && len([]rune(names[j-1].Name)) < len([]rune(names[j].Name)); j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}

	runes := []rune(text)
	isSpace := func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }
	var out []string
	seen := make(map[string]bool)
	i := 0
	// Skip leading whitespace before the first mention.
	for i < len(runes) && isSpace(runes[i]) {
		i++
	}
	for i < len(runes) && runes[i] == '@' {
		id, end := matchMentionAt(runes, i+1, names)
		if end < 0 {
			// Not a mention of anyone routable: skip the token and keep
			// scanning the leading mentions.
			i++
			for i < len(runes) && !isSpace(runes[i]) {
				i++
			}
			for i < len(runes) && isSpace(runes[i]) {
				i++
			}
			continue
		}
		if id == "" {
			// The everyone keyword matched: it addresses every member (add
			// order) and ends the scan — further mentions name members
			// already included.
			out = out[:0]
			for _, m := range members {
				out = append(out, m.AgentID)
			}
			return out
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
		i = end
		// Skip whitespace between consecutive mentions.
		for i < len(runes) && isSpace(runes[i]) {
			i++
		}
	}
	return out
}

// matchMentionAt matches one mention at offset start (just past the '@'):
// the @everyone keyword, else the longest roster name whose text matches
// there case-insensitively and ends on a boundary. Returns the recipient
// agent id ("" for the everyone keyword) and the offset just past the
// mention; end is -1 when nothing matches.
func matchMentionAt(runes []rune, start int, names []memberRef) (id string, end int) {
	// The everyone keyword first: a member literally named "Everyone" is a
	// pathological case and loses to the keyword (documented).
	if e := matchNameAt(runes, start, everyoneMention); e >= 0 {
		return "", e
	}
	for _, m := range names {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		if e := matchNameAt(runes, start, m.Name); e >= 0 {
			return m.AgentID, e
		}
	}
	return "", -1
}

// matchNameAt reports where the case-insensitive name match at offset start
// ends; -1 when the text there does not match the name followed by a
// boundary.
func matchNameAt(runes []rune, start int, name string) int {
	nr := []rune(name)
	if start+len(nr) > len(runes) {
		return -1
	}
	if !strings.EqualFold(string(runes[start:start+len(nr)]), name) {
		return -1
	}
	end := start + len(nr)
	if end < len(runes) && !mentionBoundary(runes[end]) {
		return -1
	}
	return end
}
