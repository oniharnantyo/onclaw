package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakeDocumentSearcher is a scripted DocumentSearcher recording the search
// call's identity and visibility binding.
type fakeDocumentSearcher struct {
	calls []fakeSearchCall
	hits  []domain.DocumentSectionHit
	err   error
}

type fakeSearchCall struct {
	workspaceID string
	scope       domain.DocumentRunScope
	query       string
	limit       int
}

func (f *fakeDocumentSearcher) SearchDocuments(_ context.Context, workspaceID string, scope domain.DocumentRunScope, query string, limit int) ([]domain.DocumentSectionHit, error) {
	f.calls = append(f.calls, fakeSearchCall{workspaceID: workspaceID, scope: scope, query: query, limit: limit})
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

// newTestDocumentSearch builds the search tool bound to a fake searcher.
func newTestDocumentSearch(t *testing.T, fake *fakeDocumentSearcher) tool.BaseTool {
	t.Helper()
	tr, err := NewDocumentSearch(fake, "ws-1", domain.DocumentRunScope{
		AgentID:          "ag-1",
		ChannelID:        "ch-9",
		IsChannelSession: true,
	})
	if err != nil {
		t.Fatalf("NewDocumentSearch: %v", err)
	}
	return tr
}

// runDocumentSearch invokes the tool with a raw query string and decodes the
// result envelope.
func runDocumentSearch(t *testing.T, tr tool.BaseTool, rawQuery string) (string, error, map[string]any) {
	t.Helper()
	inv, ok := tr.(tool.InvokableTool)
	if !ok {
		t.Fatalf("tool %T does not implement InvokableRun", tr)
	}
	args, err := json.Marshal(map[string]string{"query": rawQuery})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), string(args))
	var result map[string]any
	if err == nil {
		// Error paths return no payload; only decode real results.
		if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
			t.Fatalf("decode result JSON %q: %v", out, jsonErr)
		}
	}
	return out, err, result
}

func TestDocumentSearchResultShape(t *testing.T) {
	fake := &fakeDocumentSearcher{hits: []domain.DocumentSectionHit{
		{
			DocumentID:   "doc-1",
			DocumentName: "service-manual.pdf",
			Heading:      "Webhooks",
			Locator:      "p. 30",
			LocatorKind:  domain.LocatorKindPage,
			Snippet:      "Signing secrets rotate every 90 days ...",
		},
	}}
	tr := newTestDocumentSearch(t, fake)

	out, err, result := runDocumentSearch(t, tr, "  webhook signing  ")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if got := result["query"]; got != "webhook signing" {
		t.Errorf("query = %v, want the trimmed input", got)
	}
	hits, ok := result["hits"].([]any)
	if !ok {
		t.Fatalf("hits = %#v, want a non-nil array", result["hits"])
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d rows, want 1: %s", len(hits), out)
	}
	hit, ok := hits[0].(map[string]any)
	if !ok {
		t.Fatalf("hit row shape unexpected: %#v", hits[0])
	}
	wantKeys := map[string]string{
		"documentId":  "doc-1",
		"document":    "service-manual.pdf",
		"heading":     "Webhooks",
		"locator":     "p. 30",
		"locatorKind": domain.LocatorKindPage,
		"snippet":     "Signing secrets rotate every 90 days ...",
		"read":        "document.read path=references/service-manual.pdf pages=30",
	}
	for key, want := range wantKeys {
		if got := hit[key]; got != want {
			t.Errorf("hit[%q] = %v, want %q", key, got, want)
		}
	}

	// Identity and visibility scope bind at construction; the query rides
	// trimmed; the result set is bounded by the family limit.
	if len(fake.calls) != 1 {
		t.Fatalf("searcher calls = %d, want 1", len(fake.calls))
	}
	call := fake.calls[0]
	if call.workspaceID != "ws-1" {
		t.Errorf("workspaceID = %q, want ws-1", call.workspaceID)
	}
	if call.scope.AgentID != "ag-1" || call.scope.ChannelID != "ch-9" || !call.scope.IsChannelSession {
		t.Errorf("scope = %+v, want the construction-bound run scope", call.scope)
	}
	if call.query != "webhook signing" {
		t.Errorf("query = %q, want the trimmed input", call.query)
	}
	if call.limit != documentSearchHitLimit {
		t.Errorf("limit = %d, want %d", call.limit, documentSearchHitLimit)
	}
}

// TestDocumentSearchHitReadHints pins the read-hint contract
// (fix-reference-document-retrieval task 2.1): a heading-located hit carries
// the scoped document.read invocation, and a locator-less txt hit carries the
// unscoped one — the model never has to improvise a filesystem path.
func TestDocumentSearchHitReadHints(t *testing.T) {
	fake := &fakeDocumentSearcher{hits: []domain.DocumentSectionHit{
		{
			DocumentID:   "doc-1",
			DocumentName: "FDS sokratech.docx",
			Heading:      "5.1 PostLogin",
			Locator:      "5.1 PostLogin",
			LocatorKind:  domain.LocatorKindHeading,
			Snippet:      "The PostLogin payload is signed with ...",
		},
		{
			DocumentID:   "doc-2",
			DocumentName: "release-notes.txt",
			Heading:      "release-notes.txt",
			LocatorKind:  domain.LocatorKindNone,
			Snippet:      "whole-document text ...",
		},
	}}
	tr := newTestDocumentSearch(t, fake)

	out, err, result := runDocumentSearch(t, tr, "postlogin")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	hits, ok := result["hits"].([]any)
	if !ok || len(hits) != 2 {
		t.Fatalf("hits = %#v, want 2 rows: %s", result["hits"], out)
	}
	scoped, ok := hits[0].(map[string]any)
	if !ok {
		t.Fatalf("heading hit row shape unexpected: %#v", hits[0])
	}
	unscoped, ok := hits[1].(map[string]any)
	if !ok {
		t.Fatalf("txt hit row shape unexpected: %#v", hits[1])
	}
	if got, want := scoped["read"], "document.read path=references/FDS sokratech.docx section=5.1 PostLogin"; got != want {
		t.Errorf("heading hit read = %q, want %q", got, want)
	}
	if got, want := unscoped["read"], "document.read path=references/release-notes.txt"; got != want {
		t.Errorf("txt hit read = %q, want %q", got, want)
	}
}

