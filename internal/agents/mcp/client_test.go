package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	envMarker = "ONCLAW_MCP_TEST_MARKER"
	argsFlag  = "--marker=child-args-123"
)

var (
	compileOnce sync.Once
	serverBin   string
	compileErr  error
)

// compileMockServer builds the in-process mcp-go stdio server once for the
// whole test binary (mcp-go's own client tests use the same pattern).
func compileMockServer(t *testing.T) string {
	t.Helper()
	compileOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			compileErr = errors.New("cannot locate test source dir")
			return
		}
		pkgDir := filepath.Dir(thisFile)
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			goBin = "go"
		}
		dir, err := os.MkdirTemp("", "onclaw-mcp-mock")
		if err != nil {
			compileErr = err
			return
		}
		serverBin = filepath.Join(dir, "mockmcpserver")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, goBin, "build", "-o", serverBin,
			"./testdata/mockmcpserver")
		cmd.Dir = pkgDir
		if out, err := cmd.CombinedOutput(); err != nil {
			compileErr = fmt.Errorf("compile mockmcpserver: %v\n%s", err, out)
		}
	})
	if compileErr != nil {
		t.Fatalf("mock server unavailable: %v", compileErr)
	}
	return serverBin
}

// stdioConn builds a stdio connection to the mock server with one env row and
// one arg, so the factory's env/args plumbing is exercised end to end.
func stdioConn(bin string) domain.MCPConnection {
	return domain.MCPConnection{
		Transport: domain.MCPTransportStdio,
		Command:   bin,
		Args:      []string{argsFlag},
		Env:       []domain.EnvRow{{Name: envMarker, Value: "secret-123"}},
	}
}

// callTool invokes a tool on a connected client and returns the concatenated
// text content of the result.
func callTool(t *testing.T, ctx context.Context, c interface {
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}, name string) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Request.Method = "tools/call"
	req.Params.Name = name
	res, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var sb strings.Builder
	for _, item := range res.Content {
		if text, ok := item.(mcp.TextContent); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String()
}

func TestConnectStdioHandshakeListsTools(t *testing.T) {
	bin := compileMockServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := connect(ctx, stdioConn(bin))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.client.Close() }()

	names := make([]string, 0, len(conn.tools))
	for _, tool := range conn.tools {
		info, err := tool.Info(ctx)
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		names = append(names, info.Name)
	}
	for _, want := range []string{"test_tool", "echo_env", "echo_args"} {
		if !contains(names, want) {
			t.Fatalf("tools/list missing %q; got %v", want, names)
		}
	}

	// env rows reach the child process…
	if got := callTool(t, ctx, conn.client, "echo_env"); got != "secret-123" {
		t.Fatalf("echo_env = %q, want secret-123", got)
	}
	// …and so do args.
	if got := callTool(t, ctx, conn.client, "echo_args"); !strings.Contains(got, argsFlag) {
		t.Fatalf("echo_args = %q, want it to contain %q", got, argsFlag)
	}
	// A plain call works through the listed surface too.
	if got := callTool(t, ctx, conn.client, "test_tool"); got != "tool result" {
		t.Fatalf("test_tool = %q", got)
	}
}

func TestConnectStdioFailedConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := domain.MCPConnection{
		Transport: domain.MCPTransportStdio,
		Command:   "/nonexistent/onclaw-mcp-binary-xyz",
	}
	if _, err := connect(ctx, conn); err == nil {
		t.Fatal("expected error connecting to a nonexistent command")
	}
}

func TestConnectUnknownTransportRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := connect(ctx, domain.MCPConnection{Transport: "carrier_pigeon"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want domain.ErrInvalid", err)
	}
}

func TestEnvAndHeaderFlattening(t *testing.T) {
	rows := []domain.EnvRow{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}
	env := envSlice(rows)
	if len(env) != 2 || env[0] != "A=1" || env[1] != "B=2" {
		t.Fatalf("envSlice = %v", env)
	}
	hdrs := headerMap(rows)
	if hdrs["A"] != "1" || hdrs["B"] != "2" || len(hdrs) != 2 {
		t.Fatalf("headerMap = %v", hdrs)
	}
	// Marshals as a JSON object when round-tripped (sanity for shape).
	blob, err := json.Marshal(hdrs)
	if err != nil || len(blob) == 0 {
		t.Fatalf("marshal headers: %v %s", err, blob)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
