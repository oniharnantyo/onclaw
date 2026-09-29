package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/references"
)

// AttachmentPointerExtraKey marks a user-message content block as a drop-lane
// pointer note (attachments design D8). The persist and history-projection
// layers skip marked blocks when deriving message text, so the note reaches
// the model but never the visible transcript. The marker lives on the block's
// Extra map because Extra survives the session serializer round-trip.
const AttachmentPointerExtraKey = "onclaw.attachment_pointer"

// AttachmentMetaExtraKey carries the durable attachment identity on image and
// file content blocks (attachments design D6/D10): the value is an
// attachmentBlockMeta registered below, so the session serializer round-trip
// reconstructs it typed and both transcript paths (live drain, history
// projection) derive identical AttachmentMeta from it.
const AttachmentMetaExtraKey = "onclaw.attachment"

// attachmentBlockMeta is the per-block attachment identity stored under
// AttachmentMetaExtraKey. Registered because Extra values sit behind an
// interface field the ADK serializer reconstructs from registered concrete
// types (promptBlockedEvent precedent). The capability URL rides the meta,
// never the image/file payload (fix-image-attachment-lane D1): providers
// cannot fetch server-relative URLs, so the payload carries bytes only and
// the transcript's pill link resolves from here.
type attachmentBlockMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	Lane string `json:"lane"`
	URL  string `json:"url,omitempty"`
}

func init() {
	schema.Register[attachmentBlockMeta]()
}

// setAttachmentBlockMeta stamps the attachment identity onto a block.
func setAttachmentBlockMeta(block *schema.ContentBlock, meta attachmentBlockMeta) {
	if block.Extra == nil {
		block.Extra = make(map[string]any, 1)
	}
	block.Extra[AttachmentMetaExtraKey] = meta
}

// attachmentBlockMetaOf reads the stamped identity back. The typed shape is
// the registered round-trip result; the map shape covers payloads that crossed
// a plain JSON boundary.
func attachmentBlockMetaOf(block *schema.ContentBlock) (attachmentBlockMeta, bool) {
	if block == nil || block.Extra == nil {
		return attachmentBlockMeta{}, false
	}
	switch v := block.Extra[AttachmentMetaExtraKey].(type) {
	case attachmentBlockMeta:
		return v, true
	case map[string]any:
		return attachmentBlockMeta{
			ID:   attachmentMetaString(v["id"]),
			Name: attachmentMetaString(v["name"]),
			Mime: attachmentMetaString(v["mime"]),
			Size: attachmentMetaInt(v["size"]),
			Lane: attachmentMetaString(v["lane"]),
			URL:  attachmentMetaString(v["url"]),
		}, true
	default:
		return attachmentBlockMeta{}, false
	}
}

// isAttachmentPointerNote reports whether a block is a model-facing pointer
// note (D8): such blocks never render as transcript text.
func isAttachmentPointerNote(block *schema.ContentBlock) bool {
	return block != nil && block.Extra != nil && block.Extra[AttachmentPointerExtraKey] == true
}

// attachmentMetasOf derives the transcript attachment metadata of a message
// from its blocks' stamped identities and URLs (design D10). One helper for
// the live drain and the history projection, so live == hydrated by
// construction. The capability URL lives in the block meta; a non-empty
// payload URL wins as the legacy fallback — old transcripts were demoted with
// the URL on the payload and none in the meta. Block order is preserved.
func attachmentMetasOf(msg *schema.AgenticMessage) []AttachmentMeta {
	if msg == nil {
		return nil
	}
	var out []AttachmentMeta
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		meta, ok := attachmentBlockMetaOf(block)
		if !ok {
			continue
		}
		url := meta.URL
		switch {
		case block.UserInputImage != nil:
			if block.UserInputImage.URL != "" {
				url = block.UserInputImage.URL
			}
		case block.UserInputFile != nil:
			if block.UserInputFile.URL != "" {
				url = block.UserInputFile.URL
			}
		}
		out = append(out, AttachmentMeta{
			Name:     meta.Name,
			MimeType: meta.Mime,
			Size:     meta.Size,
			URL:      url,
		})
	}
	return out
}

