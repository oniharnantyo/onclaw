package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/store"
)

// saveDocumentToolDescription is the model-facing description of the
// save_document tool; the settings surfaces render the catalog entry's
// shorter one (builtin_tools.go). Pure opt-in curation
// (fix-pdf-attachment-carriage design D5): chat attachments are already
// readable from the workspace's uploads/ mirror, so no degradation route
// runs through the corpus — saving is for durable promotion, not access.
const saveDocumentToolDescription = "Save a document into this agent's durable knowledge corpus so later turns can search it with the filesystem tools. " +
	"Chat attachments are already readable from /workspace/uploads, so saving one is for durable curation, not for access. " +
	"Provide exactly one content source: attachment_key (the capability key of a chat attachment, saved under its original filename unless filename says otherwise) " +
	"or text (inline content saved under the required filename). " +
	"Prefer a descriptive filename and fill description — both are what future searches have to go on. " +
	"Rejections (unsupported kind, size cap, duplicate filename) are reported in the result's error field; they do not fail the call."

// newSaveDocumentTool builds the save_document invokable tool: the one write
// path into the session agent's knowledge scope (add-knowledge-corpus design
// D4). A chat attachment is a host-side copy from the attachments store into
// the corpus — no new trust boundary crossed; inline text saves directly.
// Every ingestion-gate rejection comes back as a readable {ok:false} tool
// result so the model can react and the turn continues; a Go error is
// returned only for infrastructure failures (store I/O).
func newSaveDocumentTool(k *knowledge.Store, atts *attachments.Store, agentID, agentSlug string) (tool.BaseTool, error) {
	if k == nil {
		return nil, errors.New("save_document: no knowledge store configured")
	}
	st := &saveDocumentTool{k: k, atts: atts, agentID: agentID, agentSlug: agentSlug}
	return utils.InferTool(ToolSaveDocument, saveDocumentToolDescription, st.run)
}

// saveDocumentTool is the save_document tool's closed-over dependencies: the
// knowledge store it saves through and the attachments store it reads
// chat-attachment content from (nil when the runtime was built without one —
// inline text still works), plus the calling agent its rows are scoped to.
type saveDocumentTool struct {
	k         *knowledge.Store
	atts      *attachments.Store
	agentID   string
	agentSlug string
}

// SaveDocumentArgs is the tool's whole input. Exactly one of AttachmentKey
// and Text must be non-empty; Filename is required for inline text and
// defaults to the attachment's original name on the attachment path.
type SaveDocumentArgs struct {
	// AttachmentKey is the capability key of a chat attachment riding this
	// conversation; its bytes are copied into the corpus.
	AttachmentKey string `json:"attachment_key,omitempty"`
	// Text is the inline document content.
	Text string `json:"text,omitempty"`
	// Filename is the document's name in the corpus. Required for inline
	// text; on the attachment path it overrides the upload's original name.
	Filename string `json:"filename,omitempty"`
	// Description is the document's discovery handle (add-knowledge-corpus
	// design D6): what future searches have to go on, especially for PDFs.
	Description string `json:"description,omitempty"`
}

// SaveDocumentOutput is the tool's whole output: ok=false with Error for
// every rejected request, the saved document's addressable facts on success.
// Path is the document's mount-scoped location the filesystem tools read it
// at (add-knowledge-corpus design D3).
type SaveDocumentOutput struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	Path        string `json:"path,omitempty"`
	Filename    string `json:"filename,omitempty"`
	MIME        string `json:"mime,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Description string `json:"description,omitempty"`
}

// run executes one save_document tool call.
func (t *saveDocumentTool) run(ctx context.Context, in SaveDocumentArgs) (SaveDocumentOutput, error) {
	var data []byte
	var mime, filename string
	switch {
	case in.AttachmentKey != "" && in.Text != "":
		return SaveDocumentOutput{OK: false,
			Error: "provide exactly one content source: attachment_key or text, not both",
		}, nil
	case in.AttachmentKey != "":
		content, err := t.readAttachment(ctx, in.AttachmentKey)
		if err != nil {
			return SaveDocumentOutput{}, err
		}
		if content == nil {
			return SaveDocumentOutput{OK: false,
				Error: fmt.Sprintf("no attachment with key %s", in.AttachmentKey),
			}, nil
		}
		data = content.Data
		mime = content.Meta.Mime
		filename = content.Meta.Name
		if in.Filename != "" {
			filename = in.Filename
		}
	case in.Text != "":
		data = []byte(in.Text)
		filename = in.Filename
	default:
		return SaveDocumentOutput{OK: false,
			Error: "provide exactly one content source: attachment_key (a chat attachment's capability key) or text (inline content)",
		}, nil
	}
	if filename == "" {
		return SaveDocumentOutput{OK: false,
			Error: "filename: filename is required (or save from an attachment, whose original filename applies)",
		}, nil
	}

	doc, err := t.k.Save(ctx, knowledge.SaveInput{
		Filename:    filename,
		MIME:        mime,
		Data:        data,
		Scope:       knowledge.ScopeAgent,
		AgentID:     t.agentID,
		AgentSlug:   t.agentSlug,
		Description: in.Description,
		Source:      knowledge.SourceAgent,
	})
	if err != nil {
		// Gate rejections (unsupported kind, oversize, unusable filename,
		// duplicate name, slugless agent) are readable results; only store
		// I/O fails the call.
		if errors.Is(err, knowledge.ErrRejected) || errors.Is(err, store.ErrDuplicate) {
			return SaveDocumentOutput{OK: false, Error: err.Error()}, nil
		}
		return SaveDocumentOutput{}, fmt.Errorf("save document: %w", err)
	}
	return SaveDocumentOutput{
		OK:          true,
		Path:        backend.KnowledgeMountPoint + "/agent/" + doc.Filename,
		Filename:    doc.Filename,
		MIME:        doc.MIME,
		Size:        doc.Size,
		Description: doc.Description,
	}, nil
}

// readAttachment resolves one capability key through the attachments store.
// An unknown key is a readable rejection (nil, nil); store I/O failures are
// Go errors.
func (t *saveDocumentTool) readAttachment(ctx context.Context, key string) (*attachments.Content, error) {
	if t.atts == nil {
		return nil, errors.New("save_document: no attachment store configured")
	}
	content, err := t.atts.Read(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	return &content, nil
}
