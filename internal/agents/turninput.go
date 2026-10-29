package agents

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The turn's model-input assembly (task 5.3, design D5/D7): the replayed
// transcript after the compaction boundary becomes user/assistant messages,
// attachments ride as multi-part content when the effective model (and its
// connector) can read them, and a regeneration's replayed context cuts at the
// original prompt — the prompt itself rides the fresh input.

// turnRequest carries what one turn dispatch adds to the session: the user
// text, the resolved attachment metadata, the run origin, and — for a
// regeneration — the variant marker and its input-shaping flags.
type turnRequest struct {
	// message is the user text (empty for an attachment-only send).
	message string
	// attachments is the send's registered attachment metadata, in send
	// order; content is read from disk at input time, never trusted from the
	// request.
	attachments []sessions.Attachment
	// variantOf marks a regeneration: the original turn's id. Reply events
	// and the live user-message marker carry it (design D7).
	variantOf string
	// suppressUserPersist drops the user_message persist (a regeneration:
	// the log keeps exactly one user message per prompt); the live marker
	// still broadcasts so the UI can show the prompt being re-asked.
	suppressUserPersist bool
	// replayBefore cuts the replayed context at this Seq (exclusive): a
	// regeneration replays only what came before the original prompt — the
	// old answer is what the new variant replaces. Zero replays everything
	// after the checkpoint boundary, the fresh-send shape.
	replayBefore int64
	// origin names who started the run (design D6): stamped on every hook
	// event the turn raises; empty reads as hooks.OriginUser.
	origin string
}

// turnComposer carries what the model-input assembly needs beyond the
// replayed events: the effective provider type and model (capability flags)
// and the attachment store (content reads). attachments may be nil —
// attachment-carrying messages degrade to pointer notes.
type turnComposer struct {
	providerType string
	model        string
	atts         *attachments.Store
	caps         ModelCapabilities
	hasCaps      bool
	// knowledge reports whether this turn exposes the /knowledge corpus
	// mount (add-knowledge-corpus design D7): true composes the corpus
	// guidance after the operating contract, false keeps the instruction
	// guidance-free.
	knowledge bool
	// skills reports whether this turn resolved a skill backend
	// (add-agent-created-skills design D1): true composes the
	// skills-authoring guidance after the knowledge guidance, false keeps
	// the instruction guidance-free.
	skills bool
	// presets is the turn's composed document-preset appendix
	// (add-create-document design D6), riding directly after the operating
	// contract; empty keeps the instruction appendix-free.
	presets string
	// memory is the turn's composed memory block (add-agent-memory design
	// D3), riding after the knowledge guidance and before the compaction
	// summary; empty keeps the instruction block-free — child turns never
	// carry it (spec agent-memory "Subagent turns receive no memory block").
	memory string
}

func (tc turnComposer) supportsPDFInput() bool {
	if tc.hasCaps {
		return tc.caps.PDFInput
	}
	return SupportsPDFInput(tc.providerType, tc.model)
}

func (tc turnComposer) supportsVideoInput() bool {
	if tc.hasCaps {
		return tc.caps.VideoInput
	}
	return SupportsVideoInput(tc.providerType, tc.model)
}

func (tc turnComposer) supportsImageInput() bool {
	if tc.hasCaps {
		return tc.caps.ImageInput
	}
	return SupportsImageInput(tc.providerType, tc.model)
}

