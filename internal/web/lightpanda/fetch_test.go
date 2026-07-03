package lightpanda

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/web"
)

// TestHelperProcess is a helper process for exec tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) < 5 {
		os.Stderr.WriteString("too few arguments")
		os.Exit(2)
	}

	// Skip the binary name argument
	args = args[1:]

	if args[0] != "fetch" || args[1] != "--dump" || args[2] != "markdown" {
		os.Stderr.WriteString("invalid arguments: " + strings.Join(args, " "))
		os.Exit(3)
	}

	url := args[3]
	if url == "http://fail.com" {
		os.Stderr.WriteString("mock failure")
		os.Exit(1)
	}

	os.Stdout.WriteString("# Mock Markdown Content\nSuccess fetching " + url)
}

func TestLightpandaFetcher_Success(t *testing.T) {
	web.AllowLoopbackForTest = true
	defer func() { web.AllowLoopbackForTest = false }()

	fetcher := &lightpandaFetcher{
		cfg: web.Config{
			LightpandaBinPath: "mock-lightpanda",
			TimeoutSeconds:    5,
			MaxBytes:          1024,
		},
		execCommand: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			if name != "mock-lightpanda" {
				t.Errorf("expected command name 'mock-lightpanda', got %q", name)
			}
			helperArgs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			return cmd
		},
	}

	ctx := context.Background()
	res, err := fetcher.Fetch(ctx, "http://example.com/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "# Mock Markdown Content\nSuccess fetching http://example.com/test"
	if res.Content != expected {
		t.Errorf("expected content %q, got %q", expected, res.Content)
	}
}

func TestLightpandaFetcher_SSRF(t *testing.T) {
	fetcher := &lightpandaFetcher{
		cfg: web.Config{
			LightpandaBinPath: "lightpanda",
		},
	}

	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, "http://127.0.0.1", nil)
	if err == nil {
		t.Error("expected SSRF block, got nil")
	}
}

func TestLightpandaFetcher_ExitError(t *testing.T) {
	web.AllowLoopbackForTest = true
	defer func() { web.AllowLoopbackForTest = false }()

	fetcher := &lightpandaFetcher{
		cfg: web.Config{
			LightpandaBinPath: "mock-lightpanda",
			TimeoutSeconds:    5,
			MaxBytes:          1024,
		},
		execCommand: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			helperArgs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			return cmd
		},
	}

	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, "http://fail.com", nil)
	if err == nil {
		t.Error("expected exit error, got nil")
	}
	if !strings.Contains(err.Error(), "mock failure") {
		t.Errorf("expected error message to contain 'mock failure', got %v", err)
	}
}
