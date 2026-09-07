package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"golang.org/x/net/html"
)

const ddgSample = `<html><body>
<div class="result">
<a href="https://example.com/a" class="result__a">Example A</a>
<a class="result__snippet">Snippet A</a>
</div>
<div class="result">
<a href="https://example.com/b" class="result__a">Example B</a>
<a class="result__snippet">Snippet B</a>
</div>
<div class="result">
<a href="https://example.com/a" class="result__a">Dup A</a>
</div>
</body></html>`

func TestWebSearch_Info(t *testing.T) {
	tl, err := NewWebSearch()
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "web.search" {
		t.Errorf("name = %q, want web.search", info.Name)
	}
	if info.ParamsOneOf == nil {
		t.Fatal("expected ParamsOneOf populated")
	}
}

func TestWebSearch_InvokableRun(t *testing.T) {
	fc := &fakeClient{
		status: 200,
		body:   ddgSample,
	}
	tl, err := NewWebSearch(WithHTTPClient(fc))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	it, ok := tl.(tool.InvokableTool)
	if !ok {
		t.Fatal("webSearchTool must implement tool.InvokableTool")
	}

	out, err := it.InvokableRun(context.Background(), `{"query":"test"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(out, "example.com/a") {
		t.Errorf("expected result with example.com/a, got %q", out)
	}
	// dedup check: example.com/a appears once
	count := strings.Count(out, "example.com/a")
	if count != 1 {
		t.Errorf("expected 1 occurrence of example.com/a (dedup), got %d", count)
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	tl, err := NewWebSearch()
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"   "}`)
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

func TestWebSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	fc := &fakeClient{
		status: 503,
		body:   "no service",
	}
	tl, err := NewWebSearch(WithHTTPClient(fc))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestWebSearch_DuckDuckGoParsing(t *testing.T) {
	// parse a real doc and verify dedup + limit
	sr := strings.NewReader(ddgSample)
	doc, err := html.Parse(sr)
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	results := extractResults(doc, 5)
	if len(results) != 2 {
		t.Errorf("expected 2 unique results, got %d", len(results))
	}
}

// fakeClient is an HTTPClient that returns canned responses.
type fakeClient struct {
	status int
	body   string
}

func (c *fakeClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     make(http.Header),
	}, nil
}
