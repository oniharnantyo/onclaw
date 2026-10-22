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
	"github.com/stretchr/testify/require"
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

// webInvoker narrows a constructed tool to its invokable surface. Named to
// stay clear of the package's other test helpers.
type webInvoker interface {
	InvokableRun(context.Context, string, ...tool.Option) (string, error)
}

func newFetchTool(t *testing.T, resp *fetchFakeResp, opts ...WebFetchOption) webInvoker {
	t.Helper()
	tl, err := NewWebFetch(append([]WebFetchOption{WithFetchHTTPClient(&fetchFakeClient{resp: resp})}, opts...)...)
	require.NoError(t, err)
	return tl.(webInvoker)
}

func TestWebFetch_HTMLExtraction(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		body:   "<html><head><title>t</title></head><body><script>x()</script><h1>Hello</h1><p>World of <b>fetch</b>.</p></body></html>",
	})
	out, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/page"}`)
	require.NoError(t, err)
	for _, want := range []string{"Hello", "World of fetch", "https://example.com/page"} {
		require.Contains(t, out, want)
	}
	require.NotContains(t, out, "x()", "script content leaked")
}

func TestWebFetch_JSONPassthrough(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"application/json"}},
		body:   `{"key": "<b>value</b>"}`,
	})
	out, err := tl.InvokableRun(context.Background(), `{"url":"https://api.example.com/data.json"}`)
	require.NoError(t, err)
	require.Contains(t, out, `value`, "JSON body should pass through unmodified")
}

func TestWebFetch_Truncation(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"text/plain"}},
		body:   strings.Repeat("a", 2<<20),
	})
	out, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/big"}`)
	require.NoError(t, err)
	require.LessOrEqual(t, len(out), (1<<20)+512, "output not truncated: %d bytes", len(out))
	require.Contains(t, out, "[truncated")
}

func TestWebFetch_SSRFBlocked(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1/x", "http://[::1]/x", "http://10.0.0.5/x",
		"http://192.168.1.1/x", "http://169.254.169.254/latest/meta-data",
		"ftp://example.com/x", "file:///etc/passwd",
	} {
		t.Run(target, func(t *testing.T) {
			tl := newFetchTool(t, &fetchFakeResp{status: 200, body: "secret"})
			_, err := tl.InvokableRun(context.Background(), fmt.Sprintf(`{"url":%q}`, target))
			require.Error(t, err, "expected %s to be blocked", target)
		})
	}
}

func TestWebFetch_SSRFOptOut(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200, body: "ok",
		header: http.Header{"Content-Type": []string{"text/plain"}},
	}, WithFetchAllowPrivate(true))
	out, err := tl.InvokableRun(context.Background(), `{"url":"http://127.0.0.1:8080/health"}`)
	require.NoError(t, err)
	require.Contains(t, out, "ok")
}

func TestWebFetch_ErrorStatus(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{status: 500})
	_, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/x"}`)
	require.Error(t, err, "expected error on non-200")
}

// TestWebFetch_RedirectRevalidated exercises the guarded client's per-hop
// CheckRedirect directly: a redirect from a public URL to a private or
// non-http target must be denied even though the original URL was fine.
func TestWebFetch_RedirectRevalidated(t *testing.T) {
	client := NewFetchHTTPClient(false)
	require.NotNil(t, client.CheckRedirect, "guarded client has no CheckRedirect")
	original, err := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	require.NoError(t, err)

	privateHop, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/secret", nil)
	require.NoError(t, err)
	require.Error(t, client.CheckRedirect(privateHop, []*http.Request{original}),
		"public→private redirect hop must be blocked")

	ftpURL, err := url.Parse("ftp://example.com/x")
	require.NoError(t, err)
	require.Error(t, client.CheckRedirect(&http.Request{URL: ftpURL}, []*http.Request{original}),
		"redirect to a non-http scheme must be blocked")

	publicHop, err := http.NewRequest(http.MethodGet, "https://example.com/next", nil)
	require.NoError(t, err)
	require.NoError(t, client.CheckRedirect(publicHop, []*http.Request{original}),
		"public→public redirect hop should be allowed")
}

func TestWebFetch_EmptyBody(t *testing.T) {
	tl := newFetchTool(t, &fetchFakeResp{
		status: 200,
		header: http.Header{"Content-Type": []string{"text/plain"}},
	})
	_, err := tl.InvokableRun(context.Background(), `{"url":"https://example.com/empty"}`)
	require.Error(t, err, "expected error on empty response body")
}