// TestDocumentSearchReadHintKinds pins the helper's per-locator-kind mapping
// against document.read's real parameter contract: page locators scope by the
// pages parameter's bare number, sheet locators by the sheet name the
// converter renders as the heading, slide and heading locators pass through
// as section titles, and locator-less hits hint the unscoped read.
func TestDocumentSearchReadHintKinds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		document    string
		locatorKind string
		locator     string
		want        string
	}{
		{"page-locator-scopes-pages", "manual.pdf", domain.LocatorKindPage, "p. 30",
			"document.read path=references/manual.pdf pages=30"},
		{"slide-locator-is-a-section-title", "deck.pptx", domain.LocatorKindSlide, "slide 5",
			"document.read path=references/deck.pptx section=slide 5"},
		{"sheet-locator-scopes-sheet-name", "data.xlsx", domain.LocatorKindSheet, "sheet Users",
			"document.read path=references/data.xlsx section=Users"},
		{"heading-locator-is-the-section-title", "fds.docx", domain.LocatorKindHeading, "5.1 PostLogin",
			"document.read path=references/fds.docx section=5.1 PostLogin"},
		{"locator-less-hints-unscoped-read", "notes.txt", domain.LocatorKindNone, "",
			"document.read path=references/notes.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := documentSearchReadHint(tc.document, tc.locatorKind, tc.locator)
			if got != tc.want {
				t.Errorf("documentSearchReadHint(%q, %q, %q) = %q, want %q", tc.document, tc.locatorKind, tc.locator, got, tc.want)
			}
		})
	}
}

func TestDocumentSearchEmptyHitsAreAnArray(t *testing.T) {
	tr := newTestDocumentSearch(t, &fakeDocumentSearcher{})

	_, err, result := runDocumentSearch(t, tr, "nothing matches this")
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	hits, ok := result["hits"].([]any)
	if !ok {
		t.Fatalf("hits = %#v, want an array (never null)", result["hits"])
	}
	if len(hits) != 0 {
		t.Errorf("hits = %v, want empty", hits)
	}
}

// TestDocumentSearchForgedIdentityFieldsIgnored pins the memory-search
// posture: the argument JSON carries no identity or scope inputs — a forged
// workspace_id changes nothing, because identity binds at construction.
func TestDocumentSearchForgedIdentityFieldsIgnored(t *testing.T) {
	fake := &fakeDocumentSearcher{}
	tr := newTestDocumentSearch(t, fake)

	inv, ok := tr.(tool.InvokableTool)
	if !ok {
		t.Fatal("tool must implement InvokableRun")
	}
	if _, err := inv.InvokableRun(context.Background(), `{"query":"x","workspace_id":"ws-evil","agent_id":"ag-evil"}`); err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("searcher calls = %d, want 1", len(fake.calls))
	}
	if fake.calls[0].workspaceID != "ws-1" || fake.calls[0].scope.AgentID != "ag-1" {
		t.Errorf("forged identity fields leaked: %+v", fake.calls[0])
	}
}

func TestDocumentSearchInputValidation(t *testing.T) {
	tr := newTestDocumentSearch(t, &fakeDocumentSearcher{})

	for name, raw := range map[string]string{
		"missing":    `{}`,
		"empty":      `{"query":""}`,
		"whitespace": `{"query":"   "}`,
		"wrong-type": `{"query":42}`,
		"malformed":  `{"query":`,
	} {
		t.Run(name, func(t *testing.T) {
			inv, ok := tr.(tool.InvokableTool)
			if !ok {
				t.Fatal("tool must implement InvokableRun")
			}
			out, err := inv.InvokableRun(context.Background(), raw)
			if err == nil {
				t.Fatalf("expected parameter error for %s, got result %s", raw, out)
			}
			if !strings.HasPrefix(err.Error(), "document.search:") {
				t.Errorf("error %q should be namespaced to the tool", err)
			}
		})
	}
}

func TestDocumentSearchSearcherErrorIsInvocationError(t *testing.T) {
	tr := newTestDocumentSearch(t, &fakeDocumentSearcher{err: errors.New("index unavailable")})

	_, err, _ := runDocumentSearch(t, tr, "anything")
	if err == nil {
		t.Fatal("expected the searcher error to surface as an invocation error")
	}
	if !strings.Contains(err.Error(), "index unavailable") {
		t.Errorf("error %q should wrap the searcher failure", err)
	}
}

func TestDocumentSearchInfoContract(t *testing.T) {
	tr := newTestDocumentSearch(t, &fakeDocumentSearcher{})

	info, err := tr.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != NameDocumentSearch {
		t.Errorf("Info.Name = %q, want %q", info.Name, NameDocumentSearch)
	}
	// The description is the family-seam contract surface: the references
	// mount, visibility filtering, the read-hint guidance, and the citation
	// instruction.
	for _, want := range []string{"references/", "read-only", "visible to this conversation", "references/<document name>", "locator", "read field", "document.read invocation"} {
		if !strings.Contains(info.Desc, want) {
			t.Errorf("Info.Desc missing %q:\n%s", want, info.Desc)
		}
	}
}