func attachmentMetaString(v any) string {
	s, _ := v.(string)
	return s
}

// attachmentMetaInt coerces the serialized size back: the registered
// round-trip yields int64, plain JSON boundaries yield int or float64
// (the serializer's primitive conversion) or json.Number.
func attachmentMetaInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	default:
		return 0
	}
	return 0
}

// Inline attachment lanes (attachments design D4), mirroring the domain lane
// values as literals so the runner stays decoupled from the attachments
// domain constants (attLaneDrop precedent).
const (
	attLaneInlineImage = "inline-image"
	attLaneInlinePDF   = "inline-pdf"
	attLaneInlineText  = "inline-text"
)

// attLaneDocument is the document-mention lane (add-reference-documents
// 10.4): the composer's documents popover sends a chip shaped
// {kind: "document", documentId, name, path} through the same
// ExecRequest.Attachments payload chat-attachment chips ride. Identity only —
// no content is carried: the bytes already sit in the run's references/
// mount, so the lane renders a pointer note and never materializes or binds
// bytes.
const attLaneDocument = "document"

// documentExts are the extensions the document.read tool supports
// (add-document-read-tool spec): PDF plus the modern office formats.
var documentExts = map[string]bool{"pdf": true, "docx": true, "xlsx": true, "pptx": true}

// isDocumentRef reports whether an attachment ref is a document the
// document.read tool handles: by extension, or by PDF mime (the mime check
// covers legacy inline-pdf lane values whose names may lack the extension).
func isDocumentRef(ref AttachmentRef) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(ref.Name)), ".")
	return documentExts[ext] || ref.MimeType == "application/pdf"
}

