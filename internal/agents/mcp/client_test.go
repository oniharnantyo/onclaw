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

	// envLeakCanary is planted in the test process (the would-be OnClaw server
	// parent) but never configured on the connection: the stdio child must
	// never see it. See openspec/changes/fix-stdio-env-leak.
	envLeakCanary = "ONCLAW_ENVLEAK_CANARY"
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

// TestStdioEnvConstructedNotInherited pins the constructed-env contract for
// stdio MCP children (openspec/changes/fix-stdio-env-leak): the child sees the
// configured env rows and a safe baseline (PATH) but none of the parent
// process's other variables, exemplified by the planted ONCLAW_ENVLEAK_CANARY.
func TestStdioEnvConstructedNotInherited(t *testing.T) {
	bin := compileMockServer(t)
	t.Setenv(envLeakCanary, "leak-if-visible")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := connect(ctx, stdioConn(bin))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.client.Close() }()

	env := childEnv(t, ctx, conn.client)

	// The configured row reaches the child…
	if got := env[envMarker]; got != "secret-123" {
		t.Fatalf("child env %s = %q, want %q", envMarker, got, "secret-123")
	}
	// …a safe baseline var survives (PATH is in the fix's baseline allowlist)…
	if got := env["PATH"]; got == "" {
		t.Fatalf("child env missing PATH: stdio children need it to shell out")
	}
	// …and the parent-only canary must NOT leak: the dial replaces cmd.Env
	// with the constructed slice instead of merging over the parent.
	if got, ok := env[envLeakCanary]; ok {
		t.Fatalf("parent-only canary %s leaked into stdio child env (got %q); dial must construct the child env, not merge over the parent", envLeakCanary, got)
	}
}

// TestConstructChildEnv pins the constructor's contract directly (task 3.1):
// baseline entries are copied from the parent when present, configured rows
// override baseline entries, parent-only variables never reach the child,
// missing baseline entries are omitted rather than synthesized as empty
// strings, and rows with empty values are preserved.
func TestConstructChildEnv(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/onclaw",
		"TZ=UTC",
		"http_proxy=http://proxy.internal:3128",
		"DATABASE_URL=postgres://secret", // parent-only, not in the allowlist
		"ONCLAW_JWT_SECRET=hush",         // parent-only, not in the allowlist
		"Malformed",                      // no '=': never a valid entry
	}
	rows := []domain.EnvRow{
		{Name: "PATH", Value: "/custom/bin"}, // configured row overrides the baseline
		{Name: "EMPTY", Value: ""},           // empty row values are preserved, not dropped
	}

	env := constructChildEnv(parent, rows)

	got := make(map[string]string, len(env))
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			t.Fatalf("malformed child env entry %q", entry)
		}
		got[name] = value
	}
	want := map[string]string{
		"PATH":       "/custom/bin", // row wins over the baseline entry
		"HOME":       "/home/onclaw",
		"TZ":         "UTC",
		"http_proxy": "http://proxy.internal:3128",
		"EMPTY":      "",
	}
	if len(got) != len(want) {
		t.Fatalf("child env keys = %v, want exactly %v", got, want)
	}
	for name, value := range want {
		if gotValue, ok := got[name]; !ok || gotValue != value {
			t.Fatalf("child env %s = (%q, %v), want %q", name, gotValue, ok, value)
		}
	}
	// Missing baseline entries (TMPDIR, HTTPS_PROXY, …) are absent outright,
	// never present as NAME= — pinned by the exact key-set match above.
}

// TestStdioEnvRowOverridesBaseline drives a real stdio dial (task 3.1): a
// configured PATH row wins over the parent's PATH, and a baseline variable
// missing from the parent (TMPDIR is unset here) is absent from the child
// rather than present as an empty string.
func TestStdioEnvRowOverridesBaseline(t *testing.T) {
	bin := compileMockServer(t)
	t.Setenv(envLeakCanary, "leak-if-visible")
	t.Setenv("PATH", "/usr/bin:/bin")
	prevTmpdir, hadTmpdir := os.LookupEnv("TMPDIR")
	os.Unsetenv("TMPDIR")
	if hadTmpdir {
		t.Cleanup(func() { os.Setenv("TMPDIR", prevTmpdir) })
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn := stdioConn(bin)
	conn.Env = append(conn.Env, domain.EnvRow{Name: "PATH", Value: "/custom/bin"})

	// The override does not stop the dial: the binary is launched by absolute
	// path, so a broken child PATH would only break child shells, not the
	// handshake.
	connected, err := connect(ctx, conn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = connected.client.Close() }()

	env := childEnv(t, ctx, connected.client)
	if got := env["PATH"]; got != "/custom/bin" {
		t.Fatalf("child env PATH = %q, want the configured row /custom/bin (row must override the baseline)", got)
	}
	if got, ok := env["TMPDIR"]; ok {
		t.Fatalf("child env TMPDIR = %q; a baseline variable missing from the parent must be omitted, not empty", got)
	}
}

// childEnv fetches the mock server's whole environment as a map by calling its
// dump_env tool.
func childEnv(t *testing.T, ctx context.Context, c interface {
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}) map[string]string {
	t.Helper()
	out := callTool(t, ctx, c, "dump_env")
	env := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			continue
		}
		env[key] = value
	}
	return env
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
	// With an empty parent, the constructed env is exactly the flattened rows.
	env := constructChildEnv(nil, rows)
	if len(env) != 2 || env[0] != "A=1" || env[1] != "B=2" {
		t.Fatalf("constructChildEnv = %v", env)
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
