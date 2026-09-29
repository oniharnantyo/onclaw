package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// NameDocumentSearch is the dotted capability name registered in the tool
// registry (add-reference-documents D4: document.search rides the
// document.* family seam — catalog entry, permission key, tool card, hook
// target all key on this string).
const NameDocumentSearch = "document.search"

// documentSearchHitLimit bounds one search result set (D4): GIN-indexed
// lookups return at most this many sections per call; the agent narrows with
// follow-up queries, never with an unbounded dump.
const documentSearchHitLimit = 8

// documentSearchReferencesMount is the model-facing mount prefix under which
// reference documents are addressed: `references/<document name>` is the path
// form document.read accepts for the run's references mount. The tool
// description and the per-hit read hints (fix-reference-document-retrieval
// D2) both build from this constant so they cannot drift apart.
const documentSearchReferencesMount = "references"

// DocumentSearcher is the narrow port document.search needs: the
// visibility-filtered full-text search over the workspace's reference-document
// section index (D4/D7 — the WHERE-clause half of the visibility predicate
// lives behind this call, evaluated per query exactly as the compose-time
// manifest evaluates it). The references service implements it; that service
// imports this package, so the port lives here and the runner-side aggregate
// (agents.DocumentTools) satisfies it structurally — never the reverse.
type DocumentSearcher interface {
	SearchDocuments(ctx context.Context, workspaceID string, scope domain.DocumentRunScope, query string, limit int) ([]domain.DocumentSectionHit, error)
}

// documentSearchTool is the read-only search over the workspace's indexed
// reference documents. The run's identity and visibility scope are fixed at
// construction from the ToolContext — the query arguments carry no identity
// fields and can never widen the visible set (the memory-search posture).
// The tool writes nothing and calls no model.
type documentSearchTool struct {
	searcher    DocumentSearcher
	workspaceID string
	scope       domain.DocumentRunScope
}

// NewDocumentSearch constructs the search tool bound to the executing run's
// identity and visibility scope. The searcher is composition-root wiring
// (never nil): the registry registers the tool only when the references
// service is wired, mirroring the schedule/memory-search precedent — an
// unwired deployment surfaces no document.search at all, not a broken one.
func NewDocumentSearch(searcher DocumentSearcher, workspaceID string, scope domain.DocumentRunScope) (tool.BaseTool, error) {
	return &documentSearchTool{searcher: searcher, workspaceID: workspaceID, scope: scope}, nil
}

// Info returns the tool schema surfaced to agentic models. The description
// carries the library contract: what references/ is, that hits are
// visibility-filtered, and how to cite.
func (t *documentSearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameDocumentSearch,
		Desc: "Search the workspace's reference documents library — files uploaded to the workspace and mounted read-only under " + documentSearchReferencesMount + "/. " +
			"Full-text search returns bounded hits (document, heading, locator, snippet, read); hits include only the documents visible to this conversation. " +
			"Each hit's read field is the exact next document.read invocation for that section — copy it verbatim instead of constructing a path. " +
			"Read only: it never modifies documents. " +
			"Cite a hit by linking " + documentSearchReferencesMount + "/<document name> together with its locator (page, slide, sheet, or heading), " +
			"then read the exact section with document.read pages/section before relying on it.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "Free-text search across the indexed section text of the visible reference documents.",
				Required: true,
			},
		}),
	}, nil
}

// documentSearchArgs is the deserialized tool-call argument shape. It
// deliberately carries no identity or scope fields.
type documentSearchArgs struct {
	Query string `json:"query"`
}

// documentSearchHit is one result row's model-facing projection (D4): the
// matched section joined to its document for citation — the wire keys the
// transcript's document.search tool card and citation chips render. Read
// carries the ready-to-use next document.read invocation (D2 of
// fix-reference-document-retrieval) so the model never improvises a path.
type documentSearchHit struct {
	DocumentID  string `json:"documentId"`
	Document    string `json:"document"`
	Heading     string `json:"heading"`
	Locator     string `json:"locator"`
	LocatorKind string `json:"locatorKind"`
	Snippet     string `json:"snippet"`
	Read        string `json:"read"`
}

// documentSearchReadHint renders one hit's `read` field (D2): the exact next
// document.read invocation for the section, spelled with document.read's real
// parameter names (path, pages, section) so the model copies it verbatim
// instead of improvising a filesystem path. Page locators ("p. 30", the
// sectioner's PDF form) scope by the pages parameter's bare number; sheet
// locators ("sheet Users") scope by the sheet name the converter renders as
// the section heading; slide and heading locators are already section titles
// document.read matches case-insensitively. A hit without a locator (txt/csv
// whole-document sections) hints the unscoped read.
func documentSearchReadHint(documentName, locatorKind, locator string) string {
	path := documentSearchReferencesMount + "/" + documentName
	if strings.TrimSpace(locator) == "" {
		return NameDocumentRead + " path=" + path
	}
	switch locatorKind {
	case domain.LocatorKindPage:
		if page, ok := strings.CutPrefix(locator, "p. "); ok && strings.TrimSpace(page) != "" {
			return NameDocumentRead + " path=" + path + " pages=" + page
		}
	case domain.LocatorKindSheet:
		if sheet, ok := strings.CutPrefix(locator, "sheet "); ok && strings.TrimSpace(sheet) != "" {
			return NameDocumentRead + " path=" + path + " section=" + sheet
		}
	}
	return NameDocumentRead + " path=" + path + " section=" + locator
}

// InvokableRun satisfies tool.InvokableTool. Argument and infrastructure
// failures return errors (the runtime's tool-error middleware turns them into
// JSON error results while the run continues); a successful search always
// returns the bounded result envelope, empty hits included.
func (t *documentSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args documentSearchArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("document.search: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("document.search: query is required")
	}

	hits, err := t.searcher.SearchDocuments(ctx, t.workspaceID, t.scope, query, documentSearchHitLimit)
	if err != nil {
		return "", fmt.Errorf("document.search: %w", err)
	}

	rows := make([]documentSearchHit, 0, len(hits))
	for _, hit := range hits {
		rows = append(rows, documentSearchHit{
			DocumentID:  hit.DocumentID,
			Document:    hit.DocumentName,
			Heading:     hit.Heading,
			Locator:     hit.Locator,
			LocatorKind: hit.LocatorKind,
			Snippet:     hit.Snippet,
			Read:        documentSearchReadHint(hit.DocumentName, hit.LocatorKind, hit.Locator),
		})
	}
	out, err := json.Marshal(map[string]any{
		"query": query,
		"hits":  rows,
	})
	if err != nil {
		return "", fmt.Errorf("document.search: encode result: %w", err)
	}
	return string(out), nil
}