// buildAttachmentUserMessage constructs the multimodal user message for a
// turn carrying attachments (attachments design D5/D9): the user's own text
// first (omitted when empty — attachment-only messages are valid turns), then
// blocks grouped by lane in spec order — one fenced text block per
// inline-text ref, one image block per inline-image ref, one file block per
// inline-pdf ref, and finally one marked pointer-note block per drop-lane ref
// naming the file and the run-scoped read-only path materializeDropLane wrote
// it to. Inline bytes are resolved here so the model sees them on every call
// of the carrying turn (design D5); any resolution failure fails the run
// before it starts. Image and file blocks carry bytes only — the capability
// URL rides the stamped attachment identity in the block meta (D1/D10),
// which persist demotes to and both transcript paths project from. req.Input
// is used as-is: skill invocations are injected before construction and are
// part of the turn's user text.
//
// mod carries the turn's resolved input-modality capability
// (fix-image-attachment-lane D4): an affirmatively-unsupported kind replaces
// its block with a marked pointer note instead of failing the run, skipping
// byte resolution entirely; supported and unknown both take the bytes path
// (unknown fails open). Degradation is per build, so a turn regenerated after
// a model switch rebuilds with the new capability.
func (r *Runner) buildAttachmentUserMessage(ctx context.Context, req ExecRequest, mod inputModality) (*schema.AgenticMessage, error) {
	for _, ref := range req.Attachments {
		switch ref.Lane {
		case attLaneInlineText, attLaneInlineImage, attLaneInlinePDF, attLaneDrop, attLaneDocument:
		default:
			return nil, fmt.Errorf("attachment %q: unknown lane %q", ref.ID, ref.Lane)
		}
	}

	var blocks []*schema.ContentBlock
	if req.Input != "" {
		blocks = append(blocks, schema.NewContentBlock(&schema.UserInputText{Text: req.Input}))
	}

	if r.attachmentBlobs == nil {
		// materializeDropLane fails drop-lane refs the same way on a runner
		// without the resolver; inline-only refs fail here identically — an
		// error path, not a defaultable dependency.
		return nil, fmt.Errorf("inline attachments: the attachment blob resolver is not configured (WithAttachmentBlobs)")
	}

	for _, ref := range req.Attachments {
		if ref.Lane != attLaneInlineText {
			continue
		}
		blob, err := r.attachmentBlobs.OpenAttachment(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("open inline-text attachment %q: %w", ref.ID, err)
		}
		if len(blob) == 0 {
			return nil, fmt.Errorf("inline-text attachment %q: empty bytes", ref.ID)
		}
		blocks = append(blocks, schema.NewContentBlock(&schema.UserInputText{
			Text: fmt.Sprintf("```%s\n%s\n```", ref.Name, blob),
		}))
	}
	for _, ref := range req.Attachments {
		if ref.Lane != attLaneInlineImage {
			continue
		}
		if mod.image == domain.InputUnsupported {
			note, err := r.degradedAttachmentNote(ctx, req, ref, "images")
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, note)
			continue
		}
		blob, err := r.attachmentBlobs.OpenAttachment(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("open inline-image attachment %q: %w", ref.ID, err)
		}
		if len(blob) == 0 {
			return nil, fmt.Errorf("inline-image attachment %q: empty bytes", ref.ID)
		}
		url, err := r.attachmentBlobs.AttachmentURL(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve inline-image attachment %q URL: %w", ref.ID, err)
		}
		block := schema.NewContentBlock(&schema.UserInputImage{
			// Bytes only (fix-image-attachment-lane D1): the connector's
			// resolveURL prefers a non-empty URL over Base64Data, and a
			// server-relative capability URL is unfetchable for providers —
			// the URL rides the meta below instead of the payload.
			Base64Data: base64.StdEncoding.EncodeToString(blob),
			MIMEType:   ref.MimeType,
		})
		setAttachmentBlockMeta(block, attachmentBlockMeta{ID: ref.ID, Name: ref.Name, Mime: ref.MimeType, Size: ref.Size, Lane: ref.Lane, URL: url})
		blocks = append(blocks, block)
	}
	for _, ref := range req.Attachments {
		if ref.Lane != attLaneInlinePDF {
			continue
		}
		if mod.pdf == domain.InputUnsupported {
			note, err := r.degradedAttachmentNote(ctx, req, ref, "PDF files")
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, note)
			continue
		}
		blob, err := r.attachmentBlobs.OpenAttachment(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("open inline-pdf attachment %q: %w", ref.ID, err)
		}
		if len(blob) == 0 {
			return nil, fmt.Errorf("inline-pdf attachment %q: empty bytes", ref.ID)
		}
		url, err := r.attachmentBlobs.AttachmentURL(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve inline-pdf attachment %q URL: %w", ref.ID, err)
		}
		block := schema.NewContentBlock(&schema.UserInputFile{
			// Bytes only (fix-image-attachment-lane D1): same connector URL
			// preference as the image lane; the URL rides the meta below.
			Base64Data: base64.StdEncoding.EncodeToString(blob),
			MIMEType:   ref.MimeType,
			Name:       ref.Name,
		})
		setAttachmentBlockMeta(block, attachmentBlockMeta{ID: ref.ID, Name: ref.Name, Mime: ref.MimeType, Size: ref.Size, Lane: ref.Lane, URL: url})
		blocks = append(blocks, block)
	}
	for _, ref := range req.Attachments {
		if ref.Lane != attLaneDrop {
			continue
		}
		// The path mirrors materializeDropLane's layout exactly: it already
		// sanitized and wrote the file by the time construction runs, so
		// filepath.Base(ref.Name) is the name on disk.
		path := filepath.Join(r.dropLaneRunDir(runKeyOf(req)), dropLaneSeg(ref.ID), filepath.Base(ref.Name))
		// Document refs (add-document-read-tool spec: "Document attachments are
		// never model-bound blocks") must name the document.read tool; any
		// document ref — including legacy inline-pdf lane values that flow
		// through here — takes the same note. Other drop files (text, code,
		// logs) keep the filesystem-tools copy.
		noteText := fmt.Sprintf("The user attached the file %s; it is mounted read-only at %s. Use the filesystem tools to read it when you need its contents.", filepath.Base(ref.Name), path)
		if isDocumentRef(ref) {
			noteText = fmt.Sprintf("The user attached the document %s; it is mounted read-only at %s. Use the document.read tool to read its contents.", filepath.Base(ref.Name), path)
		}
		note := schema.NewContentBlock(&schema.UserInputText{
			Text: noteText,
		})
		note.Extra = map[string]any{AttachmentPointerExtraKey: true}
		// The note is a text block, so its attachment identity rides the meta
		// (URL included): the drop-lane attachment still renders as a
		// transcript chip on both paths (design D10) while the note text
		// itself stays hidden from the transcript (D8).
		url, err := r.attachmentBlobs.AttachmentURL(ctx, req.WorkspaceID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve drop-lane attachment %q URL: %w", ref.ID, err)
		}
		setAttachmentBlockMeta(note, attachmentBlockMeta{ID: ref.ID, Name: ref.Name, Mime: ref.MimeType, Size: ref.Size, Lane: ref.Lane, URL: url})
		blocks = append(blocks, note)
	}
	for _, ref := range req.Attachments {
		if ref.Lane != attLaneDocument {
			continue
		}
		// Document mention (add-reference-documents 10.4): the composer's
		// chip carries identity only, and the note is the same contract —
		// name plus mount path, no bytes opened, bound, or stamped. The
		// content lives in the run's references/ mount (D8), reached through
		// the document tools the manifest teaches.
		note := schema.NewContentBlock(&schema.UserInputText{
			Text: "User referenced document \"" + ref.Name + "\" — it is available read-only at " +
				references.MountDirName + "/" + ref.Name +
				"; use document.search or document.read to consult it.",
		})
		note.Extra = map[string]any{AttachmentPointerExtraKey: true}
		blocks = append(blocks, note)
	}

	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: blocks,
	}, nil
}

