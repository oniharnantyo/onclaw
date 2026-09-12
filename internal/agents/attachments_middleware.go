package agents

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// attachmentsPlaceholderMiddleware enforces the current-turn-only attachment
// context lifetime (attachments design D5/D7) with one model-time rule keyed
// on the block's own shape: image and file blocks carrying bytes pass to the
// model untouched, URL-only blocks — an older turn's demoted references —
// collapse to a placeholder text naming the file and its workspace path. The
// block shape is the age marker, so there is no turn-boundary bookkeeping,
// and a regenerated turn carrying the same refs re-delivers their bytes
// because construction is per-run.
//
// Registered unconditionally in buildMiddlewares: shape-keyed, it costs a
// linear scan over user messages and a no-op for plain-text sessions.
type attachmentsPlaceholderMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
}

// newAttachmentsPlaceholderMiddleware returns the ADK middleware.
func newAttachmentsPlaceholderMiddleware() adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &attachmentsPlaceholderMiddleware{}
}

// BeforeModelRewriteState walks the inbound messages before every model call
// and swaps stale attachment references for placeholders. Copy-on-write: the
// state's messages are the originals the live transcript mapping reads, so a
// rewritten message is a fresh clone and the input slice is aliased, never
// mutated.
func (m *attachmentsPlaceholderMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], _ *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	if state != nil {
		state.Messages = CollapseStaleAttachmentBlocks(state.Messages)
	}
	return ctx, state, nil
}

// CollapseStaleAttachmentBlocks replaces every URL-only attachment block of a
// user message with its placeholder. Messages without stale blocks are
// aliased; when no message changes, the input slice comes back untouched.
func CollapseStaleAttachmentBlocks(msgs []*schema.AgenticMessage) []*schema.AgenticMessage {
	changed := false
	out := make([]*schema.AgenticMessage, len(msgs))
	for i, msg := range msgs {
		switch {
		case msg == nil || msg.Role != schema.AgenticRoleTypeUser:
			out[i] = msg
			continue
		case hasStaleAttachmentBlocks(msg.ContentBlocks):
			clone := *msg
			clone.ContentBlocks = placeholderBlocks(msg.ContentBlocks)
			out[i] = &clone
			changed = true
		default:
			out[i] = msg
		}
	}
	if !changed {
		return msgs
	}
	return out
}

func collapseStaleAttachmentBlocks(msgs []*schema.AgenticMessage) []*schema.AgenticMessage {
	return CollapseStaleAttachmentBlocks(msgs)
}

func hasStaleAttachmentBlocks(blocks []*schema.ContentBlock) bool {
	for _, block := range blocks {
		if isStaleAttachmentBlock(block) {
			return true
		}
	}
	return false
}

// placeholderBlocks returns the message's blocks with every stale attachment
// reference swapped for its placeholder text block; surviving blocks are
// aliased.
func placeholderBlocks(blocks []*schema.ContentBlock) []*schema.ContentBlock {
	out := make([]*schema.ContentBlock, len(blocks))
	for i, block := range blocks {
		if isStaleAttachmentBlock(block) {
			note := schema.NewContentBlock(&schema.UserInputText{Text: attachmentPlaceholderText(block)})
			// The placeholder is model-facing plumbing: marked like a
			// drop-lane pointer note (D8) so it can never surface as
			// transcript text on any path that skips marked blocks.
			note.Extra = map[string]any{AttachmentPointerExtraKey: true}
			out[i] = note
			continue
		}
		out[i] = block
	}
	return out
}

// isStaleAttachmentBlock reports whether the block is an older turn's
// attachment reference: an image or file payload whose bytes are gone while a
// capability URL survives. The URL may ride the payload (legacy demotion
// shape) or the block meta (current shape — construction never puts it on the
// payload); either marks a demoted reference. Byte-carrying blocks are never
// stale.
func isStaleAttachmentBlock(block *schema.ContentBlock) bool {
	if block == nil {
		return false
	}
	meta, _ := attachmentBlockMetaOf(block)
	switch {
	case block.UserInputImage != nil:
		return block.UserInputImage.Base64Data == "" &&
			(block.UserInputImage.URL != "" || meta.URL != "")
	case block.UserInputFile != nil:
		return block.UserInputFile.Base64Data == "" &&
			(block.UserInputFile.URL != "" || meta.URL != "")
	default:
		return false
	}
}

// attachmentPlaceholderText renders the model-facing placeholder. The file
// name and its workspace path are mandatory (spec: the pointer must be
// actionable); both come from the stamped identity's meta URL, falling back
// to the payload URL (legacy demotion shape), and the name finally to the
// URL's last segment.
func attachmentPlaceholderText(block *schema.ContentBlock) string {
	meta, _ := attachmentBlockMetaOf(block)
	mime := meta.Mime
	url := meta.URL
	switch {
	case block.UserInputImage != nil:
		if mime == "" {
			mime = block.UserInputImage.MIMEType
		}
		if block.UserInputImage.URL != "" {
			url = block.UserInputImage.URL
		}
	case block.UserInputFile != nil:
		if mime == "" {
			mime = block.UserInputFile.MIMEType
		}
		if block.UserInputFile.URL != "" {
			url = block.UserInputFile.URL
		}
	}
	name := meta.Name
	if name == "" && block.UserInputFile != nil {
		name = block.UserInputFile.Name
	}
	if name == "" {
		if i := strings.LastIndex(url, "/"); i >= 0 {
			name = url[i+1:]
		} else {
			name = url
		}
	}
	return "[attachment: " + name + " (" + mime + ") — attached in an earlier message; its contents are no longer in context. Path: " + url + "]"
}
