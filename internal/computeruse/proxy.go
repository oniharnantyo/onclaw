package computeruse

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// The adapter-side driver proxy (add-computer-use design D1: adapt, don't
// forward): a long-lived `cua-driver mcp --socket <daemon>` child — the
// driver's stable, documented MCP frontend — spoken to over stdio through
// the mcp-go client, exactly the boundary the daemon's raw socket protocol
// was rejected in favor of. The proxy is the only driver client the adapter
// ever holds; the daemon enforces no cross-client serialization, and the
// dispatch lease (design D3) is what keeps this the one actor.

// driverClient is the curated toolset's view of the vendored driver.
// Production is the mcp-go stdio proxy; tests substitute hand-written stubs.
type driverClient interface {
	// CallTool invokes one driver tool and returns the normalized result.
	CallTool(ctx context.Context, name string, args map[string]any) (DriverResult, error)
	// Close tears the frontend child down.
	Close() error
}

// DriverResult is the adapter's normalized view of one driver tool result:
// the text payload (the accessibility tree, an action receipt), the PNG
// bytes when the result carries a screenshot, and the element metadata the
// C2 secure-field annotation reads. Vendor-boundary assumption, re-verified
// at every pin bump (notes.md 7.3): text rides content blocks, screenshots
// ride image/png image blocks, element metadata rides the result's _meta
// "elements" list.
type DriverResult struct {
	Text     string
	ImagePNG []byte
	Elements []DriverElement
}

// DriverElement is one addressable element the driver's metadata marks out:
// the opaque token the model addresses it by and whether the field is
// credential-bearing (design D7's secure-field detection).
type DriverElement struct {
	Token  string
	Secure bool
}

// mcpProxy is the production driverClient: one long-lived mcp-go stdio
// client to the driver's MCP frontend, dialed lazily on first use and
// re-dialed once when the frontend child dies between calls.
type mcpProxy struct {
	mu         sync.Mutex
	driverBin  string
	socketPath string
	cli        *client.Client
}

// newMCPProxy builds the proxy; the dial itself happens on first CallTool so
// a wedged daemon costs nothing until a tool actually runs.
func newMCPProxy(driverBin, socketPath string) *mcpProxy {
	return &mcpProxy{driverBin: driverBin, socketPath: socketPath}
}

// CallTool invokes one driver tool, re-dialing once across a dead transport.
func (p *mcpProxy) CallTool(ctx context.Context, name string, args map[string]any) (DriverResult, error) {
	cli, err := p.client(ctx)
	if err != nil {
		return DriverResult{}, err
	}
	res, err := cli.CallTool(ctx, driverCallRequest(name, args))
	if err != nil {
		// The frontend child died underneath us (daemon restart, crash):
		// one fresh dial, then surface whatever that answers with.
		p.reset()
		cli, err = p.client(ctx)
		if err != nil {
			return DriverResult{}, err
		}
		res, err = cli.CallTool(ctx, driverCallRequest(name, args))
		if err != nil {
			return DriverResult{}, err
		}
	}
	if res.IsError {
		return DriverResult{}, fmt.Errorf("%s", driverResultText(res))
	}
	return normalizeDriverResult(res), nil
}

// Close tears the frontend child down; closing a closed proxy is a no-op.
func (p *mcpProxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cli == nil {
		return nil
	}
	cli := p.cli
	p.cli = nil
	return cli.Close()
}

// client dials the driver's MCP frontend on first use: stdio to
// `<driver binary> mcp --socket <daemon socket>`, the driver env contract
// riding along (the frontend is a driver process; telemetry stays off),
// then the initialize handshake.
func (p *mcpProxy) client(ctx context.Context) (*client.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cli != nil {
		return p.cli, nil
	}
	tr := transport.NewStdioWithOptions(p.driverBin, driverEnv, []string{"mcp", "--socket", p.socketPath},
		transport.WithCommandFunc(func(_ context.Context, command string, cmdEnv []string, args []string) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Env = cmdEnv
			return cmd, nil
		}))
	if err := tr.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("computer: start driver frontend: %w", err)
	}
	cli := client.NewClient(tr)
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "buddies-computer-use", Version: "1.0.0"}
	if _, err := cli.Initialize(ctx, initReq); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("computer: driver frontend handshake: %w", err)
	}
	p.cli = cli
	return cli, nil
}

// reset drops a dead client (called after a failed call, before the retry
// dial).
func (p *mcpProxy) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cli != nil {
		_ = p.cli.Close()
		p.cli = nil
	}
}

// driverCallRequest builds one tools/call request for the driver.
func driverCallRequest(name string, args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Request.Method = "tools/call"
	req.Params.Name = name
	if args != nil {
		req.Params.Arguments = args
	} else {
		req.Params.Arguments = map[string]any{}
	}
	return req
}

// normalizeDriverResult flattens the driver's content blocks into the
// adapter's view: text blocks concatenate, image/png blocks decode to PNG
// bytes, and _meta "elements" entries parse to the element metadata.
func normalizeDriverResult(res *mcp.CallToolResult) DriverResult {
	var out DriverResult
	var texts []string
	for _, c := range res.Content {
		switch block := c.(type) {
		case mcp.TextContent:
			texts = append(texts, block.Text)
		case mcp.ImageContent:
			if strings.EqualFold(block.MIMEType, "image/png") {
				if data, err := base64.StdEncoding.DecodeString(block.Data); err == nil {
					out.ImagePNG = append(out.ImagePNG, data...)
				}
			}
		}
	}
	out.Text = strings.Join(texts, "\n")
	out.Elements = parseDriverElements(res)
	return out
}

// parseDriverElements reads the driver's element metadata from the result's
// _meta (best-effort): each "elements" entry carries "token" and, when the
// field is credential-bearing, a truthy "secure".
func parseDriverElements(res *mcp.CallToolResult) []DriverElement {
	if res.Meta == nil {
		return nil
	}
	raw, ok := res.Meta.AdditionalFields["elements"].([]any)
	if !ok {
		return nil
	}
	var out []DriverElement
	for _, entry := range raw {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		token, _ := obj["token"].(string)
		if token == "" {
			continue
		}
		el := DriverElement{Token: token}
		if secure, ok := obj["secure"].(bool); ok {
			el.Secure = secure
		}
		out = append(out, el)
	}
	return out
}

// driverResultText extracts the text of an error-result content list.
func driverResultText(res *mcp.CallToolResult) string {
	var texts []string
	for _, c := range res.Content {
		if block, ok := c.(mcp.TextContent); ok {
			texts = append(texts, block.Text)
		}
	}
	if len(texts) == 0 {
		return "the driver reported an error without a message"
	}
	return strings.Join(texts, "\n")
}