// assembleTurnInput builds a turn's model input and system instruction: the
// instruction is the composed persona, the operating contract, the compaction
// summary, and the environment block — the ADK
// agent presents it as the system message — and the input messages are the
// replayed user/assistant messages after the checkpoint boundary (cut at
// req.replayBefore for a regeneration) followed by the new user input,
// multi-part when the message carries attachments. (The agent config carries
// the instruction so capability middlewares can append their own guidance to
// it.)
func assembleTurnInput(p persona, checkpoint sessions.Checkpoint, events []sessions.Event, req turnRequest, tc turnComposer) (string, []*schema.AgenticMessage, error) {
	guidance := ""
	if tc.knowledge {
		guidance = knowledgeGuidance
	}
	skillsText := ""
	if tc.skills {
		skillsText = skillsGuidance
	}
	instruction := composeSystem(p.text(), tc.presets, guidance, skillsText, tc.memory, checkpoint.Summary) +
		"\n\n" + environmentBlock(tc.providerType, tc.model, req.origin, now())
	msgs := make([]*schema.AgenticMessage, 0, 2)
	for _, ev := range events {
		if ev.Seq <= checkpoint.UpToSeq {
			continue
		}
		if req.replayBefore > 0 && ev.Seq >= req.replayBefore {
			continue
		}
		switch ev.Kind {
		case sessions.KindUserMessage:
			msg, err := buildUserTurnMessage(tc, ev.Text, ev.Attachments)
			if err != nil {
				return "", nil, err
			}
			msgs = append(msgs, msg)
		case sessions.KindAssistantMessage:
			msgs = append(msgs, &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: ev.Text}),
				},
			})
		}
	}
	fresh, err := buildUserTurnMessage(tc, req.message, req.attachments)
	if err != nil {
		return "", nil, err
	}
	msgs = append(msgs, fresh)
	return instruction, msgs, nil
}

// environmentBlock composes the per-turn environment block (design D4; spec
// agent-prompts "Turn environment block"): the effective provider and model,
// the current date, the turn's origin, and whether the user is present. It
// rides last in the system context, after the compaction summary — the mode
// signal sits closest to the fresh user input, and the contract's "Operating
// modes" section tells the model to pick its mode from it. It deliberately
// does not restate the workspace mount: the filesystem middleware appends its
// own guidance, and duplication would invite drift between two sources of the
// same fact.
func environmentBlock(providerType, model, origin string, at time.Time) string {
	if origin == "" {
		origin = hooks.OriginUser
	}
	presence := "the user is present (live chat)"
	if origin != hooks.OriginUser {
		presence = "the user is not present (autonomous run)"
	}
	return strings.Join([]string{
		"# Environment",
		"",
		fmt.Sprintf("Provider/model: %s / %s", providerType, model),
		"Current date: " + at.Format("2006-01-02"),
		fmt.Sprintf("Origin: %s — %s", origin, presence),
	}, "\n")
}

// buildUserTurnMessage builds one user message for the model: plain text
// without attachments; multi-part (design D5, spec chat-attachments "Model
// capability degradation") with them — image/PDF/video bytes ride their
// content parts when the effective model's capability flags (and the
// provider's connector) allow, and every other attachment degrades to a
// reference pointer note. Sending never fails because of a capability gap.
func buildUserTurnMessage(tc turnComposer, text string, atts []sessions.Attachment) (*schema.AgenticMessage, error) {
	blocks := make([]*schema.ContentBlock, 0, len(atts)+1)
	if text != "" {
		blocks = append(blocks, schema.NewContentBlock(&schema.UserInputText{Text: text}))
	}
	for _, att := range atts {
		block, err := attachmentBlock(tc, att)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		blocks = append(blocks, schema.NewContentBlock(&schema.UserInputText{Text: ""}))
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: blocks,
	}, nil
}

