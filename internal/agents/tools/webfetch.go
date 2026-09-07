package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/net/html"
)

// NameWebFetch is the dotted capability name registered in the tool registry.
const NameWebFetch = "web.fetch"

// EnvFetchAllowPrivate opts the instance out of the SSRF guard (private,
// loopback, and link-local targets become fetchable). It exists for local
// development against internal services; production instances should leave it
// unset.
const EnvFetchAllowPrivate = "ONCLAW_FETCH_ALLOW_PRIVATE"

const (
	defaultFetchUserAgent = "OnClaw/1.0 (agent-runtime; web.fetch)"
	// maxFetchBytes caps the response body read; larger bodies are truncated.
	maxFetchBytes = 1 << 20 // 1 MiB
	// truncationMarker is appended when the body exceeded the cap.
	truncationMarker = "\n\n[truncated: response exceeded 1 MiB]"
)

// WebFetchOption configures the web.fetch tool.
type WebFetchOption func(*webFetchTool)

// WithFetchHTTPClient overrides the default HTTP client (testing seam).
func WithFetchHTTPClient(c HTTPClient) WebFetchOption {
	return func(w *webFetchTool) { w.client = c }
}

// WithFetchAllowPrivate overrides the SSRF opt-out (testing seam).
func WithFetchAllowPrivate(v bool) WebFetchOption {
	return func(w *webFetchTool) { w.allowPrivate = v }
}

type webFetchTool struct {
	client       HTTPClient
	userAgent    string
	allowPrivate bool
}

// NewWebFetch constructs the web.fetch built-in with the SSRF guard armed
// (loopback/private/link-local denied) unless the instance opted out via
// ONCLAW_FETCH_ALLOW_PRIVATE=true.
func NewWebFetch(opts ...WebFetchOption) (tool.BaseTool, error) {
	w := &webFetchTool{
		client:       http.DefaultClient,
		userAgent:    defaultFetchUserAgent,
		allowPrivate: os.Getenv(EnvFetchAllowPrivate) == "true",
	}
	for _, opt := range opts {
		opt(w)
	}
	return w, nil
}

// NewFetchHTTPClient builds the guarded HTTP client used by web.fetch: every
// hop's resolved IPs are validated, so redirects cannot smuggle the fetch to a
// private target.
func NewFetchHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("web.fetch: resolve %s: %w", host, err)
				}
				for _, ip := range ips {
					if !isPublicIP(ip.IP) && !allowPrivate {
						return nil, fmt.Errorf("web.fetch: %s resolves to non-public address %s", host, ip.IP)
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("web.fetch: too many redirects")
			}
			return validateFetchURL(req.URL, allowPrivate)
		},
	}
}

// Info returns the tool schema surfaced to agentic models.
func (w *webFetchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameWebFetch,
		Desc: "Fetch a URL over HTTP(S) and return its readable text content. " +
			"HTML pages are reduced to text; JSON and other structured responses are returned as-is. " +
			"Use this to read a specific page found via web.search or supplied by the user.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "The absolute http(s) URL to fetch.",
				Required: true,
			},
		}),
	}, nil
}

// fetchArgs is the deserialized tool-call argument shape.
type fetchArgs struct {
	URL string `json:"url"`
}

// InvokableRun satisfies tool.InvokableTool.
func (w *webFetchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args fetchArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("web.fetch: %w", err)
	}
	u, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil {
		return "", fmt.Errorf("web.fetch: invalid url: %w", err)
	}
	if err := validateFetchURL(u, w.allowPrivate); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("web.fetch: %w", err)
	}
	req.Header.Set("User-Agent", w.userAgent)
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.8")

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web.fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("web.fetch: target returned status %d", resp.StatusCode)
	}

	body := make([]byte, 0, 4096)
	buf := make([]byte, 32*1024)
	truncated := false
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if len(body)+n > maxFetchBytes {
				body = append(body, buf[:maxFetchBytes-len(body)]...)
				truncated = true
				break
			}
			body = append(body, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	if len(body) == 0 {
		return "", fmt.Errorf("web.fetch: empty response body")
	}

	content := string(body)
	if truncated {
		content += truncationMarker
	}

	if isHTMLType(resp.Header.Get("Content-Type")) {
		content = extractReadableText(content)
		if truncated {
			content += truncationMarker
		}
	}

	out, err := json.Marshal(map[string]string{"url": u.String(), "content": content})
	if err != nil {
		return "", fmt.Errorf("web.fetch: encode result: %w", err)
	}
	return string(out), nil
}

// validateFetchURL enforces the scheme guard on a target (and, via
// CheckRedirect, on every redirect hop).
func validateFetchURL(u *url.URL, allowPrivate bool) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("web.fetch: only http and https URLs are supported")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("web.fetch: URL has no host")
	}
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("web.fetch: %s is a non-public address and blocked", host)
		}
		return nil
	}
	// Hostnames are re-validated per connection by the guarded dialer; a bare
	// IP literal is the only thing checkable here without resolving.
	return nil
}

// isPublicIP reports whether ip is a public unicast address: not loopback,
// private, link-local, unspecified, or multicast.
func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast())
}

func isHTMLType(contentType string) bool {
	mt := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}
	return mt == "" || mt == "text/html" || mt == "application/xhtml+xml"
}

// extractReadableText reduces an HTML document to its visible text, keeping
// block structure with newlines.
func extractReadableText(doc string) string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return doc
	}
	var sb strings.Builder
	var walk func(*html.Node)
	skip := func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		switch n.Data {
		case "script", "style", "noscript", "template", "head":
			return true
		}
		return false
	}
	var emit func(*html.Node)
	emit = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
			return
		}
		if skip(n) {
			return
		}
		block := n.Type == html.ElementNode && isBlockElement(n.Data)
		if block {
			sb.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			emit(c)
		}
		if block {
			sb.WriteString("\n")
		}
	}
	walk = emit
	walk(root)

	lines := strings.Split(sb.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "section", "article", "header", "footer", "main", "nav",
		"aside", "h1", "h2", "h3", "h4", "h5", "h6", "li", "ul", "ol", "table",
		"tr", "blockquote", "pre", "br", "hr", "figure", "figcaption":
		return true
	}
	return false
}
