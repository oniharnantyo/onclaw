package agents

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// buildReductionMiddleware composes the real reduction middleware exactly as
// buildMiddlewares wires it: a jailed filesystem backend over a temp agent dir
// with RootDir set to the jail mount point (/workspace).
func buildReductionMiddleware(t *testing.T) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], string) {
	t.Helper()
	ctx := context.Background()
	agentDir := t.TempDir()

	cfg := &Config{
		Name:        "reduction-offload-agent",
		Description: "reduction offload path test",
		Instruction: "You are a test agent.",
		ChatModel:   &dummyModel{},
		Filesystem: &FilesystemConfig{
			AgentDir: agentDir,
		},
	}

	handlers, err := buildMiddlewares(ctx, cfg)
	if err != nil {
		t.Fatalf("buildMiddlewares failed: %v", err)
	}
	if len(handlers) < 2 {
		t.Fatalf("expected at least 2 handlers, got %d", len(handlers))
	}
	assertHandlerType(t, handlers[1], "reduction")
	return handlers[1], agentDir
}

// assertOffloaded checks the full output landed at the jail's virtual path and
// is readable through the jail backend, then cross-checks the host file under
// the agent dir.
func assertOffloaded(t *testing.T, agentDir, virtualPath, want string) {
	t.Helper()
	ctx := context.Background()

	jail, err := backend.NewFilesystemJailedWithRoots(agentDir)
	if err != nil {
		t.Fatalf("build verification jail: %v", err)
	}
	content, err := jail.Read(ctx, &einofs.ReadRequest{FilePath: virtualPath})
	if err != nil {
		t.Fatalf("jail.Read(%q): %v", virtualPath, err)
	}
	if content.Content != want {
		t.Fatalf("offloaded content mismatch at %s: got %d bytes, want %d", virtualPath, len(content.Content), len(want))
	}

	hostPath := filepath.Join(agentDir, strings.TrimPrefix(virtualPath, backend.DefaultMountPoint+"/"))
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", hostPath, err)
	}
	if string(data) != want {
		t.Fatalf("host file %s content mismatch: got %d bytes, want %d", hostPath, len(data), len(want))
	}
}

// TestReductionTruncOffloadPath drives the real reduction middleware from
// buildMiddlewares with an oversized tool output and verifies the full output
// is offloaded to /workspace/trunc/<call_id> on the jailed backend and
// readable via jail.Read. It guards the regression where RootDir was the raw
// host agent dir: the jail rejects absolute host paths on write, so the
// offload would fail (invokable path errors; streamable notice omits the save
// and no file appears).
func TestReductionTruncOffloadPath(t *testing.T) {
	ctx := context.Background()
	reductionMW, agentDir := buildReductionMiddleware(t)

	// Exceeds the reduction default MaxLengthForTrunc (50000 chars).
	fullOutput := strings.Repeat("x", 60000)

	t.Run("invokable tool offloads to /workspace/trunc/<call_id>", func(t *testing.T) {
		callID := "invokable-oversize"
		endpoint := func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return fullOutput, nil
		}
		wrapped, err := reductionMW.WrapInvokableToolCall(ctx, endpoint, &adk.ToolContext{Name: "big.tool", CallID: callID})
		if err != nil {
			t.Fatalf("WrapInvokableToolCall failed: %v", err)
		}
		notice, err := wrapped(ctx, "{}")
		if err != nil {
			t.Fatalf("wrapped tool call failed: %v", err)
		}

		virtualPath := backend.DefaultMountPoint + "/trunc/" + callID
		if !strings.Contains(notice, virtualPath) {
			t.Fatalf("truncation notice %q does not reference offload path %q", notice, virtualPath)
		}
		assertOffloaded(t, agentDir, virtualPath, fullOutput)
	})

	t.Run("streamable tool offloads to /workspace/trunc/<call_id>", func(t *testing.T) {
		callID := "streamable-oversize"
		endpoint := func(_ context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
			sr, sw := schema.Pipe[string](2)
			go func() {
				defer sw.Close()
				sw.Send(fullOutput[:30000], nil)
				sw.Send(fullOutput[30000:], nil)
			}()
			return sr, nil
		}
		wrapped, err := reductionMW.WrapStreamableToolCall(ctx, endpoint, &adk.ToolContext{Name: "big.tool", CallID: callID})
		if err != nil {
			t.Fatalf("WrapStreamableToolCall failed: %v", err)
		}
		resp, err := wrapped(ctx, "{}")
		if err != nil {
			t.Fatalf("wrapped tool call failed: %v", err)
		}
		defer resp.Close()

		var sb strings.Builder
		for {
			chunk, recvErr := resp.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
					break
				}
				t.Fatalf("recv truncated stream: %v", recvErr)
			}
			sb.WriteString(chunk)
		}

		virtualPath := backend.DefaultMountPoint + "/trunc/" + callID
		if !strings.Contains(sb.String(), virtualPath) {
			t.Fatalf("truncation notice %q does not reference offload path %q", sb.String(), virtualPath)
		}
		assertOffloaded(t, agentDir, virtualPath, fullOutput)
	})
}
