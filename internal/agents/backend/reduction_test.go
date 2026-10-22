package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/require"
)

func TestReductionBackend_WriteAndRead(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	redBackend, err := NewReductionBackend(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, redBackend)

	targetPath := DefaultMountPoint + "/trunc/call_test_1"
	expectedContent := "large tool output payload for reduction testing"

	// 1. Write via reduction backend
	err = redBackend.Write(ctx, &einofs.WriteRequest{
		FilePath: targetPath,
		Content:  expectedContent,
	})
	require.NoError(t, err)

	// 2. Verify on-disk file existence inside the workspace
	onDiskPath := filepath.Join(tmpDir, "trunc", "call_test_1")
	data, err := os.ReadFile(onDiskPath)
	require.NoError(t, err)
	require.Equal(t, expectedContent, string(data))

	// 3. Verify readable back via the jail's Read using the mount path (/workspace/...)
	jailBackend, ok := redBackend.(interface{ Jail() einofs.Backend })
	require.True(t, ok)

	readRes, err := jailBackend.Jail().Read(ctx, &einofs.ReadRequest{
		FilePath: targetPath,
	})
	require.NoError(t, err)
	require.NotNil(t, readRes)
	require.Equal(t, expectedContent, readRes.Content)
}

func TestReductionBackend_JailEscapeProtection(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	redBackend, err := NewReductionBackend(tmpDir)
	require.NoError(t, err)

	testCases := []struct {
		name     string
		filePath string
	}{
		{
			name:     "parent directory traversal",
			filePath: "../escape.txt",
		},
		{
			name:     "mount-scoped parent directory traversal",
			filePath: DefaultMountPoint + "/../escape.txt",
		},
		{
			name:     "absolute host path outside jail",
			filePath: "/tmp/escape.txt",
		},
		{
			name:     "root host path",
			filePath: "/etc/passwd",
		},
		{
			name:     "empty path",
			filePath: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := redBackend.Write(ctx, &einofs.WriteRequest{
				FilePath: tc.filePath,
				Content:  "forbidden content",
			})
			require.Error(t, err)
		})
	}

	t.Run("nil write request", func(t *testing.T) {
		err := redBackend.Write(ctx, nil)
		require.Error(t, err)
	})
}

func TestReductionMiddleware_Integration(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	redBackend, err := NewReductionBackend(tmpDir)
	require.NoError(t, err)

	// Configure reduction middleware with a small truncation threshold
	cfg := &reduction.Config{
		Backend:           redBackend,
		RootDir:           DefaultMountPoint,
		MaxLengthForTrunc: 20,
	}

	redMW, err := reduction.New(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, redMW)

	oversizedOutput := strings.Repeat("Oversized payload chunk! ", 10) // ~260 chars > 20
	callID := "call_red_int_999"

	// Wrap an invokable tool call endpoint using the reduction middleware
	wrappedEndpoint, err := redMW.WrapInvokableToolCall(ctx, func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		return oversizedOutput, nil
	}, &adk.ToolContext{
		Name:   "heavy_tool",
		CallID: callID,
	})
	require.NoError(t, err)

	result, err := wrappedEndpoint(ctx, "{}")
	require.NoError(t, err)

	// Output should be replaced with a notice pointing to read_file and the offload path
	require.NotEqual(t, oversizedOutput, result)
	require.Contains(t, result, "read_file")
	expectedOffloadMountPath := DefaultMountPoint + "/trunc/" + callID
	require.Contains(t, result, expectedOffloadMountPath)

	// Verify the original content was persisted to disk in the jailed workspace
	onDiskFile := filepath.Join(tmpDir, "trunc", callID)
	diskBytes, err := os.ReadFile(onDiskFile)
	require.NoError(t, err)
	require.Equal(t, oversizedOutput, string(diskBytes))

	// Verify the content is readable back through the jail via the mount path
	jailBackend := redBackend.(interface{ Jail() einofs.Backend }).Jail()
	readRes, err := jailBackend.Read(ctx, &einofs.ReadRequest{
		FilePath: expectedOffloadMountPath,
	})
	require.NoError(t, err)
	require.Equal(t, oversizedOutput, readRes.Content)
}
