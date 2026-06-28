package tools

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "my secret is sk-12345678901234567890",
			expected: "my secret is [REDACTED]",
		},
		{
			input:    "openai key: sk-proj-abcdefghijklmnopqrstuvwxyz12",
			expected: "openai key: [REDACTED]",
		},
		{
			input:    "nvidia key: nvapi-abcdefghijklmnopqrstuvwxyz12",
			expected: "nvidia key: [REDACTED]",
		},
		{
			input:    "normal text without secrets",
			expected: "normal text without secrets",
		},
	}

	for _, tc := range tests {
		actual := Redact(tc.input)
		if actual != tc.expected {
			t.Errorf("Redact(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestRedactedToolDecorator(t *testing.T) {
	innerTool, err := utils.InferTool("test_tool", "a test tool that returns input",
		func(ctx context.Context, input *struct{ Text string }) (string, error) {
			return input.Text, nil
		})
	if err != nil {
		t.Fatalf("failed to infer tool: %v", err)
	}

	redacted := WrapRedacted(innerTool)

	ctx := context.Background()
	output, err := redacted.InvokableRun(ctx, `{"Text": "my secret is sk-12345678901234567890"}`)
	if err != nil {
		t.Fatalf("failed to run redacted tool: %v", err)
	}

	expected := "my secret is [REDACTED]"
	if output != expected {
		t.Errorf("expected tool output %q, got %q", expected, output)
	}
}
