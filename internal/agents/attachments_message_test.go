package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/references"
)

// ---------------------------------------------------------------------------
// Document-mention pointer note (add-reference-documents 10.4): the composer
// sends {kind: "document", documentId, name, path} through the same
// ExecRequest.Attachments lane chat-attachment chips ride; the turn carries a
// pointer note with document identity only — no content.
// ---------------------------------------------------------------------------

// TestBuildAttachmentUserMessage_DocumentMentionPointerNote pins the note
// text and the identity-only contract: exactly one marked pointer block per
// document mention, no byte lookups for it, no attachment meta stamped.
func TestBuildAttachmentUserMessage_DocumentMentionPointerNote(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{})
	r := &Runner{attachmentBlobs: blobs, onClawDir: t.TempDir()}
	req := ExecRequest{
		WorkspaceID: "ws-doc",
		Input:       "compare this with the official limits",
		Attachments: []AttachmentRef{
			{ID: "doc-1", Name: "integration-notes.md", Lane: attLaneDocument},
		},
	}

	msg, err := r.buildAttachmentUserMessage(context.Background(), req, inputModality{})
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}

	// Two blocks: the user's own text, then the pointer note.
	if len(msg.ContentBlocks) != 2 {
		t.Fatalf("block count = %d, want 2 (text, note)", len(msg.ContentBlocks))
	}
	if text := msg.ContentBlocks[0].UserInputText.Text; text != req.Input {
		t.Errorf("user text block = %q, want the input verbatim", text)
	}
	note := msg.ContentBlocks[1]
	if !isAttachmentPointerNote(note) {
		t.Fatalf("document mention block is not a marked pointer note: %+v", note.Extra)
	}
	want := "User referenced document \"integration-notes.md\" — it is available read-only at " +
		references.MountDirName + "/integration-notes.md" +
		"; use document.search or document.read to consult it."
	if got := note.UserInputText.Text; got != want {
		t.Errorf("pointer note =\n%q\nwant\n%q", got, want)
	}
	// Identity only: no attachment meta stamped, so the note never renders as
	// a file chip (the composer's own pill is the transcript surface).
	if _, ok := attachmentBlockMetaOf(note); ok {
		t.Errorf("document pointer note carries attachment meta; identity-only means none")
	}
	// No content was fetched: the document lane never opens bytes.
	if got := len(blobs.wsIDs); got != 0 {
		t.Errorf("byte lookups for a document mention = %d, want 0", got)
	}
}

// TestBuildAttachmentUserMessage_DocumentMentionRidesAttachmentsLane pins the
// "same payload lane" contract: document chips ride alongside ordinary
// attachment refs in one turn — each drop ref keeps its own note and meta,
// the document ref keeps its note — and the document ref is invisible to the
// drop-lane materialization (identity only, nothing to download).
func TestBuildAttachmentUserMessage_DocumentMentionRidesAttachmentsLane(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{"att-drop": []byte("%PDF-x")})
	r := &Runner{attachmentBlobs: blobs, onClawDir: t.TempDir()}
	req := ExecRequest{
		WorkspaceID: "ws-mixed",
		Input:       "use both",
		Attachments: []AttachmentRef{
			{ID: "att-drop", Name: "brief.pdf", MimeType: "application/pdf", Lane: attLaneDrop, Size: 6},
			{ID: "doc-2", Name: "limits.pdf", Lane: attLaneDocument},
		},
	}

	// The document ref must not trip the drop-lane fast path; only actual
	// drop refs may.
	dropOnly := ExecRequest{Attachments: []AttachmentRef{{ID: "doc-2", Name: "limits.pdf", Lane: attLaneDocument}}}
	if hasDropLaneAttachments(dropOnly) {
		t.Fatalf("document ref tripped hasDropLaneAttachments; it must never materialize")
	}
	if _, err := r.materializeDropLane(context.Background(), dropOnly); err != nil {
		t.Fatalf("materializeDropLane with a document-only turn: %v", err)
	}

	msg, err := r.buildAttachmentUserMessage(context.Background(), req, inputModality{})
	if err != nil {
		t.Fatalf("buildAttachmentUserMessage: %v", err)
	}

	// Text + drop note + document note.
	if len(msg.ContentBlocks) != 3 {
		t.Fatalf("block count = %d, want 3 (text, drop note, document note)", len(msg.ContentBlocks))
	}
	dropNote := msg.ContentBlocks[1]
	if !isAttachmentPointerNote(dropNote) || dropNote.UserInputText == nil ||
		!strings.Contains(dropNote.UserInputText.Text, "attached the document") {
		t.Fatalf("drop-lane note missing or reshaped: %+v", dropNote)
	}
	if _, ok := attachmentBlockMetaOf(dropNote); !ok {
		t.Errorf("drop-lane note lost its attachment meta (the transcript chip)")
	}
	docNote := msg.ContentBlocks[2]
	if !isAttachmentPointerNote(docNote) || docNote.UserInputText == nil ||
		!strings.Contains(docNote.UserInputText.Text, "User referenced document \"limits.pdf\"") {
		t.Errorf("document note missing or reshaped: %+v", docNote)
	}
}

// TestBuildAttachmentUserMessage_UnknownLaneStillRejected pins today's
// behavior for kinds the lane vocabulary does not name: an unknown lane value
// fails the build exactly as before.
func TestBuildAttachmentUserMessage_UnknownLaneStillRejected(t *testing.T) {
	blobs := newStubBlobs(map[string][]byte{})
	r := &Runner{attachmentBlobs: blobs, onClawDir: t.TempDir()}
	req := ExecRequest{
		WorkspaceID: "ws-unknown",
		Input:       "hi",
		Attachments: []AttachmentRef{
			{ID: "att-x", Name: "mystery.bin", Lane: "carrier-pigeon"},
		},
	}
	if _, err := r.buildAttachmentUserMessage(context.Background(), req, inputModality{}); err == nil {
		t.Fatalf("unknown lane must fail the build")
	}
}