// attachmentBlock builds one attachment's model-input block: the content block
// when the model and connector can carry it, the reference pointer note
// otherwise (spec chat-attachments: the agent can still mention the file but
// is never sent input it cannot process).
func attachmentBlock(tc turnComposer, att sessions.Attachment) (*schema.ContentBlock, error) {
	data, err := tc.readAttachment(att)
	if err != nil {
		// A lost file must not fail the send: the pointer note stands in.
		return pointerBlock(att, "the stored file could not be read"), nil
	}
	b64 := base64.StdEncoding.EncodeToString(data)

	switch {
	case att.Mime == "application/pdf":
		if tc.supportsPDFInput() && carriesPDFParts(tc.providerType) {
			return schema.NewContentBlock(&schema.UserInputFile{
				Base64Data: b64,
				MIMEType:   att.Mime,
				Name:       att.Name,
			}), nil
		}
	case strings.HasPrefix(att.Mime, "image/"):
		if tc.supportsImageInput() {
			return schema.NewContentBlock(&schema.UserInputImage{
				Base64Data: b64,
				MIMEType:   att.Mime,
			}), nil
		}
	case strings.HasPrefix(att.Mime, "video/"):
		if tc.supportsVideoInput() && carriesVideoParts(tc.providerType) {
			return schema.NewContentBlock(&schema.UserInputVideo{
				Base64Data: b64,
				MIMEType:   att.Mime,
			}), nil
		}
	case strings.HasPrefix(att.Mime, "text/"):
		// Every model reads text: the content rides inline as a text part —
		// no capability gate (uploads are capped at 1 MB; spec
		// chat-attachments "Multi-part user messages").
		return schema.NewContentBlock(&schema.UserInputText{
			Text: string(data),
		}), nil
	case attachments.IsOfficeMime(att.Mime):
		// Office documents are binary — their bytes never ride the message
		// (a 20 MB package is not context). Like the text lane, no model
		// capability gate applies: every model gets the same reference note,
		// pointing at the one tool path that makes the content readable
		// (save_document copies it into the knowledge scope, where read_file
		// renders the office view).
		return pointerBlock(att, "its bytes are not sent; save_document can copy it into your knowledge scope, where read_file presents it as a view"), nil
	}
	return pointerBlock(att, fmt.Sprintf("the active model cannot read %s input", laneName(att.Mime))), nil
}

// pointerBlock is the reference pointer note standing in for content the
// model cannot (or could not) receive: the attachment's name and capability
// key so the agent can mention the file (spec: "a reference pointer (name
// and capability link)").
func pointerBlock(att sessions.Attachment, reason string) *schema.ContentBlock {
	return schema.NewContentBlock(&schema.UserInputText{
		Text: fmt.Sprintf("[attached file not sent: %s (%s, %d bytes) — %s; attachment key %s]",
			att.Name, att.Mime, att.Size, reason, att.Key),
	})
}

// carriesPDFParts reports whether the provider's connector can carry a PDF
// file part at all (design D5): today only the gemini connector maps
// file_url parts (inline data); the claude connector and the OpenAI-shaped
// endpoints reject them, so their PDFs degrade to the pointer note even when
// the model itself reads PDFs — sending would fail the turn, and the spec
// forbids failing a send over an input gap.
func carriesPDFParts(providerType string) bool {
	return providerType == ProviderGemini
}

// carriesVideoParts reports whether the provider's connector can carry a
// video part at all (design D6): today only the gemini connector maps
// video_url parts (inline data); the claude connector and the OpenAI-shaped
// endpoints reject them, so their videos degrade to the pointer note even
// when the model itself reads video — sending would fail the turn, and the
// spec forbids failing a send over an input gap.
func carriesVideoParts(providerType string) bool {
	return providerType == ProviderGemini
}

// laneName names an attachment's lane for the pointer note.
func laneName(mime string) string {
	switch {
	case mime == "application/pdf":
		return "PDF"
	case attachments.IsOfficeMime(mime):
		return "office document"
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "text/"):
		return "text"
	default:
		return "file"
	}
}

// readAttachment resolves one attachment's bytes through the store. A turn
// without an attachment store (capability unconfigured) or an unreadable
// file returns an error the caller degrades on.
func (tc turnComposer) readAttachment(att sessions.Attachment) ([]byte, error) {
	if tc.atts == nil {
		return nil, fmt.Errorf("attachment support is not configured")
	}
	content, err := tc.atts.Read(context.Background(), att.Key)
	if err != nil {
		return nil, err
	}
	return content.Data, nil
}