// degradedAttachmentNote builds the marked pointer note replacing an inline
// attachment block the turn's model affirmatively cannot view
// (fix-image-attachment-lane D4): the run succeeds, the note reaches the
// model but never transcript text (the AttachmentPointer marker), and the
// stamped identity — capability URL included — keeps the transcript pill
// alive on both projection paths exactly as a drop-lane note does. Byte
// resolution is skipped: a degraded ref never opens its payload — but the
// capability URL must still resolve, or the pill link breaks.
func (r *Runner) degradedAttachmentNote(ctx context.Context, req ExecRequest, ref AttachmentRef, viewNoun string) (*schema.ContentBlock, error) {
	url, err := r.attachmentBlobs.AttachmentURL(ctx, req.WorkspaceID, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve %s attachment %q URL: %w", ref.Lane, ref.ID, err)
	}
	note := schema.NewContentBlock(&schema.UserInputText{
		Text: fmt.Sprintf("The user attached the file %s (%s, %s), but the current model cannot view %s; it is attached as reference only.",
			ref.Name, ref.MimeType, humanAttachmentSize(ref.Size), viewNoun),
	})
	note.Extra = map[string]any{AttachmentPointerExtraKey: true}
	setAttachmentBlockMeta(note, attachmentBlockMeta{ID: ref.ID, Name: ref.Name, Mime: ref.MimeType, Size: ref.Size, Lane: ref.Lane, URL: url})
	return note, nil
}

// humanAttachmentSize renders an attachment size for the pointer note: whole
// bytes below 1 KiB, otherwise the largest binary unit with one decimal
// ("512 B", "1.5 MB").
func humanAttachmentSize(size int64) string {
	const unit = 1024
	switch {
	case size < unit:
		return fmt.Sprintf("%d B", size)
	case size < unit*unit:
		return fmt.Sprintf("%.1f KB", float64(size)/unit)
	case size < unit*unit*unit:
		return fmt.Sprintf("%.1f MB", float64(size)/(unit*unit))
	default:
		return fmt.Sprintf("%.1f GB", float64(size)/(unit*unit*unit))
	}
}
