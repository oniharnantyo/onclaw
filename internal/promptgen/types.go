package promptgen

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/buddies/internal/store"
)

// ErrInvalid marks a generation request that cannot run as given — an empty
// slug, agent name, or brief. Callers test with errors.Is; concrete failures
// wrap it with %w and context.
var ErrInvalid = errors.New("invalid prompt generation request")

// Documents are the three generated prompt documents, as full markdown.
type Documents struct{ Identity, Soul, Bootstrap string }

// blank reports whether every document is empty or whitespace — a workspace
// holding only blanks has nothing to enhance and regenerates fresh.
func (d Documents) blank() bool {
	return strings.TrimSpace(d.Identity) == "" &&
		strings.TrimSpace(d.Soul) == "" &&
		strings.TrimSpace(d.Bootstrap) == ""
}

// Params carries everything one generation needs. No DB, no disk paths — the
// caller persists the brief and resolves the provider before calling.
type Params struct {
	// Provider is the agent's own provider: generation runs on it.
	Provider store.Provider
	// Model is the agent's configured model id.
	Model string
	// AgentName is the configured agent name; the IDENTITY document's name
	// field is load-bearing and must match it.
	AgentName string
	// Brief is the goal-and-behavior brief; persisted by the caller, required.
	Brief string
	// Instruction is an optional user change instruction (regeneration only).
	// It rides along as a requested-changes section and never replaces the
	// documents.
	Instruction string
}

// ModelFactory builds the eino chat model one generation runs on.
type ModelFactory interface {
	// Build returns the chat model for one generation on the provider's
	// configured endpoint. structured requests the provider's native
	// structured-output constraint (a JSON-schema response format or the
	// adapter's equivalent); false leaves the model free-form, with the JSON
	// contract carried by the system prompt alone.
	Build(ctx context.Context, provider store.Provider, modelName string, structured bool) (model.BaseChatModel, error)
}

// DocStore persists documents for an agent slug. Implemented over the
// workspaces manager: CommitDocuments backs up each existing document as
// <name>.bak first, and ANY backup failure aborts the whole commit (no
// document changes) so a failed generation leaves the previous documents
// untouched.
type DocStore interface {
	// ReadDocuments returns the current documents; exists=false when the
	// workspace holds none (fresh mode).
	ReadDocuments(slug string) (Documents, bool, error)
	// CommitDocuments writes all three documents.
	CommitDocuments(slug string, docs Documents) error
}
