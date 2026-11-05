package agents

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The group member turn's model-input assembly (add-agent-groups design D3):
// the packet is the turn's whole input — the member session's log is never
// replayed (design D2) — so the assembly composes the packet's standing group
// context into the system instruction and renders the transcript rows, task
// results, and the current request as the fresh user input. Everything else
// about the composition (persona, operating contract, environment block, the
// attachment multi-part path) is the ordinary turn composition.

// assembleGroupTurnInput builds a group member turn's model input from the
// packet: the instruction is the composed persona and contract with the
// session's compaction summary and the packet's standing group context
// (title, instructions, responsibility, versioned summary) appended, and the
// input messages are the packet's unread rows, the dependent task results,
// and the bounded recent window — each an author-labeled user message —
// followed by the current request, multi-part when it carries attachments.
// The replayed session events are deliberately absent: the packet replaces
// the session replay for a group turn.
func assembleGroupTurnInput(p persona, checkpoint sessions.Checkpoint, packet GroupPacket, req turnRequest, tc turnComposer) (string, []*schema.AgenticMessage, error) {
	guidance := ""
	if tc.knowledge {
		guidance = knowledgeGuidance
	}
	skillsText := ""
	if tc.skills {
		skillsText = skillsGuidance
	}
	instruction := composeSystem(p.text(), tc.presets, guidance, skillsText, tc.memory, checkpoint.Summary)
	if group := groupContextBlock(packet); group != "" {
		instruction += "\n\n" + group
	}
	instruction += "\n\n" + environmentBlock(tc.providerType, tc.model, req.origin, tc.sessionRel, now())

	msgs := make([]*schema.AgenticMessage, 0, len(packet.Unread)+len(packet.TaskResults)+len(packet.Recent)+1)
	for _, row := range packet.Unread {
		if msg := transcriptUserMessage(row); msg != nil {
			msgs = append(msgs, msg)
		}
	}
	if len(packet.TaskResults) > 0 {
		msgs = append(msgs, taskResultsUserMessage(packet.TaskResults))
	}
	for _, row := range packet.Recent {
		if msg := transcriptUserMessage(row); msg != nil {
			msgs = append(msgs, msg)
		}
	}
	fresh, err := buildUserTurnMessage(tc, req.message, req.attachments)
	if err != nil {
		return "", nil, err
	}
	msgs = append(msgs, fresh)
	return instruction, msgs, nil
}

// groupContextBlock renders the packet's standing group context for the
// system instruction: the group's title and instructions, the member's
// responsibility line, and the versioned summary of the older group history.
// Empty sections drop out — a packet without instructions composes no
// instructions section.
func groupContextBlock(packet GroupPacket) string {
	var sb strings.Builder
	if title := strings.TrimSpace(packet.GroupTitle); title != "" {
		sb.WriteString("# Agent group: " + title + "\n")
	}
	if instructions := strings.TrimSpace(packet.Instructions); instructions != "" {
		sb.WriteString("\n## Group instructions\n\n" + instructions + "\n")
	}
	if responsibility := strings.TrimSpace(packet.Responsibility); responsibility != "" {
		sb.WriteString("\n## Your responsibility\n\n" + responsibility + "\n")
	}
	if summary := strings.TrimSpace(packet.Summary); summary != "" {
		version := ""
		if packet.SummaryVersion > 0 {
			version = fmt.Sprintf(" (summary v%d)", packet.SummaryVersion)
		}
		sb.WriteString("\n## Earlier group history" + version + "\n\n" + summary + "\n")
	}
	return strings.Trim(sb.String(), "\n")
}

// transcriptUserMessage renders one transcript row as a user-role message —
// an author-labeled line, the way a fresh member reads the room. A row with
// no content renders nothing.
func transcriptUserMessage(row GroupTranscriptMessage) *schema.AgenticMessage {
	text := row.Text
	if text == "" {
		text = row.Payload
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: groupAuthorLabel(row) + text}),
		},
	}
}

// groupAuthorLabel prefixes one transcript row with its author label: users
// and the coordinator by role, members by their agent id.
func groupAuthorLabel(row GroupTranscriptMessage) string {
	switch row.AuthorKind {
	case GroupAuthorUser:
		return "[user] "
	case GroupAuthorCoordinator:
		return "[coordinator] "
	default:
		return "[agent " + row.AuthorAgentID + "] "
	}
}

// taskResultsUserMessage renders the packet's completed task results as one
// user message — the shared decisions the member's work depends on.
func taskResultsUserMessage(results []GroupTaskResult) *schema.AgenticMessage {
	lines := make([]string, 0, len(results)+1)
	lines = append(lines, "## Task results you depend on")
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("- [task %s] %s", r.TaskID, r.Result))
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: strings.Join(lines, "\n")}),
		},
	}
}
