package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

type fetchFakeResp struct {
	status int
	body   string
	header http.Header
}

type fetchFakeClient struct {
	resp *fetchFakeResp
}

func (f *fetchFakeClient) Do(req *http.Request) (*http.Response, error) {
	h := f.resp.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{
		StatusCode: f.resp.status,
		Body:       io.NopCloser(strings.NewReader(f.resp.body)),
		Header:     h,
		Request:    req,
	}, nil
}

type invokable interface {
	InvokableRun(context.Context, string, ...tool.Option) (string, error)
}

func newFetchTool(t *testing.T, resp *fetchFakeResp, opts ...WebFetchOption) invokable {
	t.Helper()
	tl, err := NewWebFetch(append([]WebFetchOption{WithFetchHTTPClient(&fetchFakeClient{resp: resp})}, opts...)...)
	if err != nil {
		t.Fatalf("NewWebFetch: %v", err)
	}
	return tl.(invokable)
}

func TestWebFetch_HTMLExtraction(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		body:   "<html><head><title>t</title></head><body><script>x()</script><h1>Hello</h1><p>World of <b>fetch</b>.</p></body></html>",
	})
	out, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/page"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	for _, want := range []string{"Hello", "World of fetch", "https://example.com/page"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "x()") {
		t.Errorf("script content leaked: %s", out)
	}
}

func TestWebFetch_JSONPassthrough(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"application/json"}},
		body:   `{"key": "<b>value</b>"}`,
	})
	out, err := tl.InvokableRun(context.Background(), `{"url":"https://api.example.com/data.json"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(out, `\{\"key\": \"`) && !strings.Contains(out, `value`) {
		t.Errorf("JSON body should pass through unmodified: %s", out)
	}
}

func TestWebFetch_Truncation(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"text/plain"}},
		body:   strings.Repeat("a", 2<<20),
	})
	out, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/big"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if len(out) > (1<<20)+512 {
		t.Errorf("output not truncated: %d bytes", len(out))
	}
	if !strings.Contains(out, "[truncated") {
		t.Errorf("truncation marker missing")
	}
}

func TestWebFetch_SSRFBlocked(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1/x", "http://[::1]/x", "http://10.0.0.5/x",
		"http://192.168.1.1/x", "http://169.254.169.254/latest/meta-data",
		"ftp://example.com/x", "file:///etc/passwd",
	} {
		tl := newFetchTool(t, &fetchFakeResp{status: 200, body: "secret"})
		if _, err := tl.InvokableRun(context.Background(), fmt.Sprintf(`{"url":%q}`, target)); err == nil {
			t.Errorf("expected %s to be blocked", target)
		}
	}
}

func TestWebFetch_SSRFOptOut(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200, body: "ok",
		header: http.Header{"Content-Type": []string{"text/plain"}},
	}, WithFetchAllowPrivate(true))
	out, err := tl.InvokableRun(context.Background(), `{"url":"http://127.0.0.1:8080/health"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected body, got %s", out)
	}
}

func TestWebFetch_ErrorStatus(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{status: 500})
	if _, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/x"}`); err == nil {
		t.Error("expected error on non-200")
	}
}

// TestWebFetch_RedirectRevalidated exercises the guarded client's per-hop
// CheckRedirect directly: a redirect from a public URL to a private or
// non-http target must be denied even though the original URL was fine.
func TestWebFetch_RedirectRevalidated(t *testing.T) {
	client := NewFetchHTTPClient(false)
	if client.CheckRedirect == nil {
		t.Fatal("guarded client has no CheckRedirect")
	}
	original, err := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	if err != nil {
		t.Fatalf("original request: %v", err)
	}

	privateHop, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/secret", nil)
	if err != nil {
		t.Fatalf("private hop: %v", err)
	}
	if err := client.CheckRedirect(privateHop, []*http.Request{original}); err == nil {
		t.Error("public→private redirect hop must be blocked")
	}

	ftpURL, err := url.Parse("ftp://example.com/x")
	if err != nil {
		t.Fatalf("ftp url: %v", err)
	}
	if err := client.CheckRedirect(&http.Request{URL: ftpURL}, []*http.Request{original}); err == nil {
		t.Error("redirect to a non-http scheme must be blocked")
	}

	publicHop, err := http.NewRequest(http.MethodGet, "https://example.com/next", nil)
	if err != nil {
		t.Fatalf("public hop: %v", err)
	}
	if err := client.CheckRedirect(publicHop, []*http.Request{original}); err != nil {
		t.Errorf("public→public redirect hop should be allowed: %v", err)
	}
}

func TestWebSearch_UsesProvider(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(stubSearchProvider{}))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	out, err := tl.(invokable).InvokableRun(context.Background(), `{"query":"golang"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(out, "https://stub.example/1") {
		t.Errorf("provider result missing: %s", out)
	}
}

type stubSearchProvider struct{}

func (stubSearchProvider) Search(context.Context, string, int) ([]SearchResult, error) {
	return []SearchResult{{Title: "One", URL: "https://stub.example/1", Snippet: "s"}}, nil
}
